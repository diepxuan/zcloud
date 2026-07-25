# Master Plan: zcloud

> **Mục tiêu:** Xây dựng cloud service Zalo — đăng nhập trình duyệt + chat real-time + lịch sử tin nhắn.
> **Chiến lược:** Dùng Web API (chat.zalo.me), Go cho toàn bộ logic, đồng bộ Android là tùy chọn sau.
> **Chính sách push:** Em toàn quyền, push trực tiếp vào `main` sau mỗi subtask. Không cần review.

---

## Kiến trúc tổng thể

```
┌──────────────┐     HTTP/WS     ┌────────────────────────────┐     Zalo API     ┌──────────┐
│   Browser    │ ◄─────────────►│  zcloudd (Go daemon)       │◄──────────────►│ Zalo Web │
│  (vanilla JS)│                 │  :8080                      │  (chat.zalo.me) │          │
└──────────────┘                 │  ┌──────────────────────┐  │                 └──────────┘
                                 │  │ src/zcloud/internal/ │  │
                                 │  │  ├── core/  (logic)  │  │
                                 │  │  ├── api/   (server) │  │
                                 │  │  └── store/ (data)   │  │
                                 │  └──────────────────────┘  │
                                 └────────────────────────────┘
```

## Cấu trúc thư mục

```
/data/zcloud/
├── src/zcloud/         # ⭐ Mã nguồn chính (Go project)
│   ├── go.mod
│   ├── cmd/zcloudd/main.go
│   ├── internal/
│   │   ├── core/       # Giao thức Zalo: mã hóa, đăng nhập, chat, websocket
│   │   ├── api/        # HTTP server: router, xử lý, ws
│   │   ├── store/      # Lưu dữ liệu: session, tin nhắn (local file + DB)
│   │   └── config.go
│   ├── web/            # Web UI (file tĩnh)
│   └── examples/
├── docs/               # ⭐ Tài liệu
│   ├── master-plan.md  # File này — tổng thể
│   ├── tasks.md        # Danh sách công việc + trạng thái
│   ├── tasks/          # Chi tiết từng công việc (00-06)
│   ├── protocol/       # Đặc tả API Zalo
│   ├── design/         # Tài liệu thiết kế Go
│   ├── references/     # Tham khảo: source code clone, ghi chép
│   └── database/       # Schema DB cho production
├── scripts/
│   └── re/             # Kịch bản RE (Node.js — tạm thời)
├── README.md
├── CHANGELOG.md
├── CLAUDE.md
├── AGENTS.md
├── SOUL.md
├── IDENTITY.md
├── USER.md
├── TOOLS.md
└── LICENSE
```

---

## Danh sách Công việc

| ID | Tên | Phụ thuộc | Trạng thái |
|----|-----|:---------:|:----------:|
| 00 | Thiết lập môi trường + Go project | — | 🔴 Chờ |
| 01 | Reverse Zalo Web API | 00 | 🔴 Chờ |
| 02 | Reverse Android Đồng bộ | *01-06 xong* | 🔴 Chờ (tùy chọn) |
| 03 | Thiết kế Core Protocol | 01 | 🔴 Chờ |
| 04 | Xây dựng Core Library (Go) | 01, 03 | 🔴 Chờ |
| 05 | Xây dựng Server Daemon | 04 | 🔴 Chờ |
| 06 | Xây dựng Web UI | 05 | 🔴 Chờ |

## Luồng thực thi

```
00 ──► 01 ──► 03 ──► 04 ──► 05 ──► 06
                                         └──► 02 (optional)
```

---

## Sub-plan cho từng Task

Mỗi task có sub-plan riêng tại `docs/tasks/<id>-<name>.md`. Các sub-plan liên kết với nhau qua:

- **Task 01 (Reverse):** output là `docs/protocol/*.md` — input cho task 03, 04
- **Task 03 (Design):** output là `docs/design/*.md` — input cho task 04
- **Task 04 (Core):** implement Go code `src/zcloud/internal/core/` — input cho task 05
- **Task 05 (Server):** implement Go code `src/zcloud/internal/api/` — input cho task 06
- **Task 06 (Web UI):** implement `src/zcloud/web/` — phụ thuộc 05

### Sub-task chi tiết

Xem tại `docs/tasks/`:
- [00-setup-env.md](tasks/00-setup-env.md) — Setup toolchain + Go project
- [01-reverse-web-api.md](tasks/01-reverse-web-api.md) — Reverse Zalo Web API
- [02-reverse-android-sync.md](tasks/02-reverse-android-sync.md) — Reverse Android (optional)
- [03-design-core.md](tasks/03-design-core.md) — Thiết kế Go interfaces
- [04-build-core.md](tasks/04-build-core.md) — Build Go core library
- [05-build-server.md](tasks/05-build-server.md) — Build Go server daemon
- [06-build-webui.md](tasks/06-build-webui.md) — Build web UI

---

## Database & Data Storage

### Local development (mặc định)
- Session: file JSON tại `$HOME/.zcloud/session/`
- Messages: chưa persist (in-memory)
- Config: env vars + CLI flags

### Production (khi có DB config)
- Session: có thể lưu DB nếu Sếp set env
- Messages: optional, DB schema tại `docs/database/schema.sql`
- Cơ chế: dùng interface `DataStore` — nếu không có DB → local file, nếu có → DB

---

## References

Các thư viện tham khảo (clone về tại `docs/references/`):
- **zca-js** (TypeScript, 567★): https://github.com/RFS-ADRENO/zca-js
- **zcago** (Go, 8★): https://github.com/Amrakk/zcago
- **Za-go** (Go, 64★): https://github.com/tranhaonguyendev/Za-go

---

## Verification Checklist

### Mỗi task khi hoàn thành
- [ ] Code compile/build không lỗi
- [ ] Test chạy qua
- [ ] Đã commit + push
- [ ] Đã cập nhật task docs (ghi chú, trạng thái)
- [ ] Đã cập nhật CHANGELOG.md
- [ ] Đã liên kết output với task kế tiếp

### Cuối dự án
- [ ] Tất cả task 00-06 hoàn thành
- [ ] zcloudd chạy được, browser truy cập được
- [ ] Login QR + chat real-time + history hoạt động
- [ ] Database schema ready
- [ ] All docs cập nhật
