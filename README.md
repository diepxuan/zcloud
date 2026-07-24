# zcloud

Cloud service liên quan Zalo.

## Mục đích

(zcloud) — cung cấp dịch vụ cloud tích hợp với hệ thống Zalo. Xem chi tiết trong `IDENTITY.md §3 Project Specs`.

## Cách sử dụng

Repo hiện ở giai đoạn **khởi tạo tài liệu** — chưa có source code. Khi có code, sẽ có:
- Hướng dẫn cài đặt tại đây.
- Lệnh build / test / lint khi có `package.json`, `pyproject.toml`, hoặc tương đương.

Cho agent làm việc trong repo: đọc `CLAUDE.md` rồi boot theo `SOUL.md §4`.

## Cấu trúc file

```
.
├── README.md        # File này — mục đích, cách dùng, cấu trúc
├── CLAUDE.md        # Hướng dẫn cho Claude Code
├── SOUL.md          # Bản sắc & nguyên tắc vận hành (lớp cao nhất)
├── IDENTITY.md      # Identity chi tiết (Bột, workspace, server, DB)
├── AGENTS.md        # Quy tắc workspace, memory, red lines
├── USER.md          # Thông tin người dùng (Sếp Duc Tran)
├── TOOLS.md         # Local environment notes
├── CHANGELOG.md     # Lịch sử thay đổi
├── LICENSE          # MIT (Copyright 2026 DXVN)
└── .gitignore       # Các file git bỏ qua
```

## Dependencies

Hiện chưa có runtime dependency (chưa có source code). Sẽ cập nhật khi có.

## Troubleshooting

| Vấn đề | Cách xử lý |
|--------|------------|
| Không tìm thấy identity | Đọc `CLAUDE.md` trước, rồi theo `SOUL.md §4` boot sequence |
| Câu trả lời không đúng voice | Nhắc "tiếng Việt, xưng Sếp/em, không emoji" |
| Cần biết quyết định có được push không | Xem `SOUL.md §5` — chỉ push khi Sếp nói rõ |
| Xung đột giữa các file identity | `SOUL.md` là lớp cao nhất |

## License

MIT — xem `LICENSE`.
