# Tasks — Reverse Zalo & Build zcloud

| ID | Tên | Trạng thái | Ghi chú |
|----|-----|:----------:|---------|
| 00 | Thiết lập môi trường + Go project | 🟢 Xong | Go 1.22, module, thư mục |
| 01 | Reverse Zalo Web API | 🟢 Xong | AES-128-CBC, login, REST, WS (từ source tham khảo) |
| 02 | Reverse Android Sync | 🕐 Tạm hoãn | Web API WS cmd 510/511 là đủ, không cần Android |
| 03 | Thiết kế Core Protocol | 🟢 Xong | types, errors, interfaces |
| 04 | Xây dựng Core Library (Go) | 🟢 Xong | encrypt, auth, chat, websocket |
| 05 | Xây dựng Server Daemon | 🟢 Xong | REST API + WS trên 8080 |
| 06 | Xây dựng Web UI | 🟢 Xong | Login page QR/cookie, Chat page |
| 07 | Multi-user Manager | 🟢 Xong | accounts + sessions trong DB |
| 08 | Zalo OA Integration | 🕐 Tạm hoãn | Schema DB sẵn, logic chưa cần |
| 09 | Database & Media Store | 🟢 Xong | SQLite + disk cho media |
| 10 | **Hoàn thiện UI/UX Chat** | 🟡 Đang làm | Fix load message, hiển thị avatar/tên, search |
| 11 | **Media download** | 🔴 Chờ | Download file từ Zalo về local |
| 12 | **Logout / quản lý session** | 🔴 Chờ | Xoá session, chuyển tài khoản |
| 13 | **Lịch sử tin nhắn cũ (pull API)** | 🔴 Chờ | REST API sync messages lịch sử |

## Mục tiêu dự án (master-plan.md)

1. ✅ **URL login QR** — `http://zcloud.diepxuan.corp:8080`
2. ✅ **Chat real-time** — Gửi/nhận qua core API + WebSocket push
3. ✅ **Lưu lịch sử + media** — SQLite messages + disk media
4. ✅ **Đồng bộ lịch sử Zalo** — WebSocket listener nền, lưu real-time
