# Tasks — Reverse Zalo & Build zcloud

> Master plan tại [master-plan.md](master-plan.md).
> File này là task list tổng quan, chi tiết từng task tại `tasks/<id>-*.md`.

---

## Task List

| ID | Tên | Phụ thuộc | Plan | Status | Ghi chú |
|----|-----|:---------:|:----:|:------:|---------|
| **00** | Setup môi trường + Go project | — | ✅ | 🔴 Pending | Toolchain, go.mod, thư mục |
| **01** | Reverse Zalo Web API | 00 | 🔴 Cần viết | 🔴 Pending | Capture login, encryption, endpoints, WS |
| **02** | Reverse Android Sync | *01-06 xong* | ❌ | 🟡 Deferred | Optional — chỉ làm khi Web API thiếu history |
| **03** | Design Core Protocol | 01 | 🔴 Cần viết | 🔴 Pending | Go interfaces design |
| **04** | Build Core Library (Go) | 01, 03 | 🔴 Cần viết | 🔴 Pending | encrypt → auth → chat → websocket |
| **05** | Build Server Daemon | 04 | 🔴 Cần viết | 🔴 Pending | REST API + WebSocket |
| **06** | Build Web UI | 05 | 🔴 Cần viết | 🔴 Pending | SPA vanilla JS |

---

## Luồng thực thi

```
00 ──► 01 ──► 03 ──► 04 ──► 05 ──► 06
                                         └──► 02 (optional)
```

## Legend

| Ký hiệu | Ý nghĩa |
|:-------:|---------|
| 🔴 Pending | Chưa bắt đầu |
| 🟡 In Progress | Đang làm |
| 🟢 Done | Hoàn thành |
| 🟡 Deferred | Tạm hoãn — làm sau |
| ❌ | Chưa có |

---

## Task outputs (liên kết giữa các task)

| Task | Output | Input cho task |
|------|--------|:--------------:|
| 00 | Thư mục + go.mod + .gitignore | 01 |
| 01 | `docs/protocol/*.md` | 03, 04 |
| 03 | `docs/design/*.md` | 04 |
| 04 | `src/zcloud/internal/core/*.go` | 05 |
| 05 | `src/zcloud/internal/api/*.go` + server binary | 06 |
| 06 | `src/zcloud/web/*` | — |
| 02 | `docs/protocol/android-*.md` | — |
