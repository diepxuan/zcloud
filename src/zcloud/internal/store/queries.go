package store

import (
	"strings"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// ====================================
// Account operations
// ====================================

// upsertAccountSQL: Postgres dùng ON CONFLICT DO NOTHING.
const upsertAccountSQL = `INSERT INTO accounts (id, display_name, account_type, user_id)
	VALUES ($1, $2, $3, '')
	ON CONFLICT (id) DO NOTHING`

const updateAccountSQL = "UPDATE accounts SET display_name = $1, avatar = $2, updated_at = NOW() WHERE id = $3"
const setAccountUserIDSQL = "UPDATE accounts SET user_id = $1, updated_at = NOW() WHERE id = $2"

func (s *Store) CreateAccount(id, displayName string, accountType int) error {
	_, err := s.db.Exec(upsertAccountSQL, id, displayName, accountType)
	return err
}

func (s *Store) UpdateAccount(id, displayName, avatar string) error {
	_, err := s.db.Exec(updateAccountSQL, displayName, avatar, id)
	return err
}

func (s *Store) SetAccountUserID(id, userID string) error {
	_, err := s.db.Exec(setAccountUserIDSQL, userID, id)
	return err
}

func (s *Store) GetAccount(id string) (*Account, error) {
	a := &Account{}
	q := "SELECT id, display_name, user_id, avatar, account_type, status, note, created_at, updated_at FROM accounts WHERE id = $1"
	err := s.db.QueryRow(q, id).Scan(&a.ID, &a.DisplayName, &a.UserID, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListAccounts trả về tất cả accounts (accountType=0) hoặc theo type.
// enabledOnly: nếu true chỉ trả account có enabled=true.
func (s *Store) ListAccounts(accountType int, enabledOnly bool) ([]Account, error) {
	q := "SELECT id, display_name, user_id, avatar, account_type, status, note, enabled, created_at, updated_at FROM accounts"
	args := []interface{}{}
	conds := []string{}
	if accountType > 0 {
		args = append(args, accountType)
		conds = append(conds, fmt.Sprintf("account_type = $%d", len(args)))
	}
	if enabledOnly {
		conds = append(conds, "enabled = TRUE")
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.UserID, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// SetAccountEnabled bật/tắt flag enabled của account.
// WS listener KHÔNG bị ảnh hưởng — account disabled vẫn listen + sync.
func (s *Store) SetAccountEnabled(accountID string, enabled bool) error {
	_, err := s.db.Exec("UPDATE accounts SET enabled = $1, updated_at = NOW() WHERE id = $2", enabled, accountID)
	return err
}

// ====================================
// Session operations
// ====================================

func (s *Store) ListActiveAccountIDs() ([]string, error) {
	q := "SELECT DISTINCT account_id FROM sessions WHERE is_active = 1 ORDER BY account_id"
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetActiveSessionsForAccounts tra ve map account_id -> true neu co session is_active=1.
// 1 query thay vi N query (toi uu cho UI tab Quan ly khi co nhieu account).
func (s *Store) GetActiveSessionsForAccounts() (map[string]bool, error) {
	q := "SELECT DISTINCT account_id FROM sessions WHERE is_active = 1"
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, nil
}

const upsertSessionSQL = `INSERT INTO sessions
	(id, account_id, user_id, cookies, secret_key, imei, user_agent, language, ws_urls, service_map, api_type, api_version, is_active, expires_at, transport, cipher_key)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	ON CONFLICT (id) DO UPDATE SET
		account_id = EXCLUDED.account_id,
		user_id = EXCLUDED.user_id,
		cookies = EXCLUDED.cookies,
		secret_key = EXCLUDED.secret_key,
		imei = EXCLUDED.imei,
		user_agent = EXCLUDED.user_agent,
		language = EXCLUDED.language,
		ws_urls = EXCLUDED.ws_urls,
		service_map = EXCLUDED.service_map,
		api_type = EXCLUDED.api_type,
		api_version = EXCLUDED.api_version,
		transport = EXCLUDED.transport,
		cipher_key = EXCLUDED.cipher_key,
		is_active = EXCLUDED.is_active,
		expires_at = EXCLUDED.expires_at`

const deactivateOtherSessionsSQL = "UPDATE sessions SET is_active = 0 WHERE account_id = $1 AND id <> $2"

func (s *Store) SaveSession(sr *Session) error {
	// Deactivate các session cũ cùng account trước khi insert.
	if _, err := s.db.Exec(deactivateOtherSessionsSQL, sr.AccountID, sr.ID); err != nil {
		return err
	}
	_, err := s.db.Exec(upsertSessionSQL, sr.ID, sr.AccountID, sr.UserID, sr.Cookies, sr.SecretKey,
		sr.IMEI, sr.UserAgent, sr.Language, sr.WSURLs, sr.ServiceMap, sr.APIType, sr.APIVersion, sr.IsActive, sr.ExpiresAt, sr.Transport, sr.CipherKey)
	return err
}

func (s *Store) LoadSession(id string) (*Session, error) {
	sr := &Session{}
	q := `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at,
		transport, cipher_key
		FROM sessions WHERE id = $1`
	err := s.db.QueryRow(q, id).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs, &sr.ServiceMap,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt,
		&sr.Transport, &sr.CipherKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

func (s *Store) GetActiveSession(accountID string) (*Session, error) {
	sr := &Session{}
	q := `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at,
		transport, cipher_key
		FROM sessions WHERE account_id = $1 AND is_active = 1 ORDER BY created_at DESC LIMIT 1`
	err := s.db.QueryRow(q, accountID).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs, &sr.ServiceMap,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt,
		&sr.Transport, &sr.CipherKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec("UPDATE sessions SET is_active = 0 WHERE id = $1", id)
	return err
}

// ====================================
// Conversation operations
// ====================================

const upsertConversationSQL = `INSERT INTO conversations
	(id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	ON CONFLICT (id, account_id) DO UPDATE SET
		name = EXCLUDED.name,
		avatar = EXCLUDED.avatar,
		conv_type = EXCLUDED.conv_type,
		last_msg_id = EXCLUDED.last_msg_id,
		last_msg_at = EXCLUDED.last_msg_at,
		unread_count = EXCLUDED.unread_count,
		updated_at = NOW()`

func (s *Store) SaveConversation(c *Conversation) error {
	_, err := s.db.Exec(upsertConversationSQL, c.ID, c.AccountID, c.Name, c.Avatar, c.ConvType, c.LastMsgID, c.LastMsgAt, c.Unread)
	return err
}

func (s *Store) GetConversation(accountID, convID string) (*Conversation, error) {
	c := &Conversation{}
	q := `SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
		FROM conversations WHERE account_id = $1 AND id = $2`
	err := s.db.QueryRow(q, accountID, convID).Scan(&c.ID, &c.AccountID, &c.Name, &c.Avatar, &c.ConvType, &c.LastMsgID, &c.LastMsgAt, &c.Unread, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// FindAccountByConvID trả account_id sở hữu convId.
// Nếu nhiều account cùng có conv (vd self-thread), trả theo enabled+lastMsgAt DESC
// (ưu tiên account enabled + có tin nhắn gần nhất).
func (s *Store) FindAccountByConvID(convID string) (string, error) {
	var accountID string
	q := `
		SELECT c.account_id
		FROM conversations c
		JOIN accounts a ON a.id = c.account_id
		WHERE c.id = $1
		ORDER BY a.enabled DESC, c.last_msg_at DESC NULLS LAST, c.updated_at DESC
		LIMIT 1`
	err := s.db.QueryRow(q, convID).Scan(&accountID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return accountID, err
}

func (s *Store) DeleteAccount(id string) error {
	s.db.Exec("DELETE FROM sessions WHERE account_id = $1", id)
	s.db.Exec("DELETE FROM conversations WHERE account_id = $1", id)
	s.db.Exec("DELETE FROM messages WHERE account_id = $1", id)
	_, err := s.db.Exec("DELETE FROM accounts WHERE id = $1", id)
	return err
}

func (s *Store) GetConversations(accountID string) ([]Conversation, error) {
	// LEFT JOIN messages để lấy preview text tin cuối (cho sidebar UI).
	// Nếu messages bị xoá, lastMsgContent = NULL → UI fallback về lastMsgId.
	// Cắt ngắn content tại MaxLastMsgPreview ký tự + "..." để tránh payload lớn.
	q := fmt.Sprintf(`
		SELECT c.id, c.account_id, c.name, c.avatar, c.conv_type,
		       c.last_msg_id, c.last_msg_at,
		       CASE WHEN m.content IS NULL OR m.content = '' THEN
		            CASE WHEN m.msg_type = 2 THEN '📷 Ảnh'
		                 WHEN m.msg_type = 3 THEN 'Sticker'
		                 WHEN m.msg_type = 5 THEN 'Voice'
		                 WHEN m.msg_type = 6 THEN 'Link'
		                 WHEN m.msg_type = 7 THEN 'Video'
		                 WHEN m.msg_type = 4 THEN 'File'
		                 ELSE ''
		            END
		            WHEN LENGTH(m.content) > %d THEN SUBSTRING(m.content, 1, %d) || '…'
		            ELSE m.content
		       END AS preview,
		       c.unread_count, c.updated_at
		FROM conversations c
		LEFT JOIN messages m ON m.account_id = c.account_id AND m.id = c.last_msg_id
		WHERE c.account_id = $1
		ORDER BY c.last_msg_at DESC NULLS LAST, c.updated_at DESC`, maxLastMsgPreview, maxLastMsgPreview)
	rows, err := s.db.Query(q, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		var preview sql.NullString
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Name, &c.Avatar, &c.ConvType, &c.LastMsgID, &c.LastMsgAt, &preview, &c.Unread, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if preview.Valid {
			c.LastMsgContent = preview.String
		}
		convs = append(convs, c)
	}
	return convs, nil
}

// ====================================
// Message operations
// ====================================

const maxLastMsgPreview = 80

const insertMessageSQL = `INSERT INTO messages
	(id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (id, account_id) DO NOTHING`

func (s *Store) SaveMessage(m *Message) error {
	_, err := s.db.Exec(insertMessageSQL, m.ID, m.AccountID, m.ConvID, m.FromID, m.FromName, m.Content, m.MsgType, m.Timestamp, m.Attachments)
	return err
}

func (s *Store) GetMessages(accountID, convID string, cursor int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments
		FROM messages WHERE account_id = $1 AND conv_id = $2 AND timestamp < $3
		ORDER BY timestamp DESC LIMIT $4`
	rows, err := s.db.Query(q, accountID, convID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.AccountID, &m.ConvID, &m.FromID, &m.FromName, &m.Content, &m.MsgType, &m.Timestamp, &m.Attachments); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// Đảo ngược: cũ → mới
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// ====================================
// Media operations
// ====================================

const insertMediaSQL = `INSERT INTO media
	(id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
	 file_size, width, height, source_url, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
	ON CONFLICT (id, account_id) DO NOTHING`

func (s *Store) SaveMedia(m *MediaFile) (string, error) {
	dir := filepath.Join(s.mediaPath, m.AccountID, m.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("media dir: %w", err)
	}
	relPath := filepath.Join(m.AccountID, m.ConvID, m.ID+"."+m.FileExt)
	_, err := s.db.Exec(insertMediaSQL, m.ID, m.AccountID, m.ConvID, m.MsgID, m.FileName, relPath,
		m.FileExt, m.MimeType, m.FileSize, m.Width, m.Height, m.SourceURL)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.mediaPath, relPath), nil
}

func (s *Store) GetUnprocessedMedia(limit int) ([]MediaFile, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
		file_size, width, height, thumb_path, ocr_text, ai_tags, ai_processed, ai_confidence,
		is_downloaded, source_url, created_at
		FROM media WHERE ai_processed = 0 AND is_downloaded = 1
		ORDER BY created_at ASC LIMIT $1`
	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []MediaFile
	for rows.Next() {
		var m MediaFile
		if err := rows.Scan(&m.ID, &m.AccountID, &m.ConvID, &m.MsgID, &m.FileName, &m.FilePath,
			&m.FileExt, &m.MimeType, &m.FileSize, &m.Width, &m.Height, &m.ThumbPath,
			&m.OCRText, &m.AITags, &m.AIProcessed, &m.AIConfidence,
			&m.IsDownloaded, &m.SourceURL, &m.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, m)
	}
	return files, nil
}

func (s *Store) MarkMediaProcessed(id, accountID, ocrText, aiTags string, confidence float64, errMsg string) error {
	status := 1
	if errMsg != "" {
		status = 2
	}
	q := `UPDATE media SET ocr_text = $1, ai_tags = $2, ai_confidence = $3, ai_processed = $4
		WHERE id = $5 AND account_id = $6`
	_, err := s.db.Exec(q, ocrText, aiTags, confidence, status, id, accountID)
	return err
}

// ====================================
// Media job operations
// ====================================

const (
	MediaJobPending = "pending"
	MediaJobRunning = "running"
	MediaJobDone    = "done"
	MediaJobFailed  = "failed"

	mediaJobDefaultMaxAttempts = 3
)

const upsertMediaJobSQL = `INSERT INTO media_jobs
	(id, account_id, conv_id, msg_id, file_name, file_ext, source_url,
	 status, attempts, max_attempts, last_error, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
	ON CONFLICT (id, account_id) DO NOTHING`

func (s *Store) SaveMediaJob(j *MediaJob) error {
	if j.Status == "" {
		j.Status = MediaJobPending
	}
	if j.MaxAttempts == 0 {
		j.MaxAttempts = mediaJobDefaultMaxAttempts
	}
	_, err := s.db.Exec(upsertMediaJobSQL, j.ID, j.AccountID, j.ConvID, j.MsgID, j.FileName,
		j.FileExt, j.SourceURL, j.Status, j.Attempts, j.MaxAttempts, j.LastError)
	return err
}

const listPendingMediaJobsSQL = `SELECT id, account_id, conv_id, msg_id, file_name, file_ext, source_url,
	local_path, status, attempts, max_attempts, last_error, created_at, updated_at
	FROM media_jobs WHERE account_id = $1 AND status = $2 ORDER BY created_at ASC LIMIT $3`

func (s *Store) ListPendingMediaJobs(accountID string, limit int) ([]MediaJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(listPendingMediaJobsSQL, accountID, MediaJobPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []MediaJob
	for rows.Next() {
		var j MediaJob
		if err := rows.Scan(&j.ID, &j.AccountID, &j.ConvID, &j.MsgID, &j.FileName,
			&j.FileExt, &j.SourceURL, &j.LocalPath, &j.Status, &j.Attempts,
			&j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

const getMediaJobSQL = `SELECT id, account_id, conv_id, msg_id, file_name, file_ext, source_url,
	local_path, status, attempts, max_attempts, last_error, created_at, updated_at
	FROM media_jobs WHERE id = $1 AND account_id = $2`

func (s *Store) GetMediaJob(id, accountID string) (*MediaJob, error) {
	j := &MediaJob{}
	err := s.db.QueryRow(getMediaJobSQL, id, accountID).Scan(&j.ID, &j.AccountID, &j.ConvID,
		&j.MsgID, &j.FileName, &j.FileExt, &j.SourceURL, &j.LocalPath,
		&j.Status, &j.Attempts, &j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

const markMediaJobRunningSQL = `UPDATE media_jobs SET status = $1, attempts = $2, last_error = $3, updated_at = NOW()
	WHERE id = $4 AND account_id = $5`

func (s *Store) MarkMediaJobRunning(id, accountID string, attempts int, errMsg string) error {
	_, err := s.db.Exec(markMediaJobRunningSQL, MediaJobRunning, attempts, errMsg, id, accountID)
	return err
}

const markMediaJobDoneSQL = `UPDATE media_jobs SET status = $1, local_path = $2, last_error = '', updated_at = NOW()
	WHERE id = $3 AND account_id = $4`

func (s *Store) MarkMediaJobDone(id, accountID, localPath string) error {
	_, err := s.db.Exec(markMediaJobDoneSQL, MediaJobDone, localPath, id, accountID)
	return err
}

const markMediaJobFailedSQL = `UPDATE media_jobs SET status = $1, last_error = $2, updated_at = NOW()
	WHERE id = $3 AND account_id = $4`

func (s *Store) MarkMediaJobFailed(id, accountID, errMsg string) error {
	_, err := s.db.Exec(markMediaJobFailedSQL, MediaJobFailed, errMsg, id, accountID)
	return err
}

const resetMediaJobPendingSQL = `UPDATE media_jobs SET status = $1, attempts = 0, local_path = '', last_error = '', updated_at = NOW()
	WHERE id = $2 AND account_id = $3`

func (s *Store) ResetMediaJobPending(id, accountID string) error {
	_, err := s.db.Exec(resetMediaJobPendingSQL, MediaJobPending, id, accountID)
	return err
}

const retryMediaJobSQL = `UPDATE media_jobs SET status = $1, attempts = $2, last_error = $3, updated_at = NOW()
	WHERE id = $4 AND account_id = $5`

func (s *Store) RetryMediaJob(id, accountID string, attempts int, errMsg string) error {
	_, err := s.db.Exec(retryMediaJobSQL, MediaJobPending, attempts, errMsg, id, accountID)
	return err
}

const listMediaJobsSQL = `SELECT id, account_id, conv_id, msg_id, file_name, file_ext, source_url,
	local_path, status, attempts, max_attempts, last_error, created_at, updated_at
	FROM media_jobs WHERE account_id = $1 ORDER BY created_at DESC LIMIT $2`

func (s *Store) ListMediaJobs(accountID string, limit int) ([]MediaJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(listMediaJobsSQL, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []MediaJob
	for rows.Next() {
		var j MediaJob
		if err := rows.Scan(&j.ID, &j.AccountID, &j.ConvID, &j.MsgID, &j.FileName,
			&j.FileExt, &j.SourceURL, &j.LocalPath, &j.Status, &j.Attempts,
			&j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// ====================================
// OA operations
// ====================================

const upsertOAConfigSQL = `INSERT INTO oa_configs
	(id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	ON CONFLICT (id) DO UPDATE SET
		oa_name = EXCLUDED.oa_name,
		access_token = EXCLUDED.access_token,
		refresh_token = EXCLUDED.refresh_token,
		secret_key = EXCLUDED.secret_key,
		webhook_url = EXCLUDED.webhook_url,
		is_verified = EXCLUDED.is_verified,
		expires_at = EXCLUDED.expires_at,
		updated_at = NOW()`

func (s *Store) SaveOAConfig(oc *OAConfig) error {
	_, err := s.db.Exec(upsertOAConfigSQL, oc.ID, oc.AccountID, oc.OAID, oc.OAName, oc.AccessToken, oc.RefreshToken, oc.SecretKey, oc.WebhookURL, oc.IsVerified, oc.ExpiresAt)
	return err
}

const getOAConfigSQL = `SELECT id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at, created_at, updated_at
	FROM oa_configs WHERE oa_id = $1`

func (s *Store) GetOAConfig(oaID string) (*OAConfig, error) {
	oc := &OAConfig{}
	err := s.db.QueryRow(getOAConfigSQL, oaID).Scan(&oc.ID, &oc.AccountID, &oc.OAID, &oc.OAName, &oc.AccessToken, &oc.RefreshToken, &oc.SecretKey, &oc.WebhookURL, &oc.IsVerified, &oc.ExpiresAt, &oc.CreatedAt, &oc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return oc, err
}

const logWebhookSQL = `INSERT INTO oa_webhook_logs (oa_id, event_id, event_type, sender_id, raw_data)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING id`

func (s *Store) LogWebhook(oaID, eventID, eventType, senderID, rawData string) (int64, error) {
	var id int64
	err := s.db.QueryRow(logWebhookSQL, oaID, eventID, eventType, senderID, rawData).Scan(&id)
	return id, err
}

const getPendingWebhooksSQL = `SELECT id, oa_id, event_id, event_type, sender_id, raw_data, processed, error_msg, created_at
	FROM oa_webhook_logs WHERE processed = 0 ORDER BY created_at ASC LIMIT $1`

func (s *Store) GetPendingWebhooks(limit int) ([]OAWebhookLog, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(getPendingWebhooksSQL, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []OAWebhookLog
	for rows.Next() {
		var w OAWebhookLog
		if err := rows.Scan(&w.ID, &w.OAID, &w.EventID, &w.EventType, &w.SenderID, &w.RawData, &w.Processed, &w.ErrorMsg, &w.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, w)
	}
	return logs, nil
}
