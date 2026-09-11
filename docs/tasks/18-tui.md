# Task 18: Terminal UI (TUI) — zcloudd tui

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.12 (T12)
- **Trạng thái:** 🟡 MOCKUP có sẵn (`./zcloudd tui` in banner + keymap dự kiến). Đợt này mới bắt đầu triển khai thật.
- **Phụ thuộc:**
  - [05-build-server.md](05-build-server.md) — daemon đã có `internal/server`
  - [07-multi-user.md](07-multi-user.md) — `accounts` + `sessions`
  - [09-database.md](09-database.md) — `store.Store` interface
  - [11-getfriends.md](11-getfriends.md) — `friends` table

## Workflow (theo yêu cầu của Sếp 11/09/2026)

```
┌─ zcloud TUI ─────────────────────────────┐
│                                           │
│  ./zcloudd  hoặc  ./zcloudd tui           │
│                                           │
│  ┌─ Màn 1: Chọn tài khoản ──────────────┐│
│  │  > Sep Duc       ws: ok     last 2m   ││
│  │    Phạm Hồng      ws: stale  last 1h  ││
│  │    Test account   ws: down  last 3d   ││
│  └──────────────────────────────────────┘│
│  ↑/↓ chọn, Enter xác nhận, / filter      │
│  ESC thoát                                │
│                                           │
│  ┌─ Màn 2: Chọn thread ─────────────────┐│
│  │  /<nhập text để filter>               ││
│  │  > Duc Tran          hello sep!  2m   ││
│  │    Cam Tu             gửi ảnh     1h  ││
│  │    Phandaitrang       ok ảnh      3h  ││
│  │    Nhóm ABC           ...        ...   ││
│  └──────────────────────────────────────┘│
│  ↑/↓ chọn, Enter mở chat, / filter       │
│  ESC thoát                                │
│                                           │
│  ┌─ Màn 3: Chat ─────────────────────────┐│
│  │  Duc Tran ───────────────────────────  ││
│  │                                        ││
│  │  [16:32] Duc Tran: hello sep!          ││
│  │  [16:33] Sep: chào Duc                ││
│  │  [16:35] Duc Tran: gửi ảnh            ││
│  │           [photo.jpg]                  ││
│  │  ...                                   ││
│  │                                        ││
│  ├────────────────────────────────────────┤
│  │ > soạn tin nhắn..._                    ││
│  └────────────────────────────────────────┘│
│  ↑/↓ cuộn, gõ text + Enter gửi           │
│  ESC thoát                                │
└───────────────────────────────────────────┘
```

3 màn tuần tự. **ESC ở bất kỳ màn nào cũng thoát chương trình** (exit 0).

## Keymap

| Phím           | Hành động                                          |
|----------------|----------------------------------------------------|
| `↑/↓` / `j/k`  | di chuyển trong list                               |
| `Enter`        | xác nhận chọn / gửi tin                            |
| `/`            | vào chế độ filter (gõ text để lọc, Enter xong)     |
| `Esc`          | **thoát TUI hoàn toàn** (cả 3 màn)                  |
| `Ctrl+C`       | thoát TUI (alternative của ESC)                     |
| `Backspace`   | xoá ký tự filter                                   |
| `q` (chỉ màn 1, 2) | thoát TUI                                       |

## Lựa chọn framework

**Chọn:** `charmbracelet/bubbletea` + `charmbracelet/lipgloss` (style) +
`charmbracelet/bubbles` (list, textinput, viewport, spinner).

ELM-style (Model/Update/View) phù hợp với flow wizard tuần tự — chuyển
màn chỉ cần đổi `m.screen` state. Không blocking stdin khi không focus.

## Sub-task

### T18.1 — Bubbletea + 3 màn tuần tự (read + write)
- Thêm dependency: `github.com/charmbracelet/bubbletea`,
  `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`.
- Refactor `internal/tui/tui.go`: thay mockup bằng `tea.NewProgram(model).Run()`.
- `internal/tui/model.go`: `Model{store, screen, accountsIdx, convsIdx,
  convsFiltered, messages, composer, filterInput, width, height}`.
  `screen` là enum: `screenAccounts | screenConvs | screenChat`.
- 3 sub-model wrap `bubbles/list` (màn 1, 2) và `bubbles/viewport` +
  `bubbles/textinput` (màn 3).
- Update: handle `tea.KeyMsg` (ESC → `tea.Quit`, `/` → focus filter input,
  `Enter` → xác nhận chọn).
- Lipgloss: header (tên màn), body (list/viewport), footer (keymap hint).

**Verify:**
- `./zcloudd tui` mở màn 1 (Accounts) trong vòng 500ms.
- `q` / `ESC` thoát sạch, exit 0, terminal về state cũ.
- Resize terminal không vỡ layout.

### T18.2 — Load data từ Postgres
- `Model.Init()` gọi `m.loadAccounts()` → `store.ListAccounts()`.
- Khi xác nhận account (Enter ở màn 1) → `m.screen = screenConvs` +
  `m.loadConversations(accountID)` → `store.GetConversations(accountID)`.
