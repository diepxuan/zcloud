# Task 02: Reverse Android Sync (DEFERRED — Optional)

## Liên kết
- **Master plan:** [tasks.md](../tasks.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** Task 01-06 hoàn thành hết
- **Trạng thái:** 🟡 Deferred — không làm trong MVP

## Khi nào làm
- Task 01-06 hoàn thành + chạy ổn
- Có nhu cầu thực tế: load lịch sử tin nhắn cũ hơn scope Web API
- Có Android device thật + thời gian

## Lý do deferred
1. Web API có history qua WS cmd 510/511 — đủ cho MVP
2. Web và Android dùng chung encryption (AES-128-CBC) — khác auth flow
3. Android APK obfuscated nặng — cần jadx + Frida, tốn thời gian
4. Các thư viện tham khảo (zca-js, zcago, Za-go) đều **không** implement Android sync
   → chứng tỏ Web API là đủ

## Nếu làm: các bước

### 02.1 — Setup RE toolchain Android
- Cài jadx (cần Java 17+)
- Download Zalo APK từ APKPure
- `jadx -d zalo-jadx zalo.apk`

### 02.2 — Phân tích
- Tim class HTTP client, endpoint patterns (loadhistory, sync)
- So sánh request Android vs Web
- Xác định IMEI/device_id headers

### 02.3 — Implement trong Go
- `internal/core/android_sync.go`
- Thêm `LoadHistory(convID, cursor)` vào ZaloClient
- Cursor-based pagination merge với Web API messages

## Output (nếu làm)
- `docs/protocol/android-sync-api.md`
- `internal/core/android_sync.go` (trong src/zcloud/)

## Ghi chú
- Không cần làm cho MVP
- Nếu sau này cần, tham khảo zca-js source để biết endpoint pattern
