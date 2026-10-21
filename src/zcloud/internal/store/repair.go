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
	oldConvID string
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

// ====================================
// Repair: tin nhóm bị lưu nhầm vào chat riêng
// ====================================
//
// Trước bản vá parser, groupMsgs thiếu grid bị fallback conv_id về uid người
// gửi. Các message cũ đã nằm sai conversation nên SaveMessage không thể sửa
// (PK là id + account_id, sync lại không đổi conv_id). Repair này dùng
// last_msg_id của conversation nhóm làm nguồn xác nhận conversation đúng.

// GroupThreadRepair mô tả kết quả một lần dọn message nhóm sai thread.
type GroupThreadRepair struct {
	Messages       int
	Media          int
	MediaJobs      int
	PrivateThreads int
	MovedFiles     int
	FileErrors     []string
}

type groupMediaMove struct {
	id        string
	fileExt   string
	oldConvID string
	newConvID string
}

// CountMisclassifiedGroupMessages đếm group last message đang trỏ tới message
// có conv_id khác. Bỏ qua message ID bị nhiều group cùng account tham chiếu.
func (s *Store) CountMisclassifiedGroupMessages(accountID string) (int, error) {
	if accountID == "" {
		return 0, fmt.Errorf("repair group: thiếu accountID")
	}
	const q = `SELECT COUNT(*)
		FROM conversations c
		JOIN messages m ON m.account_id = c.account_id AND m.id = c.last_msg_id
		WHERE c.account_id = $1
		  AND c.conv_type = 1
		  AND m.conv_id <> c.id
		  AND NOT EXISTS (
			SELECT 1 FROM conversations c2
			WHERE c2.account_id = c.account_id
			  AND c2.conv_type = 1
			  AND c2.last_msg_id = m.id
			  AND c2.id <> c.id
		  )`
	var n int
	err := s.db.QueryRow(q, accountID).Scan(&n)
	return n, err
}

