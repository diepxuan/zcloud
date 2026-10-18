# Task 29: Mock test `GetMyProfile` + `GetFriends` (T3)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5 (T3) + §5.24
- **Trạng thái:** ✅ Done (19/10/2026)
- **Tiền đề:** [27-chat-client-mock-test.md](27-chat-client-mock-test.md) (đã có
  mock harness `newChatMockSession` trong `internal/core/chat_mock_test.go`)

## Bối cảnh

`internal/core/chat.go` `GetMyProfile` và `GetFriends` chưa có test vòng đời
HTTP. Cả hai dùng chung pattern: build params → `EncodeAESCBC` → gọi
`ServiceKeyProfile` → response `data` là string → `DecodeAESCBC` → parse.

Format (đọc từ code):
- **GetMyProfile**: POST `.../api/social/friend/getprofiles/v2?zpw_ver&zpw_type`,
  body `params=<urlEscape(enc)`. Response `{error_code, data:string}`; decrypt
  ra `{data:{changed_profiles:{"<uid>_0":{displayName|zaloName, avatar}}}}`,
  hoặc flat `{changed_profiles:{...}}`. Trả `(name, avatar, err)`; `error_code!=0`
  → `("","",nil)`.
- **GetFriends**: GET `.../api/social/friend/getfriends?params=<urlEscape(enc)>&zpw_ver&zpw_type&nretry=0`.
  Response `{error_code, data:string}`; decrypt ra `{"data":[...]}` hoặc `[...]`
  mảng trực tiếp; mỗi item `{userId, displayName, zaloName, avatar}` → `[]User`.
  `body[0]=='<'` → error html; `error_code!=0` → error.

## Mục tiêu

Thêm vào `internal/core/chat_mock_test.go` (tái dùng harness sẵn có):

1. Helper trỏ `ServiceMap[ServiceKeyProfile]` về mock server (hiện helper cũ chỉ
   set `ServiceKeyChat`). Session cần `UserID` (vd `"12345"`) để key profile là
   `"12345_0"`.
2. `TestGetMyProfile_RoundTrip`: mock assert POST + path
   `/api/social/friend/getprofiles/v2` + query `zpw_ver`/`zpw_type` + form
   `params` decrypt được; trả `data` = string base64(`EncodeAESCBC`(key,
   `{"data":{"changed_profiles":{"12345_0":{"displayName":"Tên Có Dấu","zaloName":"ten.khongdau","avatar":"https://x/a.jpg"}}}}`));
   assert `name=="Tên Có Dấu"`, `avatar=="https://x/a.jpg"`.
3. `TestGetMyProfile_FlatFallback`: decrypt ra không có wrapper `data`
   (`{"changed_profiles":{"12345_0":{...}}}`) → vẫn ra name/avatar.
4. `TestGetMyProfile_ErrorCode`: `{"error_code":5}` → `("","",nil)` (không lỗi).
5. `TestGetFriends_RoundTrip`: mock assert GET + path
   `/api/social/friend/getfriends` + query `params`/`zpw_ver`/`zpw_type`; trả
   `data` string base64(`EncodeAESCBC`(key, `{"data":[{"userId":"u1","displayName":"Bạn Một","avatar":"a1"},{"userId":"u2","displayName":"Bạn Hai","avatar":"a2"}]}`));
   assert `len==2` + `userId/displayName/avatar` đúng.
6. `TestGetFriends_FlatArray`: decrypt ra mảng trực tiếp `[{"userId":"u3",...}]` → `len==1`.
7. `TestGetFriends_HTML`: body `"<html>rate</html>"` → lỗi chứa `"html"`.
8. `TestGetFriends_ErrorCode`: `{"error_code":9}` → lỗi.

## Ràng buộc

- **CHỈ** sửa/thêm `_test.go` trong `internal/core/`. KHÔNG sửa production.
- Không bật/xoá test cũ. Không đổi helper `newChatMockSession` theo cách phá test
  task 27 (có thể thêm helper mới hoặc tham số hoá).
- Key 16 byte ASCII; `SecretKey = base64(key)`.

## Kiểm chứng (output thật)

1. `cd src/zcloud && go test ./internal/core/... -run 'GetMyProfile|GetFriends' -v` → PASS.
2. `go test ./internal/core/...` → PASS 0 fail.
3. `go build ./...` + `go vet ./...` sạch.
4. `go test -cover ./internal/core/...` → ghi % (trước ~33.4%).

## Ngoài phạm vi

- `GetConversations` (phức tạp — 2 lượt request + resolveNames) để task sau.
- Không test `GetGroupInfo`/`GetGroupHistory`.
- Không đụng production.

## Rủi ro

- Thấp — chỉ test. Chú ý: GetFriends dùng GET với `params` ở **query string**,
  GetMyProfile dùng POST với `params` ở **form body** — assert đúng kiểu.

## Kết quả (19/10/2026)

- Chỉ sửa `internal/core/chat_mock_test.go` (thêm `newProfileMockSession` + 7
  test). KHÔNG đổi production.
- Verify: `go test ./internal/core/...` **PASS 0 fail** (7 test mới PASS);
  `go build`/`go vet` sạch; `gofmt -l` trên file test = clean; coverage core
  **33.4% → 37.6%**.
