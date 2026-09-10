# Design — zcloud (tài liệu thiết kế dùng chung)

> Tài liệu thiết kế kiến trúc + design system + quy ước code cho dự án zcloud.
> Áp dụng cho mọi module trong `src/zcloud/`. Khi thêm tính năng mới, đọc
> file này trước để khớp với codebase hiện có.
>
> Lịch sử cập nhật:
> - 2026-09-06: UI polish pass — sửa token self-reference (chat/login),
>   sửa CSS token self-reference, layout `mg-item` 3 dòng (avatar full
>   height, tên / ID / badge+nút ở 3 dòng riêng), stats bar 3 dòng,
>   empty state cho tab Quản lý, `switchAcc` đóng WS cũ để reconnect.
>   Bổ sung §B12–§B16 (B16: Cookie tab guide 4 bước + auto-extract script).

---

## Phần A — Kiến trúc & quy ước code

### A1. Mục tiêu dự án

Cloud service Zalo — cho phép đăng nhập QR, chat real-time, lưu lịch sử
tin nhắn + media lâu dài, đồng bộ theo chuẩn Zalo (WS cmd 510/511).

### A2. Tech stack

| Lớp | Tech |
|-----|------|
| Core | Go 1.25+, `modernc.org/sqlite`, `github.com/coder/websocket`, `github.com/jackc/pgx/v5` |
| HTTP | `net/http` + `http.ServeMux` (Go 1.22 pattern routing) |
| Storage | SQLite (mặc định, file local) hoặc Postgres (self-hosted) + disk media files |
| Web UI | Vanilla JS ES6+, HTML/CSS thuần, `go:embed` |
| Process | systemd service + `scripts/zcloudd.sh` watch mode |

### A3. Cấu trúc module

```
src/zcloud/
├── cmd/zcloudd/           # main.go — entry point
├── internal/
│   ├── core/              # Logic Zalo (không phụ thuộc HTTP)
│   │   ├── encrypt.go     # AES-128-CBC + AES-GCM + base64
│   │   ├── auth.go        # QR login, cookie login, getLoginInfo
│   │   ├── chat.go        # SendMessage, GetConversations, GetFriends…
│   │   ├── websocket.go   # WS client + cmd 510/511 + decrypt event
│   │   ├── types.go       # Message, Conversation, Session, User, Event
│   │   └── errors.go      # Sentinel errors
│   ├── api/               # HTTP layer
│   │   ├── handlers.go    # Tất cả route handler
│   │   ├── router.go      # SetupRouter + media download
│   │   ├── ws.go          # Browser WS + Zalo listener nền
│   │   ├── embed.go       # //go:embed web/*
│   │   └── web/           # login.html, chat.html, favicon.svg
│   ├── store/             # DB layer (sqlite + postgres)
│   │   ├── store.go       # Struct Store + NewSQLite/NewPostgres
│   │   ├── queries.go     # Dialect-aware CRUD queries
│   │   ├── store_sqlite.go    # SQLite migration
│   │   ├── store_postgres.go  # Postgres migration
│   │   └── types.go       # Domain types
│   └── config/            # Config loader (YAML + CLI + env)
└── examples/              # Test programs
```

### A4. Quy ước code

#### A4.1 Package boundaries
- `internal/core` không import `internal/api` và `internal/store`.
- `internal/api` import cả `core` + `store`.
- `internal/store` không import `core` (chỉ thuần SQL).
- Mọi package ngoài `cmd/` đều nằm trong `internal/` → không export ra ngoài.

#### A4.2 Naming
- File Go: snake_case.
- Struct/interface: PascalCase, có comment giải thích vai trò.
- Method receiver: 1-2 ký tự (vd `s *Store`, `c *Client`, `w *WSClient`).
- Error sentinel: prefix `Err` (vd `ErrNotLoggedIn`).
- Log prefix: `[zcloud]` cho subsystem chính.

#### A4.3 Error handling
- Trả về error, không panic (trừ init).
- API handler: dùng helper `ok(w, data)` / `fail(w, status, msg)` trong `handlers.go`.
- Log lỗi kèm context (accountId, convId, …).

#### A4.4 Logging
- Logger `*log.Logger` truyền qua `Server.Logger` — mọi module dùng chung.
- Format: `[zcloud] <timestamp> <file:line>: <message>`.
- Subsystem prefix: `zalo-ws:`, `media-download:`, `auto-refresh:`.