// RepairMisclassifiedGroupMessages chuyển group last message về đúng group,
// kéo media/media_jobs theo và cập nhật last_msg_at. Idempotent.
func (s *Store) RepairMisclassifiedGroupMessages(accountID string) (GroupThreadRepair, error) {
	var rep GroupThreadRepair
	if accountID == "" {
		return rep, fmt.Errorf("repair group: thiếu accountID")
	}

	moves, err := s.collectGroupMediaMoves(accountID)
	if err != nil {
		return rep, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return rep, fmt.Errorf("repair group begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE messages m SET conv_id = c.id
		FROM conversations c
		WHERE m.account_id = c.account_id
		  AND m.id = c.last_msg_id
		  AND c.account_id = $1
		  AND c.conv_type = 1
		  AND m.conv_id <> c.id
		  AND NOT EXISTS (
			SELECT 1 FROM conversations c2
			WHERE c2.account_id = c.account_id
			  AND c2.conv_type = 1
			  AND c2.last_msg_id = m.id
			  AND c2.id <> c.id
		  )`, accountID)
	if err != nil {
		return rep, fmt.Errorf("repair group messages: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		rep.Messages = int(n)
	}

	// Message nhóm từng là last message của chat riêng. Sau khi chuyển nó đi,
	// khôi phục last message thật của chat riêng để sidebar không bị mồ côi.
	res, err = tx.Exec(`WITH stale_private AS (
			SELECT c.account_id, c.id
			FROM conversations c
			JOIN messages m ON m.account_id = c.account_id AND m.id = c.last_msg_id
			JOIN conversations g ON g.account_id = m.account_id
				AND g.id = m.conv_id
				AND g.conv_type = 1
			WHERE c.account_id = $1
			  AND c.conv_type = 0
			  AND c.id <> m.conv_id
		), latest AS (
			SELECT s.account_id, s.id, latest_msg.id AS msg_id, latest_msg.timestamp
			FROM stale_private s
			LEFT JOIN LATERAL (
				SELECT id, timestamp
				FROM messages
				WHERE account_id = s.account_id AND conv_id = s.id
				ORDER BY timestamp DESC
				LIMIT 1
			) latest_msg ON TRUE
		)
		UPDATE conversations c SET
			last_msg_id = COALESCE(latest.msg_id, ''),
			last_msg_at = CASE WHEN latest.msg_id IS NULL THEN NULL
				ELSE to_timestamp(latest.timestamp / 1000.0) END
		FROM latest
		WHERE c.account_id = latest.account_id AND c.id = latest.id`, accountID)
	if err != nil {
		return rep, fmt.Errorf("repair group private conversations: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		rep.PrivateThreads = int(n)
	}

	res, err = tx.Exec(`UPDATE media md SET
			conv_id = m.conv_id,
			file_path = md.account_id || '/' || m.conv_id || '/' || md.id || '.' || md.file_ext
		FROM messages m
		JOIN conversations c ON c.account_id = m.account_id
			AND c.id = m.conv_id
			AND c.last_msg_id = m.id
		WHERE md.account_id = $1
		  AND md.account_id = m.account_id
		  AND md.msg_id = m.id
		  AND c.conv_type = 1
		  AND md.conv_id <> m.conv_id`, accountID)
	if err != nil {
		return rep, fmt.Errorf("repair group media: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		rep.Media = int(n)
	}

	res, err = tx.Exec(`UPDATE media_jobs mj SET
			conv_id = m.conv_id,
			local_path = CASE WHEN mj.status = 'done' THEN
				mj.account_id || '/' || m.conv_id || '/' || mj.id || '.' || mj.file_ext
			ELSE mj.local_path END
		FROM messages m
		JOIN conversations c ON c.account_id = m.account_id
			AND c.id = m.conv_id
			AND c.last_msg_id = m.id
		WHERE mj.account_id = $1
		  AND mj.account_id = m.account_id
		  AND mj.msg_id = m.id
		  AND c.conv_type = 1
		  AND mj.conv_id <> m.conv_id`, accountID)
	if err != nil {
		return rep, fmt.Errorf("repair group media_jobs: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		rep.MediaJobs = int(n)
	}

	if _, err := tx.Exec(`UPDATE conversations c SET last_msg_at = to_timestamp(m.timestamp / 1000.0)
		FROM messages m
		WHERE c.account_id = $1
		  AND c.conv_type = 1
		  AND m.account_id = c.account_id
		  AND m.id = c.last_msg_id
		  AND m.conv_id = c.id
		  AND (c.last_msg_at IS NULL
		       OR c.last_msg_at < to_timestamp(m.timestamp / 1000.0))`, accountID); err != nil {
		return rep, fmt.Errorf("repair group last_msg_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return rep, fmt.Errorf("repair group commit: %w", err)
	}

	for _, mv := range moves {
		if err := s.moveMediaFile(accountID, mv.oldConvID, mv.newConvID, mv.id, mv.fileExt); err != nil {
			rep.FileErrors = append(rep.FileErrors, err.Error())
			continue
		}
		rep.MovedFiles++
	}
	return rep, nil
}

func (s *Store) collectGroupMediaMoves(accountID string) ([]groupMediaMove, error) {
	const q = `SELECT md.id, md.file_ext, md.conv_id, c.id
		FROM media md
		JOIN messages m ON m.account_id = md.account_id AND m.id = md.msg_id
		JOIN conversations c ON c.account_id = m.account_id
			AND c.last_msg_id = m.id
			AND c.conv_type = 1
		WHERE md.account_id = $1
		  AND md.conv_id <> c.id
		  AND NOT EXISTS (
			SELECT 1 FROM conversations c2
			WHERE c2.account_id = c.account_id
			  AND c2.conv_type = 1
			  AND c2.last_msg_id = m.id
			  AND c2.id <> c.id
		  )`
	rows, err := s.db.Query(q, accountID)
	if err != nil {
		return nil, fmt.Errorf("repair group media scan: %w", err)
	}
	defer rows.Close()

	var out []groupMediaMove
	for rows.Next() {
		var mv groupMediaMove
		if err := rows.Scan(&mv.id, &mv.fileExt, &mv.oldConvID, &mv.newConvID); err != nil {
			return nil, fmt.Errorf("repair group media scan: %w", err)
		}
		out = append(out, mv)
	}
	return out, rows.Err()
}
