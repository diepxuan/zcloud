# Tasks — Reverse Zalo & Build zcloud

| ID | Tên | Trạng thái | Ghi chú |
|----|-----|:----------:|---------|
| 00 | Thiết lập môi trường + Go project | 🟢 Xong | Go 1.22, module, thư mục |
| 01 | Reverse Zalo Web API | 🟢 Xong | AES-128-CBC, login, REST, WS |
| 02 | Reverse Android Sync | 🕐 Tạm hoãn | Web API WS cmd 510/511 là đủ |
| 03 | Thiết kế Core Protocol | 🟢 Xong | types, errors, interfaces |
| 04 | Xây dựng Core Library (Go) | 🟢 Xong | encrypt, auth, chat, websocket |
| 05 | Xây dựng Server Daemon | 🟢 Xong | REST API + WS trên 8080 |
| 06 | Xây dựng Web UI | 🟢 Xong | Login QR/cookie, Chat page |
| 07 | Multi-user Manager | 🟢 Xong | accounts + sessions trong DB |
| 08 | Zalo OA Integration | 🕐 Tạm hoãn | Schema DB sẵn |
| 09 | Database & Media Store | 🟢 Xong | SQLite + disk cho media |
| 10 | **Sync lịch sử tin nhắn cũ** | 🟢 Xong | WS cmd 510/511 + REST trigger |
| 11 | **GetFriends + tab Liên hệ** | 🟢 Xong | API Zalo + UI tab contacts |
| 12 | **Logout / đổi tài khoản** | 🟢 Xong | Xoá session + redirect |
| 13 | **Media download** | 🟢 Xong | API download từ Zalo URL |
| 14 | **Tách Web UI ra file tĩnh** | 🟢 Xong | embed.FS, .html riêng |

## Mục tiêu dự án (master-plan.md)

1. ✅ **URL login QR** — `http://zcloud.diepxuan.corp:8080`
2. ✅ **Chat real-time** — Gửi/nhận qua core API + WebSocket push
3. ✅ **Lưu lịch sử + media** — SQLite messages + disk media
4. ✅ **Đồng bộ lịch sử Zalo** — WebSocket listener + cmd 510/511
