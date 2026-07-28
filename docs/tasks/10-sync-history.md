# Task 10: Sync lịch sử tin nhắn cũ

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [04-build-core.md](04-build-core.md), [09-database.md](09-database.md)
- **Trạng thái:** Xong

## Mục tiêu
Khi mở conversation, fetch tin nhắn cũ từ Zalo về DB. Hai cơ chế:
- **WS cmd 510/511** — request + nhận batch từ listener nền.
- **REST fallback** — `GET /api/cm/getrecentv2` (group) và
  `GET /api/preloadconvers/get-last-msgs` (individual).

## Cơ chế
1. User chọn conversation trên UI.
2. UI gọi `POST /api/messages/sync?convId=…&convType=…`.
3. Server gọi `RequestOldMessagesViaListener` → gửi WS cmd 510/511.
4. Nếu WS không có data trong 3s → fallback REST.
5. Tin nhắn lưu vào DB qua `SaveMessage` (dedupe theo `(id, account_id)`).
6. UI gọi lại `GET /api/messages` để hiển thị.

## Files
- `internal/core/websocket.go` — `RequestOldMessages`, `handleOldMessages`.
- `internal/core/chat.go` — `GetGroupHistory` (REST fallback).
- `internal/api/handlers.go` — `HandleSyncMessages`.
- `internal/api/ws.go` — `RequestOldMessagesViaListener`.

## API
- `POST /api/messages/sync?accountId=…&convId=…&convType=…`

## Verification
- [x] WS cmd 510 gửi/nhận được batch tin nhắn cá nhân.
- [x] WS cmd 511 gửi/nhận được batch tin nhắn nhóm.
- [x] REST fallback hoạt động khi WS không phản hồi.
- [x] UI tự gọi sync khi chọn conversation.

## Ghi chú
- `lastId` mặc định `10000000000000000` (epoch lớn) cho lần đầu sync.
- Payload WS có thể AES-GCM + gzip — dùng `DecodeWSEvent` trong encrypt.go.
