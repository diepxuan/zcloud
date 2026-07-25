-- Database schema cho zcloud production
-- SQLite (pure Go driver: modernc.org/sqlite)
--
-- Sếp set env ZCLOUD_DB_PATH=/path/to/zcloud.db
-- Nếu không set → mặc định tạo file zcloud.db tại thư mục chạy
--
-- Multi-user: mỗi user Zalo có 1 account record riêng
-- Zalo OA: webhook config + message log riêng
--
-- Media files (ảnh, video, voice): lưu trên disk, DB lưu relative path
-- Mặc định: {media_dir}/{user_id}/{conversation_id}/{file_id}.ext

-- ====================================
-- Accounts — tài khoản người dùng zcloud
-- Mỗi account = 1 người dùng, có thể có nhiều session Zalo
-- ====================================
CREATE TABLE IF NOT EXISTS accounts (
    id              TEXT PRIMARY KEY,                     -- UUID tự sinh
    display_name    TEXT DEFAULT '',                      -- Tên hiển thị
    avatar          TEXT DEFAULT '',
    account_type    INTEGER DEFAULT 1,                    -- 1: Zalo User, 2: Zalo OA
    status          INTEGER DEFAULT 1,                    -- 1: active, 0: disabled
    note            TEXT DEFAULT '',                      -- Ghi chú
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ====================================
-- Zalo sessions — phiên đăng nhập Zalo
-- Mỗi account có thể có nhiều session (nếu login/logout nhiều lần)
-- ====================================
CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,                     -- zpw_sek hoặc UUID
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    user_id         TEXT NOT NULL,                        -- UID Zalo
    cookies         TEXT NOT NULL,                        -- JSON mã hóa
    secret_key      TEXT NOT NULL,                        -- zpw_enk (base64)
    imei            TEXT NOT NULL,
    user_agent      TEXT DEFAULT '',
    language        TEXT DEFAULT 'vi',
    ws_urls         TEXT DEFAULT '[]',                    -- JSON array
    api_type        INTEGER DEFAULT 30,
    api_version     INTEGER DEFAULT 665,
    is_active       INTEGER DEFAULT 1,                    -- 1: đang dùng
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at      DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);

-- ====================================
-- Conversations — hội thoại Zalo
-- Gắn với account_id để phân biệt giữa các user
-- + thêm loại OA_GROUP cho Zalo OA conversations
-- ====================================
CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT NOT NULL,                        -- conversation ID Zalo
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    name            TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    conv_type       INTEGER DEFAULT 0,                    -- 0: cá nhân, 1: nhóm, 2: OA
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
    id              TEXT NOT NULL,                        -- msgId Zalo
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT NOT NULL,
    from_id         TEXT NOT NULL,
    from_name       TEXT DEFAULT '',
    content         TEXT DEFAULT '',
    msg_type        INTEGER DEFAULT 1,                    -- 1:text, 2:image, 3:sticker, 4:file...
    timestamp       INTEGER NOT NULL,                     -- Unix ms
    attachments     TEXT DEFAULT '[]',                    -- JSON array
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_msgs_conv ON messages(account_id, conv_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_msgs_ts  ON messages(timestamp);

-- ====================================
-- Media — file đã tải về
-- ====================================
CREATE TABLE IF NOT EXISTS media (
    id          TEXT NOT NULL,
    account_id  TEXT NOT NULL REFERENCES accounts(id),
    conv_id     TEXT NOT NULL,
    msg_id      TEXT DEFAULT '',
    file_name   TEXT NOT NULL,
    file_path   TEXT NOT NULL,                            -- Relative path từ media dir
    mime_type   TEXT DEFAULT '',
    file_size   INTEGER DEFAULT 0,
    width       INTEGER DEFAULT 0,
    height      INTEGER DEFAULT 0,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_media_conv ON media(account_id, conv_id);

-- ====================================
-- Zalo OA — cấu hình Official Account
-- ====================================
CREATE TABLE IF NOT EXISTS oa_configs (
    id              TEXT PRIMARY KEY,                     -- UUID
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    oa_id           TEXT NOT NULL,                        -- OA ID Zalo
    access_token    TEXT NOT NULL,                        -- Token gọi API OA
    refresh_token   TEXT DEFAULT '',
    secret_key      TEXT NOT NULL,                        -- Key xác thực webhook
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
    event_type  TEXT NOT NULL,                            -- message, follow, unfollow, ...
    raw_data    TEXT NOT NULL,                            -- JSON gốc từ Zalo
    processed   INTEGER DEFAULT 0,                        -- 0: chờ, 1: đã xử lý
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oa_logs ON oa_webhook_logs(oa_id, created_at DESC);
