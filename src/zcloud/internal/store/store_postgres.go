package store

import (
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver cho database/sql
)

// ====================================
// Postgres-specific migration + dialect helpers
// ====================================

// migratePostgres tạo schema Postgres (khi cần migration mới).
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
		migrationMediaJobsPG,
		migrationContactsPG,
	}
	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("postgres migration failed: %w\nSQL: %s", err, m[:min(80, len(m))])
		}
	}
	if err := s.ensureSessionServiceMapPG(); err != nil {
		return err
	}
	if err := s.ensureAccountUserIDPG(); err != nil {
		return err
	}
	if err := s.ensureAccountEnabledPG(); err != nil {
		return err
	}
	if err := s.ensureAccountDisabledReasonPG(); err != nil {
		return err
	}
	if err := s.ensureAccountSyncV2PG(); err != nil {
		return err
	}
	if err := s.ensureAccountTransportPG(); err != nil {
		return err
	}
	if err := s.ensureZaloAccountsTablePG(); err != nil {
		return err
	}
	if err := s.ensureAccountZaloAccountIDPG(); err != nil {
		return err
	}
	if err := s.backfillZaloAccountsPG(); err != nil {
		return err
	}
	return s.ensureSessionTransportPG()
}

// ensureSessionServiceMapPG thêm cột service_map nếu thiếu.
// Dùng information_schema (chuẩn SQL) để kiểm tra cột đã tồn tại.
func (s *Store) ensureSessionServiceMapPG() error {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'sessions' AND column_name = 'service_map'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check service_map: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS service_map TEXT DEFAULT '{}'`); err != nil {
		return fmt.Errorf("add service_map: %w", err)
	}
	return nil
}

// ====================================
// Schema (Postgres)
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

// migrationZaloAccountsPG tạo bảng zalo_accounts — lưu thông tin Zalo user
// (zalo_user_id, display_name, avatar, phone) **không bao giờ xoá khi logout**.
// accounts.zalo_account_id FK trỏ về đây (ON DELETE RESTRICT = bảo vệ nếu
// còn accounts tham chiếu). Sep account vẫn hiển thị trong panel Quản lý
// sau khi logout, chỉ status dot=off vì không có session.
const migrationZaloAccountsPG = `
CREATE TABLE IF NOT EXISTS zalo_accounts (
    id              TEXT PRIMARY KEY,
    zalo_user_id    TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    avatar          TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

// ensureAccountUserIDPG thêm cột user_id vào accounts nếu thiếu.
func (s *Store) ensureAccountUserIDPG() error {
	var exists bool
	// current_schema() để không leak từ public (regression T22.3).
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'accounts' AND column_name = 'user_id'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.user_id: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS user_id TEXT DEFAULT ''`); err != nil {
		return fmt.Errorf("add accounts.user_id: %w", err)
	}
	return nil
}

// ensureSessionTransportPG thêm cột transport + cipher_key vào sessions nếu thiếu.
// Mặc định transport='' (web), cipher_key rỗng.
func (s *Store) ensureSessionTransportPG() error {
	for _, col := range []struct {
		def string
		typ string
	}{
		{"transport", "TEXT NOT NULL DEFAULT ''"},
		{"cipher_key", "TEXT NOT NULL DEFAULT ''"},
	} {
		var exists bool
		err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sessions' AND column_name=$1)`, col.def).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check sessions.%s: %w", col.def, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE sessions ADD COLUMN IF NOT EXISTS %s %s", col.def, col.typ)); err != nil {
			return fmt.Errorf("add sessions.%s: %w", col.def, err)
		}
	}
	return nil
}

