# Task 09: Database & Media Store

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [03-design-core.md](03-design-core.md)
- **Trạng thái:** Xong

## Mục tiêu
Lưu trữ persistent: SQLite (accounts, sessions, conversations, messages, media,
OA configs, OA webhook logs) + media files trên disk.

## Tech
- **SQLite driver:** `modernc.org/sqlite` (pure Go, không cần CGO).
- **WAL mode** + `busy_timeout=5000` + `foreign_keys=ON`.
- **Migrations** trong `internal/store/store.go` (chuỗi SQL embedded).

## Bảng chính
- `accounts` — tài khoản người dùng, multi-user, FK từ sessions.
- `sessions` — phiên Zalo, JSON cookies, secret_key base64, ws_urls JSON array.
- `conversations` — hội thoại, PK `(id, account_id)`, conv_type 0/1/2.
- `messages` — tin nhắn, PK `(id, account_id)`, index `(conv_id, ts DESC)`.
- `media` — file media + trường AI (ocr_text, ai_tags, ai_processed).
- `oa_configs` — cấu hình OA, schema sẵn.
- `oa_webhook_logs` — log webhook OA, schema sẵn.

## Path
- DB mặc định: `./storages/database/zcloud.db`.
- Media: `./storages/media/{accountID}/{convID}/{fileID}.{ext}`.

## Files
- `internal/store/store.go` — Store struct, migrations, queries.
- `docs/database/schema.sql` — schema reference (sync từ store.go).

## Verification
- [x] WAL mode bật (kiểm tra `journal_mode`).
- [x] Foreign key hoạt động.
- [x] Migration tự chạy khi `New()`.
- [x] Index phục vụ query `messages(account_id, conv_id, timestamp DESC)`.
