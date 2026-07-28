# Tasks — zcloud (single source of truth)

> File này merge nội dung từ `master-plan.md` (kiến trúc + 4 mục tiêu) và
> `audit.md` (trạng thái chi tiết từng module + tồn đọng). Cập nhật
> 28/07/2026.

---

## 1. Mục tiêu dự án (4 mục tiêu chính)

1. **URL cho Sếp đăng nhập bằng QR code** — `http://zcloud.diepxuan.corp:8080`
2. **Chat real-time với user Zalo khác** — gửi/nhận qua core API + WS push
3. **Lưu lịch sử chat và media lâu dài** — SQLite messages + disk media
4. **Đồng bộ lịch sử theo chuẩn Zalo** — WebSocket cmd 510/511

**Chiến lược:** Dùng Web API (chat.zalo.me), Go cho toàn bộ logic.

---

## 2. Kiến trúc tổng thể

```
┌──────────────┐     HTTP/WS     ┌──────────────────────────────────┐     Zalo API     ┌──────────────┐
│  Browser A   │ ◄─────────────►│  zcloudd (Go daemon)             │◄──────────────►│ Zalo Web     │
│  (user A)    │                 │  :8080                            │  (chat.zalo.me) │ (user A)     │
├──────────────┤                 │  ┌─────────────────────────────┐ │                 ├──────────────┤
│  Browser B   │                 │  │ Multi-User Manager          │ │                 │ Zalo Web     │
│  (user B)    │                 │  │ ├── user A → core client A  │ │                 │ (user B)     │
└──────────────┘                 │  │ ├── user B → core client B  │ │                 └──────────────┘
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

Xem chi tiết thiết kế tại `docs/design.md`.

---

## 3. Trạng thái 14 tasks

| ID | Tên | Trạng thái | Chi tiết |
|:--:|-----|:----------:|----------|
| 00 | Thiết lập môi trường + Go project | 🟢 Xong | [00-setup-env.md](tasks/00-setup-env.md) |
| 01 | Reverse Zalo Web API | 🟢 Xong | [01-reverse-web-api.md](tasks/01-reverse-web-api.md) |
| 02 | Reverse Android Sync | 🕐 Tạm hoãn | [02-reverse-android-sync.md](tasks/02-reverse-android-sync.md) |
| 03 | Thiết kế Core Protocol | 🟢 Xong | [03-design-core.md](tasks/03-design-core.md) |
| 04 | Xây dựng Core Library (Go) | 🟢 Xong | [04-build-core.md](tasks/04-build-core.md) |
| 05 | Xây dựng Server Daemon | 🟢 Xong | [05-build-server.md](tasks/05-build-server.md) |
| 06 | Xây dựng Web UI | 🟢 Xong | [06-build-webui.md](tasks/06-build-webui.md) |
| 07 | Multi-user Manager | 🟢 Xong | [07-multi-user.md](tasks/07-multi-user.md) |
| 08 | Zalo OA Integration | 🕐 Tạm hoãn | [08-zalo-oa.md](tasks/08-zalo-oa.md) |
| 09 | Database & Media Store | 🟢 Xong | [09-database.md](tasks/09-database.md) |
| 10 | Sync lịch sử tin nhắn cũ | 🟢 Xong | [10-sync-history.md](tasks/10-sync-history.md) |
| 11 | GetFriends + tab Liên hệ | 🟢 Xong | [11-getfriends.md](tasks/11-getfriends.md) |
| 12 | Logout / đổi tài khoản | 🟢 Xong | [12-logout.md](tasks/12-logout.md) |
| 13 | Media download | 🟢 Xong | [13-media-download.md](tasks/13-media-download.md) |
| 14 | Tách Web UI ra file tĩnh | 🟢 Xong | [14-split-webui.md](tasks/14-split-webui.md) |

---

## 4. Audit chi tiết (cập nhật 28/07/2026)

### 4.1 Core Library (`internal/core/`)

| Module | File | Trạng thái | Ghi chú |
|--------|------|:----------:|---------|
| AES-128-CBC encrypt/decrypt | `encrypt.go` | ✅ | Có test, zero IV, PKCS7 padding |
| AES-GCM decrypt + WS event | `encrypt.go` | ✅ | `DecodeWSEvent` |
| Key generation | `encrypt.go` | ✅ | zcid, zcid_ext, deriveEncryptKey, sign key |
| QR login flow | `auth.go` | ✅ | 2 bước poll QR + cookie login |
| Cookie login | `auth.go` | ✅ | Inject cookie → getLoginInfo |
| Send message | `chat.go` | ✅ | POST form body + AES-CBC + zpw_ver/type |
| GetConversations | `chat.go` | ✅ | Sync conversations + resolveNames |
| GetMyProfile | `chat.go` | ✅ | Tên + avatar user |
| GetGroupInfo | `chat.go` | ✅ | Tên + avatar nhóm |
| GetGroupHistory | `chat.go` | ✅ | REST fallback `/api/cm/getrecentv2` |
| GetFriends | `chat.go` | ✅ | Parse fallback `{data: [...]}` + flat |
| WebSocket client | `websocket.go` | ✅ | Connect + cmd 501/521/510/511 |
| WS cmd 510/511 | `websocket.go` | ✅ | Request + handle old messages |
| Types & errors | `types.go`, `errors.go` | ✅ | Message, Conversation, Session, Event |

### 4.2 HTTP Server (`internal/api/`)

| API | File | Trạng thái |
|-----|------|:----------:|
| `GET /` Login page | `handlers.go` | ✅ |
| `GET /chat` Chat page | `handlers.go` | ✅ |
| `GET /api/qr/create` | `handlers.go` | ✅ |
| `POST /api/qr/poll` | `handlers.go` | ✅ |
| `POST /api/login/cookie` | `handlers.go` | ✅ |
| `GET /api/account` | `handlers.go` | ✅ |
| `GET /api/account/list` | `handlers.go` | ✅ |
| `GET /api/conversations` | `handlers.go` | ✅ |
| `GET /api/conversations/sync` | `handlers.go` | ✅ |
| `GET /api/messages` | `handlers.go` | ✅ |
| `POST /api/messages/send` | `handlers.go` | ✅ |
| `POST /api/messages/sync` | `handlers.go` | ✅ |
| `GET /api/friends` | `handlers.go` | ✅ |
| `POST /api/logout` | `handlers.go` | ✅ |
| `POST /api/media/download` | `router.go` | ✅ |
| `GET /media/` | `router.go` | ✅ |
| `GET /ws` Browser WS | `ws.go` | ✅ |

### 4.3 Database & Store (`internal/store/`)

| Bảng | Trạng thái |
|------|:----------:|
| accounts | ✅ Multi-user |
| sessions | ✅ Active/inactive, DeleteAccount |
| conversations | ✅ GetConversation |
| messages | ✅ Cursor paginate |
| media | ✅ + trường AI (ocr_text, ai_tags, ai_processed) |
| oa_configs | ✅ Schema sẵn |
| oa_webhook_logs | ✅ Schema sẵn |

### 4.4 Web UI (`internal/api/web/` — file tĩnh embed.FS)

| Tính năng | Trạng thái |
|-----------|:----------:|
| QR login | ✅ |
| Cookie login | ✅ |
| Danh sách hội thoại | ✅ |
| Load tin nhắn (cursor) | ✅ |
| Gửi tin nhắn | ✅ |
| Tên người gửi (resolveNames) | ✅ |
| Tab Liên hệ (Friends) | ✅ |
| Logout / Đổi tài khoản | ✅ |
| Sync tin nhắn cũ | ✅ |
| Search bỏ dấu tiếng Việt | ✅ |
| Logo ZCloud + favicon | ✅ |

---

## 5. Tồn đọng cần làm tiếp (theo thứ tự ưu tiên)

| # | Tính năng | Mức độ | Ghi chú |
|:-:|-----------|:------:|---------|
| T1 | WS AES-GCM decrypt hoàn chỉnh | 🟡 Medium | Có cipherKey nhưng chưa dùng |
| T2 | Auto-detect media trong WS event | 🟡 Medium | Hiện phải gọi API download thủ công |
| T3 | Integration test (chat/store/api) | 🟡 Medium | Chỉ có `encrypt_test.go` |
| T4 | Zalo OA webhook (task 08) | 🟢 Optional | Schema sẵn, thiếu handler |
| T5 | Logging tập trung | 🔵 Low | `fmt.Printf` lẫn `log.Printf` |
| T6 | Dọn `zcloudd` binary trong git history | 🔵 Low | Đã ignore, history cũ |

---

## 6. References

| Lib | Lang | Stars | Path |
|-----|------|-------|------|
| zca-js | TypeScript | 567★ | `docs/references/zca-js/` |
| zcago | Go | 8★ | `docs/references/zcago/` |
| Za-go | Go | 64★ | `docs/references/za-go/` |

---

## 7. Verification gần nhất (28/07/2026)

- `go build ./...` → PASS
- `go test ./...` → core PASS, các package khác "no test files"
- Working tree clean trên `main` @ `4f9ae0f` (docs sync), HEAD tổng `origin/main` = `4f9ae0f`.
