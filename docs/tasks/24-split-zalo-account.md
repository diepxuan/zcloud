# Task 24: Tách bảng `zalo_accounts` ra khỏi `accounts` (3-tier identity model)

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §3 (mới)
- **Trạng thái:** 🟡 Đề xuất — chờ Sếp duyệt
- **Ngày tạo:** 13/09/2026
- **Phụ thuộc:**
  - [12-logout.md](12-logout.md) — luồng logout hiện tại (xoá cả account)
  - [09-database.md](09-database.md) — schema Postgres hiện tại

## Bối cảnh

Hiện tại bảng `accounts` (zcloud) pha trộn 2 loại thông tin:

1. **Identity Zalo**: `user_id` (UID), `display_name`, `avatar` — thông tin Sếp **là ai** trên Zalo
2. **State zcloud**: `enabled`, `transport`, `syncv2_state`, `disabled_reason` — **zcloud quản lý** account đó thế nào

Vấn đề thật của code hiện tại (sau refactor lần trước) — `internal/store/queries.go:310` `Store.DeleteAccount(id)` đang **nửa nạc nửa mỡ**:

- **Đã đúng (1 nửa):** giữ row `accounts` sau logout (đã có `SetAccountUserID` + `UpdateAccount` từ 02/09/2026 → identity Zalo không mất khi logout).
- **Chưa đúng (1 nửa):** vẫn `DELETE FROM messages WHERE account_id = $1` + `DELETE FROM conversations WHERE account_id = $1` ở `queries.go:317-325` → mất toàn bộ data zcloud đã thu thập được khi logout. Đây là gap cần fix.

Mục tiêu task24:

1. Tách `zalo_accounts` (identity, **không xoá khi logout**) ↔ `accounts` (state zcloud, **giữ row** nhưng `transport=''` khi logout) ↔ `sessions` (credentials, xoá khi logout).
2. Phân biệt rõ 2 hành vi:
   - **Logout** → xoá sessions + reset transport + reset syncv2_state theo điều kiện. **GIỮ** messages/conversations/media (data đã thu thập thuộc về zcloud, không mất khi user chỉ đổi session).
   - **Xoá account** (nút Xoá trong UI) → cascade sạch toàn bộ footprint của user Zalo đó.
3. Tầng `accounts` không còn pha trộn 2 loại thông tin — phù hợp với T13 (multi-account filter) và T18.3 (TUI composer chọn account).

**Phạm vi đợt này:** chỉ áp dụng cho `account_type=1` (Zalo User). OA (`account_type=2`) giữ nguyên schema vì task 08 tạm hoãn, schema OA chưa chạm đến.

### Hành vi logout hiện tại (cần sửa)

- `HandleLogout` (`internal/api/handlers.go:819`) → `StopZaloListener(req.AccountID)` + `Store.DeleteAccount(req.AccountID)`.
- `Store.DeleteAccount` hiện xoá: `messages` + `conversations` + `sessions`, reset `transport=''` + `cipher_key=''`. **KHÔNG xoá row `accounts`** (đã soft-delete từ refactor trước).
- Nhưng vẫn xoá `messages`/`conversations` → mất data đã sync, dù row `accounts` còn. Đây là inconsistency cần fix trong task24.

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

### Logout flow — TÁCH RÕ 2 HÀNH VI (sửa so với task24 gốc)

```go
// === HÀNH VI 1: LogoutAccount (mới, thay thế DeleteAccount cũ) ===
// Trigger: Sếp bấm "Đăng xuất" trong UI, hoặc session auth_expired.
// GIỮ identity (zalo_accounts) + zcloud state (accounts row) + data đã sync.
// Chỉ xoá credentials + reset state.
func LogoutAccount(accountID string) error {
    // 1. Xoá sessions (cookies + secret_key + cipher_key)
    DELETE FROM sessions WHERE account_id = $1;
    // 2. Reset transport — nếu đang pc, giữ 'pc' để login lại dùng cùng transport;
    //    nếu đang web, giữ 'web'. CHỈ clear khi Sếp chủ động yêu cầu đổi transport.
    //    Mặc định: KHÔNG clear transport ở logout thường; chỉ clear khi cần force refresh.
    // 3. Reset syncv2_state theo điều kiện (xem câu hỏi #6 mới).
    UPDATE accounts
       SET cipher_key = '',
           syncv2_state = CASE WHEN transport_changed THEN '{}' ELSE syncv2_state END
     WHERE id = $1;
    // KHÔNG xoá messages, conversations, media, contacts.
}

// === HÀNH VI 2: DeleteZaloAccount (mới, UI nút "Xoá") ===
// Trigger: Sếp bấm "Xoá" trong UI — muốn xoá hẳn user Zalo khỏi zcloud.
// Cascade: zalo_accounts → accounts → sessions → messages → conversations → media → contacts.
func DeleteZaloAccount(zaID string) error {
    DELETE FROM zalo_accounts WHERE id = $1;
    // FK CASCADE tự xoá phần còn lại.
}
```