#### A4.5 HTTP
- Dùng `http.ServeMux` pattern (Go 1.22): `mux.HandleFunc("POST /api/x", h)`.
- Response chuẩn: `APIResponse{OK bool, Data, Error, Code}`.
- Status: 200 OK, 400 input, 401 auth, 404 not found, 500 server.

#### A4.6 Database
- Tất cả schema trong `internal/store/store_sqlite.go` + `store_postgres.go` dạng `const migrationX`.
- Thêm bảng = thêm `const` + push vào slice `migrations`.
- Queries dialect-aware trong `queries.go` qua `if s.backend == BackendPostgres`.
- Không sửa schema khi chưa được yêu cầu (xem SOUL.md §3).

#### A4.7 WebSocket (browser)
- Endpoint: `GET /ws?accountId=…`.
- Message JSON: `{type: "new_message" | "old_message" | "media_downloaded" | …, data: {...}}`.

#### A4.8 WebSocket (Zalo)
- Endpoint: từ `zpw_ws` trong login response (vd `wss://ws1-msg.chat.zalo.me`).
- Frame: `version(1) + cmd(2 LE) + subCmd(1) + payload`.
- Payload có thể AES-GCM + gzip → dùng `core.DecodeWSEvent`.

### A5. Crypto

- **AES-128-CBC** với IV zero, PKCS7 padding — cho REST params.
- **AES-GCM** cho WS event data.
- **MD5** cho chữ ký: `md5("zsecure" + type + sortedParams)`.
- Key = `base64_decode(zpw_enk)` từ login response.

### A6. Zalo API endpoints (đã dùng)

| Method | Path | Mục đích |
|--------|------|----------|
| GET | `/api/preloadconvers/get-last-msgs` | Sync conversations |
| GET | `/api/message/getmsgids` | Old msg IDs |
| GET | `/api/message/getmsgdetail` | Old msg detail |
| GET | `/api/friend/getfriends` | Friends list |
| GET | `/api/profile/get基本信息` (search) | User profile |
| GET | `/api/group/get-group-info` | Group info |
| GET | `/api/cm/getrecentv2` | Group history (REST fallback) |
| POST | `/api/message/sms` | Send message |

WS cmds: 501/521 (new msg), 510/511 (old msg), 1 (ping).

### A6.1 Desktop sync (PC cross-device / backup)

Phần lõi Web vẫn giữ nguyên. Khi task T11 cần mở rộng ra các endpoint và
WS cmds desktop, các helper mới ở `internal/core/desktop_sync.go` chỉ build
request theo payload thật Zalo PC 26.8.10; wire schema đầy đủ cần handshake
trust device / WASM để active flow.

| Method | Path | Helper | Ghi chú |
|--------|------|--------|---------|
| GET | `/api/message/get_crossdb` | `Client.GetCrossDB` | params encrypted |
| GET | `/api/message/pull_mobile_msg` | `Client.PullMobileMsg` | pc_name/public_key/seq_id |
| GET | `/api/message/cancel_pull_mobile_msg` | `Client.CancelPullMobileMsg` | huỷ session sync |
| GET | `/api/transfer-sync-v2/request-sync` | `Client.RequestTransferSync` | reqId + data |
| GET | `/api/message/get_backupmsginfo` | `Client.GetBackupMsgInfo` | thông tin backup |

WS cmds desktop (`internal/core/websocket.go`): 590 RequestSyncMessage,
591 AckDeleteSyncSession, 592 RequestMobileWakeUp, 630 InitBackupSession,
631 CreateBackupSession, 632 GetBackupPcMetadata, 633 SignalRestoreOnMobile,
634 GetBackupConfigs. Frames 590-592/630-634 đi qua `WSClient.handleDesktopSync`
chỉ log payload (chưa parse message model vì schema chưa đủ).

### A6.2 Media queue (auto-download bền vững)

Cấu trúc đường đi mới thay cho goroutine download tức thì trong WS event:

```
Zalo WS → api.handleZaloEvent
        → api.enqueueMessageMediaJobs → store.SaveMediaJob (bảng media_jobs)
                                          ↓
                       api.MediaWorker.loop (mỗi 5s)
                        → ListPendingMediaJobs(accountID)
                        → processJob: download HTTP + retry → SaveMedia meta
                                          ↓
                       globalWS.Broadcast("media_downloaded")
```

