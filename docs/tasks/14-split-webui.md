# Task 14: Tách Web UI ra file tĩnh

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [05-build-server.md](05-build-server.md), [06-build-webui.md](06-build-webui.md)
- **Trạng thái:** Xong

## Mục tiêu
Tách HTML/CSS/JS ra file riêng trong `internal/api/web/`, embed vào binary
bằng `go:embed` thay vì inline trong Go source.

## Cấu trúc
```
internal/api/
├── web/
│   ├── login.html    # Trang QR login
│   ├── chat.html     # Trang chat + tab Liên hệ + Quản lý
│   └── favicon.svg   # Logo ZCloud
├── embed.go          # //go:embed web/*
└── handlers.go       # HandleLoginPage, HandleChatPage — trả về embed.FS
```

## Files
- `internal/api/embed.go` — `//go:embed web/*` + `var WebFS embed.FS`.
- `internal/api/handlers.go` — `HandleLoginPage` / `HandleChatPage` đọc từ
  `WebFS`.
- `internal/api/router.go` — route `GET /`, `GET /chat`, `GET /favicon.svg`.

## Verification
- [x] Binary build với HTML embedded, không cần file ngoài.
- [x] HTML render giống bản inline cũ (CSS + JS hoạt động).
- [x] Favicon SVG serve đúng.

## Ghi chú
- Sửa bug `%` trong CSS bị `fmt.Fprintf` làm hỏng (`%!(MISSING)`) — đổi sang
  `strings.Replace` hoặc raw string.
