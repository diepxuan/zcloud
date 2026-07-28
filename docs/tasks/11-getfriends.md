# Task 11: GetFriends + tab Liên hệ

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [04-build-core.md](04-build-core.md), [06-build-webui.md](06-build-webui.md)
- **Trạng thái:** Xong

## Mục tiêu
Tab "Liên hệ" trên UI hiển thị danh sách bạn bè từ Zalo API.

## Zalo API
- Endpoint: `GET /api/friend/getfriends?zpw_ver=…&zpw_type=…`.
- Response: JSON có `data` (array friend) hoặc flat array.
- Parse có fallback cho cả 2 dạng.

## Files
- `internal/core/chat.go` — `GetFriends(ctx)` trả về `[]User`.
- `internal/api/handlers.go` — `HandleFriends`.
- `internal/api/web/chat.html` — tab Liên hệ, JS load + render.

## API
- `GET /api/friends?accountId=…`

## Verification
- [x] Trả về đúng số lượng friend.
- [x] Tên + avatar hiển thị đúng.
- [x] Search hoạt động (bỏ dấu tiếng Việt).
- [x] Check HTML response tránh crash khi Zalo trả về HTML.

## Ghi chú
- Nếu response là HTML (rate limit / block) → return error thay vì parse.
