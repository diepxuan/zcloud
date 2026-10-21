# Task 31: Fix tin nhắn nhóm bị ghi/gom nhầm vào chat riêng

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §3 và §5.26
- **Trạng thái:** 🟡 In Progress (21/10/2026)
- **Mức độ:** Cao — sai conversation của dữ liệu, ảnh hưởng trực tiếp chat và lịch sử
- **Liên quan:**
  - [21-fix-newmessage-parse.md](21-fix-newmessage-parse.md) — unwrap `groupMsgs`
  - [30-realtime-webui.md](30-realtime-webui.md) — browser WS và dispatch realtime
  - `internal/core/websocket_message.go` — parser conversation của message
  - `internal/store/queries.go` — preview sidebar

## Hiện tượng

Tin nhắn thuộc conversation nhóm xuất hiện trong chat riêng hoặc preview của
conversation nhóm lấy nội dung từ chat riêng.

## Bằng chứng dữ liệu (21/10/2026)

PostgreSQL remote `postgresql.diepxuan.corp:5432`, database `zcloud`:

- 94 conversation nhóm (`conv_type=1`).
- 44 conversation nhóm có `last_msg_id` trỏ tới message mà
  `messages.conv_id` là conversation khác.
- 41 case thuộc account đang bật `acc_2291602426651082808`.
- 3 case thuộc account đã tắt `acc_559609701372941728`.
- 23 message trong 44 case nằm rõ trong 17 chat riêng đang tồn tại.
- 93/94 conversation nhóm không có `last_msg_at`.
- Không có message ID nào nằm đồng thời trong hai conversation cùng account:
  message bị ghi vào sai conversation, không phải copy hai lần.
- 82/94 conversation nhóm chưa resolve được tên, vẫn hiển thị ID số. Đây là
  vấn đề riêng, không phải nguyên nhân trực tiếp của việc trộn tin.

Ví dụ account đang bật:

- Group `7594197834360446865` có `last_msg_id=8394889109935`.
- Message `8394889109935` có `conv_id=1117310679873051764` — chat riêng
  `Parent`.
- Group `2170309999183745771` có `last_msg_id=8394883611068`, cũng nằm
  trong chat riêng `Parent`.
- Chat riêng `Parent` có 7 message đang được các group khác nhau dùng làm
  tin cuối.

## Nguyên nhân

### 1. Parser bỏ mất loại thread

`wsMessage.toMessage` chỉ ưu tiên `grid`; nếu `grid` rỗng, tin không phải
self sẽ lấy `uidFrom` làm conversation:

```go
convID := skipZero(m.Grid)
if convID == "" {
    if isSelf {
        convID = firstNonEmptyID(m.IDTo, m.ToID, m.ConvID)
    } else {
        convID = firstNonEmptyID(fromID, m.IDTo, m.ToID, m.ConvID)
    }
}
```

`handleNewMessages`/`handleOldMessages` biết `ThreadGroup` nhưng
`parseMessages` không truyền thông tin này vào parser. Vì vậy payload
`groupMsgs` thiếu `grid` bị ghi vào chat riêng của người gửi.

Tham chiếu zca-js `GroupMessage` dùng `data.idTo` làm `threadId`; không dùng
`uidFrom` và không bắt buộc có `grid`.

### 2. Preview sidebar join không đúng conversation

`GetConversations` join message chỉ theo `(account_id, id)`, thiếu
`m.conv_id = c.id`. Khi `last_msg_id` trỏ sai, sidebar vẫn lấy nội dung
message từ conversation khác.

### 3. UI không truyền account khi load/sync

- `POST /api/messages/sync` từ UI chỉ gửi `convId`, backend yêu cầu
  `accountId` nên trả 400.
- `GET /api/messages` từ UI không truyền `accountId`, backend tự chọn account
  theo conv. Nếu cùng conv ID xuất hiện ở nhiều account, có nguy cơ đọc nhầm
  account.

## Phạm vi fix

### T31.1 — Parser thread-aware

- Đổi `toMessage(session, tt)`.
- Với `ThreadGroup`:
  - conversation ưu tiên `grid`;
  - fallback `idTo`, `convId`, `toid`;
  - không dùng `fromID`, không fallback về `selfUID`.
- Nếu group message vẫn không xác định được conversation: skip + log, không
  ghi vào chat riêng.
- Truyền `ThreadUser`/`ThreadGroup` đúng nguồn `msgs`/`groupMsgs` trong
  `GetConversations`.

### T31.2 — Preview scoped theo conversation

- Join thêm `m.conv_id = c.id`.
- Tin cuối không đúng conversation thì không được dùng làm preview.

### T31.3 — UI account scoped

- `/api/messages/sync` gửi cả `accountId` + `convId`.
- `/api/messages` gửi `accountId` của conversation đang mở.

### T31.4 — Repair dữ liệu cũ

Thêm repair idempotent:

- Chỉ xét conversation `conv_type=1` có `last_msg_id` trỏ tới message khác
  `conv_id`.
- Bỏ qua message ID đang bị nhiều group cùng account nhận làm last message
  (ambiguous).
- Chuyển `messages.conv_id`, `media.conv_id`, `media_jobs.conv_id` về group.
- Di chuyển file media nếu file đã tải.
- Cập nhật `conversations.last_msg_at` từ timestamp message.
- Chạy qua startup repair giống self-thread repair hiện có.

Không thay đổi schema.

## Test hồi quy

- Group `groupMsgs` không có `grid`, có `idTo=group-id` → ConvID đúng group.
- Group `groupMsgs` không có `grid`/`idTo` → không emit event vào private.
- Group old message 511 thiếu `grid` → ConvID đúng.
- Direct message giữ nguyên hành vi cũ.
- Preview group không lấy message của private conversation.
- Repair chuyển message/media/job, cập nhật `last_msg_at`, idempotent.
- JS syntax + UI smoke test pass.
- Build + unit test pass.

## Verification

- [ ] Parser tests pass.
- [ ] Store integration tests pass với `-tags testdb`.
- [ ] `go test ./...` pass.
- [ ] `./scripts/check-js.sh` pass.
- [ ] `./scripts/check-ui.sh` pass.
- [ ] Restart service; log repair hiển thị số row đã sửa.
- [ ] DB sau repair: 0 group `last_msg_id` trỏ message khác `conv_id`.
- [ ] API sidebar group không còn preview từ chat riêng.

## Rủi ro và kiểm soát

- **Payload group không có `idTo/convId/toid`:** bỏ qua event thay vì đoán;
  ưu tiên không ghi bẩn dữ liệu.
- **Repair sai do `last_msg_id` của Zalo sai:** chỉ xử lý quan hệ đã được
  conversation nhóm xác nhận; bỏ qua trường hợp một message được nhiều group
  cùng account tham chiếu.
- **Media file:** lỗi move file không rollback DB; media giữ `source_url` để
  worker tải lại.

## Không nằm trong task này

- Resolve toàn bộ tên/avatar group đang hiển thị ID số.
- Backfill toàn bộ lịch sử group cũ hơn phạm vi các message đã xác định.
- Thay đổi schema.
