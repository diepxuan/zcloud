package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// ====================================
// Repair: tin 1-1 bị gom nhầm vào thread mang uid của chính account
// ====================================
//
// Trước bản vá `wsMessage.toMessage`, tin nhắn đến 1-1 (Zalo gửi `idTo` = uid
// của mình hoặc sentinel "0") bị lưu với `conv_id` = uid của chính account thay
// vì uid người gửi. Hệ quả: mở thread người gửi thì chỉ thấy tin mình gửi đi.
// Bản vá chỉ đúng cho tin mới; các row cũ cần được chuyển về đúng thread vì
// SaveMessage dùng INSERT OR IGNORE nên sync lại không ghi đè.

// SelfThreadRepair mô tả kết quả một lần dọn thread sai.
type SelfThreadRepair struct {
	Messages   int      // số row messages được chuyển thread
	Media      int      // số row media được chuyển thread
	MediaJobs  int      // số row media_jobs được chuyển thread
	MovedFiles int      // số file media đã di chuyển trên disk
	FileErrors []string // lỗi di chuyển file (không chặn phần DB)
}

// CountSelfThreadMessages đếm số tin đến đang nằm nhầm trong thread `selfUID`.
func (s *Store) CountSelfThreadMessages(accountID, selfUID string) (int, error) {
	if accountID == "" || selfUID == "" {
		return 0, fmt.Errorf("repair: thiếu accountID hoặc selfUID")
	}
	q := `SELECT COUNT(*) FROM messages
		WHERE account_id = ? AND conv_id = ? AND from_id <> '' AND from_id <> ?`
	if s.backend == BackendPostgres {
		q = `SELECT COUNT(*) FROM messages
			WHERE account_id = $1 AND conv_id = $2 AND from_id <> '' AND from_id <> $3`
	}
	var n int
	err := s.db.QueryRow(q, accountID, selfUID, selfUID).Scan(&n)
	return n, err
}

