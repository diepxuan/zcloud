package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver, không cần CGO
)

// Store quản lý toàn bộ persistent data
type Store struct {
	db        *sql.DB
	dbPath    string
	mediaPath string
}

// MediaConfig cấu hình lưu media files
type MediaConfig struct {
	BasePath string // Thư mục gốc (mặc định: ./zcloud-media)
	MaxSize  int64  // Kích thước tối đa mỗi file (bytes), 0 = không giới hạn
}

// New mở hoặc tạo database
func New(dbPath string, mediaPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = filepath.Join(".", "storages", "database", "zcloud.db")
	}
	if mediaPath == "" {
		mediaPath = filepath.Join(".", "storages", "media")
	}

	// Tạo thư mục nếu chưa có
	for _, dir := range []string{filepath.Dir(dbPath), mediaPath} {
		if dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
			}
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// WAL mode cho performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("store: wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("store: busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("store: fk: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath, mediaPath: mediaPath}

	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return s, nil
}

// DB trả về *sql.DB
func (s *Store) DB() *sql.DB   { return s.db }
func (s *Store) Path() string  { return s.dbPath }
func (s *Store) MediaPath() string { return s.mediaPath }

// Close đóng database
func (s *Store) Close() error { return s.db.Close() }

// ====================================
// MediaFile path helpers
// ====================================

// MediaFilePath trả về đường dẫn đầy đủ cho file media
// Cấu trúc: {mediaPath}/{accountID}/{convID}/{fileID}.{ext}
func (s *Store) MediaFilePath(accountID, convID, fileID, ext string) string {
	return filepath.Join(s.mediaPath, accountID, convID, fileID+"."+ext)
}

// MediaDir tạo và trả về thư mục chứa media
func (s *Store) MediaDir(accountID, convID string) string {
	dir := filepath.Join(s.mediaPath, accountID, convID)
	os.MkdirAll(dir, 0755)
	return dir
}

// ====================================
// Migrations
// ====================================

func (s *Store) migrate() error {
	migrations := []string{
		migrationAccounts,
		migrationSessions,
		migrationConversations,
		migrationMessages,
		migrationMedia,
		migrationOA,
		migrationOAWebhook,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m[:80])
		}
	}
	return nil
}

const migrationAccounts = `
CREATE TABLE IF NOT EXISTS accounts (
    id              TEXT PRIMARY KEY,
    display_name    TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    account_type    INTEGER DEFAULT 1,      -- 1: Zalo User, 2: Zalo OA
    status          INTEGER DEFAULT 1,      -- 1: active, 0: disabled
    note            TEXT DEFAULT '',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);`

const migrationSessions = `
CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    user_id         TEXT NOT NULL,
    cookies         TEXT NOT NULL,
    secret_key      TEXT NOT NULL,
    imei            TEXT NOT NULL,
    user_agent      TEXT DEFAULT '',
    language        TEXT DEFAULT 'vi',
    ws_urls         TEXT DEFAULT '[]',
    api_type        INTEGER DEFAULT 30,
    api_version     INTEGER DEFAULT 665,
    is_active       INTEGER DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at      DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);`

const migrationConversations = `
CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    name            TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    conv_type       INTEGER DEFAULT 0,     -- 0: cá nhân, 1: nhóm, 2: OA
    last_msg_id     TEXT DEFAULT '',
    last_msg_at     DATETIME,
    unread_count    INTEGER DEFAULT 0,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_convs_account ON conversations(account_id, updated_at DESC);`

const migrationMessages = `
CREATE TABLE IF NOT EXISTS messages (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    from_id         TEXT NOT NULL,
    from_name       TEXT DEFAULT '',
    content         TEXT DEFAULT '',
    msg_type        INTEGER DEFAULT 1,     -- 1:text, 2:image, 3:sticker, 4:file...
    timestamp       INTEGER NOT NULL,
    attachments     TEXT DEFAULT '[]',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_msgs_conv ON messages(account_id, conv_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_msgs_ts  ON messages(timestamp);`