// ensureAccountDisabledReasonPG thêm cột disabled_reason vào accounts nếu thiếu.
// ensureAccountSyncV2PG thêm cột syncv2_state JSONB vào accounts nếu thiếu.
// Lưu blob JSON state machine SyncV2 (ed25519 keypair, last_seq_id, phase, ...).
// Rỗng mặc định — chỉ fill khi account start syncv2 backup flow (T22.3).
// Lọc theo current_schema() tránh cross-schema leak từ public.
func (s *Store) ensureAccountSyncV2PG() error {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'accounts' AND column_name = 'syncv2_state'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.syncv2_state: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS syncv2_state JSONB NOT NULL DEFAULT '{}'::jsonb`); err != nil {
		return fmt.Errorf("add accounts.syncv2_state: %w", err)
	}
	return nil
}

// ensureAccountTransportPG thêm cột transport TEXT vào accounts nếu thiếu.
// Mặc định '' (web). Field này song song với sessions.transport để
// ListAccounts/GetAccount có thể lọc account theo transport (vd chỉ
// liệt kê PC transport khi login trusted-device).
func (s *Store) ensureAccountTransportPG() error {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'accounts' AND column_name = 'transport'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.transport: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS transport TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add accounts.transport: %w", err)
	}
	return nil
}

func (s *Store) ensureAccountDisabledReasonPG() error {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='accounts' AND column_name='disabled_reason')`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.disabled_reason: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS disabled_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add accounts.disabled_reason: %w", err)
	}
	return nil
}
// Default TRUE cho backward compat — account cũ vẫn enabled mặc định.
func (s *Store) ensureAccountEnabledPG() error {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'accounts' AND column_name = 'enabled'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.enabled: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE`); err != nil {
		return fmt.Errorf("add accounts.enabled: %w", err)
	}
	return nil
}

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

const migrationMediaJobsPG = `
CREATE TABLE IF NOT EXISTS media_jobs (
    id              TEXT NOT NULL,
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    conv_id         TEXT DEFAULT '',
    msg_id          TEXT DEFAULT '',
    file_name       TEXT DEFAULT '',
    file_ext        TEXT DEFAULT '',
    source_url      TEXT NOT NULL,
    local_path      TEXT DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER DEFAULT 0,
    max_attempts    INTEGER DEFAULT 3,
    last_error      TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_media_jobs_status ON media_jobs(status, created_at);`

const migrationContactsPG = `
CREATE TABLE IF NOT EXISTS contacts (
    account_id      TEXT NOT NULL REFERENCES accounts(id),
    user_id         TEXT NOT NULL,
    name            TEXT DEFAULT '',
    avatar          TEXT DEFAULT '',
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (account_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_contacts_account ON contacts(account_id);`

// ensureZaloAccountsTablePG tạo bảng zalo_accounts nếu chưa có.
// Xem migrationZaloAccountsPG ở trên.
func (s *Store) ensureZaloAccountsTablePG() error {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='zalo_accounts')`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check zalo_accounts: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(migrationZaloAccountsPG); err != nil {
		return fmt.Errorf("create zalo_accounts: %w", err)
	}
	return nil
}

// ensureAccountZaloAccountIDPG thêm cột accounts.zalo_account_id FK → zalo_accounts.id.
// RESTRICT: không xoá zalo_account nếu còn accounts tham chiếu (bảo vệ data).
func (s *Store) ensureAccountZaloAccountIDPG() error {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='accounts' AND column_name='zalo_account_id')`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check accounts.zalo_account_id: %w", err)
	}
	if exists {
		return nil
	}
	// Step 1: add nullable column (no FK yet to allow backfill).
	// zalo_account_id nullable (NULL = tài khoản chưa qua T24, vd OA).
	// Dùng NULL thay vì '' vì FK RESTRICT không match được empty string.
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS zalo_account_id TEXT DEFAULT NULL`); err != nil {
		return fmt.Errorf("add accounts.zalo_account_id: %w", err)
	}
	// Step 2: add FK constraint if missing. ON DELETE RESTRICT = không xoá zalo_acc khi còn acc reference.
	var fkExists bool
	err = s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.table_constraints WHERE table_schema=current_schema() AND table_name='accounts' AND constraint_name='accounts_zalo_account_id_fkey')`).Scan(&fkExists)
	if err != nil {
		return fmt.Errorf("check FK: %w", err)
	}
	if !fkExists {
		if _, err := s.db.Exec(`ALTER TABLE accounts ADD CONSTRAINT accounts_zalo_account_id_fkey FOREIGN KEY (zalo_account_id) REFERENCES zalo_accounts(id) ON DELETE RESTRICT`); err != nil {
			return fmt.Errorf("add FK: %w", err)
		}
	}
	return nil
}

// backfillZaloAccountsPG backfill zalo_accounts từ accounts hiện có + set FK.
// Chạy 1 lần sau migration: với mỗi account có user_id, tạo zalo_account nếu
// chưa có (id='za_'+user_id) rồi set accounts.zalo_account_id.
func (s *Store) backfillZaloAccountsPG() error {
	// Chỉ backfill account chưa có zalo_account_id mà có user_id.
	rows, err := s.db.Query(`SELECT id, user_id, display_name, avatar FROM accounts WHERE zalo_account_id IS NULL AND user_id <> ''`)
	if err != nil {
		return fmt.Errorf("query accounts to backfill: %w", err)
	}
	defer rows.Close()
	type backfill struct{ id, userID, name, avatar string }
	var bs []backfill
	for rows.Next() {
		var b backfill
		if err := rows.Scan(&b.id, &b.userID, &b.name, &b.avatar); err != nil {
			return err
		}
		bs = append(bs, b)
	}
	if len(bs) == 0 {
		return nil
	}
	for _, b := range bs {
		zaID := "za_" + b.userID
		// Insert zalo_account (idempotent — ON CONFLICT DO NOTHING).
		if _, err := s.db.Exec(`INSERT INTO zalo_accounts (id, zalo_user_id, display_name, avatar) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`, zaID, b.userID, b.name, b.avatar); err != nil {
			return fmt.Errorf("insert zalo_account %s: %w", zaID, err)
		}
		if _, err := s.db.Exec(`UPDATE accounts SET zalo_account_id = $1 WHERE id = $2`, zaID, b.id); err != nil {
			return fmt.Errorf("update account %s: %w", b.id, err)
		}
	}
	return nil
}
