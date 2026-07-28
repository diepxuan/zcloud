# Changelog

Tất cả thay đổi đáng chú ý của zcloud được ghi tại đây.

Định dạng theo [Keep a Changelog](https://keepachangelog.com/vi-VN/1.1.0/),
dự án tuân thủ [Semantic Versioning](https://semver.org/lang/vi/) khi có versioning.

## [Unreleased]

### Added
- Source code Go đầy đủ — build pass (`go build ./...`).
- `zcloudd` daemon chạy trên port 8080, expose REST API + WebSocket.
- Multi-user manager — nhiều tài khoản Zalo trên 1 daemon.
- Web UI — vanilla JS, file tĩnh embed vào binary.
- SQLite store — 7 bảng (accounts, sessions, conversations, messages, media,
  oa_configs, oa_webhook_logs).
- Media download từ Zalo URL.
- Tab Liên hệ (GetFriends), tab Quản lý (account list), nút Đổi TK (logout).
- WS cmd 510/511 sync tin nhắn cũ + REST fallback.

### Changed
- 2026-07-28: Merge `audit.md` + `master-plan.md` vào `docs/tasks.md` (single
  source of truth). Xoá `docs/audit.md` và `docs/master-plan.md`.
- 2026-07-28: Sync `docs/database/schema.sql` từ `internal/store/store.go`
  (media + OA fields, indexes).
- 2026-07-28: Tạo file `.md` cho tasks 07-14 (trước chỉ có 00-06).
- 2026-07-28: Viết `docs/design.md` — tài liệu thiết kế dùng chung.
- 2026-07-28: Viết `MEMORY.md` và bắt đầu ghi `memory/YYYY-MM-DD.md`.

## Notes
- Task 02 (Reverse Android Sync) — tạm hoãn, Web API WS đủ dùng.
- Task 08 (Zalo OA Integration) — schema sẵn, tạm hoãn implement handler.
- Tasks 10-14 đã xong (xem `docs/tasks.md` bảng trạng thái).