const migrationMedia = `
CREATE TABLE IF NOT EXISTS media (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    msg_id          TEXT DEFAULT '',
    file_name       TEXT NOT NULL,           -- Tên gốc
    file_path       TEXT NOT NULL,           -- Path relative từ mediaPath
    file_ext        TEXT DEFAULT '',
    mime_type       TEXT DEFAULT '',
    file_size       INTEGER DEFAULT 0,
    width           INTEGER DEFAULT 0,
    height          INTEGER DEFAULT 0,
    width_thumb     INTEGER DEFAULT 0,       -- Kích thước thumbnail
    height_thumb    INTEGER DEFAULT 0,
    thumb_path      TEXT DEFAULT '',          -- Path thumbnail (nếu có)
    ocr_text        TEXT DEFAULT '',          -- OCR text (cho AI agent sau)
    ai_tags         TEXT DEFAULT '[]',        -- JSON tags: ["invoice","product","customer",...]
    ai_processed    INTEGER DEFAULT 0,        -- 0: chờ xử lý, 1: đã xử lý, 2: lỗi
    ai_confidence   REAL DEFAULT 0,           -- Độ tin cậy AI (0-1)
    is_downloaded   INTEGER DEFAULT 0,        -- 0: chưa tải, 1: đã tải về local
    source_url      TEXT DEFAULT '',           -- URL gốc từ Zalo
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_media_conv ON media(account_id, conv_id);
CREATE INDEX IF NOT EXISTS idx_media_ocr  ON media(ocr_text) WHERE ocr_text != '';
CREATE INDEX IF NOT EXISTS idx_media_ai   ON media(ai_processed) WHERE ai_processed = 0;`

const migrationOA = `
CREATE TABLE IF NOT EXISTS oa_configs (
    id              TEXT PRIMARY KEY,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    oa_id           TEXT NOT NULL,
    oa_name         TEXT DEFAULT '',
    access_token    TEXT NOT NULL,
    refresh_token   TEXT DEFAULT '',
    secret_key      TEXT NOT NULL,
    webhook_url     TEXT DEFAULT '',
    is_verified     INTEGER DEFAULT 0,
    expires_at      DATETIME,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);`

const migrationOAWebhook = `
CREATE TABLE IF NOT EXISTS oa_webhook_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    oa_id       TEXT NOT NULL REFERENCES oa_configs(id),
    event_id    TEXT DEFAULT '',
    event_type  TEXT NOT NULL,                -- message, follow, unfollow, ...
    sender_id   TEXT DEFAULT '',
    raw_data    TEXT NOT NULL,
    processed   INTEGER DEFAULT 0,            -- 0: chờ, 1: đã xử lý, 2: lỗi
    error_msg   TEXT DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_oa_logs ON oa_webhook_logs(oa_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_oa_pending ON oa_webhook_logs(processed) WHERE processed = 0;`

// ====================================
// Account operations
// ====================================

// CreateAccount tạo tài khoản người dùng mới
func (s *Store) CreateAccount(id, displayName string, accountType int) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO accounts (id, display_name, account_type) VALUES (?, ?, ?)",
		id, displayName, accountType,
	)
	return err
}

// UpdateAccount cập nhật thông tin tài khoản
func (s *Store) UpdateAccount(id, displayName, avatar string) error {
	_, err := s.db.Exec(
		"UPDATE accounts SET display_name = ?, avatar = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		displayName, avatar, id,
	)
	return err
}

