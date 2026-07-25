-- Database schema cho zcloud production
--
-- Sếp set env ZCLOUD_DB_PATH=zcloud.db (SQLite) hoặc connect string
-- Nếu không set → dùng file-based storage (local session JSON)
--
-- Schema này chỉ cần khi:
--   1. Cần persist messages lâu dài
--   2. Nhiều user cùng dùng
--   3. Session quản lý tập trung

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    cookies     TEXT NOT NULL,       -- JSON encrypted
    secret_key  TEXT NOT NULL,       -- zpw_enk
    imei        TEXT NOT NULL,
    user_agent  TEXT,
    ws_urls     TEXT,                -- JSON array
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT PRIMARY KEY,
    name            TEXT,
    avatar          TEXT,
    type            INTEGER DEFAULT 0,  -- 0: individual, 1: group
    last_msg_id     TEXT,
    last_msg_at     DATETIME,
    unread_count    INTEGER DEFAULT 0,
    updated_at      DATETIME
);

CREATE TABLE IF NOT EXISTS messages (
    id              TEXT PRIMARY KEY,
    conv_id         TEXT NOT NULL REFERENCES conversations(id),
    from_id         TEXT NOT NULL,
    content         TEXT,
    msg_type        INTEGER DEFAULT 1,  -- 1:text, 2:image, 3:file...
    timestamp       INTEGER NOT NULL,   -- Unix ms
    attachments     TEXT,               -- JSON array
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_messages_conv ON messages(conv_id, timestamp DESC);
CREATE INDEX idx_sessions_user ON sessions(user_id);
