package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// ====================================
// Helper dialect
// ====================================

// upsertAccountSQL: SQLite dùng INSERT OR REPLACE, Postgres dùng ON CONFLICT.
func (s *Store) upsertAccountSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO accounts (id, display_name, account_type) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO NOTHING`
	}
	return "INSERT OR IGNORE INTO accounts (id, display_name, account_type) VALUES (?, ?, ?)"
}

func (s *Store) updateAccountSQL() string {
	if s.backend == BackendPostgres {
		return "UPDATE accounts SET display_name = $1, avatar = $2, updated_at = NOW() WHERE id = $3"
	}
	return "UPDATE accounts SET display_name = ?, avatar = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
}

// ====================================
// Account operations
// ====================================

func (s *Store) CreateAccount(id, displayName string, accountType int) error {
	_, err := s.db.Exec(s.upsertAccountSQL(), id, displayName, accountType)
	return err
}

func (s *Store) UpdateAccount(id, displayName, avatar string) error {
	_, err := s.db.Exec(s.updateAccountSQL(), displayName, avatar, id)
	return err
}

func (s *Store) GetAccount(id string) (*Account, error) {
	a := &Account{}
	q := "SELECT id, display_name, avatar, account_type, status, note, created_at, updated_at FROM accounts WHERE id = ?"
	if s.backend == BackendPostgres {
		q = "SELECT id, display_name, avatar, account_type, status, note, created_at, updated_at FROM accounts WHERE id = $1"
	}
	err := s.db.QueryRow(q, id).Scan(&a.ID, &a.DisplayName, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *Store) ListAccounts(accountType int) ([]Account, error) {
	q := "SELECT id, display_name, avatar, account_type, status, note, created_at, updated_at FROM accounts"
	args := []interface{}{}
	if accountType > 0 {
		if s.backend == BackendPostgres {
			q += " WHERE account_type = $1"
		} else {
			q += " WHERE account_type = ?"
		}
		args = append(args, accountType)
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
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
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
func (s *Store) SaveSession(sr *Session) error {
	// Deactivate các session cũ cùng account trước khi insert.
	updateQ := "UPDATE sessions SET is_active = 0 WHERE account_id = ? AND id <> ?"
	if s.backend == BackendPostgres {
		updateQ = "UPDATE sessions SET is_active = 0 WHERE account_id = $1 AND id <> $2"
	}
	if _, err := s.db.Exec(updateQ, sr.AccountID, sr.ID); err != nil {
		return err
	}
	// Insert hoặc update.
	insertQ := s.upsertSessionSQL()
	_, err := s.db.Exec(insertQ, sr.ID, sr.AccountID, sr.UserID, sr.Cookies, sr.SecretKey,
		sr.IMEI, sr.UserAgent, sr.Language, sr.WSURLs, sr.ServiceMap, sr.APIType, sr.APIVersion, sr.IsActive, sr.ExpiresAt)
	return err
}

func (s *Store) upsertSessionSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO sessions
			(id, account_id, user_id, cookies, secret_key, imei, user_agent, language, ws_urls, service_map, api_type, api_version, is_active, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
				is_active = EXCLUDED.is_active,
				expires_at = EXCLUDED.expires_at`
	}
	return `INSERT OR REPLACE INTO sessions
		(id, account_id, user_id, cookies, secret_key, imei, user_agent, language, ws_urls, service_map, api_type, api_version, is_active, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func (s *Store) LoadSession(id string) (*Session, error) {
	sr := &Session{}
	q := `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at
		FROM sessions WHERE id = ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
			ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at
			FROM sessions WHERE id = $1`
	}
	err := s.db.QueryRow(q, id).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs, &sr.ServiceMap,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

func (s *Store) GetActiveSession(accountID string) (*Session, error) {
	sr := &Session{}
	q := `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at
		FROM sessions WHERE account_id = ? AND is_active = 1 ORDER BY created_at DESC LIMIT 1`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
			ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at
			FROM sessions WHERE account_id = $1 AND is_active = 1 ORDER BY created_at DESC LIMIT 1`
	}
	err := s.db.QueryRow(q, accountID).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs, &sr.ServiceMap,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

