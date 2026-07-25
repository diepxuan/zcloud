# Task 05: Build Server Daemon

## Liên kết
- **Master plan:** [master-plan.md](../master-plan.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** [04-build-core.md](04-build-core.md) ✅
- **Kế tiếp:** [06-build-webui.md](06-build-webui.md)
- **Tham khảo:** (không có — tự xây dựng)

## Mục tiêu
Xây dựng HTTP server (REST + WebSocket) sử dụng core library.

## Cấu trúc files

```
src/zcloud/internal/
├── config.go          # Server config + CLI flags + env
├── api/
│   ├── router.go      # HTTP router (net/http ServeMux)
│   ├── authmw.go      # Auth middleware (token/basic)
│   ├── handlers.go    # REST handlers
│   └── ws.go          # WebSocket handler → core bridge
├── store/
│   └── session.go     # Session persist (local file / DB)
├── cmd/
│   └── zcloudd/
│       └── main.go    # Entry point
```

## Thứ tự implement

### 05.1 — config.go + main.go
- Config struct: Port, SessionPath, LogLevel, DBPath (optional)
- CLI flags: `--port`, `--session-dir`, `--db-path`
- Env vars: `ZCLOUD_PORT`, `ZCLOUD_SESSION_DIR`, `ZCLOUD_DB_PATH`
- main.go: parse config → init core client → init store → start server
- Graceful shutdown (SIGTERM/SIGINT)

### 05.2 — Session store (local file / DB)
**Interface:**
```go
type SessionStore interface {
    Save(session *core.Session) error
    Load() (*core.Session, error)
    Delete() error
    Exists() bool
}
```

**Local implementation:** JSON encrypt file tại `sessionPath`
**DB implementation:** configurable — nếu `DBPath` set → dùng SQLite

### 05.3 — REST API handlers

**Endpoints:**
```
GET  /api/health              → {"ok": true}
GET  /api/login/qr            → QR code PNG image
POST /api/login/check         → {"ok": true, "session": {...}}
GET  /api/session             → current session info
DELETE /api/logout            → clear session

GET  /api/conversations       → [{id, name, lastMsg, ...}]
GET  /api/conversations/{id}/messages?cursor= → messages list
POST /api/messages            → send message {to, content, type}
POST /api/messages/{id}/delete → delete message

GET  /api/friends             → friend list
GET  /api/profile             → user profile
GET  /ws                      → WebSocket upgrade
```

**Response format:**
```json
{"ok": true, "data": ...}
{"ok": false, "error": "message", "code": 401}
```

### 05.4 — WebSocket handler
- Upgrade HTTP → WebSocket (`coder/websocket`)
- Bridge core events → client: `{"type": "new_message", "data": {...}}`
- Client → Server: `{"type": "ping"}` → pong
- Auth check trước khi upgrade

### 05.5 — Static file serving
- Serve `web/` directory
- Catch-all → index.html (cho SPA routing)

## Output
- `src/zcloud/internal/api/*.go` — 4 files
- `src/zcloud/internal/store/*.go`
- `src/zcloud/internal/config.go`
- `src/zcloud/cmd/zcloudd/main.go`
- Binary: `go build ./cmd/zcloudd/` → `zcloudd`

## Verification
- [ ] `go build ./cmd/zcloudd/` → binary thành công
- [ ] `./zcloudd --help` → flags hiển thị đúng
- [ ] `curl :8080/api/health` → `{"ok": true}`
- [ ] Mọi endpoint trả về JSON format chuẩn
- [ ] WebSocket upgrade thành công
- [ ] Static file serving đúng (nếu web/ có file)

## Ghi chú
- Dùng `net/http` standard library — không cần framework
- Session store ưu tiên local file, DB interface sẵn để Sếp cấu hình sau
- WebSocket dùng `coder/websocket` (thay gorilla vì gorilla đã archive)
