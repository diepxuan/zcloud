# Task 13: Media download

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [09-database.md](09-database.md)
- **Trạng thái:** Xong

## Mục tiêu
Tải media từ URL Zalo về local disk, lưu metadata vào DB, serve qua
`GET /media/...`.

## Files
- `internal/api/router.go` — `HandleMediaDownload`, `HandleMedia`.
- `internal/store/store.go` — `SaveMedia`, `GetMedia`.

## API
- `POST /api/media/download` body `{accountId, convId, msgId, url, fileName}`
  → tải file về `storages/media/{accountID}/{convID}/{fileID}.{ext}`,
  insert row vào `media`, trả về `mediaId` + local URL.
- `GET /media/{accountID}/{convID}/{fileName}` — serve file tĩnh.

## Verification
- [x] File lưu đúng thư mục, đúng extension.
- [x] Metadata (mime_type, file_size) lưu DB.
- [x] Serve qua `/media/` với Content-Type đúng.

## Ghi chú
- Auto-detect extension từ URL hoặc Content-Type.
- Hiện chưa auto-download khi nhận WS event media (xem tasks.md §5 Tồn đọng).