func (s *Store) DeleteSession(id string) error {
	q := "UPDATE sessions SET is_active = 0 WHERE id = ?"
	if s.backend == BackendPostgres {
		q = "UPDATE sessions SET is_active = 0 WHERE id = $1"
	}
	_, err := s.db.Exec(q, id)
	return err
}

// ====================================
// Conversation operations
// ====================================

func (s *Store) SaveConversation(c *Conversation) error {
	q := s.upsertConversationSQL()
	_, err := s.db.Exec(q, c.ID, c.AccountID, c.Name, c.Avatar, c.ConvType, c.LastMsgID, c.LastMsgAt, c.Unread)
	return err
}

func (s *Store) upsertConversationSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO conversations
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
	}
	return `INSERT OR REPLACE INTO conversations
		(id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
}

func (s *Store) GetConversation(accountID, convID string) (*Conversation, error) {
	c := &Conversation{}
	q := `SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
		FROM conversations WHERE account_id = ? AND id = ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
			FROM conversations WHERE account_id = $1 AND id = $2`
	}
	err := s.db.QueryRow(q, accountID, convID).Scan(&c.ID, &c.AccountID, &c.Name, &c.Avatar, &c.ConvType, &c.LastMsgID, &c.LastMsgAt, &c.Unread, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *Store) DeleteAccount(id string) error {
	if s.backend == BackendPostgres {
		s.db.Exec("DELETE FROM sessions WHERE account_id = $1", id)
		s.db.Exec("DELETE FROM conversations WHERE account_id = $1", id)
		s.db.Exec("DELETE FROM messages WHERE account_id = $1", id)
		_, err := s.db.Exec("DELETE FROM accounts WHERE id = $1", id)
		return err
	}
	s.db.Exec("DELETE FROM sessions WHERE account_id = ?", id)
	s.db.Exec("DELETE FROM conversations WHERE account_id = ?", id)
	s.db.Exec("DELETE FROM messages WHERE account_id = ?", id)
	_, err := s.db.Exec("DELETE FROM accounts WHERE id = ?", id)
	return err
}

func (s *Store) GetConversations(accountID string) ([]Conversation, error) {
	// NULLS LAST cho Postgres; SQLite dùng expression IS NULL/='0001-01-01' (chuẩn cũ).
	var q string
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
			FROM conversations
			WHERE account_id = $1
			ORDER BY last_msg_at DESC NULLS LAST, updated_at DESC`
	} else {
		q = `SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
			FROM conversations
			WHERE account_id = ?
			ORDER BY (last_msg_at IS NULL OR last_msg_at = '0001-01-01 00:00:00'),
			         last_msg_at DESC,
			         updated_at DESC`
	}
	rows, err := s.db.Query(q, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Name, &c.Avatar, &c.ConvType, &c.LastMsgID, &c.LastMsgAt, &c.Unread, &c.UpdatedAt); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, nil
}

// ====================================
// Message operations
// ====================================

func (s *Store) SaveMessage(m *Message) error {
	q := s.insertMessageSQL()
	_, err := s.db.Exec(q, m.ID, m.AccountID, m.ConvID, m.FromID, m.FromName, m.Content, m.MsgType, m.Timestamp, m.Attachments)
	return err
}