- Bảng `media_jobs`: lưu `pending/running/done/failed`, `attempts`,
  `max_attempts` (mặc định 3), `last_error`, `local_path`.
- API vận hành:
  - `GET /api/media/jobs?accountId=&limit=` — liệt kê job gần nhất.
  - `POST /api/media/redownload` body `{accountId, convId, msgId, fileId,
    fileName, fileExt, url}` — xoá file đã có, `ResetMediaJobPending`,
    worker sẽ tải lại.
- Khi nhận WS message có attachment media, `handleZaloEvent` gọi
  `enqueueMessageMediaJobs` (thay cho `maybeAutoDownloadMedia` cũ). Nếu job
  cũ `failed` hoặc `done` mà file không còn → `ResetMediaJobPending`.

### A7. Data flow

#### Login
```
Browser → POST /api/qr/create → core.CreateQRLogin → {token, image}
Browser → poll /api/qr/poll → core.PollQRLogin → session
                                    ↓
                            store.CreateAccount + SaveSession
                                    ↓
                            core.NewClient + StartZaloListener
```

#### Chat real-time
```
Zalo WS → core.WSClient.readLoop → core.handleFrame
                                ↓
              api.handleZaloEvent → store.SaveMessage
                                ↓
              api.WSManager.Broadcast → Browser WS
```

#### Send message
```
Browser → POST /api/messages/send → core.Client.SendMessage
                                ↓
                              Zalo REST API
                                ↓
                              store.SaveMessage
                                ↓
                              ok(data)
```

### A8. Operations

#### Build
```bash
cd src/zcloud && go build -o ../../zcloudd ./cmd/zcloudd/
```

#### Run
- Production: `systemctl start zcloud` (service quản lý qua
  `scripts/zcloudd.sh` watch mode).
- Dev: `./scripts/zcloud.sh start|stop|restart|logs|status`.

#### Env / Config
- File YAML: `~/.config/ductn/zcloud.yml` (override path qua `ZCLOUD_CONFIG`).
- Thứ tự ưu tiên: defaults < YAML < env vars < CLI flags.
- Env: `ZCLOUD_PORT`, `ZCLOUD_DB_PATH`, `ZCLOUD_DB_BACKEND`, `ZCLOUD_DB_PASSWORD`,
  `ZCLOUD_PG_HOST`, `ZCLOUD_PG_PORT`, … (xem `internal/config/config.go`).

#### Backend DB
- `sqlite` (mặc định) — file tại `~/.config/ductn/zcloud.yml > database.sqlite.path`.
- `postgres` — set `database.backend: postgres` + điền `database.postgres.*`.

#### Restart
- `systemctl restart zcloud` — KHÔNG start binary tay.
- Khi sửa code trong `src/zcloud/`, watch mode (`inotifywait`) tự build + restart.

#### Restart Zalo listener
- `StopZaloListener(accountID)` + `StartZaloListener(...)` — chỉ stop/start WS nền.
- Endpoint REST: `POST /api/account/restart?accountId=X`.
- **KHÔNG dùng `/api/logout` để restart listener** — logout xoá account, session,
  conversation, message, media.

### A9. Testing

- Hiện có: `internal/core/encrypt_test.go` (AES-CBC + PKCS7), `chat_ack_test.go`.
- Cần thêm (xem `docs/tasks.md` §5 T3):
  - `internal/core/chat_test.go` — SendMessage mock.
  - `internal/store/store_test.go` — migrations + CRUD.
  - `internal/api/handlers_test.go` — HTTP API với `httptest`.

### A10. Conventions khi viết code mới

- Đọc file liên quan trước (grep, rg).
- Không sửa schema nếu chưa được yêu cầu.
- Commit nhỏ, mỗi commit là 1 thay đổi rõ ràng.
- Format: `<loại>(<phạm vi>): <mô tả>` (vd `feat(api): thêm /api/friends`).
- Push thẳng vào `main`, không cần PR (theo SOUL.md §5).
- Trước khi commit: `go build ./...` + `go test ./...` pass.

---

## Phần B — Design System (Web UI)

> Áp dụng cho mọi file trong `src/zcloud/internal/api/web/`. Mục tiêu:
> giao diện sạch, hiện đại, dễ đọc, nhất quán, không phụ thuộc framework.

