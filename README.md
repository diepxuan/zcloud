# zcloud

Cloud service Zalo (chat.zalo.me) — đăng nhập QR, chat real-time, lưu lịch sử
tin nhắn và media, đồng bộ theo chuẩn Zalo (WebSocket cmd 510/511).

## Tính năng

- **Đăng nhập QR** — mở `http://zcloud.diepxuan.corp:8080`, quét QR bằng Zalo.
- **Cookie login** — paste cookie Zalo vào ô login.
- **Multi-user** — nhiều tài khoản Zalo trên cùng daemon, mỗi browser tab độc lập.
- **Chat real-time** — gửi/nhận qua WS, tự đồng bộ conversation + messages.
- **Lưu lịch sử** — PostgreSQL + media trên disk.
- **Sync lịch sử cũ** — WS cmd 510/511, REST fallback.
- **Tab Liên hệ** — danh sách bạn bè từ Zalo API.
- **Logout / Đổi tài khoản** — xoá session + redirect login.

## Yêu cầu hệ thống

- Go 1.25+
- PostgreSQL 14+ (cần để lưu account, session, conversation, message, media, OA)
- Quyền `CREATE` / `SELECT` / `INSERT` / `UPDATE` / `DELETE` trên database zcloud.

## Cài đặt

```bash
# Chuẩn bị Postgres
sudo -u postgres createuser -s zcloud
sudo -u postgres createdb -O zcloud zcloud

# Build
cd src/zcloud && go build -o ../../zcloudd ./cmd/zcloudd/

# Chạy (qua systemd — đã setup sẵn)
systemctl restart zcloud

# Hoặc dev
./scripts/zcloud.sh start
```

## Cấu hình

Mặc định đọc `~/.config/ductn/zcloud.yml`. Xem `docs/design.md` §A8 để biết
thứ tự ưu tiên (defaults < YAML < env < CLI).

Ví dụ YAML tối thiểu (`/root/.config/ductn/zcloud.yml`):

```yaml
server:
  port: 8080
  domain: zcloud.diepxuan.corp

database:
  postgres:
    host: 127.0.0.1
    port: 5432
    user: zcloud
    password: ${ZCLOUD_DB_PASSWORD}   # expand từ env hoặc service
    dbname: zcloud
    sslmode: disable
    max_open_conns: 50
    max_idle_conns: 10

media:
  dir: /data/zcloud/storages/media
```

### Biến môi trường

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `ZCLOUD_PORT` | `8080` | HTTP port |
| `ZCLOUD_DOMAIN` | `zcloud.diepxuan.corp` | Domain cho QR + browser |
| `ZCLOUD_MEDIA_DIR` | `./storages/media` | Thư mục media |
| `ZCLOUD_LOG_LEVEL` | `0` | 0=info, 1=debug, 2=verbose |
| `ZCLOUD_PG_HOST` | `127.0.0.1` | Postgres host |
| `ZCLOUD_PG_PORT` | `5432` | Postgres port |
| `ZCLOUD_PG_USER` | `zcloud` | Postgres user |
| `ZCLOUD_PG_PASSWORD` | _empty_ | Postgres password (ưu tiên hơn YAML) |
| `ZCLOUD_PG_DBNAME` | `zcloud` | Postgres dbname |
| `ZCLOUD_PG_SSLMODE` | `disable` | sslmode |
| `ZCLOUD_DB_PASSWORD` | _empty_ | Postgres password (env đặc biệt) |
| `ZCLOUD_TEST_DSN` | _empty_ | DSN dùng cho integration test (`go test -tags testdb`) |
| `ZCLOUD_CONFIG` | _empty_ | Đường dẫn YAML config (override XDG) |

## Tech stack

- **Core:** Go 1.25+
- **HTTP:** `net/http` (Go 1.22 pattern routing)
- **WebSocket:** `github.com/coder/websocket` + native WS cho Zalo
- **Storage:** PostgreSQL (`github.com/jackc/pgx/v5`) + disk media
- **Web UI:** Vanilla JS ES6+, HTML/CSS thuần, `go:embed`

Xem chi tiết tại `docs/design.md`.

## Cấu trúc dự án

```
.
├── src/zcloud/                 # Source code Go
│   ├── cmd/zcloudd/            # main.go
│   ├── cmd/dbquery/            # Inspect DB nhanh (pgx)
│   └── internal/
│       ├── core/               # Logic Zalo (encrypt, auth, chat, ws)
│       ├── api/                # HTTP + WebSocket + web UI
│       ├── store/              # Postgres migrations + queries (Postgres-only)
│       └── config/             # Env config
├── docs/
│   ├── tasks.md                # Master plan + audit (single source)
│   ├── design.md               # Thiết kế kiến trúc + quy ước code
│   ├── database/schema.sql     # Schema DB (tham khảo Postgres DDL)
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
5. `docs/tasks.md` — danh sách task + trạng thái
6. `docs/tasks/<id>.md` — chi tiết task cần làm
7. `docs/design.md` — thiết kế kiến trúc + quy ước code
8. `MEMORY.md` — long-term memory

## Test

```bash
# Unit test — không cần Postgres
go test ./...

# Integration test — cần Postgres (Postgres-only từ 11/09/2026)
ZCLOUD_TEST_DSN="postgres://zcloud:pass@127.0.0.1:5432/zcloud?sslmode=disable" \
    go test -tags testdb ./...
```

Mỗi integration test tạo schema riêng (`zcloud_t_<TestName>`) và drop khi
test xong, nên chạy song song an toàn.

## License

MIT — xem `LICENSE`.
