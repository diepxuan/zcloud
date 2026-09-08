# Task 16: Auto-sync media đầy đủ + cross-device/backup sync (T11)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Trạng thái:** 🟢 Deferred — làm SAU khi T1+T2+T3 (xem §5.3) hoàn thành ổn định.
- **Phụ thuộc:** [10-sync-history.md](10-sync-history.md), [13-media-download.md](13-media-download.md), [15-reverse-zalo-pc.md](15-reverse-zalo-pc.md).

## Mục tiêu
Mở rộng đồng bộ dữ liệu lên mức desktop-class:

1. **Auto-download media đầy đủ khi sync** — không chỉ image, mà gồm video/file/voice/sticker/GIF. Dedupe + retry + TTL handling.
2. **PC cross-device sync** — khai thác AES-GCM layout + cmd 590–592/630–634 từ bundle Zalo PC 26.8.10 (xem `docs/protocol/pc-desktop.md`).
3. **Backup / restore** — đồng bộ ZCloud media, family album, trusted-device WASM key exchange.
4. **Integration test end-to-end** — gửi từ Zalo client khác → zcloud nhận + lưu DB + tải media + broadcast browser.

## Phạm vi kỹ thuật (dự kiến)
- `internal/core/websocket.go`: thêm handler cho cmd 590–592/630–634, dùng `cipherKey` (AES-GCM) đầy đủ.
- `internal/api/ws.go`: media worker queue với retry/backoff + rate-limit.
- `internal/store/`: bảng `media_jobs` (pending/running/done/failed) + dead-letter.
- `internal/api/router.go`: API trigger force-redownload + status job.
- Tests: giả lập WS payload AES-GCM (T1 đã làm), giả lập media download (HTTP fake server).

## Lý do deferred
T1+T2+T3 cần chạy ổn định trước để có baseline. Nếu làm song song sẽ khó tách lỗi (sync message vs sync media vs PC protocol).

## Verification (khi làm)
- [ ] Sync 100 tin media → 100 file trong `/storages/media/{account}/{conv}/`.
- [ ] Resume sau khi kill giữa chừng: job pending chạy lại, không tải trùng.
- [ ] AES-GCM WS payload giải mã đúng với cipherKey từ session.