### B1. Nguyên tắc chung

1. **Mobile-first** — layout responsive từ 360px trở lên.
2. **Accessibility** — focus ring rõ, contrast >= 4.5:1, button có aria-label khi icon-only.
3. **Zero dependency** — chỉ vanilla JS + CSS thuần, embed trong binary qua `go:embed`.
4. **Tốc độ** — không load font ngoài, không CDN, không JS framework. Mở < 50ms.
5. **Ngôn ngữ** — tiếng Việt cho user-facing text, code/identifier tiếng Anh.

### B2. Design Tokens (CSS Variables)

Tất cả giá trị visual (color, spacing, radius, shadow) định nghĩa trong `:root`
của mỗi file HTML, dùng CSS variable. KHÔNG hardcode màu/spacing trong component.

```css
:root {
  /* === Brand === */
  --c-brand:        #0068ff;   /* Zalo blue, primary CTA */
  --c-brand-hover:  #0052cc;
  --c-brand-soft:   #e8f0ff;   /* Nền nhạt cho active state, badge */

  /* === Neutral (text + surface) === */
  --c-text:         #1a1a1a;   /* Body text chính */
  --c-text-2:       #4a4a4a;   /* Text phụ */
  --c-text-3:       #888;      /* Caption, placeholder */
  --c-text-onbrand: #fff;      /* Text trên nền brand */
  --c-bg:           #f5f5f7;   /* Page background */
  --c-surface:      #fff;      /* Card, panel, modal */
  --c-surface-2:    #f0f0f2;   /* Hover, input bg */
  --c-border:       #e0e0e3;   /* Divider */
  --c-border-2:     #d0d0d3;   /* Input border */

  /* === Semantic === */
  --c-success:      #2e7d32;
  --c-warning:      #f57c00;
  --c-danger:       #c62828;
  --c-info:         #0277bd;

  /* === Spacing scale (4px base) === */
  --s-1: 4px;  --s-2: 8px;  --s-3: 12px;  --s-4: 16px;
  --s-5: 20px; --s-6: 24px; --s-7: 32px;  --s-8: 48px;

  /* === Radius === */
  --r-sm: 4px;   /* Input, badge */
  --r-md: 8px;   /* Card, button */
  --r-lg: 12px;  /* Modal */
  --r-pill: 999px; /* Avatar, tag */

  /* === Shadow (elevation) === */
  --sh-0: none;
  --sh-1: 0 1px 2px rgba(0,0,0,.06);                  /* Card mỏng */
  --sh-2: 0 2px 8px rgba(0,0,0,.08);                  /* Card nổi */
  --sh-3: 0 8px 24px rgba(0,0,0,.12);                 /* Modal */
  --sh-4: 0 16px 48px rgba(0,0,0,.16);                /* Lightbox */

  /* === Typography === */
  --ff-base: -apple-system, BlinkMacSystemFont, "Segoe UI",
              "Helvetica Neue", Arial, "PingFang SC",
              "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  --ff-mono: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  --fz-xs:  11px;   /* Caption */
  --fz-sm:  12px;   /* Meta, badge */
  --fz-md:  14px;   /* Body */
  --fz-lg:  16px;   /* Heading 3, button */
  --fz-xl:  20px;   /* Heading 2 */
  --fz-2xl: 28px;   /* Heading 1 */
  --lh:     1.5;
  --fw-r:   400;
  --fw-m:   500;
  --fw-b:   600;

  /* === Layout === */
  --sb-w:    60px;   /* Sidebar icon width */
  --pn-w:    320px;  /* Panel width (chat list, friends, mgmt) */
  --hdr-h:   56px;   /* Header height */
  --input-h: 40px;   /* Input field height */

  /* === Animation === */
  --ease:   cubic-bezier(.2,.8,.2,1);
  --dur-1:  150ms;
  --dur-2:  250ms;
}
```

### B3. Component patterns

#### B3.1 Button

| Variant | Background | Text | Border | Dùng khi |
|---------|-----------|------|--------|----------|
| `primary` | `--c-brand` | `--c-text-onbrand` | none | CTA chính: Gửi, Lưu, Đăng nhập |
| `secondary` | `--c-surface-2` | `--c-text` | `--c-border` | Hành động phụ: Huỷ, Restart |
| `ghost` | transparent | `--c-text-2` | none | Icon-only trên toolbar |
| `danger` | `--c-surface-2` | `--c-danger` | `--c-danger` | Xoá, logout |

