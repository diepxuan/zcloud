# Task 04: Build Core Library (Go)

## Liên kết
- **Master plan:** [tasks.md](../tasks.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** [01-reverse-web-api.md](01-reverse-web-api.md) ✅, [03-design-core.md](03-design-core.md) ✅
- **Kế tiếp:** [05-build-server.md](05-build-server.md)
- **Tham khảo:** zcago `internal/cryptox/`, `internal/httpx/`, `session/`, `listener/`; Za-go `internal/util/utils.go`, `internal/app/state.go`

## Mục tiêu
Implement toàn bộ logic Zalo trong Go — `src/zcloud/internal/core/`.

## Cấu trúc files

```
src/zcloud/internal/core/
├── encrypt.go          # AES-128-CBC + AES-GCM + PKCS7 + key gen
├── encrypt_test.go     # Unit test encryption
├── auth.go             # QR login + cookie login
├── session.go          # Session struct + persist (file-based)
├── chat.go             # REST API calls (conversations, messages, friends)
├── websocket.go        # WebSocket real-time listener
├── client.go           # ZaloClient interface + impl
├── types.go            # Message, Conversation, Event, Session, ...
├── errors.go           # Custom errors
└── client_test.go      # Integration test (manual)
```

## Thứ tự implement

### 04.1 — encrypt.go
**Tham khảo:** zcago `internal/cryptox/aes.go`, `internal/httpx/encryption.go`
- AES-128-CBC encode/decode (zero IV)
- AES-GCM decode (cho WebSocket event)
- PKCS7 padding/unpadding
- Key generation: zcid, zcid_ext, encryptKey
- Request signing: MD5("zsecure" + type + sorted values)
- `EncryptParams()`, `DecryptResponse()`, `DecryptEvent()`

### 04.2 — types.go + errors.go
- Struct: Session, Message, Conversation, Event, User, Attachment
- Types: MsgType, EventType, ConvType, ThreadType
- Errors: ErrNotLoggedIn, ErrSessionExpired, ErrAPI, ErrNetwork

### 04.3 — auth.go
**Tham khảo:** zcago `session/auth/login.go`, `session/auth/login_qr.go`
- QR login flow:
  1. GET id.zalo.me → JS version
  2. POST logininfo → verify-client → qr/generate
  3. Poll waiting-scan → waiting-confirm
  4. GET checksession → userinfo
  5. GET getLoginInfo → zpw_enk, zpw_ws
  6. GET getServerInfo → settings
- Cookie login: load cookies → gọi getLoginInfo → getServerInfo
- IMEI generation (UUID + MD5 user-agent)
- Session hydrate từ cookies có sẵn

### 04.4 — session.go
- Session struct + JSON serialization
- File-based persist: `Save(path)`, `Load(path)`, `Delete()`
- Valid() check + auto-refresh
- SecretKey base64 decode → bytes

### 04.5 — chat.go
**Tham khảo:** zcago `api/` directory, `internal/httpx/`
- HTTP client với cookie jar
- Build request: encrypt params → sign → form body → send
- Decrypt response
- Methods:
  - `GetConversations()`
  - `GetMessages(convID, cursor)` — dùng WS cmd 510/511
  - `SendMessage(to, threadType, content)`
  - `GetFriends()`
  - `GetProfile()`

### 04.6 — websocket.go
**Tham khảo:** zcago `listener/`, `internal/websocketx/`
- Kết nối `wss://wpa.chat.zalo.me/`
- Binary protocol: [version(1) + cmd(2 LE) + subCmd(1) + JSON payload]
- Key exchange (CMD 1,1) → lấy cipherKey
- Ping/pong keepalive
- Event dispatch: CMD 501/521 → new messages, 510/511 → old messages
- Auto-reconnect (exponential backoff)
- AES-GCM decrypt event data
- Graceful shutdown via context

### 04.7 — client.go
- ZaloClient interface assembly
- NewClient(sessionDir) → *Client
- Full flow: Login → Listen → GetConversations → SendMessage → Close

### 04.8 — examples/basic.go
- QR login → lấy conversations → send message → listen WebSocket
- `go run examples/basic.go`

## Dependencies (thêm vào go.mod)

```go
require (
    github.com/coder/websocket v1.8.12       // WebSocket (thay gorilla)
    github.com/google/uuid v1.6.0            // UUID cho IMEI
)
```

## Output
- `src/zcloud/internal/core/*.go` — 8-10 files
- `src/zcloud/examples/basic.go` — ví dụ
- `go test ./internal/core/...` — PASS

## Verification
- [ ] `go test -v -run TestEncrypt ./internal/core/...` → PASS
- [ ] Encrypt/decrypt unit test với dữ liệu mẫu từ source tham khảo
- [ ] `go build ./...` không lỗi compile
- [ ] `go vet ./...` clean
- [ ] Encrypt/decrypt AES-128-CBC đúng
- [ ] Key generation (zcid, encryptKey) đúng spec
- [ ] Request signing (MD5) đúng

## Ghi chú
- Chỉ test encryption ở phase này (không cần account thật)
- Auth + chat + websocket cần account thật — test manual
- Android sync (task 02) không cần — history qua WS cmd 510/511