**Quan trọng:** `Store.DeleteAccount` cũ (queries.go:310) sẽ được **đổi tên** thành `LogoutAccount` và đổi behavior: **bỏ `DELETE FROM messages` + `DELETE FROM conversations`** — chỉ xoá sessions + reset state. Caller (`HandleLogout`) sẽ gọi `LogoutAccount`. Nút "Xoá" UI mới gọi `DeleteZaloAccount` qua handler `HandleDeleteAccount`.

### UI — Panel Quản lý sau logout

- Vẫn hiển thị: tên (từ `zalo_accounts.display_name`) + avatar (từ `zalo_accounts.avatar`)
- Status dot: **off** (đỏ) — không có session active
- Nút **Restart**: ẩn (không có session để restart)
- Nút **Re-login** (đã có cho `auth_expired`): hiển thị để Sếp login lại
- Nút **SyncV2**: disabled nếu `accounts.transport=''`
- Checkbox **Hiển thị**: vẫn hoạt động (độc lập với session)
- Data đã sync (conversations/messages): vẫn hiển thị — chỉ WS realtime không nhận khi không có session

## Code change scope

| File | Thay đổi |
|------|----------|
| `internal/store/store_postgres.go` | + `migrationZaloAccountsPG`, + `ensureZaloAccountsTablePG`, + `ensureAccountZaloAccountIDPG`, + `backfillZaloAccountsPG` |
| `internal/store/queries.go` | + `ZaloAccount` struct, + `UpsertZaloAccount`, + `FindAccountByZaloAccountID`, + `FindOrCreateAccountByZaloAccountID`, + `GetZaloAccount`, + `ListZaloAccounts`, + `DeleteZaloAccount`, + `LogoutAccount` (mới, thay thế DeleteAccount cũ) |
| `internal/store/queries.go` | `Account` struct thêm `ZaloAccountID`; `ListAccounts` JOIN `zalo_accounts` để lấy display_name/avatar mới nhất; `GetAccount` tương tự |
| `internal/store/queries.go` | `DeleteAccount` (cũ) → đổi tên `LogoutAccount` + bỏ `DELETE FROM messages/conversations` (chỉ xoá sessions + reset state) |
| `internal/store/store.go` | Update interface `Store` để expose `LogoutAccount` + `DeleteZaloAccount` + các method mới |
| `internal/api/handlers.go` | `HandleCookieLogin`: gọi `UpsertZaloAccount` + `FindOrCreateAccountByZaloAccountID` thay vì `CreateAccount` cứng |
| `internal/api/handlers.go` | `HandlePCLogin`: tương tự HandleCookieLogin |
| `internal/api/handlers.go` | `HandleLogout`: gọi `LogoutAccount` thay vì `DeleteAccount` (đổi tên) |
| `internal/api/handlers.go` | `HandleDeleteAccount` (mới, UI nút Xoá): gọi `DeleteZaloAccount` |
| `internal/api/handlers.go` | `HandleAccountList`: thêm field `hasActiveSession` per account (đã có sẵn) + field `transport` per account |
| `internal/api/web/chat.html` | `renderAccounts`: hiển thị status dot `off` + badge "Đã logout" nếu `!a.hasActiveSession`; ẩn nút Restart trong trường hợp này; thêm nút "Xoá" gọi `HandleDeleteAccount` |
| `internal/api/ws.go` | Verify `enqueueMessageMediaJobs` key theo `a.ID` ổn định (vì `a.ID` giờ deterministic = `acc_<user_id>`, không đổi qua các lần login) |
| `internal/store/queries_test.go` | Test mới: `TestUpsertZaloAccount`, `TestFindOrCreateAccountByZaloAccountID`, `TestLogoutAccount_PreservesMessages`, `TestDeleteZaloAccount_Cascade`, `TestLoginFlow_ReusesAccount` |