```css
.btn{padding:var(--s-2) var(--s-4);border-radius:var(--r-md);
     font-size:var(--fz-md);font-weight:var(--fw-m);
     border:1px solid transparent;cursor:pointer;
     transition:background var(--dur-1) var(--ease)}
.btn.primary{background:var(--c-brand);color:var(--c-text-onbrand)}
.btn.primary:hover{background:var(--c-brand-hover)}
.btn:disabled{opacity:.5;cursor:not-allowed}
.btn:focus-visible{outline:2px solid var(--c-brand);outline-offset:2px}
```

#### B3.2 Input / Textarea

- Height `--input-h`, padding 0 `--s-3`.
- Border 1px `--c-border-2`, focus chuyển `--c-brand`.
- Background `--c-surface` cho input trên nền sáng; transparent khi inline trong header.
- Placeholder dùng `--c-text-3`.

#### B3.3 Card

- Background `--c-surface`, border-radius `--r-md`, shadow `--sh-1`.
- Padding `--s-4` mặc định; `--s-3` cho card dày đặt (chat list).
- Hover: shadow `--sh-2` (nếu clickable).

#### B3.4 Modal

- Overlay: `rgba(0,0,0,.5)`, position fixed full screen, z-index cao nhất.
- Card: `--c-surface`, `--r-lg`, `--sh-3`, max-width 90vw, max-height 90vh.
- Header: title + close button (`✕`), border-bottom `--c-border`.
- Footer (nếu có): right-aligned action buttons.
- Animation: fade-in overlay + scale-up card 0.96 → 1.0.

#### B3.5 Badge / Status dot

- 8px circle, no border.
- Status:
  - `on` (xanh lá): `--c-success` — listener đang chạy.
  - `off` (xám): `--c-text-3` — không có session.
  - `err` (cam): `--c-warning` — session OK nhưng listener chưa start.
- Tooltip qua `title="..."` cho text mô tả.

#### B3.6 Chat bubble

- Outgoing (mình gửi): background `--c-brand`, text `--c-text-onbrand`,
  align-self `flex-end`, border-bottom-right-radius `var(--r-sm)`.
- Incoming: background `--c-surface`, text `--c-text`, align-self `flex-start`,
  border-bottom-left-radius `var(--r-sm)`.
- Border-radius còn lại `--r-md` (12px).
- Max-width 70% (mobile: 85%).
- Timestamp `var(--fz-xs)` opacity 0.6, right-aligned.
- Sender name `var(--fz-sm)` màu brand, chỉ hiển thị với incoming + group.

#### B3.7 Image grid (attachments)

- Container flex-wrap với gap `--s-1`.
- 1 ảnh: max 320×320, radius `--r-md`.
- 2-3 ảnh: 2 cột, 160×160 vuông object-fit cover.
- 4+ ảnh: 3 cột, 120×120 vuông object-fit cover.
- Click → lightbox (xem B3.8).

#### B3.8 Lightbox

- Full screen fixed, z-index 1000.
- Background `rgba(0,0,0,.85)`.
- Image max 95vw × 90vh, object-fit contain.
- Caption dưới đáy nếu có.
- Đóng: click backdrop hoặc nhấn Esc.

### B4. Layout — `/chat`

```
┌────────────────────────────────────────────────────────────┐
│ Header (height=var(--hdr-h))                               │
│ ┌──┬─────────────────────┬──────────────────────────────┐  │
│ │Av│ User name + avatar   │  Action buttons (optional)   │  │
│ └──┴─────────────────────┴──────────────────────────────┘  │
├────┬──────────────┬───────────────────────────────────────┤
│    │ Panel (320px)│ Chat area                              │
│ SB │              │ ┌────────────────────────────────────┐ │
│(60 │  - Chat list │ │ Messages                           │ │
│ px)│  - Friends   │ │  - bubbles                         │ │
│    │  - Mgmt      │ │  - image grid                      │ │
│    │              │ │  - ack badges                      │ │
│    │              │ │                                    │ │
│    │              │ ├────────────────────────────────────┤ │
│    │              │ │ Input + Send button                │ │
│    │              │ └────────────────────────────────────┘ │
└────┴──────────────┴───────────────────────────────────────┘
```

