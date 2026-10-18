# Task 28: Fix `SendMessage` bỏ msgId thật (parse `data` sai) 

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.23
- **Trạng thái:** 🟡 In Progress
- **Nguồn:** phát hiện qua [27-chat-client-mock-test.md](27-chat-client-mock-test.md)

## Bối cảnh (bug thật — đã kiểm chứng)

`internal/core/chat.go` `SendMessage`, khối parse `sendResp.Data`
(`internal/core/chat.go` ~dòng 132–147):

```go
if sendResp.Data != nil {
    var dataMap struct {
        MsgID    string `json:"msgId"`
        CliMsgID string `json:"cliMsgId"`
    }
    rawData := *sendResp.Data
    var dataStr string
    if json.Unmarshal(rawData, &dataStr) == nil {          // data là JSON string
        if dec, err := DecodeAESCBC(rawKey, dataStr); err == nil {
            _ = json.Unmarshal(dec, &dataMap)              // dataMap.MsgID set đúng
        }
    }
    if err := json.Unmarshal(rawData, &dataMap); err == nil && dataMap.MsgID != "" {
        clientID = dataMap.MsgID                           // ← KHÔNG BAO GIỜ chạy khi data là string
    }
}
```

**Vấn đề:** response thật của Zalo có `data` là **string** (base64 AES-CBC).
Vì vậy `json.Unmarshal(rawData, &dataMap)` **luôn fail** (string → struct) →
guard `err == nil` chặn → `clientID` không được gán `msgId` thật → `Message.ID`
rơi về `clientId` (timestamp tự sinh `time.Now().UnixMilli()`).

**Format thật (references):**
- `docs/references/zca-js/src/utils.ts` `handleZaloResponse`:
  `JSON.parse(decodeAES(secretKey, jsonData.data))` → payload giải mã là
  envelope `{error_code, data: <payload>}`, lấy `decodedData.data`.
- `docs/references/za-go/internal/util/utils.go` `NormalizeDecodedData`: bóc
  đệ quy `{error_code:0, data: X}`.
- → `msgId` thật nằm ở `decrypted.data.msgId` (payload gửi tin thường là
  `{msgId, cliMsgId, ...}`).

**Bằng chứng ảnh hưởng (DB prod `zcloud`):** 62 tin outbound của account Sep
(`acc_2291602426651082808`) có id kiểu timestamp (`178xxxxxxx`). Self-join cho
thấy cặp trùng nội dung lệch vài ms, ví dụ `(1789317379409, 8259927263054)` →
tin gửi từ UI **bị lưu 2 dòng** (1 id timestamp do `HandleSendMessage` lưu,
1 id msgId thật từ WS echo), do `SaveMessage` dedupe theo `ON CONFLICT
(id, account_id)` không khớp.

## Mục tiêu (định nghĩa hoàn thành)

1. `SendMessage` đọc đúng `msgId` thật cho cả 2 dạng payload sau khi giải mã:
   - top-level `{"msgId":"..."}`;
   - envelope `{"error_code":0,"data":{"msgId":"..."}}` (dạng thật).
2. Giữ **fallback** về `clientId` khi không lấy được msgId (không hồi quy).
3. `go test ./internal/core/...` **PASS 0 fail** (gồm `TestSendMessage_RoundTrip`).
4. Thêm/đổi test để phủ dạng envelope thật.

## Fix đề xuất (`internal/core/chat.go`, thay đúng khối parse `Data`)

```go
if sendResp.Data != nil {
    rawData := *sendResp.Data

    // data có thể là object thô hoặc string base64 (AES-CBC) đã mã hoá.
    decoded := rawData
    var dataStr string
    if json.Unmarshal(rawData, &dataStr) == nil && dataStr != "" {
        if dec, err := DecodeAESCBC(rawKey, dataStr); err == nil {
            decoded = dec
        }
    }

    // Sau giải mã có thể là {msgId} top-level hoặc {error_code,data:{msgId}}.
    var outer struct {
        MsgID string           `json:"msgId"`
        Data  *json.RawMessage `json:"data"`
    }
    if json.Unmarshal(decoded, &outer) == nil {
        if outer.MsgID != "" {
            clientID = outer.MsgID
        } else if outer.Data != nil {
            var inner struct {
                MsgID string `json:"msgId"`
            }
            if json.Unmarshal(*outer.Data, &inner) == nil && inner.MsgID != "" {
                clientID = inner.MsgID
            }
        }
    }
}
```

## Thay đổi chi tiết

1. `internal/core/chat.go`: thay khối parse `sendResp.Data` như trên. Không đổi
   phần build request / send khác.
2. `internal/core/chat_mock_test.go`:
   - `TestSendMessage_RoundTrip`: server trả `data` = JSON string
     `base64(EncodeAESCBC(key, "{\"error_code\":0,\"data\":{\"msgId\":\"m-1\"}}"))`
     (dạng **envelope thật**); assert `Message.ID == "m-1"`.
   - Thêm `TestSendMessage_DataTopLevelMsgID`: `data` string base64 decode thẳng
     ra `{"msgId":"t-7"}` → `ID == "t-7"`.
   - Giữ `TestSendMessage_DataPlainObject` (object thô) → `ID == "x-9"`.
   - Giữ nguyên các test lỗi (ErrorCode/HTML/InvalidJSON/NotLoggedIn).

## Ràng buộc

- Chỉ sửa `internal/core/chat.go` (khối parse `Data`) + `chat_mock_test.go`.
- Không đổi signature, không đổi schema, không đụng DB.
- Không dọn/án các bản ghi trùng cũ trong DB (việc riêng, cần Sếp duyệt).

## Kiểm chứng (chạy, ghi output thật)

1. `cd src/zcloud && go test ./internal/core/... -run 'SendMessage' -v` → PASS (6 case).
2. `go test ./internal/core/...` → **PASS 0 fail**.
3. `go build ./...` + `go vet ./...` sạch.
4. `go test -cover ./internal/core/...` → ghi %.

## Ngoài phạm vi (ghi nhận)

- Dọn 62+ cặp bản ghi trùng đã lưu trong prod (cần Sếp duyệt, làm sau).
- `GetConversations`/`GetMyProfile`/`GetFriends` chưa có mock test (task sau).

## Rủi ro

- Thay đổi đường gửi tin đang chạy prod. Giảm thiểu: giữ fallback `clientId`;
  payload lạ → không lấy được msgId → hành vi y như cũ. Verify bằng unit test +
  live send (marker) ở thread Trần Ngọc Đức xác nhận không còn lưu trùng.
