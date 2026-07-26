# Master Plan: zcloud

> **Mục tiêu:** Xây dựng cloud service Zalo với đầy đủ:
> 1. URL cho Sếp đăng nhập bằng QR code
> 2. Chat real-time với user Zalo khác
> 3. Lưu lịch sử chat và media lâu dài (SQLite + disk)
> 4. Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)
> 
> **Chiến lược:** Dùng Web API (chat.zalo.me), Go cho toàn bộ logic, đồng bộ Android là tùy chọn sau.
> **Chính sách push:** Em toàn quyền, push trực tiếp vào `main` sau mỗi subtask. Không cần review.
> **Hoàn thành dự án:** Khi đạt đủ 4 mục tiêu trên, dự án coi như hoàn tất.

---

## Kiến trúc tổng thể

```
┌──────────────┐     HTTP/WS     ┌──────────────────────────────────┐     Zalo API     ┌──────────────┐
│  Browser A   │ ◄─────────────►│  zcloudd (Go daemon)             │◄──────────────►│ Zalo Web     │
│  (user A)    │                 │  :8080                            │  (chat.zalo.me) │ (user A)     │
├──────────────┤                 │  ┌─────────────────────────────┐ │                 ├──────────────┤
│  Browser B   │                 │  │ Multi-User Manager          │ │                 │ Zalo Web     │
│  (user B)    │                 │  │ ├── user A → core client A  │ │                 │ (user B)     │
├──────────────┤                 │  │ ├── user B → core client B  │ │                 ├──────────────┤
│  Zalo OA     │◄──Webhook─────►│  │ └── ...                     │ │                 │ Zalo OA API  │
│  webhook     │                 │  │ ┌─────────────────────────┐ │ │                 │              │
└──────────────┘                 │  │ │ Zalo OA Processor       │ │ │                 └──────────────┘
                                 │  │ │ - xác thực webhook      │ │ │
                                 │  │ │ - xử lý event OA        │ │ │
                                 │  │ │ - gửi tin qua API OA    │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  │ ┌─────────────────────────┐ │ │
                                 │  │ │ Core (logic Zalo user)  │ │ │
                                 │  │ │ - encrypt, auth, chat   │ │ │
                                 │  │ │ - websocket             │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  │ ┌─────────────────────────┐ │ │
                                 │  │ │ Store (DB + Media)      │ │ │
                                 │  │ │ - SQLite (multi-user)   │ │ │
                                 │  │ │ - media files on disk   │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  └─────────────────────────────┘ │
                                 └──────────────────────────────────┘
```

## Cấu trúc thư mục

```
/data/zcloud/
├── src/zcloud/         # ⭐ Mã nguồn chính (Go project)
│   ├── go.mod
│   ├── cmd/zcloudd/main.go
│   ├── internal/
│   │   ├── core/       # Core Zalo user: encrypt, auth, chat, websocket
│   │   ├── oa/         # Zalo OA: xác thực webhook, xử lý event, gửi tin
│   │   ├── user/       # Multi-user manager — quản lý N user
│   │   ├── api/        # HTTP server: router, handlers, ws
│   │   ├── store/      # Storage: SQLite + media files
│   │   └── config/     # Cấu hình
│   ├── web/            # Web UI (file tĩnh)
│   └── examples/
├── docs/
│   ├── master-plan.md  # File này — tổng thể
│   ├── tasks.md        # Danh sách công việc + trạng thái
│   ├── tasks/          # Chi tiết từng công việc
│   ├── protocol/       # Đặc tả API Zalo
│   ├── design/         # Tài liệu thiết kế
│   ├── references/     # Source tham khảo
│   └── database/       # Schema DB
├── scripts/
│   └── re/             # Kịch bản RE (Node.js — tạm)
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
| 00 | Thiết lập môi trường + Go project | — | 🟢 Xong |
| 01 | Reverse Zalo Web API | 00 | 🟢 Xong |
| 02 | Reverse Android Đồng bộ | *01-06 xong* | 🟡 Tạm hoãn |
| 03 | Thiết kế Core Protocol | 01 | 🟢 Xong |
| 04 | Xây dựng Core Library (Go) | 01, 03 | 🟢 Xong |
| 05 | Xây dựng Server Daemon | 04 | 🟢 Xong |
| 06 | Xây dựng Web UI | 05 | 🟡 Đang làm |
| 07 | Multi-user Manager | 04 | 🟢 Xong |
| 08 | Zalo OA Integration | 04 | 🔴 Chờ |
| 09 | Database & Media Store | 04 | 🟢 Xong |

## Luồng thực thi

```
00 ──► 01 ──► 03 ──► 04 ──► 05 ──► 06
               │              │
               ▼              ▼
               07 ───────────► 08 (Zalo OA)
               
Luồng song song: 07 (multi-user) chạy cùng lúc với 04, không block 05.
08 (Zalo OA) có thể làm sau khi 04 + 07 ổn định.
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
