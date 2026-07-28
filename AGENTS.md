# AGENTS.md — Workspace Zcloud

> Project zcloud. Đây là workspace của agent Bột.

## Trình tự khởi động
Mỗi session startup, đọc theo thứ tự:
1. `SOUL.md` — bản sắc, nguyên tắc vận hành
2. `USER.md` — thông tin Sếp Duc Tran
3. `IDENTITY.md` — chi tiết identity dự án
4. `TOOLS.md` — ghi chép local
5. `docs/tasks.md` — **danh sách task + trạng thái (single source of truth)**
6. `docs/design.md` — **thiết kế kiến trúc + quy ước code (đọc trước khi code)**
7. `docs/tasks/<công-việc-cần-làm>.md` — **chi tiết công việc**
8. `MEMORY.md` — trí nhớ dài hạn
9. `memory/<hôm-nay>.md` — daily log (nếu có)

## Dự án: zcloud
- **Mục tiêu:** Xây dựng cloud service Zalo đầy đủ (xem `docs/tasks.md` §1):
  1. ✅ Có URL cho Sếp đăng nhập bằng QR code
  2. ✅ Có thể chat real-time với user Zalo khác
  3. ✅ Lưu lịch sử chat và media lâu dài (SQLite + disk)
  4. ✅ Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)
- **Source code:** `src/zcloud/` (Go module `github.com/diepxuan/zcloud`)
- **Task list + master plan + audit:** `docs/tasks.md`
- **Thiết kế:** `docs/design.md`
- **Chính sách push:** Em toàn quyền quyết định, push trực tiếp vào `main` sau mỗi subtask. Không cần review.
- **Công nghệ:** Go core + server, SQLite + disk storage, vanilla JS web UI

## Cấu trúc thư mục
- `src/zcloud/` — Source code chính
- `docs/` — Tài liệu: tasks, design, schema, references, tasks/<id>.md
- `scripts/` — Service manager (zcloud.sh + zcloudd.sh watch mode)
- `docs/references/` — Source tham khảo (zca-js, zcago, Za-go)

## Lưu ý cho các session sau
- Đã reverse Zalo Web API (mã hóa, đăng nhập, REST, WebSocket) — xem `docs/design.md`
- Các thư viện tham khảo có sẵn tại `docs/references/`
- Công việc 02 (Android sync) đã tạm hoãn — không cần làm
- Công việc cần làm kế tiếp: trong `docs/tasks.md` §5 "Tồn đọng cần làm tiếp"
- **Mỗi lần code xong 1 subtask → commit + push vào `main` ngay, không cần hỏi**

## Giới hạn đỏ
- Không sửa schema khi chưa được yêu cầu
- Trước khi sửa config hệ thống → kiểm tra trước + merge, không overwrite
- `trash > rm`
- Khi nghi ngờ → hỏi Sếp

## Trí nhớ
- Ghi chú hàng ngày: `memory/YYYY-MM-DD.md`
- Dài hạn: `MEMORY.md` (chỉ main session)
- Ghi lại quyết định, ngữ cảnh, bài học — không ghi bí mật
