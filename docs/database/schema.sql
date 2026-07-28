-- Database schema cho zcloud — đồng bộ với internal/store/store.go migrations
-- SQLite (pure Go driver: modernc.org/sqlite)
--
-- Env ZCLOUD_DB_PATH=/path/to/zcloud.db override vị trí DB.
-- Mặc định: ./storages/database/zcloud.db
-- Media files (ảnh, video, voice): lưu trên disk.
-- Mặc định: {media_dir}/{account_id}/{conversation_id}/{file_id}.{ext}

-- ====================================
-- Accounts — tài khoản người dùng zcloud
-- ====================================
CREATE TABLE IF NOT EXISTS accounts (
    id              TEXT PRIMARY KEY,
    display_name    TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    account_type    INTEGER DEFAULT 1,      -- 1: Zalo User, 2: Zalo OA
    status          INTEGER DEFAULT 1,      -- 1: active, 0: disabled
    note            TEXT DEFAULT '',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ====================================
-- Sessions — phiên đăng nhập Zalo
-- ====================================
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
CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);

-- ====================================
-- Conversations — hội thoại Zalo
-- ====================================
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
CREATE INDEX IF NOT EXISTS idx_convs_account ON conversations(account_id, updated_at DESC);

-- ====================================
-- Messages — tin nhắn Zalo
-- ====================================
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
CREATE INDEX IF NOT EXISTS idx_msgs_ts  ON messages(timestamp);

-- ====================================
-- Media — file media + trường AI
-- ====================================
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
CREATE INDEX IF NOT EXISTS idx_media_ai   ON media(ai_processed) WHERE ai_processed = 0;

-- ====================================
-- Zalo OA — cấu hình Official Account
-- ====================================
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
);

-- ====================================
-- Zalo OA — log webhook events
-- ====================================
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
CREATE INDEX IF NOT EXISTS idx_oa_pending ON oa_webhook_logs(processed) WHERE processed = 0;