### Phạm vi phát hiện vấn đề cần check trước khi code

Em sẽ grep + verify trước khi triển khai:

1. **Tất cả callsite của `Store.DeleteAccount`** — chỉ có 1 ở handlers.go:832. Refactor an toàn.
2. **FK từ `messages`/`conversations` về `accounts`** — nếu không có ON DELETE CASCADE, `DeleteZaloAccount` sẽ fail. Cần ensure migration thêm cascade hoặc dùng `CASCADE` trong `DELETE FROM zalo_accounts`.
3. **`enqueueMessageMediaJobs` trong `internal/api/ws.go`** — key theo `a.ID`. Vì `a.ID` giờ deterministic, không cần đổi.
4. **`syncv2_admin.go` / `syncv2_handler.go`** — kiểm tra có gọi `DeleteAccount` không (theo grep, không có).

## Câu hỏi Sếp cần quyết trước khi code

1. **Account ID cũ giữ nguyên hay mới khi login lại?** Em đề xuất: **`"acc_" + zalo_user_id`** deterministic. Sep login lần 1 → `acc_2291602426651082808`. Logout. Login lần 2 cùng UID → reuse `acc_2291602426651082808`. Ưu: continuity, contacts/conversations cũ vẫn map đúng. Nhược: nếu Sếp test add nhiều lần cùng UID thì chỉ có 1 account (cũ) — không có "test account" riêng. **Em đề xuất chấp nhận nhược này**, nếu cần test multi thì Sếp tạo account Zalo khác.

2. **`syncv2_state` reset khi logout?** Em đề xuất: **reset về `{}` chỉ khi `transport` đổi** (vd web → pc). Lý do:
   - ed25519 keypair chỉ valid cho session Zalo, nhưng không bị ràng buộc cứng với `transport` (cùng transport login lại → keypair vẫn valid).
   - Reset hết mỗi logout → request-sync lại từ đầu, lãng phí Phase B.
   - Reset chỉ khi transport đổi → đúng flow "đổi transport = đổi keypair".

3. **Account reuse khi login:** nếu cùng UID mà có nhiều `accounts` rows (vd test cũ) → reuse **row cũ nhất** (theo `created_at ASC LIMIT 1`) hay **enabled=true đầu tiên**? Em đề xuất: **`enabled=true` đầu tiên** (sort created_at ASC). Nếu Sếp xoá hết thì tạo mới.

4. **UI filter:** Panel Quản lý hiển thị cả account đã logout (status off) hay ẩn? Em đề xuất: **hiển thị** (đúng yêu cầu Sếp), để Sếp thấy "đã logout Trần Ngọc Đức" và bấm login lại.

5. **Nút "Xoá" trong UI:** giữ hay bỏ? Em đề xuất: **giữ** — `DeleteZaloAccount` cascade xoá sạch. Sếp dùng khi muốn xoá hẳn account Zalo khỏi zcloud (vd khách hàng rời đi).

6. **`syncv2_state` có giữ khi cùng transport không?** (mới, từ review lần này) Em đề xuất: **giữ nguyên nếu `transport` không đổi**. Phase B keypair ed25519 không bị Zalo server-side ràng buộc với session cụ thể, chỉ cần `transport=pc`. Reset chỉ khi Sếp chủ động đổi transport. Cost nếu sai: phải request-sync lại, mất 5-10 phút.

