package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// ====================================
// SQLite-specific migration + dialect helpers
// ====================================

// migrateSQLite tạo schema nếu chưa có. Các migration dùng cú pháp SQLite.
func (s *Store) migrateSQLite() error {
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
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m[:min(80, len(m))])
		}
	}
	return s.ensureSessionServiceMapSQLite()
}

// ensureSessionServiceMapSQLite thêm cột service_map nếu thiếu.
// PRAGMA table_info chỉ có ở SQLite.
func (s *Store) ensureSessionServiceMapSQLite() error {
	rows, err := s.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return fmt.Errorf("sessions pragma: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt, pk interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("sessions pragma scan: %w", err)
		}
		if name == "service_map" {
			return nil
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN service_map TEXT DEFAULT '{}'`); err != nil {
		return fmt.Errorf("sessions add service_map: %w", err)
	}
	return nil
}

// helper chuyển đổi INSERT OR IGNORE/REPLACE của SQLite sang SQL chuẩn.
func insertIgnoreSQLite(table string, cols string) string {
	return "INSERT OR IGNORE INTO " + table + " (" + cols + ") VALUES "
}
func insertReplaceSQLite(table string, conflictCols string, setCols string) string {
	return "INSERT OR REPLACE INTO " + table + " " // bổ sung VALUES bên ngoài
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ====================================
// Schema (SQLite) — giữ nguyên từ store.go gốc
// ====================================

const migrationAccounts = `
CREATE TABLE IF NOT EXISTS accounts (
    id              TEXT PRIMARY KEY,
    display_name    TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    account_type    INTEGER DEFAULT 1,
    status          INTEGER DEFAULT 1,
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
    service_map     TEXT DEFAULT '{}',
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
    conv_type       INTEGER DEFAULT 0,
    last_msg_id     TEXT DEFAULT '',
    last_msg_at     DATETIME,
    unread_count    INTEGER DEFAULT 0,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_convs_account ON conversations(account_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_convs_msgat ON conversations(account_id, last_msg_at DESC, updated_at DESC);`

const migrationMessages = `
CREATE TABLE IF NOT EXISTS messages (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    from_id         TEXT NOT NULL,
    from_name       TEXT DEFAULT '',
    content         TEXT DEFAULT '',
    msg_type        INTEGER DEFAULT 1,
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
    file_name       TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    file_ext        TEXT DEFAULT '',
    mime_type       TEXT DEFAULT '',
    file_size       INTEGER DEFAULT 0,
    width           INTEGER DEFAULT 0,
    height          INTEGER DEFAULT 0,
    width_thumb     INTEGER DEFAULT 0,
    height_thumb    INTEGER DEFAULT 0,
    thumb_path      TEXT DEFAULT '',
    ocr_text        TEXT DEFAULT '',
    ai_tags         TEXT DEFAULT '[]',
    ai_processed    INTEGER DEFAULT 0,
    ai_confidence   REAL DEFAULT 0,
    is_downloaded   INTEGER DEFAULT 0,
    source_url      TEXT DEFAULT '',
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
    event_type  TEXT NOT NULL,
    sender_id   TEXT DEFAULT '',
    raw_data    TEXT NOT NULL,
    processed   INTEGER DEFAULT 0,
    error_msg   TEXT DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_oa_logs ON oa_webhook_logs(oa_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_oa_pending ON oa_webhook_logs(processed) WHERE processed = 0;`

// ====================================
// Đảm bảo import sql được dùng (cho compiler)
// ====================================
var _ = sql.ErrNoRows
