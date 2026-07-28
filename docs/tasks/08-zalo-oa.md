# Task 08: Zalo OA Integration

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [09-database.md](09-database.md)
- **Trạng thái:** TẠM HOÃN — schema sẵn, chưa implement logic

## Mục tiêu (khi triển khai)
Webhook receiver cho Zalo Official Account — nhận event từ Zalo OA, lưu log,
gửi trả lời qua OA API.

## Schema (đã có)
- `oa_configs` — access_token, refresh_token, secret_key xác thực webhook.
- `oa_webhook_logs` — lưu raw payload từ Zalo + trạng thái xử lý.

## Chưa làm
- Handler `POST /api/oa/webhook` — xác thực HMAC với `secret_key`, parse event.
- Worker xử lý event pending (`oa_webhook_logs.processed = 0`).
- API gửi tin nhắn OA (`https://openapi.zalo.me/v2.0/oa/message`).

## Lý do hoãn
- Hiện tại Sếp chỉ dùng Zalo User, chưa có OA đăng ký.
- Schema DB đã sẵn — khi cần chỉ thêm handler, không sửa schema.

## Verification (khi triển khai)
- [ ] Nhận webhook `follow`/`unfollow`/`message` từ Zalo.
- [ ] Log payload vào `oa_webhook_logs`.
- [ ] Gửi tin nhắn OA thành công qua API.