func (s *Store) insertMessageSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO messages
			(id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id, account_id) DO NOTHING`
	}
	return `INSERT OR IGNORE INTO messages
		(id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func (s *Store) GetMessages(accountID, convID string, cursor int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments
		FROM messages WHERE account_id = ? AND conv_id = ? AND timestamp < ?
		ORDER BY timestamp DESC LIMIT ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments
			FROM messages WHERE account_id = $1 AND conv_id = $2 AND timestamp < $3
			ORDER BY timestamp DESC LIMIT $4`
	}
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

func (s *Store) SaveMedia(m *MediaFile) (string, error) {
	dir := filepath.Join(s.mediaPath, m.AccountID, m.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("media dir: %w", err)
	}
	relPath := filepath.Join(m.AccountID, m.ConvID, m.ID+"."+m.FileExt)
	q := s.insertMediaSQL()
	_, err := s.db.Exec(q, m.ID, m.AccountID, m.ConvID, m.MsgID, m.FileName, relPath,
		m.FileExt, m.MimeType, m.FileSize, m.Width, m.Height, m.SourceURL)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.mediaPath, relPath), nil
}

func (s *Store) insertMediaSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO media
			(id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
			file_size, width, height, source_url, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
			ON CONFLICT (id, account_id) DO NOTHING`
	}
	return `INSERT OR IGNORE INTO media
		(id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
		file_size, width, height, source_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
}

func (s *Store) GetUnprocessedMedia(limit int) ([]MediaFile, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
		file_size, width, height, thumb_path, ocr_text, ai_tags, ai_processed, ai_confidence,
		is_downloaded, source_url, created_at
		FROM media WHERE ai_processed = 0 AND is_downloaded = 1
		ORDER BY created_at ASC LIMIT ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
			file_size, width, height, thumb_path, ocr_text, ai_tags, ai_processed, ai_confidence,
			is_downloaded, source_url, created_at
			FROM media WHERE ai_processed = 0 AND is_downloaded = 1
			ORDER BY created_at ASC LIMIT $1`
	}
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
	q := `UPDATE media SET ocr_text = ?, ai_tags = ?, ai_confidence = ?, ai_processed = ?
		WHERE id = ? AND account_id = ?`
	if s.backend == BackendPostgres {
		q = `UPDATE media SET ocr_text = $1, ai_tags = $2, ai_confidence = $3, ai_processed = $4
			WHERE id = $5 AND account_id = $6`
	}
	_, err := s.db.Exec(q, ocrText, aiTags, confidence, status, id, accountID)
	return err
}

// ====================================
// OA operations
// ====================================

func (s *Store) SaveOAConfig(oc *OAConfig) error {
	q := s.upsertOAConfigSQL()
	_, err := s.db.Exec(q, oc.ID, oc.AccountID, oc.OAID, oc.OAName, oc.AccessToken, oc.RefreshToken, oc.SecretKey, oc.WebhookURL, oc.IsVerified, oc.ExpiresAt)
	return err
}

func (s *Store) upsertOAConfigSQL() string {
	if s.backend == BackendPostgres {
		return `INSERT INTO oa_configs
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
	}
	return `INSERT OR REPLACE INTO oa_configs
		(id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func (s *Store) GetOAConfig(oaID string) (*OAConfig, error) {
	oc := &OAConfig{}
	q := `SELECT id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at, created_at, updated_at
		FROM oa_configs WHERE oa_id = ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at, created_at, updated_at
			FROM oa_configs WHERE oa_id = $1`
	}
	err := s.db.QueryRow(q, oaID).Scan(&oc.ID, &oc.AccountID, &oc.OAID, &oc.OAName, &oc.AccessToken, &oc.RefreshToken, &oc.SecretKey, &oc.WebhookURL, &oc.IsVerified, &oc.ExpiresAt, &oc.CreatedAt, &oc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return oc, err
}

func (s *Store) LogWebhook(oaID, eventID, eventType, senderID, rawData string) (int64, error) {
	q := "INSERT INTO oa_webhook_logs (oa_id, event_id, event_type, sender_id, raw_data) VALUES (?, ?, ?, ?, ?)"
	if s.backend == BackendPostgres {
		q = "INSERT INTO oa_webhook_logs (oa_id, event_id, event_type, sender_id, raw_data) VALUES ($1, $2, $3, $4, $5) RETURNING id"
	}
	if s.backend == BackendPostgres {
		var id int64
		err := s.db.QueryRow(q, oaID, eventID, eventType, senderID, rawData).Scan(&id)
		return id, err
	}
	r, err := s.db.Exec(q, oaID, eventID, eventType, senderID, rawData)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) GetPendingWebhooks(limit int) ([]OAWebhookLog, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id, oa_id, event_id, event_type, sender_id, raw_data, processed, error_msg, created_at
		FROM oa_webhook_logs WHERE processed = 0 ORDER BY created_at ASC LIMIT ?`
	if s.backend == BackendPostgres {
		q = `SELECT id, oa_id, event_id, event_type, sender_id, raw_data, processed, error_msg, created_at
			FROM oa_webhook_logs WHERE processed = 0 ORDER BY created_at ASC LIMIT $1`
	}
	rows, err := s.db.Query(q, limit)
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
