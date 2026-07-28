# CLAUDE.md — Hướng dẫn cho Claude Code trong dự án zcloud

## Repository

| Thuộc tính | Giá trị |
|------------|---------|
| Tên | zcloud |
| Mô tả | Cloud service Zalo (chat.zalo.me) |
| License | MIT — `LICENSE` (Bản quyền 2026 DXVN) |
| Workspace | `/data/zcloud` |

## Công nghệ sử dụng

| Thành phần | Công nghệ | Vị trí |
|-----------|-----------|--------|
| Core | Go 1.22+, AES-128-CBC + AES-GCM | `src/zcloud/internal/core/` |
| HTTP server | `net/http` (Go 1.22 pattern routing) | `src/zcloud/internal/api/` |
| WebSocket (browser) | native `net/http` hijacker | `src/zcloud/internal/api/ws.go` |
| WebSocket (Zalo) | native + frame parser | `src/zcloud/internal/core/websocket.go` |
| Storage | SQLite (`modernc.org/sqlite`, pure Go) | `src/zcloud/internal/store/` |
| Web UI | Vanilla JS ES6+, HTML/CSS thuần | `src/zcloud/internal/api/web/` |
| Embed | `go:embed web/*` | `src/zcloud/internal/api/embed.go` |

## Nhận dạng & Quy ước

Theo `SOUL.md §4` thứ tự boot:
`SOUL.md` → `USER.md` → `IDENTITY.md` → `TOOLS.md` → `docs/tasks.md` →
`docs/tasks/<công-việc>.md` → `MEMORY.md` → `memory/<hôm-nay>.md`

| File | Vai trò |
|------|---------|
| `docs/tasks.md` | **Single source of truth** — master plan + audit + trạng thái |
| `docs/design.md` | Thiết kế kiến trúc + quy ước code (đọc trước khi code) |
| `docs/tasks/<id>-<tên>.md` | Chi tiết từng task (sub-plan, kiểm tra) |
| `docs/database/schema.sql` | Schema DB — sync từ `store.go` |
| `docs/references/` | Source tham khảo (zca-js, zcago, Za-go) |
| `MEMORY.md` | Long-term memory (main session) |
| `memory/YYYY-MM-DD.md` | Daily log |

## Mục tiêu dự án

1. ✅ URL cho Sếp đăng nhập bằng QR code
2. ✅ Chat real-time với user Zalo khác
3. ✅ Lưu lịch sử chat và media lâu dài (SQLite + disk)
4. ✅ Đồng bộ lịch sử theo chuẩn Zalo (WebSocket cmd 510/511)

Trạng thái chi tiết từng task + module xem `docs/tasks.md`.

## Nguyên tắc làm việc

1. **Mỗi task** = đọc `docs/tasks.md` → đọc chi tiết task → code → kiểm tra → commit → push.
2. **Go là source chính thức** — toàn bộ logic reverse (mã hóa, đăng nhập, chat, WS).
3. **Node.js** chỉ dùng tạm ở `scripts/re/` — không đưa vào production.
4. **Chính sách push:** nhánh `main`, push trực tiếp sau mỗi subtask. Không tạo PR.
5. **CSDL:** SQLite qua `modernc.org/sqlite`. Env `ZCLOUD_DB_PATH` override.
6. **Kiểm tra liên kết:** mỗi task xong → xem output đúng spec → cập nhật task kế tiếp.

## Quy tắc cốt lõi (SOUL.md §2-3)

- Ngôn ngữ: **chỉ tiếng Việt**. Xưng hô Sếp / em / đệ.
- Phong cách: nhanh, gọn, chính xác — không emoji, không rườm rà.
- **Không bịa thông tin** — phải từ nguồn chính thức.
- **Không tạo/sửa schema** nếu không được yêu cầu.
- Báo cáo phải kèm **bằng chứng**: file đổi, kết quả test.

## Mã hóa Zalo (đã xác nhận)