- Khi xác nhận conv (Enter ở màn 2) → `m.screen = screenChat` +
  `m.loadMessages(convID)` → `store.GetMessages(convID, limit=50)`.
- Filter: gõ `/` → focus textinput, gõ text → lọc `convsFiltered` theo
  `displayName` (case-insensitive contains) + parse ID nếu text toàn số.
- Spinner trong khi load (`bubbles/spinner`).
- Auto-refresh messages mỗi 3s qua `tea.Tick` (subscribe channel nội bộ
  đẩy từ `internal/api` sau — Phase 2 sẽ wire).

**Verify:**
- Đăng nhập 1 account QR trên web → `./zcloudd tui` mở, thấy account.
- Chọn account → thấy conversations.
- Gõ `/sep` → list lọc theo "sep".
- Chọn conv → thấy 50 messages mới nhất.

### T18.3 — Composer + gửi tin nhắn
- Màn 3: bottom panel textinput, focus khi vào màn (không cần phím `n`).
- Enter để gửi: gọi `core.Client.SendMessage(text, convID)` qua
  `internal/core/chat.go` (đã có sẵn).
- Hiển thị echo trong viewport ngay lập tức (optimistic) + marker
  `[sent]` khi nhận WS ack.
- Text rỗng + Enter → bỏ qua.
- Text quá dài (>2000 char) → chia nhỏ tự động (Zalo giới hạn).

**Verify:**
- Soạn "test tui" → Enter → tin xuất hiện trong viewport + thực sự gửi
  tới Zalo.
- **Dùng conv Trần Ngọc Đức (`4866700441106275565`) cho smoke test** —
  xem quy ước §5.4 trong `docs/tasks.md`.
- Tin nhận realtime: nhờ Sếp gửi từ điện thoại → TUI hiển thị trong 3s.

### T18.5 — Thêm acc login bằng cookie (zpsid + zpw_sek, bỏ script)

Sếp yêu cầu 11/09/2026: thay modal nhập cookie hiện tại (dùng script trong
DevTools để extract + parseCookie auto) bằng form nhập thủ công 2 field.
Lý do: script gặp `NotAllowedError: Document is not focused` khi DevTools
mở detached hoặc user click sang tab khác — copy thủ công 2 ô đơn giản
hơn, không cần focus.

**Field cần Sếp điền (chỉ 2):**

| Field     | Mô tả                                                         | Bắt buộc |
|-----------|---------------------------------------------------------------|:--------:|
| `zpsid`   | Session ID — copy từ DevTools → Application → Cookies → `zpsid` | ✅       |
| `zpw_sek` | Secret key — copy từ DevTools → Application → Cookies → `zpw_sek` | ✅       |

Các cookie phụ (`__zi`, `zpw_seck`, `__zpw_sek`, `app.event.id`, `clientId`,
`isDark`) đã bị loại khỏi script — chúng là cookies tracking/theme Zalo
PC, KHÔNG cần cho `core.CookieLogin` (chỉ cần `zpsid` + `zpw_sek` để gọi
`getLoginInfo` ở `wpa.chat.zalo.me` thành công).

**Cách copy trong DevTools:**

1. Mở https://chat.zalo.me, đăng nhập xong.
2. F12 → tab **Application** → mục **Cookies** → `https://chat.zalo.me`.
3. Tìm `zpsid` → double-click cột **Value** → Ctrl+C.
4. Tìm `zpw_sek` → double-click cột **Value** → Ctrl+C.
5. Paste vào 2 ô tương ứng.

**Sửa `internal/api/handlers.go:HandleCookieLogin`:**

Đổi request body từ `{cookie: string}` thành `{zpsid, zpw_sek string}`.
Build cookies map:
```go
cookies := map[string]string{"zpsid": req.ZPSID, "zpw_sek": req.ZPWSEK}
```
rồi gọi `core.CookieLogin(ctx, cookies, "", "")` như cũ. Validate phía
server: nếu `zpsid` hoặc `zpw_sek` rỗng → fail 400 "thiếu field".

**Sửa `internal/api/web/chat.html` modal "Đăng nhập bằng Cookie":**

Bỏ 4 step hướng dẫn script + nút "Sao chép script" + nút "Dán cookie
từ clipboard" + `CK_SCRIPT` + `copyCookieScript()` + `pasteCookieFromClipboard()`.
Thay bằng:
```html
<div class="ck-fields">
  <label>zpsid
    <input id="ck-zpsid" placeholder="Session ID từ DevTools → Application → Cookies → zpsid">
  </label>
  <label>zpw_sek
    <input id="ck-zpwsek" type="password" placeholder="Secret key từ DevTools → Application → Cookies → zpw_sek">
  </label>
</div>
<div class="ck-help">
  Mở <a href="https://chat.zalo.me" target="_blank">chat.zalo.me</a> đã đăng nhập →
  F12 → Application → Cookies → chat.zalo.me → copy value của <b>zpsid</b> và <b>zpw_sek</b>.
</div>
```

`zpw_sek` dùng `type="password"` để browser che value khi Sếp gõ
(tránh shoulder-surfing) — vẫn paste được bình thường.