// GetAccount lấy thông tin tài khoản
func (s *Store) GetAccount(id string) (*Account, error) {
	a := &Account{}
	err := s.db.QueryRow(
		"SELECT id, display_name, avatar, account_type, status, note, created_at, updated_at FROM accounts WHERE id = ?", id,
	).Scan(&a.ID, &a.DisplayName, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListAccounts lấy danh sách tài khoản
func (s *Store) ListAccounts(accountType int) ([]Account, error) {
	q := "SELECT id, display_name, avatar, account_type, status, note, created_at, updated_at FROM accounts"
	if accountType > 0 {
		q += " WHERE account_type = ?"
	}
	q += " ORDER BY created_at DESC"

	rows, err := s.db.Query(q, accountType)
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

func (s *Store) SaveSession(sr *Session) error {
	q := `INSERT OR REPLACE INTO sessions
		(id, account_id, user_id, cookies, secret_key, imei, user_agent, language, ws_urls, api_type, api_version, is_active, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(q, sr.ID, sr.AccountID, sr.UserID, sr.Cookies, sr.SecretKey,
		sr.IMEI, sr.UserAgent, sr.Language, sr.WSURLs, sr.APIType, sr.APIVersion, sr.IsActive, sr.ExpiresAt)
	return err
}

func (s *Store) LoadSession(id string) (*Session, error) {
	sr := &Session{}
	err := s.db.QueryRow(
		`SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, api_type, api_version, is_active, created_at, expires_at
		FROM sessions WHERE id = ?`, id,
	).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

// GetActiveSession lấy session đang active của account
func (s *Store) GetActiveSession(accountID string) (*Session, error) {
	sr := &Session{}
	err := s.db.QueryRow(
		`SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, api_type, api_version, is_active, created_at, expires_at
		FROM sessions WHERE account_id = ? AND is_active = 1 ORDER BY created_at DESC LIMIT 1`, accountID,
	).Scan(&sr.ID, &sr.AccountID, &sr.UserID, &sr.Cookies, &sr.SecretKey,
		&sr.IMEI, &sr.UserAgent, &sr.Language, &sr.WSURLs,
		&sr.APIType, &sr.APIVersion, &sr.IsActive, &sr.CreatedAt, &sr.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sr, err
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec("UPDATE sessions SET is_active = 0 WHERE id = ?", id)
	return err
}

// ====================================
// Conversation operations
// ====================================

func (s *Store) SaveConversation(c *Conversation) error {
	q := `INSERT OR REPLACE INTO conversations
		(id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := s.db.Exec(q, c.ID, c.AccountID, c.Name, c.Avatar, c.ConvType, c.LastMsgID, c.LastMsgAt, c.Unread)
	return err
}

func (s *Store) GetConversation(accountID, convID string) (*Conversation, error) {
	c := &Conversation{}
	err := s.db.QueryRow(
		`SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
		FROM conversations WHERE account_id = ? AND id = ?`, accountID, convID,
	).Scan(&c.ID, &c.AccountID, &c.Name, &c.Avatar, &c.ConvType, &c.LastMsgID, &c.LastMsgAt, &c.Unread, &c.UpdatedAt)
	if err == sql.ErrNoRows { return nil, nil }
	return c, err
}

func (s *Store) DeleteAccount(id string) error {
	s.db.Exec("DELETE FROM sessions WHERE account_id = ?", id)
	s.db.Exec("DELETE FROM conversations WHERE account_id = ?", id)
	s.db.Exec("DELETE FROM messages WHERE account_id = ?", id)
	_, err := s.db.Exec("DELETE FROM accounts WHERE id = ?", id)
	return err
}

func (s *Store) GetConversations(accountID string) ([]Conversation, error) {
	rows, err := s.db.Query(
		`SELECT id, account_id, name, avatar, conv_type, last_msg_id, last_msg_at, unread_count, updated_at
		FROM conversations WHERE account_id = ? ORDER BY updated_at DESC`, accountID,
	)
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
	q := `INSERT OR IGNORE INTO messages
		(id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(q, m.ID, m.AccountID, m.ConvID, m.FromID, m.FromName, m.Content, m.MsgType, m.Timestamp, m.Attachments)
	return err
}

func (s *Store) GetMessages(accountID, convID string, cursor int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments
		FROM messages WHERE account_id = ? AND conv_id = ? AND timestamp < ?
		ORDER BY timestamp DESC LIMIT ?`

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

// SaveMedia lưu thông tin file media, trả về đường dẫn media
func (s *Store) SaveMedia(m *MediaFile) (string, error) {
	// Tạo thư mục account/conv
	dir := filepath.Join(s.mediaPath, m.AccountID, m.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("media dir: %w", err)
	}

	// File path: accountID/convID/fileID.ext (relative)
	relPath := filepath.Join(m.AccountID, m.ConvID, m.ID+"."+m.FileExt)

	q := `INSERT OR IGNORE INTO media
		(id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
		file_size, width, height, source_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`

	_, err := s.db.Exec(q, m.ID, m.AccountID, m.ConvID, m.MsgID, m.FileName, relPath,
		m.FileExt, m.MimeType, m.FileSize, m.Width, m.Height, m.SourceURL)
	if err != nil {
		return "", err
	}

	return filepath.Join(s.mediaPath, relPath), nil
}

// GetUnprocessedMedia lấy danh sách media chưa xử lý AI
func (s *Store) GetUnprocessedMedia(limit int) ([]MediaFile, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, account_id, conv_id, msg_id, file_name, file_path, file_ext, mime_type,
		file_size, width, height, thumb_path, ocr_text, ai_tags, ai_processed, ai_confidence,
		is_downloaded, source_url, created_at
		FROM media WHERE ai_processed = 0 AND is_downloaded = 1
		ORDER BY created_at ASC LIMIT ?`, limit,
	)
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

// MarkMediaProcessed đánh dấu media đã xử lý AI
func (s *Store) MarkMediaProcessed(id, accountID, ocrText, aiTags string, confidence float64, errMsg string) error {
	status := 1
	if errMsg != "" {
		status = 2
	}
	q := `UPDATE media SET ocr_text = ?, ai_tags = ?, ai_confidence = ?, ai_processed = ?
		WHERE id = ? AND account_id = ?`
	_, err := s.db.Exec(q, ocrText, aiTags, confidence, status, id, accountID)
	return err
}

// ====================================
// OA operations
// ====================================

func (s *Store) SaveOAConfig(oc *OAConfig) error {
	q := `INSERT OR REPLACE INTO oa_configs
		(id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(q, oc.ID, oc.AccountID, oc.OAID, oc.OAName, oc.AccessToken, oc.RefreshToken, oc.SecretKey, oc.WebhookURL, oc.IsVerified, oc.ExpiresAt)
	return err
}

func (s *Store) GetOAConfig(oaID string) (*OAConfig, error) {
	oc := &OAConfig{}
	err := s.db.QueryRow(
		`SELECT id, account_id, oa_id, oa_name, access_token, refresh_token, secret_key, webhook_url, is_verified, expires_at, created_at, updated_at
		FROM oa_configs WHERE oa_id = ?`, oaID,
	).Scan(&oc.ID, &oc.AccountID, &oc.OAID, &oc.OAName, &oc.AccessToken, &oc.RefreshToken, &oc.SecretKey, &oc.WebhookURL, &oc.IsVerified, &oc.ExpiresAt, &oc.CreatedAt, &oc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return oc, err
}

// LogWebhook lưu log webhook event
func (s *Store) LogWebhook(oaID, eventID, eventType, senderID, rawData string) (int64, error) {
	r, err := s.db.Exec(
		"INSERT INTO oa_webhook_logs (oa_id, event_id, event_type, sender_id, raw_data) VALUES (?, ?, ?, ?, ?)",
		oaID, eventID, eventType, senderID, rawData,
	)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// GetPendingWebhooks lấy webhook chưa xử lý
func (s *Store) GetPendingWebhooks(limit int) ([]OAWebhookLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, oa_id, event_id, event_type, sender_id, raw_data, processed, error_msg, created_at
		FROM oa_webhook_logs WHERE processed = 0 ORDER BY created_at ASC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []OAWebhookLog
	for rows.Next() {
		var l OAWebhookLog
		if err := rows.Scan(&l.ID, &l.OAID, &l.EventID, &l.EventType, &l.SenderID, &l.RawData, &l.Processed, &l.ErrorMsg, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// ====================================
// Data types
// ====================================

type Account struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Avatar      string    `json:"avatar"`
	AccountType int       `json:"accountType"` // 1: Zalo User, 2: Zalo OA
	Status      int       `json:"status"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Session struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"accountId"`
	UserID     string    `json:"userId"`
	Cookies    string    `json:"cookies"`
	SecretKey  string    `json:"secretKey"`
	IMEI       string    `json:"imei"`
	UserAgent  string    `json:"userAgent"`
	Language   string    `json:"language"`
	WSURLs     string    `json:"wsUrls"`
	APIType    uint      `json:"apiType"`
	APIVersion uint      `json:"apiVersion"`
	IsActive   int       `json:"isActive"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type Conversation struct {
	ID        string       `json:"id"`
	AccountID string       `json:"accountId"`
	Name      string       `json:"name"`
	Avatar    string       `json:"avatar"`
	ConvType  int          `json:"convType"`  // 0: cá nhân, 1: nhóm, 2: OA
	LastMsgID string       `json:"lastMsgId"`
	LastMsgAt sql.NullTime `json:"lastMsgAt"`
	Unread    int          `json:"unread"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

type Message struct {
	ID          string `json:"id"`
	AccountID   string `json:"accountId"`
	ConvID      string `json:"convId"`
	FromID      string `json:"fromId"`
	FromName    string `json:"fromName"`
	Content     string `json:"content"`
	MsgType     int    `json:"msgType"`
	Timestamp   int64  `json:"timestamp"`
	Attachments string `json:"attachments"`
}

type MediaFile struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"accountId"`
	ConvID       string    `json:"convId"`
	MsgID        string    `json:"msgId"`
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"filePath"`
	FileExt      string    `json:"fileExt"`
	MimeType     string    `json:"mimeType"`
	FileSize     int64     `json:"fileSize"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	ThumbPath    string    `json:"thumbPath"`
	WidthThumb   int       `json:"widthThumb"`
	HeightThumb  int       `json:"heightThumb"`
	OCRText      string    `json:"ocrText"`
	AITags       string    `json:"aiTags"`
	AIProcessed  int       `json:"aiProcessed"`
	AIConfidence float64   `json:"aiConfidence"`
	IsDownloaded int       `json:"isDownloaded"`
	SourceURL    string    `json:"sourceUrl"`
	CreatedAt    time.Time `json:"createdAt"`
}

type OAConfig struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"accountId"`
	OAID         string    `json:"oaId"`
	OAName       string    `json:"oaName"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	SecretKey    string    `json:"secretKey"`
	WebhookURL   string    `json:"webhookUrl"`
	IsVerified   int       `json:"isVerified"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type OAWebhookLog struct {
	ID        int       `json:"id"`
	OAID      string    `json:"oaId"`
	EventID   string    `json:"eventId"`
	EventType string    `json:"eventType"`
	SenderID  string    `json:"senderId"`
	RawData   string    `json:"rawData"`
	Processed int       `json:"processed"`
	ErrorMsg  string    `json:"errorMsg"`
	CreatedAt time.Time `json:"createdAt"`
}
