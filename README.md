# zcloud

Cloud service Zalo (chat.zalo.me) — đăng nhập QR, chat real-time, lưu lịch sử
tin nhắn và media, đồng bộ theo chuẩn Zalo (WebSocket cmd 510/511).

## Tính năng

- **Đăng nhập QR** — mở `http://zcloud.diepxuan.corp:8080`, quét QR bằng Zalo.
- **Cookie login** — paste cookie Zalo vào ô login.
- **Multi-user** — nhiều tài khoản Zalo trên cùng daemon, mỗi browser tab độc lập.
- **Chat real-time** — gửi/nhận qua WS, tự đồng bộ conversation + messages.
- **Lưu lịch sử** — SQLite + media trên disk.
- **Sync lịch sử cũ** — WS cmd 510/511, REST fallback.
- **Tab Liên hệ** — danh sách bạn bè từ Zalo API.
- **Logout / Đổi tài khoản** — xoá session + redirect login.

## Cài đặt

```bash
# Build
cd src/zcloud && go build -o ../../zcloudd ./cmd/zcloudd/

# Chạy (qua systemd)
systemctl restart zcloud

# Hoặc dev
./scripts/zcloud.sh start
```

## Tech stack

- **Core:** Go 1.22+
- **HTTP:** `net/http` (Go 1.22 pattern routing)
- **WebSocket:** `github.com/coder/websocket` + native WS cho Zalo
- **Storage:** SQLite (`modernc.org/sqlite`, pure Go) + disk media
- **Web UI:** Vanilla JS ES6+, HTML/CSS thuần, `go:embed`

Xem chi tiết tại `docs/design.md`.

## Cấu trúc dự án

```
.
├── src/zcloud/                 # Source code Go
│   ├── cmd/zcloudd/            # main.go
│   └── internal/
│       ├── core/               # Logic Zalo (encrypt, auth, chat, ws)
│       ├── api/                # HTTP + WebSocket + web UI
│       ├── store/              # SQLite migrations + queries
│       └── config/             # Env config
├── docs/
│   ├── tasks.md                # Master plan + audit (single source)
│   ├── design.md               # Thiết kế kiến trúc + quy ước code
│   ├── database/schema.sql     # Schema DB
│   ├── tasks/<id>.md           # Chi tiết từng task
│   └── references/             # Source tham khảo (zca-js, zcago, Za-go)
├── scripts/
│   ├── zcloud.sh               # Service manager (start|stop|restart|logs|status)
│   └── zcloudd.sh              # Watch mode (systemd gọi)
├── MEMORY.md                   # Long-term memory
├── memory/YYYY-MM-DD.md        # Daily log
├── CLAUDE.md                   # Hướng dẫn cho Claude Code
└── AGENTS.md                   # Quy tắc workspace
```

## Cho agent làm việc trong repo

Đọc theo thứ tự (theo `AGENTS.md`):

1. `SOUL.md` — bản sắc + nguyên tắc
2. `USER.md` — thông tin Sếp
3. `IDENTITY.md` — chi tiết identity
4. `TOOLS.md` — local notes
5. `docs/master-plan.md` *(đã merge vào tasks.md)* — master plan
6. `docs/tasks.md` — danh sách task + trạng thái
7. `docs/tasks/<id>.md` — chi tiết task cần làm
8. `MEMORY.md` — long-term memory

## License

MIT — xem `LICENSE`.
