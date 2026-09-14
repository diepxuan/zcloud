package store

import (
	"strings"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	q := "SELECT id, display_name, user_id, avatar, account_type, status, note, enabled, disabled_reason, transport, zalo_account_id, syncv2_state, created_at, updated_at FROM accounts WHERE id = $1"
	err := s.db.QueryRow(q, id).Scan(&a.ID, &a.DisplayName, &a.UserID, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.Enabled, &a.DisabledReason, &a.Transport, &a.ZaloAccountID, &a.SyncV2State, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListAccounts trả về tất cả accounts (accountType=0) hoặc theo type.
// enabledOnly: nếu true chỉ trả account có enabled=true.
func (s *Store) ListAccounts(accountType int, enabledOnly bool) ([]Account, error) {
	q := "SELECT id, display_name, user_id, avatar, account_type, status, note, enabled, disabled_reason, transport, zalo_account_id, syncv2_state, created_at, updated_at FROM accounts"
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
		var zaID sql.NullString
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.UserID, &a.Avatar, &a.AccountType, &a.Status, &a.Note, &a.Enabled, &a.DisabledReason, &a.Transport, &zaID, &a.SyncV2State, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if zaID.Valid {
			a.ZaloAccountID = zaID.String
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// GetAccountSyncV2State trả về raw JSON string của accounts.syncv2_state
// (T22.3). Trả về "{}" nếu account không tồn tại (an toàn cho caller parse).
func (s *Store) GetAccountSyncV2State(accountID string) (string, error) {
	var raw string
	err := s.db.QueryRow(`SELECT syncv2_state::text FROM accounts WHERE id = $1`, accountID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "{}", nil
	}
	if err != nil {
		return "", err
	}
	return raw, nil
}

// SetAccountSyncV2State lưu JSON blob SyncV2 state cho account.
// Caller phải đảm bảo `stateJSON` là JSON hợp lệ (vd `core.SyncV2State`
// marshal ra). Trả về error nếu account không tồn tại.
func (s *Store) SetAccountSyncV2State(accountID, stateJSON string) error {
	res, err := s.db.Exec(`UPDATE accounts SET syncv2_state = $1::jsonb, updated_at = NOW() WHERE id = $2`, stateJSON, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("account not found: %s", accountID)
	}
	return nil
}

// SetAccountEnabled bật/tắt flag enabled của account.
// WS listener KHÔNG bị ảnh hưởng — account disabled vẫn listen + sync.
func (s *Store) SetAccountEnabled(accountID string, enabled bool) error {
	reason := ""
	if !enabled {
		reason = "user"
	}
	_, err := s.db.Exec("UPDATE accounts SET enabled = $1, disabled_reason = $2, updated_at = NOW() WHERE id = $3", enabled, reason, accountID)
	return err
}

// SetAccountDisabledReason: đánh dấu account bị auto-disable (vd zpw_sek fail).
// Khi reason != "" sẽ được giữ nguyên; khi reason == "" xoá flag.
func (s *Store) SetAccountDisabledReason(accountID, reason string) error {
	_, err := s.db.Exec("UPDATE accounts SET disabled_reason = $1, updated_at = NOW() WHERE id = $2", reason, accountID)
	return err
}

// SetAccountTransport cập nhật transport hiện tại của account. Caller phải
// tự xử lý reset syncv2_state khi transport đổi (xem T24.4 + câu hỏi Sếp #6).
func (s *Store) SetAccountTransport(accountID, transport string) error {
	_, err := s.db.Exec("UPDATE accounts SET transport = $1, updated_at = NOW() WHERE id = $2", transport, accountID)
	return err
}

// ====================================
// ZaloAccount operations (T24 — 3-tier identity)
// ====================================
//
// Tầng immutable: lưu thông tin identity Zalo (UID, display_name, avatar,
// phone) **không bao giờ xoá khi logout**. Mục đích:
//   - Giữ continuity: Sep logout rồi login lại → vẫn nhận ra "đây là Sep".
//   - Tách rõ "đổi session" (logout) vs "xoá user" (DeleteZaloAccount).
//
// Quan hệ 1-N với accounts: 1 zalo_account có thể có nhiều accounts rows
// (vd multi-account filter T13, hoặc cùng UID qua nhiều transport). Hiện
// tại Sếp chọn reuse 1 account cho cùng UID (câu hỏi #3) → thực tế chỉ 1.

const upsertZaloAccountSQL = `INSERT INTO zalo_accounts (id, zalo_user_id, display_name, avatar, phone, updated_at)
	VALUES ($1, $2, $3, $4, $5, NOW())
	ON CONFLICT (zalo_user_id) DO UPDATE SET
		display_name = EXCLUDED.display_name,
		avatar = EXCLUDED.avatar,
		phone = COALESCE(NULLIF(EXCLUDED.phone, ''), zalo_accounts.phone),
		updated_at = NOW()`

// UpsertZaloAccount tạo hoặc cập nhật zalo_account theo zalo_user_id (unique).
// Idempotent — có thể gọi nhiều lần an toàn (vd refresh profile khi user đổi
// tên trên Zalo). Trả về ID 'za_<user_id>'.
//
// Phone: nếu truyền rỗng giữ nguyên phone cũ (COALESCE trong SQL).
func (s *Store) UpsertZaloAccount(zaloUserID, displayName, avatar, phone string) (string, error) {
	id := "za_" + zaloUserID
	_, err := s.db.Exec(upsertZaloAccountSQL, id, zaloUserID, displayName, avatar, phone)
	if err != nil {
		return "", fmt.Errorf("upsert zalo_account %s: %w", zaloUserID, err)
	}
	return id, nil
}

// GetZaloAccount trả về ZaloAccount theo ID ('za_<user_id>'). Trả nil nếu
// không có (caller check trước khi dùng).
func (s *Store) GetZaloAccount(id string) (*ZaloAccount, error) {
	za := &ZaloAccount{}
	q := `SELECT id, zalo_user_id, display_name, avatar, phone, created_at, updated_at
		FROM zalo_accounts WHERE id = $1`
	err := s.db.QueryRow(q, id).Scan(&za.ID, &za.ZaloUserID, &za.DisplayName, &za.Avatar, &za.Phone, &za.CreatedAt, &za.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return za, nil
}

// GetZaloAccountByUserID trả về ZaloAccount theo UID. Trả nil nếu không có.
func (s *Store) GetZaloAccountByUserID(zaloUserID string) (*ZaloAccount, error) {
	za := &ZaloAccount{}
	q := `SELECT id, zalo_user_id, display_name, avatar, phone, created_at, updated_at
		FROM zalo_accounts WHERE zalo_user_id = $1`
	err := s.db.QueryRow(q, zaloUserID).Scan(&za.ID, &za.ZaloUserID, &za.DisplayName, &za.Avatar, &za.Phone, &za.CreatedAt, &za.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return za, nil
}

// ListZaloAccounts trả về tất cả zalo_accounts (sort theo created_at DESC).
func (s *Store) ListZaloAccounts() ([]ZaloAccount, error) {
	q := `SELECT id, zalo_user_id, display_name, avatar, phone, created_at, updated_at
		FROM zalo_accounts ORDER BY created_at DESC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ZaloAccount
	for rows.Next() {
		var za ZaloAccount
		if err := rows.Scan(&za.ID, &za.ZaloUserID, &za.DisplayName, &za.Avatar, &za.Phone, &za.CreatedAt, &za.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, za)
	}
	return out, nil
}

// FindAccountByZaloAccountID tìm account theo FK zalo_account_id. Trả về
// account ID (vd 'acc_<user_id>') hoặc '' nếu không có.
//
// Em chọn trả về string thay vì *Account để caller tự quyết có tạo mới
// hay không (vd FindOrCreateAccountByZaloAccountID).
func (s *Store) FindAccountByZaloAccountID(zaloAccountID string) (string, error) {
	var id string
	q := `SELECT id FROM accounts WHERE zalo_account_id = $1
		ORDER BY enabled DESC, created_at ASC LIMIT 1`
	err := s.db.QueryRow(q, zaloAccountID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// FindOrCreateAccountByZaloAccountID tìm account theo FK zalo_account_id.
//   - Có row enabled=true đầu tiên (sort created_at ASC) → trả về ID cũ.
//   - Có row nhưng tất cả enabled=false → trả về ID cũ nhất (để login lại dùng).
//     Sếp tự bật enabled = true qua UI nếu muốn dùng.
//   - Không có row nào → tạo mới với ID 'acc_<zalo_user_id>' (deterministic).
//
// Lưu ý: caller phải gọi UpsertZaloAccount trước để có zalo_account_id.
func (s *Store) FindOrCreateAccountByZaloAccountID(zaloAccountID, zaloUserID, displayName, avatar string) (string, error) {
	// Tìm row cũ (ưu tiên enabled=true, fallback row cũ nhất).
	id, err := s.FindAccountByZaloAccountID(zaloAccountID)
	if err != nil {
		return "", err
	}
	if id != "" {
		// Cập nhật display_name/avatar cho khớp profile Zalo mới nhất.
		// (Khi Sếp đổi tên hiển thị trên Zalo → login lại zcloud sẽ update.)
		_, _ = s.db.Exec(`UPDATE accounts SET display_name = $1, avatar = $2, updated_at = NOW() WHERE id = $3`,
			displayName, avatar, id)
		return id, nil
	}
	// Tạo mới.
	id = "acc_" + zaloUserID
	if err := s.CreateAccount(id, displayName, 1); err != nil {
		return "", fmt.Errorf("create account %s: %w", id, err)
	}
	if avatar != "" {
		_ = s.UpdateAccount(id, displayName, avatar)
	}
	if err := s.SetAccountUserID(id, zaloUserID); err != nil {
		return "", fmt.Errorf("set user_id %s: %w", id, err)
	}
	if _, err := s.db.Exec(`UPDATE accounts SET zalo_account_id = $1, updated_at = NOW() WHERE id = $2`,
		zaloAccountID, id); err != nil {
		return "", fmt.Errorf("set zalo_account_id %s: %w", id, err)
	}
	return id, nil
}

// DeleteZaloAccount xoá zalo_account theo ID. FK ON DELETE RESTRICT trong
// skeleton migration → nếu còn accounts tham chiếu sẽ FAIL (bảo vệ data).
// Để xoá sạch cả footprint, caller phải xoá accounts trước (UI nút Xoá sẽ
// gọi DeleteAccountByZaloAccountID trước rồi mới gọi DeleteZaloAccount).
//
// Note: Hàm này CHỈ xoá row zalo_accounts. Nếu cần cascade toàn bộ
// (accounts → sessions → messages/conversations/media), dùng
// DeleteAccountByZaloAccountID thay thế.
func (s *Store) DeleteZaloAccount(id string) error {
	res, err := s.db.Exec(`DELETE FROM zalo_accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete zalo_account %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("zalo_account %s not found", id)
	}
	return nil
}

// DeleteAccountByZaloAccountID cascade xoá sạch footprint của zalo_account:
//   1. Xoá accounts tham chiếu (FK CASCADE sẽ xoá sessions; messages/conversations/
//      media/contacts thì cần xoá tường minh vì FK không cascade tới đó).
//   2. Xoá zalo_accounts row.
//
// Thứ tự quan trọng: xoá zalo_account TRƯỚC sẽ trigger RESTRICT → fail nếu còn
// accounts. Nên xoá accounts (và data liên quan) trước.
func (s *Store) DeleteAccountByZaloAccountID(zaloAccountID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 1. Tìm tất cả accounts tham chiếu.
	rows, err := tx.Query(`SELECT id FROM accounts WHERE zalo_account_id = $1`, zaloAccountID)
	if err != nil {
		return err
	}
	var accountIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		accountIDs = append(accountIDs, id)
	}
	rows.Close()
	// 2. Với mỗi account: xoá data liên quan (FK không cascade đến messages/...).
	for _, aid := range accountIDs {
		if _, err := tx.Exec(`DELETE FROM messages WHERE account_id = $1`, aid); err != nil {
			return fmt.Errorf("delete messages %s: %w", aid, err)
		}
		if _, err := tx.Exec(`DELETE FROM conversations WHERE account_id = $1`, aid); err != nil {
			return fmt.Errorf("delete conversations %s: %w", aid, err)
		}
		if _, err := tx.Exec(`DELETE FROM media WHERE account_id = $1`, aid); err != nil {
			return fmt.Errorf("delete media %s: %w", aid, err)
		}
		if _, err := tx.Exec(`DELETE FROM contacts WHERE account_id = $1`, aid); err != nil {
			return fmt.Errorf("delete contacts %s: %w", aid, err)
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE account_id = $1`, aid); err != nil {
			return fmt.Errorf("delete sessions %s: %w", aid, err)
		}
		if _, err := tx.Exec(`DELETE FROM accounts WHERE id = $1`, aid); err != nil {
			return fmt.Errorf("delete account %s: %w", aid, err)
		}
	}
	// 3. Xoá zalo_account (bây giờ không còn FK tham chiếu).
	if _, err := tx.Exec(`DELETE FROM zalo_accounts WHERE id = $1`, zaloAccountID); err != nil {
		return fmt.Errorf("delete zalo_account %s: %w", zaloAccountID, err)
	}
	return tx.Commit()
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

func (s *Store) SaveSession(sr *Session) error {
	// Multi-session aware (T24.3 + câu hỏi Sếp #3): KHÔNG deactivate session cũ
	// cùng account. Mỗi login tạo 1 session mới (vd PC + Web backup song song).
	// StartZaloListener chỉ pick session active mới nhất (GetActiveSession),
	// các session khác đứng im — phục vụ khi PC fail muốn rơi sang Web.
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

// LoadSessionByAccountID trả về session mới nhất của account, kể cả khi
// is_active=0. Dùng cho HandleAccountRestart: nếu session active không có
// (do bị deactivate), vẫn load được để thử autoRefresh bằng cookie cũ.
func (s *Store) LoadSessionByAccountID(accountID string) (*Session, error) {
	sr := &Session{}
	q := `SELECT id, account_id, user_id, cookies, secret_key, imei, user_agent, language,
		ws_urls, service_map, api_type, api_version, is_active, created_at, expires_at,
		transport, cipher_key
		FROM sessions WHERE account_id = $1 ORDER BY created_at DESC LIMIT 1`
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

// DeleteExpiredSessions xoá sessions cũ hơn maxAge. Trả về số row đã xoá.
//
// Quyết định câu hỏi Sếp #3 (multi-session): 1 zalo_account có thể có nhiều
// sessions (vd PC chính + Web backup). Sessions quá maxAge → tự xoá để:
//   - Tránh DB phình (mỗi login ~5KB cookies + ws_urls).
//   - Tránh nhầm lẫn session expired khi StartZaloListener pick active mới nhất.
//
// Logic: so sánh created_at (không expires_at) vì expires_at do Zalo set
// (thường ~1 năm), còn ta muốn cleanup session "không dùng đã lâu".
// Created_at quá 30 ngày + is_active=0 → xoá (is_active=1 vẫn giữ để
// StartZaloListener dùng).
func (s *Store) DeleteExpiredSessions(maxAge time.Duration) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE created_at < NOW() - $1::interval AND is_active = 0`,
		fmt.Sprintf("%d seconds", int(maxAge.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
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

// LogoutAccount: hành vi "đăng xuất" — KHÔNG xoá data zcloud đã thu thập.
//
// Tách rõ 2 hành vi (T24.3):
//   - LogoutAccount (đây): xoá sessions + reset cipher_key. GIỮ messages/
//     conversations/media/contacts + identity (zalo_accounts). Sếp logout rồi
//     login lại cùng UID → vẫn thấy data cũ.
//   - DeleteAccountByZaloAccountID: cascade xoá sạch toàn bộ footprint của
//     user Zalo đó (UI nút "Xoá").
//
// Quyết định câu hỏi Sếp #7: KHÔNG reset transport khi logout thường.
// Lý do: Sếp muốn transport=pc là primary, transport=web chỉ fallback.
// Reset mỗi logout sẽ buộc chọn lại → phiền. Reset chỉ khi Sếp chủ động
// đổi (UI modal riêng) hoặc khi login fail do transport mismatch.
//
// Quyết định câu hỏi Sếp #6: syncv2_state KHÔNG reset khi logout thường.
// Lý do: ed25519 keypair không bị Zalo server-side ràng buộc với session cụ
// thể — chỉ cần transport=pc. Reset mỗi logout → phải request-sync lại,
// mất 5-10 phút Phase B. Reset chỉ khi transport đổi (vd pc → web).
func (s *Store) LogoutAccount(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 1. Xoá sessions (credentials hết hạn khi logout).
	if _, err := tx.Exec("DELETE FROM sessions WHERE account_id = $1", id); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	// 2. Reset cipher_key (per-session secret); giữ transport + syncv2_state.
	// Reset cipher_key trong sessions (per-session secret — table accounts không có cột này).
	if _, err := tx.Exec("UPDATE sessions SET cipher_key = '' WHERE account_id = $1", id); err != nil {
		return fmt.Errorf("reset cipher_key: %w", err)
	}
	return tx.Commit()
}

// DeleteAccount: alias ngược cho LogoutAccount để backward-compat với code
// hiện đang gọi (handlers.go HandleLogout). Sẽ được đổi sang LogoutAccount
// sau khi refactor handlers — tạm thời giữ để không break build.
func (s *Store) DeleteAccount(id string) error {
	return s.LogoutAccount(id)
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

// insertMessageSQL: merge logic thay vì DO NOTHING.
// - INSERT nếu (id, account_id) chưa tồn tại.
// - Nếu conflict (row đã có, vd do WS sync trước với from_name rỗng):
//   chỉ UPDATE các field phụ (from_name, attachments) khi row cũ rỗng
//   và payload mới có. KHÔNG BAO GIỜ ghi đè:
//     - conv_id, from_id, content, timestamp, msg_type (data gốc từ Zalo,
//       DeliveryAck JSON có thể wrap content — không muốn mất text thật).
// Mục tiêu: lưu nhiều data nhất có thể từ nhiều nguồn (WS realtime, WS
// sync history 510/511, REST get-last-msgs) mà KHÔNG mất/ghi đè/duplicate.
const insertMessageSQL = `INSERT INTO messages
	(id, account_id, conv_id, from_id, from_name, content, msg_type, timestamp, attachments)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (id, account_id) DO UPDATE SET
		from_name   = CASE WHEN EXCLUDED.from_name != '' AND messages.from_name = ''
		                    THEN EXCLUDED.from_name ELSE messages.from_name END,
		attachments = CASE WHEN EXCLUDED.attachments != '[]' AND messages.attachments = '[]'
		                    THEN EXCLUDED.attachments ELSE messages.attachments END`

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

// ====================================
// Contact operations (cache friends per account)
// ====================================

// upsertContactSQL: insert hoặc update name/avatar/updated_at khi user_id
// đã tồn tại cho account_id. KHÔNG xoá contact cũ — strategy này giữ data
// ngay cả khi upstream trả về thiếu (Zalo rate-limit 429 hoặc bug upstream).
const upsertContactSQL = `INSERT INTO contacts (account_id, user_id, name, avatar, updated_at)
	VALUES ($1, $2, $3, $4, NOW())
	ON CONFLICT (account_id, user_id) DO UPDATE SET
		name = EXCLUDED.name,
		avatar = EXCLUDED.avatar,
		updated_at = NOW()`

const listContactsByAccountsSQL = `SELECT account_id, user_id, name, avatar
	FROM contacts WHERE account_id = ANY($1)`

func (s *Store) UpsertContact(accountID, userID, name, avatar string) error {
	_, err := s.db.Exec(upsertContactSQL, accountID, userID, name, avatar)
	return err
}

func (s *Store) ListContactsByAccounts(accountIDs []string) ([]Contact, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(listContactsByAccountsSQL, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.AccountID, &c.UserID, &c.Name, &c.Avatar); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) CountContactsByAccounts(accountIDs []string) (map[string]int, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT account_id, COUNT(*) FROM contacts WHERE account_id = ANY($1) GROUP BY account_id`,
		accountIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var id string
		var cnt int
		if err := rows.Scan(&id, &cnt); err != nil {
			return nil, err
		}
		out[id] = cnt
	}
	return out, nil
}