Mobile (< 768px): SB ẩn, panel full width hoặc ẩn, chat area full screen.

### B5. Layout — `/login`

- Center card 420px max-width.
- Header: logo ZCloud + tên app.
- Tabs: QR / Cookie (segment control).
- QR pane: ảnh QR 240×240 + hint text + trạng thái.
- Cookie pane: textarea + button primary.

### B6. Iconography

- Dùng emoji Unicode cho icon đơn giản (⚙ 💬 👥 ➕ ✓ ✓✓ ⟲) — không cần SVG lib.
- SVG icon (trong `favicon.svg`, `logo.svg`) chỉ dùng cho logo.
- KHÔNG vẽ icon mới bằng CSS/SVG inline — giữ codebase nhỏ.

### B7. Empty states

Mỗi list rỗng (chat list, friends, messages) phải có:
- Icon lớn mờ (emoji 48px `--c-text-3` opacity .5).
- Text ngắn gợi ý hành động (vd "Chọn hội thoại để bắt đầu").
- Optional: button CTA (vd "Đồng bộ ngay").

### B8. Accessibility checklist

- [ ] Mọi button có text hoặc `aria-label`.
- [ ] Modal có `role="dialog"` + focus trap (tối thiểu focus vào element đầu khi mở).
- [ ] Color không phải cách duy nhất truyền tải thông tin (status dot có title).
- [ ] Form input có `<label>` hoặc `aria-label`.
- [ ] ESC đóng modal.
- [ ] Tab order hợp lý (modal > page).

### B9. Migration từ CSS cũ

Mọi file HTML trong `internal/api/web/` phải:
1. Bắt đầu `<style>` bằng block `:root {...}` chứa tokens.
2. Replace tất cả giá trị hardcode (`#0068ff`, `8px`, `border-radius:8px`...) bằng `var(--c-...)`, `var(--s-...)`, `var(--r-...)`.
3. Component lặp lại > 2 lần → tách thành class dùng chung.
4. KHÔNG xoá comment giải thích tiếng Việt — giữ cho người đọc sau.

### B10. Anti-patterns (KHÔNG làm)

- ❌ Hardcode màu (`#0068ff`) trong nhiều chỗ — dùng `var(--c-brand)`.
- ❌ Font-size > 18px cho body text — khó đọc.
- ❌ Letter-spacing âm hoặc dương > 0.5px — làm text khó scan.
- ❌ Padding < 8px trên touch target < 44×44px — khó bấm mobile.
- ❌ Animation quay vô tận khi không loading.
- ❌ Modal không có close button hoặc không đóng bằng Esc.
- ❌ Box-shadow đậm (alpha > 0.2) cho element nhỏ — trông nặng nề.
- ❌ Color palette 1 tone — luôn kết hợp neutral + 1 accent (xem SOUL §frontend guidance).

### B11. Versioning design

- Mỗi lần thay đổi tokens (`--c-brand`, spacing scale...) → bump version trong
  comment đầu file HTML.
- Thay đổi component pattern → update `docs/design.md` Phần B.

---


### B12. Token integrity — không self-reference

**Không** khai báo `var` tham chiếu chính nó — trình duyệt sẽ bỏ qua, thuộc
tính dùng var đó fallback về initial. Đã sửa 2026-09-06:
- SAI: `--c-success-glow: var(--c-success-glow)`
- ĐÚNG: `--c-success-glow: rgba(46,125,50,.45)`

### B13. Mgmt stats bar (3 dòng)

Thanh thống kê trong panel Quản lý gồm 3 dòng riêng biệt:
```
Tổng tài khoản              3
Đang nghe WS                2
Có session active           3
```
CSS `.mg-stats` flex column, mỗi `.mg-stat-row` `display:flex;
justify-content:space-between; align-items:center; padding:var(--s-2) var(--s-4)`.
Số dùng `fw-b`, màu brand, `fz-lg`.

### B14. Mgmt item pattern — 3 dòng, avatar chiếm full height

Mỗi item trong panel Quản lý layout 3 dòng:
```
┌────┐  ● Tên tài khoản
│ AV │  0123456789
│44px│  [Đang dùng] [Restart] [Xoá]
└────┘
```
- `.mg-item`: `display:flex; align-items:stretch; gap:var(--s-3)`.
- `.mg-av`: `width:44px; align-self:stretch; border-radius:var(--r-pill); min-height:44px`.
- `.mg-body`: `flex:1; flex-direction:column; gap:var(--s-1)`.
- Row 1: status dot + tên (fw-b).
- Row 2: account ID (mono, fz-xs).
- Row 3: badge "Đang dùng" (chỉ khi `id === currentAccountId`) + Restart + Xoá.

