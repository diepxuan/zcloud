# Task 24: Tách bảng `zalo_accounts` ra khỏi `accounts` (3-tier identity model)

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §6 (mới)
- **Trạng thái:** 🟡 Đề xuất — chờ Sếp duyệt
- **Ngày tạo:** 13/09/2026
- **Phụ thuộc:**
  - [12-logout.md](12-logout.md) — luồng logout hiện tại (xoá cả account)
  - [09-database.md](09-database.md) — schema Postgres hiện tại

## Bối cảnh

Hiện tại bảng `accounts` (zcloud) pha trộn 2 loại thông tin:

1. **Identity Zalo**: `user_id` (UID), `display_name`, `avatar` — thông tin Sếp **là ai** trên Zalo
2. **State zcloud**: `enabled`, `transport`, `syncv2_state`, `disabled_reason` — **zcloud quản lý** account đó thế nào

Khi logout, cần xoá session (cookies) nhưng muốn **giữ** identity Zalo để lần login kế tiếp:
- Hiển thị lại trong panel Quản lý (tên + avatar)
- Không phải nhập lại thông tin
- SyncV2 state nếu transport=pc được bảo toàn

Hiện tại `HandleLogout` gọi `Store.DeleteAccount(accountID)` xoá luôn row `accounts` → mất hoàn toàn → login lại tạo acc_id mới (vd `acc_NEW`), mất continuity với `contacts`/`conversations` cũ.

## Đề xuất: 3-tier identity model

```
┌─────────────────────────────────────────────────────────┐
│  zalo_accounts (immutable identity — KHÔNG xoá logout) │
│  ─────────────────────────────────────────────────────  │
│  id              TEXT PK ('za_<user_id>')               │
│  zalo_user_id    TEXT UNIQUE                             │
│  display_name    TEXT                                    │
│  avatar          TEXT                                    │
│  phone           TEXT                                    │
│  created_at/updated_at                                    │
└────────────────────────┬────────────────────────────────┘
                         │ 1 zalo_account : N accounts
                         │ FK ON DELETE CASCADE
┌────────────────────────▼────────────────────────────────┐
│  accounts (zcloud-side state — xoá được qua UI)         │
│  ─────────────────────────────────────────────────────  │
│  id              TEXT PK ('acc_<user_id>' hoặc custom)  │
│  zalo_account_id TEXT FK → zalo_accounts.id (CASCADE)   │
│  display_name/avatar (cache, có thể override local)     │
│  enabled, transport, syncv2_state, status, note, ...    │
└────────────────────────┬────────────────────────────────┘
                         │ FK ON DELETE CASCADE
┌────────────────────────▼────────────────────────────────┐
│  sessions (credentials — xoá khi logout)                │
│  ─────────────────────────────────────────────────────  │
│  id, account_id, cookies, secret_key, cipher_key, ...   │
│  user_id, imei, user_agent, ws_urls, service_map, ...   │
│  transport, api_type/version, is_active, expires_at     │
└─────────────────────────────────────────────────────────┘
```

### Tại sao CASCADE chứ không phải RESTRICT?

- **RESTRICT** = không xoá `zalo_accounts` khi còn `accounts` tham chiếu. Vì 1 zalo_account luôn có ≥1 accounts reference → **không bao giờ xoá được account từ UI**. Khó chịu.
- **CASCADE** = xoá `zalo_accounts` → tự động xoá tất cả `accounts` tham chiếu → cascade xoá sessions, messages, conversations, media, contacts. Sếp bấm "Xoá" UI → sạch hoàn toàn.
- **Logout** không động vào `zalo_accounts` (FK không trigger) → giữ nguyên identity.

## Schema

```sql
-- Bảng MỚI: thông tin Zalo user, KHÔNG bao giờ xoá khi logout.
CREATE TABLE zalo_accounts (
    id              TEXT PRIMARY KEY,           -- 'za_<user_id>'
    zalo_user_id    TEXT NOT NULL UNIQUE,       -- UID thuần (vd 2291602426651082808)
    display_name    TEXT NOT NULL DEFAULT '',
    avatar          TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Migration: thêm FK zalo_account_id vào accounts.
-- Step 1: nullable (backfill an toàn).
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS zalo_account_id TEXT DEFAULT '';
-- Step 2: FK constraint (CASCADE — xoá zalo_acc kéo theo accounts).
ALTER TABLE accounts ADD CONSTRAINT accounts_zalo_account_id_fkey
    FOREIGN KEY (zalo_account_id) REFERENCES zalo_accounts(id)
    ON DELETE CASCADE;
```