- AES-128-CBC (IV zero) — **không phải AES-ECB** như tài liệu cũ.
- Khóa: Base64-giải mã `zpw_enk` từ đăng nhập.
- Chữ ký: MD5("zsecure" + loại + các tham số đã sắp xếp).
- WebSocket event: AES-GCM + giải nén gzip (khi Zalo gửi cipher).

## Tham khảo (clone sẵn tại `docs/references/`)

| Thư viện | Ngôn ngữ | Sao |
|----------|:--------:|:---:|
| zca-js | TypeScript | 567★ |
| zcago | Go | 8★ |
| Za-go | Go | 64★ |

## Giới hạn đỏ (từ AGENTS.md)

- Không tự ý push/tạo PR/merge/phục hồi — chỉ push khi Sếp nói rõ.
- Không sửa config hệ thống (crontab, systemd, nginx) khi chưa hỏi.
- `trash > rm`. Khi nghi ngờ → hỏi Sếp.

## Quy tắc vận hành (NGHIÊM TÚC)

- **KHÔNG BAO GIỜ start daemon thủ công.** Dự án dùng systemd service
  (`zcloud.service`) + `scripts/zcloudd.sh` watch mode. Khi code thay đổi,
  service tự rebuild + restart. Chạy tay gây conflict port, crash service,
  làm Sếp không vào được web.
- **Khi cần restart:** chỉ dùng `systemctl restart zcloud` hoặc
  `./scripts/zcloud.sh restart`. Không kill process tay, không start binary
  trực tiếp.
- **Trước khi động vào hệ thống:** kiểm tra process tree
  (`ps aux | grep zcloud`), port (`ss -tlnp | grep 8080`), service status
  (`systemctl status zcloud`). Nếu service đang chạy, DÙNG NÓ.
- **Khi có lỗi:** báo cáo Sếp nguyên nhân + bằng chứng (log, code). Không tự
  ý kill process, xoá DB, rename script để "fix nhanh".
- **Không xoá DB** trừ khi Sếp yêu cầu cụ thể.

## Debug workflow — tuân thủ NGHIÊM NGẶT

Khi Sếp báo lỗi, em phải làm theo thứ tự sau, KHÔNG ĐƯỢC LỆCH BƯỚC:

1. **XÁC ĐỊNH lỗi trước** — dùng `curl` gọi thẳng API endpoint đó, xem
   response có gì. Không suy luận, không đoán.
2. **XEM LOG service** — `journalctl -u zcloud.service -n 50 --output=cat`.
   Lỗi luôn có log. Nếu không thấy thì do:
   - Request không tới được service (port/sai endpoint).
   - Hoặc code không log lỗi (cần thêm log).
3. **So sánh rendered output từ `curl` với source code** — nếu HTML sai thì
   lỗi ở file template hoặc cách render.
4. **KHÔNG BAO GIỜ đổ lỗi cho cache, service restart, build, browser** khi
   chưa chứng minh được. 9/10 lỗi là do code sai. Chỉ nói "do cache" khi
   đã `curl` thấy response đúng mà trình duyệt hiển thị sai, và đã thử
   incognito.
5. **KHÔNG commit khi chưa test response.** Trước khi commit phải:
   - Build thành công (`go build ./...`).
   - Dùng `curl` gọi API/trang vừa sửa, verify response đúng.
   - Đợi service restart (2-3s), verify lại lần nữa.
6. **Khi Sếp nói "vẫn lỗi"** — không commit tiếp, không fix tiếp. Dừng
   lại, đọc lại code, phân tích nguyên nhân gốc. Nếu không tìm được, hỏi
   Sếp cho em xem console log (F12).
7. **Rollback nếu cần** — `git revert` commit gây lỗi, rồi làm lại từ
   đầu, KHÔNG fix chồng fix.

## Kỷ luật Git

- Mỗi subtask = 1 commit. **Push ngay vào `main`**, không cần review.
- **Không force push.**
- Định dạng commit: `<loại>(<phạm vi>): <mô tả>`
  - `feat(core): thêm mã hóa AES-128-CBC`
  - `docs(protocol): thêm đặc tả WebSocket`
  - `fix(api): xử lý session hết hạn`
  - `docs(tasks): merge audit + master-plan, sync schema`