Item đang dùng: `.mg-item.cur` background `--c-brand-soft`. Empty state qua
`.mg-empty` / `.mg-empty-i` / `.mg-empty-sub`.

### B15. WS reconnect khi đổi account

Mỗi account có 1 WS browser connection (`wsr`). `switchAcc` phải đóng `wsr` cũ
trước khi reconnect cho account mới:
```js
function switchAcc(id){
  if(wsr){try{wsr.onclose=null; wsr.close()}catch(e){} wsr=null;}
  ca=id; uh(); sy(); st('ch');
}
```


### B16. Cookie tab guide — 4 bước + auto-extract script

Tab Cookie trong modal Thêm tài khoản dùng **4 bước rõ ràng** + script JS
auto-extract cookie từ `document.cookie` của `chat.zalo.me`. User copy script,
paste vào Console của Zalo → cookie tự in ra + tự copy vào clipboard →
user quay lại zcloud bấm **Dán cookie** để điền vào textarea.

```
┌──────────────────────────────────────────┐
│ 1  Mở chat.zalo.me, F12 → Console        │
│ 2  Bấm [Sao chép script], paste vào      │
│    Console của Zalo, Enter                │
│ 3  Console in dòng "zcloud_cookie:..."    │
│    → tự copy vào clipboard                │
│ 4  Quay lại đây, bấm [Dán cookie]        │
└──────────────────────────────────────────┘
[📋 Sao chép script]   (đổi thành ✓ Đã sao chép 1.5s)
<details><summary>Xem script</summary><pre>...</pre></details>
[📥 Dán cookie từ clipboard]
[textarea                ]
[🔵 Đăng nhập bằng Cookie]
```

**Script CK_SCRIPT (chạy trong Console của `chat.zalo.me`):**
```js
(()=>{const c=document.cookie;if(!c){console.log("zcloud_cookie: ");return}
const pairs=c.split(";").map(p=>p.trim()).filter(p=>p);
const out=pairs.filter(p=>/^(zpsid|zpw_sek|__zi|zpw_seck|__zpw_sek|app.event.id|clientId|isDark)=/.test(p));
const line=out.join("; ");
console.log("zcloud_cookie: "+line);
try{navigator.clipboard.writeText(line)}catch(e){}})();
```

Chỉ lọc các cookie Zalo cần (`zpsid`, `zpw_sek`, `__zi`...), bỏ tracking.
Format output: `name1=value1; name2=value2` — khớp `parseCookie()` server.

**Component CSS:**
- `.ck-guide` — container hướng dẫn: nền `surface-2`, border, radius md.
- `.ck-step` — flex row, gap s-2; number circle `.ck-num` 20×20 pill brand.
- `.ck-copy` — button outline brand; state `.ck-copy.ok` background success.
- `.ck-code-wrap` — `<details>` mặc định đóng, summary "Xem script".
- `.ck-code` — `<pre>` font mono, max-height 140px scroll dọc.

**Paste logic:**
- Match `/zcloud_cookie:\s*(.+)/` → lấy phần sau prefix.
- Hoặc match `/zpsid=/` → lấy raw (user paste thủ công).
- Không match → báo lỗi "Clipboard không có cookie zpsid".

**Clipboard API:**
- `navigator.clipboard.writeText` cần HTTPS hoặc localhost. Khi fail → fallback
  select text trong `<pre>` để user Ctrl+C.
- `navigator.clipboard.readText` tương tự; nếu fail → báo user dán thủ công.


## Phần C — Tham chiếu

- `docs/references/zca-js/` — TypeScript SDK Zalo (tham khảo protocol).
- `docs/references/zcago/` — Go SDK nhỏ (tham khảo encrypt/chat).
- `docs/references/za-go/` — Go SDK phổ biến hơn (tham khảo SendMessage REST).
- `docs/protocol/pc-desktop.md` — Reverse Zalo PC 26.8.10 (tham khảo AES-GCM + cmd).
- `docs/tasks.md` — Trạng thái task + tồn đọng.