### Migration an toàn

Vì hiện tại `accounts.user_id` đã có (sau commit 02/09/2026), migration backfill:

```sql
-- Với mỗi account có user_id, tạo zalo_account tương ứng.
INSERT INTO zalo_accounts (id, zalo_user_id, display_name, avatar)
SELECT 'za_' || user_id, user_id, display_name, avatar
FROM accounts
WHERE user_id <> ''
ON CONFLICT (id) DO NOTHING;

UPDATE accounts SET zalo_account_id = 'za_' || user_id
WHERE user_id <> '' AND (zalo_account_id = '' OR zalo_account_id IS NULL);
```

## Hành vi

### Login flow (HandleCookieLogin / HandlePCLogin / HandleCreateAccountFromProfile)

```go
// 1. Zalo API login → session.UserID + session.Cookies + session.SecretKey + ...
// 2. UpsertZaloAccount: tìm zalo_user_id, có → UPDATE name/avatar (cache Sếp đổi tên), chưa → INSERT.
zaID := "za_" + session.UserID
store.UpsertZaloAccount(zaID, session.UserID, displayName, avatar, phone)
// 3. FindAccountByZaloAccountID: tìm accounts.zalo_account_id, có → reuse acc_id, chưa → tạo mới.
accID := store.FindOrCreateAccountByZaloAccountID(zaID)
// 4. SaveSession.
store.SaveSession(accID, session, transport, cipherKey)
```

### Logout flow

```go
// KHÔNG xoá zalo_accounts, KHÔNG xoá accounts. Chỉ:
// 1. DELETE FROM sessions WHERE account_id = X (cookies + secret_key + cipher_key)
// 2. UPDATE accounts SET transport = '' WHERE id = X
// 3. UPDATE accounts SET syncv2_state = '{}' WHERE id = X (reset SyncV2 — nếu transport đổi từ pc→web, keypair cũ vô nghĩa)
```

### UI — Panel Quản lý sau logout

- Vẫn hiển thị: tên (từ `zalo_accounts.display_name`) + avatar (từ `zalo_accounts.avatar`)
- Status dot: **off** (đỏ) — không có session active- Nút **Restart**: ẩn (không có session để restart)
- Nút **Re-login** (đã có cho `auth_expired`): hiển thị để Sếp login lại
- Nút **SyncV2**: disabled nếu `accounts.transport=''`
- Checkbox **Hiển thị**: vẫn hoạt động (độc lập với session)

### Xoá account (UI nút "Xoá")

```go
// Xoá zalo_account → cascade xoá accounts → cascade xoá sessions, messages, ...
store.DeleteZaloAccount(zaID)
// Hiệu ứng: 1 query xoá sạch toàn bộ footprint của user Zalo đó khỏi zcloud.
```

## Code change scope

| File | Thay đổi |
|------|----------|
| `internal/store/store_postgres.go` | + `migrationZaloAccountsPG`, + `ensureZaloAccountsTablePG`, + `ensureAccountZaloAccountIDPG`, + `backfillZaloAccountsPG` |
| `internal/store/queries.go` | + `ZaloAccount` struct, + `UpsertZaloAccount`, + `FindAccountByZaloAccountID`, + `FindOrCreateAccountByZaloAccountID`, + `GetZaloAccount`, + `ListZaloAccounts`, + `DeleteZaloAccount`, refactor `Account` struct thêm `ZaloAccountID` |
| `internal/store/queries.go` | `ListAccounts` JOIN `zalo_accounts` để lấy display_name/avatar mới nhất; `GetAccount` tương tự |
| `internal/store/queries.go` | `DeleteAccount` đổi: xoá `zalo_accounts` (cascade xoá accounts + sessions + ...); giữ method `LogoutAccount` mới chỉ xoá sessions + reset transport |
| `internal/api/handlers.go` | `HandleCookieLogin`: gọi `UpsertZaloAccount` + `FindOrCreateAccountByZaloAccountID` thay vì `CreateAccount` cứng |
| `internal/api/handlers.go` | `HandlePCLogin`: tương tự HandleCookieLogin |
| `internal/api/handlers.go` | `HandleLogout`: chỉ xoá sessions + reset transport; gọi `LogoutAccount` thay vì `DeleteAccount` |
| `internal/api/handlers.go` | `HandleDeleteAccount` (mới, nếu UI cần nút Xoá): gọi `DeleteZaloAccount` |
| `internal/api/web/chat.html` | `renderAccounts`: hiển thị status dot `off` + badge "Đã logout" nếu `!a.hasActiveSession`; ẩn nút Restart trong trường hợp này |
| `internal/api/handlers.go` | `HandleAccountList`: thêm field `hasActiveSession` per account để UI render đúng |