// RepairSelfThreadMessages chuyển mọi tin đến đang nằm trong thread `selfUID`
// về thread của người gửi, rồi kéo media + media_jobs liên quan đi theo (kể cả
// file trên disk vì đường dẫn media chứa convID).
func (s *Store) RepairSelfThreadMessages(accountID, selfUID string) (SelfThreadRepair, error) {
	var rep SelfThreadRepair
	if accountID == "" || selfUID == "" {
		return rep, fmt.Errorf("repair: thiếu accountID hoặc selfUID")
	}

	// Bước 1: messages — conv_id sai thành uid người gửi.
	res, err := s.db.Exec(s.repairMessagesSQL(), accountID, selfUID, selfUID)
	if err != nil {
		return rep, fmt.Errorf("repair messages: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		rep.Messages = int(n)
	}

	// Bước 2: media — bám theo conv_id mới của message tương ứng. Đọc trước để
	// biết file nào cần di chuyển, vì sau UPDATE thì không truy lại được đường
	// dẫn cũ.
	moves, err := s.collectMediaMoves(accountID, selfUID)
	if err != nil {
		return rep, err
	}
	if len(moves) > 0 {
		if rep.Media, err = s.repointMedia(accountID, selfUID); err != nil {
			return rep, err
		}
	}
	if rep.MediaJobs, err = s.repointMediaJobs(accountID, selfUID); err != nil {
		return rep, err
	}

	// Bước 3: di chuyển file trên disk. Lỗi ở đây không rollback DB — media
	// vẫn còn `source_url` nên worker có thể tải lại.
	for _, mv := range moves {
		if err := s.moveMediaFile(accountID, selfUID, mv.newConvID, mv.id, mv.fileExt); err != nil {
			rep.FileErrors = append(rep.FileErrors, err.Error())
			continue
		}
		rep.MovedFiles++
	}
	return rep, nil
}

func (s *Store) repairMessagesSQL() string {
	if s.backend == BackendPostgres {
		return `UPDATE messages SET conv_id = from_id
			WHERE account_id = $1 AND conv_id = $2 AND from_id <> '' AND from_id <> $3`
	}
	return `UPDATE messages SET conv_id = from_id
		WHERE account_id = ? AND conv_id = ? AND from_id <> '' AND from_id <> ?`
}

// mediaMove là một file media cần đổi thư mục theo thread mới.
type mediaMove struct {
	id        string
	fileExt   string
	newConvID string
}

// collectMediaMoves lấy các media đang nằm trong thread sai kèm thread đúng
// (suy ra từ messages đã được sửa ở bước 1).
func (s *Store) collectMediaMoves(accountID, selfUID string) ([]mediaMove, error) {
	q := `SELECT md.id, md.file_ext, m.conv_id
		FROM media md JOIN messages m ON m.id = md.msg_id AND m.account_id = md.account_id
		WHERE md.account_id = ? AND md.conv_id = ? AND m.conv_id <> ?`
	if s.backend == BackendPostgres {
		q = `SELECT md.id, md.file_ext, m.conv_id
			FROM media md JOIN messages m ON m.id = md.msg_id AND m.account_id = md.account_id
			WHERE md.account_id = $1 AND md.conv_id = $2 AND m.conv_id <> $3`
	}
	rows, err := s.db.Query(q, accountID, selfUID, selfUID)
	if err != nil {
		return nil, fmt.Errorf("repair media scan: %w", err)
	}
	defer rows.Close()

	var out []mediaMove
	for rows.Next() {
		var mv mediaMove
		if err := rows.Scan(&mv.id, &mv.fileExt, &mv.newConvID); err != nil {
			return nil, fmt.Errorf("repair media scan: %w", err)
		}
		out = append(out, mv)
	}
	return out, rows.Err()
}

func (s *Store) repointMedia(accountID, selfUID string) (int, error) {
	q := `UPDATE media SET
			conv_id = (SELECT m.conv_id FROM messages m
				WHERE m.id = media.msg_id AND m.account_id = media.account_id),
			file_path = account_id || '/' || (SELECT m.conv_id FROM messages m
				WHERE m.id = media.msg_id AND m.account_id = media.account_id)
				|| '/' || id || '.' || file_ext
		WHERE account_id = ? AND conv_id = ?
		  AND EXISTS (SELECT 1 FROM messages m
			WHERE m.id = media.msg_id AND m.account_id = media.account_id AND m.conv_id <> ?)`
	if s.backend == BackendPostgres {
		q = `UPDATE media SET conv_id = m.conv_id,
				file_path = media.account_id || '/' || m.conv_id || '/' || media.id || '.' || media.file_ext
			FROM messages m
			WHERE m.id = media.msg_id AND m.account_id = media.account_id
			  AND media.account_id = $1 AND media.conv_id = $2 AND m.conv_id <> $3`
	}
	res, err := s.db.Exec(q, accountID, selfUID, selfUID)
	if err != nil {
		return 0, fmt.Errorf("repair media: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) repointMediaJobs(accountID, selfUID string) (int, error) {
	q := `UPDATE media_jobs SET
			conv_id = (SELECT m.conv_id FROM messages m
				WHERE m.id = media_jobs.msg_id AND m.account_id = media_jobs.account_id)
		WHERE account_id = ? AND conv_id = ?
		  AND EXISTS (SELECT 1 FROM messages m
			WHERE m.id = media_jobs.msg_id AND m.account_id = media_jobs.account_id AND m.conv_id <> ?)`
	if s.backend == BackendPostgres {
		q = `UPDATE media_jobs SET conv_id = m.conv_id
			FROM messages m
			WHERE m.id = media_jobs.msg_id AND m.account_id = media_jobs.account_id
			  AND media_jobs.account_id = $1 AND media_jobs.conv_id = $2 AND m.conv_id <> $3`
	}
	res, err := s.db.Exec(q, accountID, selfUID, selfUID)
	if err != nil {
		return 0, fmt.Errorf("repair media_jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// moveMediaFile đổi thư mục file media từ thread cũ sang thread mới.
func (s *Store) moveMediaFile(accountID, oldConvID, newConvID, fileID, ext string) error {
	src := s.MediaFilePath(accountID, oldConvID, fileID, ext)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := s.MediaFilePath(accountID, newConvID, fileID, ext)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("media dir %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move %s -> %s: %w", src, dst, err)
	}
	return nil
}
