# Task 27: Integration test `chat.go` client với mock Zalo server (T3)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5 (tồn đọng T3) + §5.22
- **Trạng thái:** 🟡 In Progress
- **Phụ thuộc:** `internal/core/chat.go`, `internal/core/encrypt.go`

## Bối cảnh

`internal/core/chat.go` là file logic lớn nhất của core (SendMessage,
GetConversations, GetMyProfile, GetFriends, GetGroupInfo…) nhưng **chưa có
test nào** cho vòng đời HTTP request/response thật (build request + mã hoá
AES-CBC + ký + parse/decrypt response). Coverage `internal/core` chỉ ~31%.

Task này dựng **mock Zalo HTTP server** bằng `httptest` để phủ
`Client.SendMessage` end-to-end, gồm cả nhánh thành công, lỗi server, HTML
(rate-limit), JSON hỏng, và chưa đăng nhập.

## Kiến thức cần nắm (đã xác minh trong code)

- `NewClient(session)` tạo `http.Client` (có cookie jar).
- `SendMessage(ctx, to, content, msgType)`:
  - `SecretKey` (base64) → `rawKey` ([]byte) → `EncodeAESCBC(rawKey, json(params))`.
  - POST form (`params=<enc>`) tới
    `serviceBaseURL(session, ServiceKeyChat, fallback) + "/api/message/sms?zpw_ver=<APIVersion>&zpw_type=<APIType>&nretry=0"`.
  - Response JSON: `{"error_code":int, "error_message":..., "error_title":..., "data":<raw>}`.
    - `body[0]=='<'` → error "zalo html".
    - `error_code != 0` → error "send error ...".
    - `data` nếu là string base64 → `DecodeAESCBC(rawKey, data)` → lấy `msgId`.
  - Trả `Message{ID: msgId-or-clientId, Content: content, ...}`.
- `serviceBaseURL` đọc `session.ServiceMap[ServiceKeyChat][0]` → trỏ mock
  server qua `ServiceMap = map[string][]string{ServiceKeyChat: {srv.URL}}`.
- `EncodeAESCBC`/`DecodeAESCBC` dùng IV zero + PKCS7.

## Mục tiêu (định nghĩa hoàn thành)

Tạo `internal/core/chat_mock_test.go` (KHÔNG build tag — thuần Go, không cần DB)
với các test sau:

1. `TestSendMessage_RoundTrip`:
   - Mock server kiểm: method POST; path `/api/message/sms`; query có `zpw_ver`
     + `zpw_type`; form `params` **decrypt được** (`DecodeAESCBC(rawKey, params)`)
     thành JSON chứa `message`, `toid`, `clientId`, `imei` đúng giá trị.
   - Server trả `{"error_code":0,"data":"<json-string base64(EncodeAESCBC(key, {"msgId":"m-1"}))>"}`.
   - Assert `SendMessage` trả `Message.ID == "m-1"`, `Content == "hello"`, không lỗi.
2. `TestSendMessage_ErrorCode`: server trả `{"error_code":123,"error_message":"nope"}` → error chứa `"send error"`.
3. `TestSendMessage_HTMLResponse`: server trả `200` body `<html>rate limit</html>` → error chứa `"zalo html"`.
4. `TestSendMessage_InvalidJSON`: server trả `not json` → error chứa `"parse"`.
5. `TestSendMessage_NotLoggedIn`: `Session.SecretKey == ""` → `ErrNotLoggedIn`, KHÔNG gọi server (đếm request = 0).
6. `TestSendMessage_DataPlainObject` (tuỳ chọn): `data` là object thô `{"msgId":"x-9"}` → `Message.ID == "x-9"`.

## Ràng buộc

- **CHỈ** thêm file test `_test.go`. KHÔNG sửa production code.
- Không cần DB, không build tag, chạy được với `go test ./internal/core/...`.
- Dùng `net/http/httptest`, `t.Cleanup(srv.Close)`.
- Key test: 16 byte ASCII (vd `"0123456789abcdef"`), `SecretKey = base64(key)`.
- Giữ phong cách test hiện có; helper đặt tên rõ.

## Kiểm chứng (chạy, ghi output thật)

1. `cd src/zcloud && go test ./internal/core/... -run 'SendMessage' -v` → PASS.
2. `go test ./internal/core/...` → PASS, 0 fail.
3. `go build ./...` + `go vet ./...` sạch.
4. `go test -cover ./internal/core/...` → ghi % (trước ~31.2%).

## Ngoài phạm vi

- Chưa test `GetConversations`/`GetMyProfile`/`GetFriends` (làm task sau).
- Không test `SetHeaders` cookie/UA chi tiết.
- Không đụng `syncv2`, `desktop_sync`, `websocket`.

## Rủi ro

- Thấp — chỉ test. Rủi ro chính: mock server không khớp format request/response
  thật → test sai. Đã chốt format ở mục "Kiến thức cần nắm" (đọc từ code).