7. **`transport` có reset khi logout?** (mới, liên quan #6) Em đề xuất: **KHÔNG reset `transport` khi logout thường**. Chỉ reset khi:
   - Sếp chọn "Đổi transport" trong UI (modal riêng).
   - Login lại fail do transport mismatch (vd login web nhưng transport=pc cũ).
   - Sếp bấm nút "Reset" trong panel Quản lý.

   Mặc định logout chỉ xoá sessions + cipher_key, **giữ transport + syncv2_state** để login lại dùng đúng config cũ.

## Lệnh test thủ công

```bash
# 1. Login Sep (cookie hoặc pc)
curl -X POST http://127.0.0.1:8080/api/login/cookie \
  -H 'Content-Type: application/json' \
  -d '{"zpsid":"...","zpw_sek":"..."}'

# 2. Verify zalo_accounts + accounts mapping
PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT za.id AS za, za.display_name, a.id AS acc, a.transport,
    (SELECT COUNT(*) FROM sessions WHERE account_id=a.id) AS sessions,
    (SELECT COUNT(*) FROM messages WHERE account_id=a.id) AS messages
  FROM accounts a JOIN zalo_accounts za ON za.id = a.zalo_account_id;
"

# 3. Logout — verify messages/conversations CÒN (không bị xoá như code cũ)
curl -X POST http://127.0.0.1:8080/api/logout \
  -H 'Content-Type: application/json' \
  -d '{"accountId":"acc_2291602426651082808"}'

PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT za.id, za.display_name, a.id, a.transport,
    (SELECT COUNT(*) FROM sessions WHERE account_id=a.id) AS sessions,
    (SELECT COUNT(*) FROM messages WHERE account_id=a.id) AS messages
  FROM accounts a JOIN zalo_accounts za ON za.id = a.zalo_account_id
  WHERE za.zalo_user_id='2291602426651082808';
"
-- Expect: sessions=0, messages>0 (giữ data), accounts row còn, zalo_accounts row còn

# 4. Login lại — verify cùng acc_id được reuse
curl -X POST http://127.0.0.1:8080/api/login/cookie \
  -H 'Content-Type: application/json' \
  -d '{"zpsid":"...","zpw_sek":"..."}'

PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT za.id, za.display_name, a.id, a.transport,
    (SELECT COUNT(*) FROM sessions WHERE account_id=a.id) AS sessions
  FROM accounts a JOIN zalo_accounts za ON za.id = a.zalo_account_id
  WHERE za.zalo_user_id='2291602426651082808';
"
-- Expect: a.id KHÔNG đổi (= acc_2291602426651082808), sessions=1

# 5. UI: mở http://zcloud.diepxuan.corp:8080 → /chat → panel Quản lý
#    → Sep hiển thị với dot off + badge "Đã logout" + nút Re-login + data cũ vẫn xem được

# 6. Test nút Xoá
curl -X POST http://127.0.0.1:8080/api/account/delete \
  -H 'Content-Type: application/json' \
  -d '{"accountId":"acc_2291602426651082808"}'

PGPASSWORD='Ductn@7691' psql -h postgresql.diepxuan.corp -U zcloud -d zcloud -c "
  SELECT COUNT(*) FROM zalo_accounts WHERE zalo_user_id='2291602426651082808';
  SELECT COUNT(*) FROM accounts WHERE user_id='2291602426651082808';
  SELECT COUNT(*) FROM messages WHERE account_id='acc_2291602426651082808';
"
-- Expect: cả 3 = 0 (cascade sạch)
```

## Review notes (13/09/2026)

Review từ session này:

- **Mô tả hiện trạng đã sửa**: `Store.DeleteAccount` không xoá row `accounts` (đã soft-delete từ refactor trước), nhưng vẫn xoá `messages`/`conversations` — gap thật là inconsistency này, không phải "mất hoàn toàn identity".
- **Thiết kế đã điều chỉnh**: Tách rõ `LogoutAccount` vs `DeleteZaloAccount` ở cả schema behavior và code scope.
- **Câu hỏi #6 #7 mới**: về `syncv2_state` theo transport và `transport` không reset khi logout thường.
- **Phạm vi**: chỉ `account_type=1` đợt này; OA giữ nguyên.

Sếp duyệt task24 với 3 điều chỉnh trên + trả lời 7 câu hỏi. Sau đó em triển khai theo thứ tự:

1. Migration `zalo_accounts` + backfill + FK nullable → FK NOT NULL (2 step an toàn).
2. `UpsertZaloAccount` + `FindOrCreateAccountByZaloAccountID` + `LogoutAccount` (refactor từ DeleteAccount) + `DeleteZaloAccount` + 5 unit test.
3. Refactor `HandleCookieLogin` / `HandlePCLogin` theo 4-step + `HandleLogout` gọi `LogoutAccount` + `HandleDeleteAccount` mới.
4. UI `renderAccounts` nhánh logged-out + nút Xoá.
5. Build + smoke live với Trần Ngọc Đức (conv `4866700441106275565`) + grep callsite `DeleteAccount` cũ để cleanup.
