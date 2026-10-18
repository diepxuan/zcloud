# Task 25: Tách `chat.html` module hóa (H1)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.19 (H1)
- **Trạng thái:** ✅ Done (18/10/2026)
- **Phụ thuộc:** [14-split-webui.md](14-split-webui.md) (đã tách UI ra
  file tĩnh `internal/api/web/`), `docs/design.md` Phần B (design system)

## Bối cảnh

`internal/api/web/chat.html` hiện **1671 dòng**, gồm 3 khối:

| Khối | Dòng | Nội dung |
|------|------|----------|
| `<head>` + `<style>` | 1–658 | meta + 652 dòng CSS (design tokens + component) |
| Body markup | 659–753 | header, sidebar, 3 panel, container (không có logic) |
| `<script>` | 754–1662 | 909 dòng JS (fetch API, render, event handler) |

File lớn khó bảo trì, khó review diff. Mục tiêu: tách CSS + JS ra file
riêng, `chat.html` chỉ còn markup shell.

## Mục tiêu (định nghĩa hoàn thành)

- `chat.html` chỉ còn markup (< ~120 dòng), **không** còn `<style>`,
  **không** còn `<script>` chứa logic.
- `web/chat.css` chứa toàn bộ CSS (nguyên trạng, **không** đổi token/giá trị).
- `web/chat.js` chứa toàn bộ JS (nguyên trạng logic, không đổi hành vi).
- Route mới `GET /static/{file}` phục vụ file tĩnh từ `embed.FS`.
- **Không đổi hành vi UI/UX** (parity với baseline).

## Ràng buộc (BẮT BUỘC)

- Vanilla JS + CSS thuần — **không** framework, **không** CDN, **không** dependency ngoài.
- **KHÔNG** đổi design tokens, **KHÔNG** hardcode màu/spacing mới (xem `docs/design.md` Phần B, `TOOLS.md`).
- Dùng `embed.FS` sẵn có. `//go:embed web/*` **đã đủ** vì `chat.css`/`chat.js`
  nằm trực tiếp trong `web/` (top-level) — không bắt buộc đổi directive.
- JS phải nạp dạng **classic script** (KHÔNG `type="module"`, KHÔNG bọc IIFE)
  vì markup dùng inline `onclick="fn()"` — cần hàm ở global scope.
- Giữ **vị trí nạp tương đương** khối inline cũ: đặt
  `<script src="/static/chat.js"></script>` ngay trước `</body>`.
- Đặt `<link rel="stylesheet" href="/static/chat.css">` tại vị trí khối
  `<style>` cũ trong `<head>`.

## Thay đổi chi tiết

### 1. `internal/api/web/chat.css` (mới)
Copy nguyên trạng nội dung trong khối `<style>`…`</style>` (dòng 7–658),
bỏ 2 tag `<style>`/`</style>`. Không sửa một ký tự CSS nào.

### 2. `internal/api/web/chat.js` (mới)
Copy nguyên trạng nội dung trong khối `<script>`…`</script>` (dòng 754–1662),
bỏ 2 tag. Không sửa logic.

### 3. `internal/api/web/chat.html` (sửa)
- Trong `<head>`: thay khối `<style>…</style>` bằng
  `<link rel="stylesheet" href="/static/chat.css">`.
- Trước `</body>`: thay khối `<script>…</script>` bằng
  `<script src="/static/chat.js"></script>`.
- Giữ nguyên toàn bộ markup body (dòng 659–753).
- (tuỳ chọn) xoá các HTML comment rác watch-test sau `</body></html>`.

### 4. `internal/api/router.go` + handler
- Thêm route: `mux.HandleFunc("GET /static/{file}", s.HandleStatic)`.
- Viết `HandleStatic(w, r)` (đặt cùng file router.go hoặc handlers.go):
  - `name := r.PathValue("file")`.
  - Whitelist cứng: `chat.css`, `chat.js` (và `login.css`, `login.js` nếu tồn tại).
    Nếu `name` không nằm whitelist → `http.NotFound`.
  - `data, err := webFS.ReadFile("web/" + name)`; lỗi → 404.
  - Content-Type theo đuôi: `.css` → `text/css; charset=utf-8`,
    `.js` → `application/javascript; charset=utf-8`.
  - `w.Header().Set("Cache-Control", "no-store")` (tránh asset cũ sau
    deploy qua watch mode + atomic rename).

### 5. `internal/api/embed.go`
Giữ nguyên `//go:embed web/*`. Chỉ đổi nếu build báo thiếu file.

### 6. `docs/design.md` Phần A3
Cập nhật listing thư mục `web/`: thêm `chat.css`, `chat.js`.

## Kiểm chứng (chạy theo thứ tự, ghi lại output)

1. **Baseline** (trước khi sửa): `./scripts/check-ui.sh` → phải PASS. Lưu output.
2. `cd src/zcloud && go build ./...` → PASS.
3. `node --check src/zcloud/internal/api/web/chat.js` → PASS (JS syntax).
4. `./scripts/check-js.sh chat.html` → chat.html không còn inline `<script>` (0 block) — OK.
5. `curl -s -o /dev/null -w '%{http_code} %{content_type}\n' http://127.0.0.1:8080/static/chat.css`
   → `200 text/css; charset=utf-8`. Tương tự `chat.js` → `200 application/javascript; charset=utf-8`.
6. `curl -s http://127.0.0.1:8080/chat | grep -E 'chat\.css|chat\.js'` → thấy cả 2 tham chiếu.
7. **Sau khi sửa**: `./scripts/check-ui.sh` → PASS (cùng assertions như baseline).
8. Đợi watch mode rebuild (`journalctl -u zcloud -f`) rồi lặp lại bước 5–7.

## Ngoài phạm vi

- Không đổi logic JS/CSS.
- Không dọn dead code `accOpts` trong `HandleChatPage` (đã thành no-op vì
  `chat.html` không còn `%s`).
- Không tách JS thành nhiều module nhỏ (làm tiếp sau nếu cần).
- Không sửa `login.html` (có thể áp dụng tương tự sau).

## Rủi ro

- Bọc JS trong module/IIFE → inline `onclick` vỡ. **Bắt buộc classic script.**
- Asset cũ do cache → dùng `Cache-Control: no-store`.
- Watch mode (`zcloudd serv watch`) tự rebuild + restart khi lưu file; guard
  `check_js` chặn JS syntax error (không restart nếu lỗi) nhưng KHÔNG chặn
  lỗi runtime/DOM → phải test DOM bằng `check-ui.sh`.

## Rollback

`git checkout -- src/zcloud/internal/api/web/chat.html
src/zcloud/internal/api/router.go src/zcloud/internal/api/embed.go`
(và xoá `chat.css`/`chat.js`) trước khi commit, hoặc revert commit sau khi commit.
