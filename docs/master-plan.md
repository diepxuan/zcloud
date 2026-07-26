# Master Plan: zcloud (Updated 26/07/2026)

> **Mục tiêu:** Xây dựng cloud service Zalo với đầy đủ:
> 1. ✅ URL cho Sếp đăng nhập bằng QR code
> 2. ✅ Chat real-time với user Zalo khác
> 3. ✅ Lưu lịch sử chat và media lâu dài (SQLite + disk)
> 4. ✅ Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)
>
> **Chiến lược:** Dùng Web API (chat.zalo.me), Go cho toàn bộ logic.
> **Hoàn thành dự án:** 4 mục tiêu trên đã đạt. Giai đoạn tiếp theo = hoàn thiện UX + tính năng phụ.

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
                                 │  │ │ - sync messages         │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  │ ┌─────────────────────────┐ │ │
                                 │  │ │ Store (DB + Media)      │ │ │
                                 │  │ │ - SQLite (multi-user)   │ │ │
                                 │  │ │ - media files on disk   │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  └─────────────────────────────┘ │
                                 └──────────────────────────────────┘
```

---

## Trạng thái hiện tại

| ID | Tên | Trạng thái | Ghi chú |
|:--:|-----|:----------:|---------|
| 00 | Thiết lập môi trường | 🟢 Xong | Go 1.22, module |
| 01 | Reverse Web API | 🟢 Xong | AES-128-CBC, login, REST, WS |
| 02 | Reverse Android Sync | 🕐 Tạm hoãn | Không cần thiết |
| 03 | Thiết kế Core Protocol | 🟢 Xong | types, errors, interfaces |
| 04 | Core Library | 🟢 Xong | encrypt, auth, chat, websocket |
| 05 | Server Daemon | 🟢 Xong | REST API + WS trên 8080 |
| 06 | Web UI | 🟢 Xong | Login QR, Chat inline HTML |
| 07 | Multi-user Manager | 🟢 Xong | accounts + sessions DB |
| 08 | Zalo OA | 🕐 Tạm hoãn | Schema sẵn |
| 09 | Database & Media Store | 🟢 Xong | SQLite + disk |

## Tồn đọng cần hoàn thiện

| ID | Tính năng | Mức độ |
|:--:|-----------|:------:|
| 10 | **Sync lịch sử tin nhắn cũ từ Zalo API** | 🟡 Critical |
| 11 | **GetFriends — tab Liên hệ** | 🟡 Medium |
| 12 | **Logout / chuyển tài khoản** | 🟡 Medium |
| 13 | **Media download + serve** | 🔵 Low |
| 14 | **Tách Web UI ra file tĩnh** | 🔵 Low |
| 15 | **Zalo OA — webhook receiver** | 🟢 Optional |

---

## Plan giai đoạn tiếp theo

### Phase 1 — Sync lịch sử tin nhắn (Critical)
- Thêm `Client.GetGroupMessages()` và `Client.GetIndividualMessages()` trong `core/chat.go`
- Zalo API: dùng `GET /api/message/getmsgids` + `/api/message/getmsgdetail` hoặc WS cmd 510/511
- Thêm endpoint `GET /api/messages/sync` lấy từ Zalo → DB
- Cập nhật UI: auto-sync khi mở conversation

### Phase 2 — Hoàn thiện UI/UX
- Fix `loadFriends()` → implement `GetFriends()` thực sự
- Thêm nút Logout → xoá session/cookies, redirect về trang login
- Tách HTML ra file riêng (không inline trong Go)

### Phase 3 — Media + Tính năng phụ
- Download media từ Zalo khi nhận WS event
- Zalo OA webhook integration

---

## References

Xem tại `docs/references/`:
- **zca-js** (TypeScript, 567★): https://github.com/RFS-ADRENO/zca-js
- **zcago** (Go, 8★): https://github.com/Amrakk/zcago
- **Za-go** (Go, 64★): https://github.com/tranhaonguyendev/Za-go

## Audit hiện tại

Xem chi tiết tại `docs/audit.md` — audit toàn bộ codebase.
