# Task 07: Multi-user Manager

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [04-build-core.md](04-build-core.md), [05-build-server.md](05-build-server.md), [09-database.md](09-database.md)
- **Trạng thái:** Xong

## Mục tiêu
Một daemon `zcloudd` quản lý nhiều tài khoản Zalo song song. Mỗi browser tab
gắn với một `accountId`, server dùng `Client` riêng (kèm `WSClient` listener
nền) cho mỗi user.

## Thiết kế
- Bảng `accounts` — mỗi user Zalo = 1 row (`acc_<userId>`).
- Bảng `sessions` — FK `account_id`, lưu cookies + secret_key + ws_urls.
- `Server.clients map[string]*core.Client` — runtime map `accountId → Client`.
- `StartZaloListener(accountID)` chạy WS listener nền cho mỗi account.
- WS event → lưu DB → broadcast tới browser WS (`/ws`).

## Files
- `internal/store/store.go` — `CreateAccount`, `SaveSession`, `GetAccount`,
  `ListAccounts(type)`, `DeleteAccount`.
- `internal/api/handlers.go` — `HandleAccountList`, `HandleLogout`,
  `HandlePollQR` (tạo account sau QR thành công).
- `internal/api/ws.go` — `WSManager` per-account, `runZaloListener`.

## API endpoints
- `GET /api/account?accountId=…` — thông tin 1 account.
- `GET /api/account/list` — danh sách accounts (type=1 = Zalo user).
- `POST /api/logout` — xoá account + sessions + DB rows.

## Verification
- [x] 2 tài khoản cùng đăng nhập, mỗi tab chat độc lập.
- [x] Logout xoá row trong `accounts` + `sessions`.
- [x] Refresh session định kỳ (mỗi 30 phút) tránh lỗi 600.

## Ghi chú
- Mỗi account có thể có nhiều session (re-login), chỉ giữ session active.
- WS listener chạy goroutine riêng, tự reconnect khi mất kết nối.