## Câu hỏi Sếp cần quyết trước khi code

1. **Account ID cũ giữ nguyên hay mới khi login lại?** Em đề xuất: **`"acc_" + zalo_user_id`** deterministic. Sep login lần 1 → `acc_2291602426651082808`. Logout. Login lần 2 cùng UID → reuse `acc_2291602426651082808`. Ưu: continuity, contacts/conversations cũ vẫn map đúng. Nhược: nếu Sếp test add nhiều lần cùng UID thì chỉ có 1 account (cũ) — không có "test account" riêng. **Em đề xuất chấp nhận nhược này**, nếu cần test multi thì Sếp tạo account Zalo khác.

2. **`syncv2_state` reset khi logout?** Em đề xuất: **reset về `{}`** vì:
   - ed25519 keypair chỉ valid cho 1 session Zalo. Login lại tạo session mới → keypair cũ invalid (server-side check)
   - Transport đổi (web → pc) thì càng cần keypair mới
   - Cost: Phase B lần đầu tiên sau logout sẽ request-sync lại từ đầu

3. **Account reuse khi login:** nếu cùng UID mà có nhiều `accounts` rows (vd test cũ) → reuse **row cũ nhất** (theo `created_at ASC LIMIT 1`) hay **enabled=true đầu tiên**? Em đề xuất: **`enabled=true` đầu tiên** (sort created_at ASC). Nếu Sếp xoá hết thì tạo mới.

4. **UI filter:** Panel Quản lý hiển thị cả account đã logout (status off) hay ẩn? Em đề xuất: **hiển thị** (đúng yêu cầu Sếp), để Sếp thấy "đã logout Trần Ngọc Đức" và bấm login lại.

5. **Nút "Xoá" trong UI:** giữ hay bỏ? Em đề xuất: **giữ** — `DeleteZaloAccount` cascade xoá sạch. Sếp dùng khi muốn xoá hẳn account Zalo khỏi zcloud (vd khách hàng rời đi).

## Lệnh test thủ công

```bash
# 1. Login Sep (cookie hoặc pc)
curl -X POST http://127.0.0.1:8080/api/login/pc \
  -H 'Content-Type: application/json' \
  -d '{"zpsid":"...","zpw_sek":"...","cipherKey":"..."}'

# 2. Verify zalo_accounts + accounts mapping
PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT za.id AS za, za.display_name, a.id AS acc, a.transport,
    (SELECT COUNT(*) FROM sessions WHERE account_id=a.id) AS sessions
  FROM accounts a JOIN zalo_accounts za ON za.id = a.zalo_account_id;
"

# 3. Logout
curl -X POST http://127.0.0.1:8080/api/logout \
  -H 'Content-Type: application/json' \
  -d '{"accountId":"acc_2291602426651082808"}'

# 4. Verify Sep vẫn hiển thị trong panel (zalo_acc + acc row còn, sessions=0)
PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT za.id, za.display_name, a.id, a.transport,
    (SELECT COUNT(*) FROM sessions WHERE account_id=a.id) AS sessions
  FROM accounts a JOIN zalo_accounts za ON za.id = a.zalo_account_id
  WHERE za.zalo_user_id='2291602426651082808';
"

# 5. UI: mở http://zcloud.diepxuan.corp:8080 → /chat → panel Quản lý
#    → Sep hiển thị với dot off + badge "Đã logout" + nút Re-login
```
