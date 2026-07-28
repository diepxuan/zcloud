# Task 12: Logout / đổi tài khoản

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [07-multi-user.md](07-multi-user.md), [09-database.md](09-database.md)
- **Trạng thái:** Xong

## Mục tiêu
Cho phép user logout khỏi 1 account hoặc xoá hẳn account đó.

## Hành vi
- Nút "Đổi TK" trong header → xoá session hiện tại, redirect `/`.
- Tab Quản lý → liệt kê accounts, có nút Switch (chọn lại) và Xoá.
- Xoá account = xoá `sessions` (FK cascade) + xoá `conversations` + `messages`
  + `media` + WS listener dừng.

## Files
- `internal/api/handlers.go` — `HandleLogout`.
- `internal/store/store.go` — `DeleteAccount(id)`.
- `internal/api/ws.go` — `StopZaloListener(accountID)`.
- `internal/api/web/chat.html` — JS gọi logout + UI button.

## API
- `POST /api/logout` body `{accountId: "…"}` — xoá account + session.

## Verification
- [x] Sau logout, account không còn trong `accounts`.
- [x] WS listener dừng, không còn event broadcast.
- [x] Browser redirect về `/` (login page).
- [x] Re-login cùng account tạo row mới.
