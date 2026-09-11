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
  3. ✅ Lưu lịch sử chat và media lâu dài (PostgreSQL + disk)
  4. ✅ Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)
- **Source code:** `src/zcloud/` (Go module `github.com/diepxuan/zcloud`)
- **Task list + master plan + audit:** `docs/tasks.md`
- **Thiết kế:** `docs/design.md`
  - **Phần A**: Kiến trúc + quy ước code (đọc trước khi sửa backend).
  - **Phần B**: Design system — tokens, components, layout (đọc trước khi sửa UI).
  - **Phần C**: Tham chiếu.
- **Chính sách push:** Em toàn quyền quyết định, push trực tiếp vào `main` sau mỗi subtask. Không cần review.
- **Công nghệ:** Go core + server, PostgreSQL (pgx) + disk storage, vanilla JS web UI

## Cấu trúc thư mục
- `src/zcloud/` — Source code chính
- `docs/` — Tài liệu: tasks, design, schema, references, tasks/<id>.md
- `scripts/` — Service manager (zcloud.sh + zcloudd.sh watch mode)
- `scripts/` — Service manager (đã port vào `zcloudd serv` — script bash cũ trong `tmp/scratch/trash/`)
- `docs/references/` — Source tham khảo (zca-js, zcago, Za-go)

## Service manager (`zcloudd serv`)
Quản lý service đã được port từ bash script vào binary. CLI shape:
- `zcloudd` — chạy HTTP server (foreground)
- `zcloudd serv` — chạy HTTP server foreground (cho systemd ExecStart)
- `zcloudd serv watch` — watch + auto-rebuild + restart server (dev)
- `zcloudd serv start|stop|restart` — systemctl wrapper
- `zcloudd serv status` — trạng thái systemd + port
- `zcloudd serv logs [-f]` — journalctl -u zcloud
- `zcloudd serv install` — (re)generate `/etc/systemd/system/zcloud.service`
- `zcloudd tui` — terminal UI (mockup, sắp triển khai)
- Config (DB password, port, domain...) đọc từ `~/.config/ductn/zcloud.yml`
- Script bash cũ `scripts/zcloud.sh` + `scripts/zcloudd.sh` đã được chuyển vào `tmp/scratch/trash/`

## Lưu ý cho các session sau
- Đã reverse Zalo Web API (mã hóa, đăng nhập, REST, WebSocket) — xem `docs/design.md`
- Các thư viện tham khảo có sẵn tại `docs/references/`
- Công việc 02 (Android sync) đã tạm hoãn — không cần làm
- Công việc cần làm kế tiếp: trong `docs/tasks.md` §5 "Tồn đọng cần làm tiếp"
- **Mỗi lần code xong 1 subtask → commit + push vào `main` ngay, không cần hỏi**

## Quy ước khi sửa Web UI (`internal/api/web/`)

1. **Đọc `docs/design.md` Phần B** trước khi sửa HTML/CSS.
2. **Dùng CSS variables** (`var(--c-brand)`, `var(--s-4)`, `var(--r-md)`...). KHÔNG hardcode màu/spacing.
3. **Component lặp lại > 2 lần** → tách thành class dùng chung.
4. **Mobile-first**: layout phải responsive từ 360px.
5. **Không load dependency ngoài** (CDN, font, framework) — vanilla JS thuần, embed trong binary.
6. **Tiếng Việt** cho user-facing text, **tiếng Anh** cho code/identifier.
7. **Không tự ý thay đổi design tokens** (`--c-brand`, spacing scale...) — hỏi Sếp.
8. **Build verify** sau khi sửa: `cd src/zcloud && go build ./...` phải pass.
9. **Smoke test UI**: mở `/chat` + `/` qua browser, kiểm tra render.

## Giới hạn đỏ
- Không sửa schema khi chưa được yêu cầu
- Trước khi sửa config hệ thống → kiểm tra trước + merge, không overwrite
- `trash > rm`
- Khi nghi ngờ → hỏi Sếp

## Trí nhớ
- Ghi chú hàng ngày: `memory/YYYY-MM-DD.md`
- Dài hạn: `MEMORY.md` (chỉ main session)
- Ghi lại quyết định, ngữ cảnh, bài học — không ghi bí mật
