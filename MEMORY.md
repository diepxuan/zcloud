# MEMORY.md — Long-term memory (main session)

> Trí nhớ dài hạn của agent Bột trong dự án zcloud. Ghi các quyết định,
> bài học, context quan trọng. KHÔNG ghi bí mật (cookies, token, password).

---

## Định danh dự án

- **Tên:** zcloud
- **Repo:** `github.com/diepxuan/zcloud` (public)
- **Workspace:** `/data/zcloud`
- **Sếp:** Trần Ngọc Đức (Duc Tran), GMT+7
- **Sếp email:** caothu91@gmail.com
- **Sếp GitHub:** diepxuan (repo `github.com/diepxuan/zcloud`)
- **Git author mặc định workspace:** `Trần Ngọc Đức <caothu91@gmail.com>`
- **Commit CI/agent hiện tại:** `root <root@zcloud.diepxuan.corp>` (giữ nguyên 65 commit gần nhất)
- **Agent:** Bột, ngôn ngữ tiếng Việt, xưng Sếp/em/đệ
- **Quyết định cuối cùng:** Sếp (theo SOUL.md §1)

## Quyết định kiến trúc quan trọng

- **Dùng Zalo Web API** (chat.zalo.me), KHÔNG dùng Android protocol.
  Lý do: Web API ổn định, đủ chức năng, ít bị khoá account.
- **AES-128-CBC** (không phải AES-ECB) cho REST params — đã xác nhận qua
  zca-js + za-go.
- **AES-GCM** cho WS event data khi Zalo trả cipher.
- **Pure Go SQLite** (`modernc.org/sqlite`) — tránh CGO, dễ cross-compile.
- **Vanilla JS** cho UI — không framework, đơn giản, dễ debug, embed.FS
  build 1 binary duy nhất.
- **Multi-user trong 1 daemon** — `Server.clients map[accountId]*Client`.

## Quy tắc bất di bất dịch (từ SOUL.md / CLAUDE.md)

1. Không tự ý start daemon thủ công → dùng `systemctl restart zcloud`.
2. Không sửa schema nếu chưa được yêu cầu (xem SOUL.md §3).
3. Commit format `<loại>(<phạm vi>): <mô tả>`.
4. Push vào `main` trực tiếp, không cần PR.
5. `trash > rm`. Khi nghi ngờ → hỏi Sếp.
6. Không đổ lỗi cache/service/build khi chưa `curl` kiểm chứng.

## Bài học kỹ thuật

- **`fmt.Fprintf(w, "%s", html)` làm hỏng `%` trong CSS** → dùng raw string
  hoặc `strings.Replace`. Đã từng debug mất 2 commit.
- **`fmt.Fprintf` không tự escape `%` trong HTML tĩnh** — phải dùng
  `embed.FS` + `template.HTML`.
- **Zalo trả về HTML** khi rate limit → check `Content-Type` trước khi
  parse JSON (vd `GetFriends`).
- **WS URL thay đổi** theo version API → đọc `zpw_ws` từ login response,
  không hardcode.
- **cipherKey từ WS key exchange cần `base64.StdEncoding.DecodeString`**
  trước khi dùng AES-GCM.
- **`lastId` mặc định `10000000000000000`** (epoch lớn) cho lần đầu sync
  old messages.

## Tồn đọng còn lại (cập nhật 28/07/2026)

- **WS AES-GCM decrypt chưa dùng** — Zalo vẫn gửi plain JSON nên chưa cần,
  nhưng nên implement để chuẩn bị.
- **Auto-detect media trong WS event** — hiện phải gọi `/api/media/download`
  thủ công.
- **Integration test** — chỉ có `encrypt_test.go`. Cần test cho chat/store/api.
- **Logging tập trung** — `fmt.Printf` lẫn `log.Printf` chưa thống nhất.
- **Zalo OA webhook** — schema sẵn, chưa implement logic (task 08 tạm hoãn).
- **Binary trong git history** — đã ignore nhưng chưa dọn.

## Tham khảo (clone tại docs/references/)

| Lib | Lang | Stars | Vai trò |
|-----|------|-------|---------|
| zca-js | TS | 567 | Tham chiếu chính — login + WS chi tiết nhất |
| zcago | Go | 8 | Tham chiếu Go cho encrypt/chat |
| Za-go | Go | 64 | Tham chiếu Go cho SendMessage REST |

## Liên kết nội bộ quan trọng

- `docs/design.md` — tài liệu thiết kế dùng chung (đọc trước khi code).
- `docs/tasks.md` — danh sách task + trạng thái (single source of truth).
- `docs/tasks/<id>.md` — chi tiết từng task.
- `docs/database/schema.sql` — schema DB (sync từ store.go).
- `CLAUDE.md` — hướng dẫn cho Claude Code, debug workflow.
- `AGENTS.md` — quy tắc workspace, boot sequence.
