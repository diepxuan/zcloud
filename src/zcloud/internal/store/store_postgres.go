package store

import (
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver cho database/sql
)

// ====================================
// Postgres-specific migration + dialect helpers
// ====================================

// migratePostgres tạo schema Postgres. Tương đương store_sqlite.go nhưng:
// - DATETIME → TIMESTAMP WITH TIME ZONE
// - INTEGER PRIMARY KEY AUTOINCREMENT → BIGSERIAL PRIMARY KEY
// - TEXT DEFAULT CURRENT_TIMESTAMP → TIMESTAMPTZ DEFAULT NOW()
// - Indexes tương đương (Postgres hỗ trợ DESC và partial WHERE)
func (s *Store) migratePostgres() error {
	migrations := []string{
		migrationAccountsPG,
		migrationSessionsPG,
		migrationConversationsPG,
		migrationMessagesPG,
		migrationMediaPG,
		migrationOAPG,
		migrationOAWebhookPG,
	}
	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("postgres migration failed: %w\nSQL: %s", err, m[:min(80, len(m))])
		}
	}
	return s.ensureSessionServiceMapPG()
}

// ensureSessionServiceMapPG thêm cột service_map nếu thiếu.
// Dùng information_schema thay cho PRAGMA table_info (chỉ có ở SQLite).
func (s *Store) ensureSessionServiceMapPG() error {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'sessions' AND column_name = 'service_map'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check service_map: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN service_map TEXT DEFAULT '{}'`); err != nil {
		return fmt.Errorf("add service_map: %w", err)
	}
	return nil
}

// ====================================
// Schema (Postgres) — ánh xạ 1:1 từ SQLite
// ====================================

const migrationAccountsPG = `
CREATE TABLE IF NOT EXISTS accounts (
    id              TEXT PRIMARY KEY,
    display_name    TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    account_type    INTEGER DEFAULT 1,
    status          INTEGER DEFAULT 1,
    note            TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);`

const migrationSessionsPG = `
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
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);`

const migrationConversationsPG = `
CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    name            TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    conv_type       INTEGER DEFAULT 0,
    last_msg_id     TEXT DEFAULT '',
    last_msg_at     TIMESTAMPTZ,
    unread_count    INTEGER DEFAULT 0,
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_convs_account ON conversations(account_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_convs_msgat ON conversations(account_id, last_msg_at DESC NULLS LAST, updated_at DESC);`

const migrationMessagesPG = `
CREATE TABLE IF NOT EXISTS messages (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    from_id         TEXT NOT NULL,
    from_name       TEXT DEFAULT '',
    content         TEXT DEFAULT '',
    msg_type        INTEGER DEFAULT 1,
    timestamp       BIGINT NOT NULL,
    attachments     TEXT DEFAULT '[]',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_msgs_conv ON messages(account_id, conv_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_msgs_ts  ON messages(timestamp);`

const migrationMediaPG = `
CREATE TABLE IF NOT EXISTS media (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    msg_id          TEXT DEFAULT '',
    file_name       TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    file_ext        TEXT DEFAULT '',
    mime_type       TEXT DEFAULT '',
    file_size       BIGINT DEFAULT 0,
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
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_media_conv ON media(account_id, conv_id);
CREATE INDEX IF NOT EXISTS idx_media_ocr  ON media(ocr_text) WHERE ocr_text != '';
CREATE INDEX IF NOT EXISTS idx_media_ai   ON media(ai_processed) WHERE ai_processed = 0;`

const migrationOAPG = `
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
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);`

const migrationOAWebhookPG = `
CREATE TABLE IF NOT EXISTS oa_webhook_logs (
    id          BIGSERIAL PRIMARY KEY,
    oa_id       TEXT NOT NULL REFERENCES oa_configs(id),
    event_id    TEXT DEFAULT '',
    event_type  TEXT NOT NULL,
    sender_id   TEXT DEFAULT '',
    raw_data    TEXT NOT NULL,
    processed   INTEGER DEFAULT 0,
    error_msg   TEXT DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oa_logs ON oa_webhook_logs(oa_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_oa_pending ON oa_webhook_logs(processed) WHERE processed = 0;`
