# Design — zcloud (tài liệu thiết kế dùng chung)

> Tài liệu thiết kế kiến trúc và quy ước kỹ thuật cho dự án zcloud.
> Áp dụng cho mọi module trong `src/zcloud/`. Khi thêm tính năng mới, đọc
> file này trước để khớp với codebase hiện có.

## 1. Mục tiêu

Cloud service Zalo — cho phép đăng nhập QR, chat real-time, lưu lịch sử
tin nhắn + media lâu dài, đồng bộ theo chuẩn Zalo (WS cmd 510/511).

## 2. Tech stack

| Lớp | Tech |
|-----|------|
| Core | Go 1.22+, `modernc.org/sqlite`, `github.com/coder/websocket` |
| HTTP | `net/http` + `http.ServeMux` (Go 1.22 pattern routing) |
| Storage | SQLite (WAL mode) + disk media files |
| Web UI | Vanilla JS ES6+, HTML/CSS thuần, `go:embed` |
| Process | systemd service + `scripts/zcloudd.sh` watch mode |

## 3. Cấu trúc module

```
src/zcloud/
├── cmd/zcloudd/           # main.go — entry point
├── internal/
│   ├── core/              # Logic Zalo (không phụ thuộc HTTP)
│   │   ├── encrypt.go     # AES-128-CBC + AES-GCM + base64
│   │   ├── auth.go        # QR login, cookie login, getLoginInfo
│   │   ├── chat.go        # SendMessage, GetConversations, GetFriends…
│   │   ├── websocket.go   # WS client + cmd 510/511 + decrypt event
│   │   ├── types.go       # Message, Conversation, Session, User, Event
│   │   └── errors.go      # Sentinel errors
│   ├── api/               # HTTP layer
│   │   ├── handlers.go    # Tất cả route handler
│   │   ├── router.go      # SetupRouter + media download
│   │   ├── ws.go          # Browser WS + Zalo listener nền
│   │   ├── embed.go       # //go:embed web/*
│   │   └── web/           # login.html, chat.html, favicon.svg
│   ├── store/             # SQLite layer
│   │   └── store.go       # Migrations + queries
│   └── config/            # Env config
└── examples/              # Test programs
```

## 4. Quy ước code

### 4.1 Package boundaries
- `internal/core` không import `internal/api` và `internal/store`.
- `internal/api` import cả `core` + `store`.
- `internal/store` không import `core` (chỉ thuần SQL).
- Mọi package ngoài `cmd/` đều nằm trong `internal/` → không export ra ngoài.

### 4.2 Naming
- File Go: snake_case.
- Struct/interface: PascalCase, có comment giải thích vai trò.
- Method receiver: 1-2 ký tự (vd `s *Store`, `c *Client`, `w *WSClient`).
- Error sentinel: prefix `Err` (vd `ErrNotLoggedIn`).
- Log prefix: `[zcloud]` cho subsystem chính.

### 4.3 Error handling
- Trả về error, không panic (trừ init).
- API handler: dùng helper `ok(w, data)` / `fail(w, status, msg)` trong
  `handlers.go`.
- Log lỗi kèm context (accountId, convId, …).

### 4.4 Logging
- Hiện tại: `fmt.Printf` cho debug + `log.Printf` cho production.
- TODO: tập trung về `*log.Logger` truyền qua `Server.Logger`.

### 4.5 HTTP
- Dùng `http.ServeMux` pattern (Go 1.22): `mux.HandleFunc("POST /api/x", h)`.
- Response chuẩn: `APIResponse{OK bool, Data, Error, Code}`.
- Status: 200 OK, 400 input, 401 auth, 404 not found, 500 server.

### 4.6 Database
- Tất cả schema trong `internal/store/store.go` dạng `const migrationX`.
- Thêm bảng = thêm `const` + push vào slice `migrations`.
- Không sửa `schema.sql` khi chưa sync code (xem SOUL.md §3).

### 4.7 WebSocket (browser)
- Endpoint: `GET /ws?accountId=…`.
- Message JSON: `{type: "new_message" | "sync_done" | …, data: {...}}`.

### 4.8 WebSocket (Zalo)
- Endpoint: từ `zpw_ws` trong login response (vd `wss://ws1-msg.chat.zalo.me`).
- Frame: `version(1) + cmd(2 LE) + subCmd(1) + payload`.
- Payload có thể AES-GCM + gzip → dùng `core.DecodeWSEvent`.

