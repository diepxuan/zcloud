# Task 33: Kiểm tra group thiếu tin nhắn cũ

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §3 và §5.28
- **Trạng thái:** ✅ Done (21/10/2026)
- **Mức độ:** Cao — lịch sử nhóm không đầy đủ và cursor có thể bị hỏng
- **Liên quan:**
  - [10-sync-history.md](10-sync-history.md) — luồng sync 510/511 + REST fallback
  - [31-fix-group-thread-mix.md](31-fix-group-thread-mix.md) — parser thread-aware
  - `internal/api/handlers.go` — `HandleSyncMessages`
  - `internal/core/chat.go` — `GetGroupHistory`, `GetGroupHistoryV2`
  - `internal/store/queries.go` — lưu message và refresh last message

## Hiện tượng

Nhiều conversation nhóm không có tin nhắn hoặc chỉ có một phần lịch sử dù
Zalo vẫn còn dữ liệu. Một số group có `last_msg_id` trỏ tới message không có
trong DB, hoặc message đang nằm sai conversation do parser cũ.

## Bằng chứng trước fix

PostgreSQL live ngày 21/10/2026:

- `acc_2291602426651082808`: 86 group, 43 group có 0 message, 31 group có
  1-9 message, 44 group có `last_msg_id` không tồn tại trong DB.
- `acc_559609701372941728`: 9 group, 5 group có 0 message, 2 group có
  1-9 message, 5 group có `last_msg_id` không tồn tại trong DB.
- Tổng cộng 48 group có 0 message.
- Có 3 message vừa sync bị lưu với `timestamp=0`, làm hỏng cursor lịch sử.

## Nguyên nhân

1. `HandleSyncMessages` coi việc gửi WS 510/511 là đã sync xong, không chờ
   message vào DB nên REST fallback gần như không chạy.
2. UI gọi `lm()` ngay sau khi bắn sync, trước khi server kịp ghi DB.
3. `/api/group/history` trả 404 trên các host hiện tại; legacy
   `/api/cm/getrecentv2` phải dùng `zpw_ver=645`, không dùng version session.
4. `getrecentv2` bọc payload trong `data` dạng chuỗi JSON, có trường hợp còn
   bọc thêm một envelope `{error_code,error_message,data}` bên trong.
5. `ts` có thể là chuỗi; parser cũ chỉ đọc `float64` nên lưu thành `0`.
6. Message ảnh cũ có `content` là object `href/thumb/params`, không có
   `msgType`; parser cũ để `msg_type=0`, content thành chuỗi `map[...]`.
7. `SaveMessage` không đổi `conv_id` khi conflict. Các message nhóm từng bị
   parser cũ ghi vào chat riêng vì vậy sync lại vẫn không quay về group.

## Phạm vi fix

### T33.1 — Chờ WS rồi fallback REST

- Đếm message trước/sau khi gửi 510/511.
- Chờ tối đa 2 giây để WS ghi DB; nếu không tăng thì fallback REST.
- Thử V2 `/api/group/history`, sau đó legacy `/api/cm/getrecentv2`.
- Thử lần lượt các host group, log lỗi từng nguồn.
- Trả `synced` theo số message thực sự thêm vào DB thay vì số request đã gửi.

### T33.2 — Parser lịch sử nhóm

- Dùng chung parser cho V1/V2.
- Hỗ trợ `groupMsgs`, `msgs`, array trực tiếp và `data` lồng chuỗi JSON.
- Giải mã tối đa 3 lớp envelope `error_code/data`.
- Đọc timestamp dạng number hoặc string; bỏ message thiếu `ts`/`ts <= 0`.
- Chuyển content ảnh có `href/thumb/params` thành attachment + `MsgTypeImage`.
- Giữ content kỹ thuật JSON không còn hiện trực tiếp trong bubble.

### T33.3 — Sửa message nằm sai conversation

- Thêm `Store.SaveGroupHistoryMessage`.
- Khi API lịch sử nhóm xác nhận message thuộc group nhưng row đang ở
  conversation khác, chuyển `messages.conv_id` về group.
- Kéo `media` + `media_jobs` theo, cập nhật `file_path/local_path`, và di
  chuyển file media trên disk sau khi commit DB.
- Nếu chat riêng đang dùng message đó làm last message, khôi phục last message
  còn lại của chat riêng.
- Không ghi đè content/timestamp/from_id gốc; idempotent khi sync lại.

### T33.4 — UI

- `sc()` hiển thị “Đang tải...”, chờ `/api/messages/sync` trả xong rồi mới
  gọi `lm()`.
- Nếu sync thêm message thì reset cursor trước khi load lại.

Không thay đổi schema.

## Kiểm chứng live

- Group mẫu `7594197834360446865`: 7 message → 10 message.
- 3 message ảnh cũ được lưu đúng timestamp và attachment, không còn
  `timestamp=0`; content không còn dạng `map[...]`.
- Group `5032666330621624354` (ANTT TDP11 NAM LÝ): 0 → 1 message; message
  `8385823210323` được chuyển từ chat riêng về đúng group.
- Group `5770111624378323430` (LINH4G.FUN): 0 → 1 message.
- Quét 47 group còn trống: phục hồi 1 group, 46 group còn lại Zalo trả
  `groupMsgs` rỗng.
- Mẫu `8139581331285759744` (An Đô - Điệp Xuân QB): API trả metadata hợp lệ
  với `groupMsgs=[]`; không phải lỗi parser.

## Kết quả sau fix

- `acc_2291602426651082808`: 86 group, 41 group còn 0 message, 32 group có
  1-9 message, 44 group có `last_msg_id` chưa có trong DB.
- `acc_559609701372941728`: 9 group, 5 group còn 0 message, 2 group có
  1-9 message, 5 group có `last_msg_id` chưa có trong DB.
- Tổng group trống: 48 → 46.
- Hiện còn 49 `last_msg_id` chưa có row trong DB: 46 thuộc group trống,
  3 group có lịch sử một phần nhưng Zalo không trả message cuối tương ứng.
- `last_msg_at IS NULL`: 65 → 63.
- Message `timestamp <= 0`: 3 → 0.

## Test hồi quy

- `TestParseGroupHistoryPayloadNestedString`.
- `TestDecodeGroupHistoryDataNestedEnvelope`.
- `TestGroupHistoryMessagesSkipInvalidTimestamp`.
- `TestGroupHistoryMessagesParseStructuredPhoto`.
- `TestNeedsGroupHistoryFallback`.
- `TestGroupHistoryBaseURLs`.
- `TestCountAndRefreshConversationLastMessage`.
- `TestSaveGroupHistoryMessageMovesMisclassifiedMessage`.
- `go test ./...` pass.
- `go test -tags testdb` cho store integration pass.
- `go build ./...` pass.
- `./scripts/check-js.sh` pass.
- `./scripts/check-ui.sh` pass.

## Rủi ro và kiểm soát

- Chỉ chuyển conversation khi API lịch sử nhóm xác nhận message ID thuộc group.
- Message thiếu timestamp bị bỏ qua thay vì lưu cursor `0`.
- REST fallback có timeout và throttle khi quét nhiều group.
- Group Zalo thật sự trả lịch sử rỗng được giữ nguyên, không bịa dữ liệu.
- File media move lỗi không rollback DB; media vẫn còn `source_url` để tải lại.

## Không nằm trong task này

- Bịa/khôi phục lịch sử mà Zalo không trả về.
- Xoá conversation hoặc message không liên quan.
- Thay đổi schema.
- Gửi tin test vào thread khách hàng.