**Sửa `submitCookie()`** trong `chat.html`:

Đọc 2 input thay vì textarea:
```js
var zpsid = document.getElementById('ck-zpsid').value.trim();
var zpwsek = document.getElementById('ck-zpwsek').value.trim();
if (!zpsid || !zpwsek) { er.textContent = 'Nhập cả zpsid và zpw_sek'; ...; return; }
fetch('/api/login/cookie', { method:'POST', body: JSON.stringify({zpsid, zpw_sek: zpwsek}) })
```
giữ nguyên phần xử lý response.

**Verify:**
- [ ] Đăng nhập Zalo web, F12 → Application → Cookies → chat.zalo.me.
- [ ] Copy `zpsid` và `zpw_sek`, paste vào 2 ô trong modal.
- [ ] Bấm "Đăng nhập bằng Cookie" → server trả `{accountId}`.
- [ ] Account xuất hiện trong list, WS listener start, có thể chat.
- [ ] Bỏ trống 1 trong 2 ô → báo "Nhập cả zpsid và zpw_sek".
- [ ] Không còn script/DevTools/copy-paste chuỗi dài.

### T18.6 — Polish + resize + cleanup
- Detect terminal không hỗ trợ TUI (không có TTY) → in hướng dẫn dùng
  `zcloudd serv` rồi exit 1 thay vì crash.
- Cleanup screen khi thoát (gửi `\x1b[?1049l` để thoát alternate buffer).
- Width/height responsive: list chiếm toàn bộ, viewport tính padding
  cho composer (1 dòng) + header (1 dòng).
- Hiển thị trạng thái WS ở header màn 1: `ws: ok` / `stale` / `down`
  (cần `internal/api.GetListenerStatus()` — wire sau nếu dễ).

**Verify:**
- Chạy `./zcloudd tui < /dev/null` → in hướng dẫn thay vì crash.
- Resize terminal nhỏ xuống 20 dòng → list vẫn render không vỡ.

## Files sẽ tạo / sửa

- **Mới:**
  - `internal/tui/model.go` — Model + Update + View + Init.
  - `internal/tui/screens.go` — render từng màn (1, 2, 3).
  - `internal/tui/filter.go` — helper filter list theo text.
  - `internal/tui/tui_test.go` — unit test cho keymap (ESC luôn thoát).
- **Sửa:**
  - `internal/tui/tui.go` — thay mockup bằng `tea.NewProgram(model).Run()`.
  - `go.mod` — thêm bubbletea + lipgloss + bubbles.
  - `internal/store/store.go` — check signature `ListAccounts`,
    `GetConversations`, `GetMessages`; bổ sung cursor param nếu cần.
  - `internal/api/` — nếu cần `GetListenerStatus()` cho WS health indicator.

## Verification chung

- [ ] `./zcloudd` hoặc `./zcloudd tui` mở TUI, render đúng, resize không vỡ.
- [ ] ESC ở màn 1, 2, 3 đều thoát sạch (exit 0).
- [ ] Filter `/` hoạt động ở màn 1 (theo tên account) và màn 2 (theo tên/ID conv).
- [ ] Đọc đúng accounts + conversations + messages từ Postgres.
- [ ] Gửi tin từ TUI xuất hiện trong web UI cùng account (verify với
      Trần Ngọc Đức theo §5.4).
- [ ] Tin nhận realtime từ Zalo hiển thị trong TUI trong 3s.
- [ ] `go test ./...` pass.
- [ ] Không cần thêm port mới (TUI đọc trực tiếp Postgres, không qua HTTP).

## Không nằm trong task này

- Zalo OA webhook (task 08, 🟢 Optional).
- Auto-sync media end-to-end (T11.5–T11.7).
- Multi-user đồng thời trên TUI (chỉ cho phép 1 TUI instance tại 1 thời
  điểm — khoá qua file lock `~/.cache/zcloud/tui.lock`).
- Service panel (start/stop/watch/logs) — không cần trong flow wizard.
- Persist state (chọn account lần trước) — chưa cần.
- Search overlay đa năng — chỉ cần filter đơn giản ở màn 1, 2.

## Rủi ro

- **Terminal compatibility**: bubbletea yêu cầu 256 màu + alternate screen.
  Test trên `xterm-256color`, `tmux`, `alacritty`, `gnome-terminal`.
- **Latency khi DB lớn**: nếu `messages` >100k row / conv, cần cursor pagination
  + index `(account_id, conv_id, id DESC)`. Check trước khi ship.
- **Crash khi daemon không chạy**: nếu TUI mở mà daemon chưa start, chỉ
  thấy "0 accounts" — không crash. Document rõ trong help footer.

## Ghi chú quan trọng

**KHÔNG được đổi behavior của `./zcloudd` no-arg** — nó hiện chạy
`server.Run()` (commit `6019140`). Watch mode fork `./zcloudd` no-arg
để lấy HTTP server. Đổi no-arg sang thứ khác sẽ phá watch. TUI chỉ
làm `./zcloudd tui` thôi.
