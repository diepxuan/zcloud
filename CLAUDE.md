# CLAUDE.md — Hướng dẫn cho Claude Code trong dự án zcloud

## Repository

| Thuộc tính | Giá trị |
|------------|---------|
| Tên | zcloud |
| Mô tả | Cloud service Zalo (chat.zalo.me) |
| License | MIT — `LICENSE` (Bản quyền 2026 DXVN) |
| Workspace | `/root/.openclaw/workspace/projects/zcloud/` |

## Công nghệ sử dụng

| Thành phần | Công nghệ | Vị trí |
|-----------|-----------|--------|
| Thư viện core | Go 1.22+ | `src/zcloud/internal/core/` |
| HTTP server | Go net/http | `src/zcloud/internal/api/` |
| WebSocket | `github.com/coder/websocket` | `src/zcloud/internal/core/` |
| Web UI | Vanilla JS (ES6+ modules) | `src/zcloud/web/` |
| Lưu session | File JSON mã hóa / DB | `src/zcloud/internal/store/` |

## Nhận dạng & Quy ước

Theo `SOUL.md §4` thứ tự boot:
`SOUL.md` → `USER.md` → `IDENTITY.md` → `TOOLS.md` → `docs/master-plan.md` → `docs/tasks.md` → `docs/tasks/<công-việc>.md`

| File | Vai trò |
|------|---------|
| `docs/master-plan.md` | Master plan tổng thể — đọc trước khi làm bất kỳ công việc nào |
| `docs/tasks.md` | Danh sách công việc + trạng thái |
| `docs/tasks/<id>-<tên>.md` | Chi tiết công việc — sub-plan, kiểm tra |
| `docs/protocol/` | Đặc tả API Zalo |
| `docs/design/` | Tài liệu thiết kế Go |
| `docs/database/` | Schema DB cho production |
| `docs/references/` | Source tham khảo (zca-js, zcago, Za-go) |

## Mục tiêu dự án (theo master-plan.md)

1. ✅ URL cho Sếp đăng nhập bằng QR code
2. ✅ Chat real-time với user Zalo khác
3. ✅ Lưu lịch sử chat và media lâu dài (SQLite + disk)
4. ✅ Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)

Hoàn thành 4 mục tiêu = dự án hoàn tất. Xem chi tiết tại `docs/master-plan.md`.

## Nguyên tắc làm việc

1. **Mỗi công việc** = đọc master plan → đọc chi tiết công việc → code → kiểm tra → commit → push
2. **Go là source chính thức** — toàn bộ logic reverse (mã hóa, đăng nhập, chat, websocket)
3. **Node.js** chỉ dùng tạm ở `scripts/re/` — không đưa vào production
4. **Chính sách push:** nhánh `main`, push trực tiếp sau mỗi subtask. Không tạo PR.
5. **CSDL:** File local mặc định. Nếu Sếp set env DB → dùng SQLite. Schema tại `docs/database/schema.sql`
6. **Kiểm tra liên kết:** Mỗi công việc xong → xem output có đúng spec không → cập nhật công việc kế tiếp

## Quy tắc cốt lõi (SOUL.md §2-3)

- Ngôn ngữ: **chỉ tiếng Việt**. Xưng hô Sếp / em / đệ
- Phong cách: nhanh, gọn, chính xác — không emoji, không rườm rà
- **Không bịa thông tin** — phải từ nguồn chính thức
- **Không tạo/sửa schema** nếu không được yêu cầu
- Báo cáo phải kèm **bằng chứng**: file đổi, kết quả test

## Mã hóa Zalo (đã xác nhận)

- AES-128-CBC (IV zero) — **không phải AES-ECB** như tài liệu cũ
- Khóa: Base64-giải mã `zpw_enk` từ đăng nhập
- Chữ ký: MD5("zsecure" + loại + các tham số đã sắp xếp)
- WebSocket event: AES-GCM + giải nén gzip

## Tham khảo (clone sẵn tại `docs/references/`)

| Thư viện | Ngôn ngữ | Sao |
|----------|:--------:|:---:|
| zca-js | TypeScript | 567★ |
| zcago | Go | 8★ |
| Za-go | Go | 64★ |

## Giới hạn đỏ (từ AGENTS.md)

- Không tự ý push/tạo PR/merge/phục hồi — chỉ push khi Sếp nói rõ
- Không sửa config hệ thống (crontab, systemd, nginx) khi chưa hỏi
- `trash > rm`. Khi nghi ngờ → hỏi Sếp

## Quy tắc vận hành (NGHIÊM TÚC)

- **KHÔNG BAO GIỜ start daemon thủ công.** Dự án dùng systemd service (`zcloud.service`) + `scripts/zcloudd.sh` watch mode. Khi code thay đổi, service tự rebuild + restart. Chạy tay gây conflict port, crash service, làm Sếp không vào được web.
- **Khi cần restart:** chỉ dùng `systemctl restart zcloud` hoặc `./scripts/zcloud.sh restart`. Không kill process tay, không start binary trực tiếp.
- **Trước khi động vào hệ thống:** kiểm tra process tree (`ps aux | grep zcloud`), port (`ss -tlnp | grep 8080`), service status (`systemctl status zcloud`). Nếu service đang chạy, DÙNG NÓ.
- **Khi có lỗi:** báo cáo Sếp nguyên nhân + bằng chứng (log, code). Không tự ý kill process, xoá DB, rename script để "fix nhanh".
- **Không xoá DB** trừ khi Sếp yêu cầu cụ thể.

## Kỷ luật Git

- Mỗi subtask = 1 commit. **Push ngay vào `main`**, không cần review.
- **Không force push.**
- Định dạng commit: `<loại>(<phạm vi>): <mô tả>`
  - `feat(core): thêm mã hóa AES-128-CBC`
  - `docs(protocol): thêm đặc tả WebSocket`
  - `fix(api): xử lý session hết hạn`