## 5. Crypto

- **AES-128-CBC** với IV zero, PKCS7 padding — cho REST params.
- **AES-GCM** cho WS event data.
- **MD5** cho chữ ký: `md5("zsecure" + type + sortedParams)`.
- Key = `base64_decode(zpw_enk)` từ login response.

## 6. Zalo API endpoints (đã dùng)

| Method | Path | Mục đích |
|--------|------|----------|
| GET | `/api/preloadconvers/get-last-msgs` | Sync conversations |
| GET | `/api/message/getmsgids` | Old msg IDs |
| GET | `/api/message/getmsgdetail` | Old msg detail |
| GET | `/api/friend/getfriends` | Friends list |
| GET | `/api/profile/get基本信息` (search) | User profile |
| GET | `/api/group/get-group-info` | Group info |
| GET | `/api/cm/getrecentv2` | Group history (REST fallback) |
| POST | `/api/message/sms` | Send message |

WS cmds: 501/521 (new msg), 510/511 (old msg), 1 (ping).

## 7. Data flow

### Login
```
Browser → POST /api/qr/create → core.CreateQRLogin → {token, image}
Browser → poll /api/qr/poll → core.PollQRLogin → session
                                    ↓
                            store.CreateAccount + SaveSession
                                    ↓
                            core.NewClient + StartZaloListener
```

### Chat real-time
```
Zalo WS → core.WSClient.readLoop → core.handleFrame
                                ↓
              api.handleZaloEvent → store.SaveMessage
                                ↓
              api.WSManager.Broadcast → Browser WS
```

### Send message
```
Browser → POST /api/messages/send → core.Client.SendMessage
                                ↓
                              Zalo REST API
                                ↓
                              store.SaveMessage
                                ↓
                              ok(data)
```

## 8. Operations

### Build
```bash
cd src/zcloud && go build -o ../../zcloudd ./cmd/zcloudd/
```

### Run
- Production: `systemctl start zcloud` (service quản lý qua
  `scripts/zcloudd.sh` watch mode).
- Dev: `./scripts/zcloud.sh start|stop|restart|logs|status`.

### Env
| Var | Mặc định | Mô tả |
|-----|----------|-------|
| `ZCLOUD_DB_PATH` | `./storages/database/zcloud.db` | Đường dẫn SQLite |
| `ZCLOUD_MEDIA_PATH` | `./storages/media` | Thư mục media |
| `ZCLOUD_PORT` | `8080` | Port HTTP |

### Restart
- `systemctl restart zcloud` — KHÔNG start binary tay (xem CLAUDE.md
  "Quy tắc vận hành").

### Restart Zalo listener nội bộ
- Khi sửa code trong `src/zcloud/`, `scripts/zcloudd.sh` watch mode sẽ tự
  build + restart service. Không cần thao tác tay thêm.
- Muốn restart chỉ riêng Zalo listener cho một account, dùng:
  - `StopZaloListener(accountID)` — dừng goroutine/listener, giữ nguyên session.
  - `StartZaloListener(...)` — khởi động lại listener cho account.
- **KHÔNG gọi `/api/logout` để restart listener.** `/api/logout` xoá account,
  session, conversation, message, media trong DB; chỉ dùng khi Sếp thực sự
  muốn xoá tài khoản.
- Khi đổi session/cookie, đảm bảo chỉ giữ 1 session active cho mỗi account để
  tránh nhiều WebSocket cùng lúc bị Zalo kick.

## 9. Testing

- Hiện có: `internal/core/encrypt_test.go` (AES-CBC + PKCS7).
- Cần thêm (xem audit.md tồn đọng):
  - `internal/core/chat_test.go` — SendMessage mock.
  - `internal/store/store_test.go` — migrations + CRUD.
  - `internal/api/handlers_test.go` — HTTP API với `httptest`.

## 10. Conventions khi viết code mới

- Đọc file liên quan trước (grep, rg).
- Không sửa schema nếu chưa được yêu cầu.
- Commit nhỏ, mỗi commit là 1 thay đổi rõ ràng.
- Format: `<loại>(<phạm vi>): <mô tả>` (vd `feat(api): thêm /api/friends`).
- Push thẳng vào `main`, không cần PR.
- Trước khi commit: `go build ./...` + `go test ./...` pass.
