# Task 17: Đồng bộ dữ liệu (T1+T2+T3)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.3
- **Trạng thái:** 🟡 Đang làm (đợt 09/2026)
- **Phụ thuộc:** [10-sync-history.md](10-sync-history.md), [13-media-download.md](13-media-download.md)

## Mục tiêu
Sếp yêu cầu làm 3 phần trước, ghi T11 làm sau:

### T1 — Verify + integration test sync WS 510/511
- Test mô phỏng payload WS cho `handleOldMessages` → assert `SaveMessage` dedupe theo `(id, account_id)`.
- Test sync group qua REST fallback (`GetGroupHistoryV2`).
- Smoke test thật: cần Sếp login QR lại (T7 block) rồi gửi tin từ client khác → DB có row.

### T2 — Auto-sync nền
- Scheduler định kỳ (10 phút) duyệt mọi account active + mọi conversation.
- Với mỗi conv: gọi `RequestOldMessagesViaListener(convID, convType)` với `lastId = max(message_id)` trong DB.
- Throttle 1s giữa các conv, exponential backoff khi Zalo 5xx/rate-limit.
- Bỏ qua conv đang sync (có lock `sync_state`).

### T3 — Đồng bộ media kèm tin nhắn
- Trong `handleNewMessages` + `handleOldMessages` (handler trong `internal/api/ws.go`):
  - Sau khi `SaveMessage`, nếu `msg.Type ∈ {image, video, file, voice, sticker, gif}` → enqueue `mediaDownloadInfo` vào channel.
  - Worker (đã có `maybeAutoDownloadMedia` nhưng chỉ chạy trên attachment đầu) → chạy lại với toàn bộ attachments.
- Dedupe: skip nếu file tồn tại trên disk theo path `/storages/media/{account}/{conv}/{fileID}.{ext}`.
- Retry tối đa 3 lần, log lỗi.

## Files sẽ sửa
- `internal/core/websocket.go` — `handleOldMessages` (giữ nguyên logic chính, có thể tách helper `parseAndSave`).
- `internal/api/ws.go` — thêm scheduler `runAutoSync()` + mở rộng `maybeAutoDownloadMedia` xử lý nhiều attachments.
- `internal/api/handlers.go` — `HandleSyncMessages` có thể truyền `lastId` từ client.
- `internal/store/` — method `MaxMessageID(accountID, convID)` + `ListConversations(accountID)`.
- `internal/core/chat.go` — đảm bảo `GetGroupHistoryV2` đã dedupe OK.
- Tests mới: `internal/core/sync_test.go`, `internal/api/sync_test.go`.

## Verification
- [ ] `go test ./...` pass.
- [ ] Sync thật: gửi tin từ client khác → `/api/messages` có message mới trong 5s.
- [ ] Sync media: gửi ảnh từ client khác → file xuất hiện trong `/storages/media/...`.
- [ ] Auto-sync: kill browser, gửi tin, đợi 10 phút, mở browser thấy message (qua `/api/messages`).
