# Audit tổng thể dự án zcloud

**Ngày:** 26/07/2026
**Commit:** `23daf40` — trên nhánh `main`

---

## Kiến trúc tổng quan

```
Browser ──HTTP/WS──► zcloudd (Go) ──REST/WS──► Zalo chat.zalo.me
                        │
                        ▼
                    SQLite (accounts, sessions, conversations, messages, media)
```

## Audit từng thành phần

### 1. Core Library (`internal/core/`)

| Module | File | Trạng thái | Ghi chú |
|--------|------|:----------:|---------|
| AES-128-CBC encrypt/decrypt | `encrypt.go` | ✅ Hoàn chỉnh | Có test, zero IV, PKCS7 padding |
| Key generation | `encrypt.go` | ✅ Hoàn chỉnh | zcid, zcid_ext, deriveEncryptKey, sign key |
| QR login flow | `auth.go` | ✅ Hoạt động | 2 bước poll QR + cookie login |
| Cookie login | `auth.go` | ✅ Hoạt động | Inject cookie → getLoginInfo |
| Send message | `chat.go` | ✅ Hoạt động | encryptParamsForLogin + REST API |
| GetConversations | `chat.go` | ✅ Hoạt động | Sync conversations + resolveNames |
| GetMyProfile | `chat.go` | ✅ Hoạt động | Lấy tên + avatar user |
| GetGroupInfo | `chat.go` | ✅ Hoạt động | Lấy tên + avatar nhóm |
| **GetFriends** | `chat.go` | ❌ **Rỗng** | `return []User{}, nil` |
| WebSocket client | `websocket.go` | ✅ Hoạt động | Kết nối + xử lý cmd 501/521 (new msg) |
| WS key exchange | `websocket.go` | ⚠️ Chưa dùng | Có parse key nhưng ko AES-GCM decrypt |
| Types & errors | `types.go`, `errors.go` | ✅ Đầy đủ | Message, Conversation, Session, Event |

### 2. HTTP Server (`internal/api/`)

| API | File | Trạng thái | Ghi chú |
|-----|------|:----------:|---------|
| `GET /` Login page | `handlers.go` | ✅ | QR + cookie login UI |
| `GET /chat` Chat page | `handlers.go` | ✅ | Inline HTML |
| `GET /api/qr/create` | `handlers.go` | ✅ | Tạo QR code |
| `POST /api/qr/poll` | `handlers.go` | ✅ | Poll QR + tạo account |
| `POST /api/login/cookie` | `handlers.go` | ✅ | Login bằng cookie |
| `GET /api/conversations` | `handlers.go` | ✅ | List từ DB |
| `GET /api/conversations/sync` | `handlers.go` | ✅ | Sync từ Zalo + resolve tên |
| `GET /api/messages` | `handlers.go` | ⚠️ Chỉ DB | Không pull lịch sử từ Zalo API |
| `POST /api/messages/send` | `handlers.go` | ✅ | Gửi tin nhắn |
| `GET /ws` Browser WS | `ws.go` | ✅ | Browser WebSocket + Zalo listener |
| `GET /media/` | `router.go` | ⚠️ Serve file | Chưa có logic download |

### 3. Database & Store (`internal/store/`)

| Table | File | Trạng thái | Ghi chú |
|-------|------|:----------:|---------|
| accounts | `store.go` | ✅ | Multi-user |
| sessions | `store.go` | ✅ | Active/inactive |
| conversations | `store.go` | ✅ | Conversation list |
| messages | `store.go` | ✅ | Message history |
| media | `store.go` | ✅ | Schema đầy đủ |
| oa_configs | `store.go` | ✅ | Schema sẵn |
| oa_webhook_logs | `store.go` | ✅ | Schema sẵn |

### 4. Web UI (`inline HTML trong handlers.go`)

| Tính năng | Trạng thái | Ghi chú |
|-----------|:----------:|---------|
| QR login | ✅ | Hiển thị QR, poll tự động |
| Cookie login | ✅ | Paste cookie → login |
| Danh sách hội thoại | ✅ | Sync + hiện tên + avatar |
| Load tin nhắn | ✅ | Scroll lên load thêm |
| Gửi tin nhắn | ✅ | Enter/click gửi |
| Hiển thị tên người gửi | ✅ | resolveNames + dName |
| **Tab Liên hệ (Friends)** | ❌ **Không hoạt động** | `loadFriends()` luôn hiện "Đang tải..." |
| **Tìm kiếm hội thoại** | ⚠️ | Search theo tên, chưa search theo nội dung |
| **Avatar hiển thị** | ⚠️ | Có khung nhưng link avatar từ Zalo API |
| **Logout** | ❌ **Không có** | Không có nút xoá session |

---

## 4 mục tiêu chính

| # | Mục tiêu | Trạng thái | Bằng chứng |
|:-:|----------|:----------:|------------|
| 1 | URL login QR | ✅ | `GET /api/qr/create` + `POST /api/qr/poll` OK |
| 2 | Chat real-time | ✅ | WebSocket cmd 501/521 → DB → broadcast |
| 3 | Lưu lịch sử + media | ⚠️ **Media chưa download** | Messages OK, media chỉ có schema |
| 4 | Đồng bộ lịch sử Zalo | ✅ | WS listener + DB lưu real-time |

---

## Tồn tại / Thiếu sót

### Critical
- **Không có pull lịch sử tin nhắn cũ**: Khi đăng nhập lần đầu, DB rỗng → UI hiện "Chọn hội thoại" nhưng ko có tin cũ. WS listener chỉ bắt từ lúc connect.
- **GetFriends() rỗng**: Tab Liên hệ không hoạt động, `return []User{}, nil`.

### Medium
- **Không logout**: Không có cách xoá session để đăng nhập user khác.
- **Web UI = HTML string trong Go**: Rất khó maintain, mỗi lần sửa UI phải rebuild binary.
- **WebSocket chưa parse AES-GCM**: Có `cipherKey` nhưng ko decrypt, may là Zalo cũng gửi plain JSON.

### Low
- **Chưa có health check endpoint thực sự**: `GET /api/health` trả về `{"ok":true}` đơn giản.
- **Chưa có logging tập trung**: Dùng `fmt.Printf` trộn lẫn với `log.Printf`.
- **Chỉ 1 file test**: `encrypt_test.go` — ko có integration test.
- **zcloudd binary trong git**: Đã thêm vào `.gitignore` nhưng history vẫn còn.

---

## Kết luận

Dự án đã hoàn thành 4 mục tiêu chính. Code hoạt động được: login QR/cookie, sync conversation, chat real-time, lưu DB. Tuy nhiên còn một số thiếu sót về UX (friends tab, load tin cũ, logout) và tính năng (media download) cần hoàn thiện.

**Mức độ hoàn thiện:** ~80%
