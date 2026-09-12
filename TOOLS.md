# TOOLS.md - Local Notes

Skills define _how_ tools work. This file is for _your_ specifics — the stuff that's unique to your setup.

## What Goes Here

Things like:

- Camera names and locations
- SSH hosts and aliases
- Preferred voices for TTS
- Speaker/room names
- Device nicknames
- Anything environment-specific

## Design tokens (Web UI)

Khi sửa `internal/api/web/*.html`, **ĐỌC `docs/design.md` Phần B TRƯỚC**.
Tokens định nghĩa trong `:root` của mỗi file HTML — KHÔNG hardcode.

### Brand & neutral colors

| Variable | Value | Dùng cho |
|----------|-------|----------|
| `--c-brand` | `#0068ff` | CTA chính, link, active state (Zalo blue) |
| `--c-brand-hover` | `#0052cc` | Hover CTA |
| `--c-brand-soft` | `#e8f0ff` | Nền nhạt active state, badge nền |
| `--c-text` | `#1a1a1a` | Body text |
| `--c-text-2` | `#4a4a4a` | Text phụ |
| `--c-text-3` | `#888` | Caption, placeholder |
| `--c-bg` | `#f5f5f7` | Page background |
| `--c-surface` | `#fff` | Card, modal, panel |
| `--c-surface-2` | `#f0f0f2` | Hover, input bg |
| `--c-border` | `#e0e0e3` | Divider |
| `--c-border-2` | `#d0d0d3` | Input border |

### Semantic colors

| Variable | Value | Dùng cho |
|----------|-------|----------|
| `--c-success` | `#2e7d32` | Listener đang chạy, success toast |
| `--c-warning` | `#f57c00` | Session lỗi, warning toast |
| `--c-danger` | `#c62828` | Xoá, error toast |
| `--c-info` | `#0277bd` | Info, link phụ |

### Spacing (4px base)

`--s-1` 4px · `--s-2` 8px · `--s-3` 12px · `--s-4` 16px · `--s-5` 20px · `--s-6` 24px · `--s-7` 32px · `--s-8` 48px

### Radius

`--r-sm` 4px (input, badge) · `--r-md` 8px (card, button) · `--r-lg` 12px (modal) · `--r-pill` 999px (avatar)

### Shadow

`--sh-0` none · `--sh-1` 0 1px 2px rgba(0,0,0,.06) · `--sh-2` 0 2px 8px rgba(0,0,0,.08) · `--sh-3` 0 8px 24px rgba(0,0,0,.12) · `--sh-4` 0 16px 48px rgba(0,0,0,.16)

### Typography

- `--ff-base`: system font stack (PingFang SC / Microsoft YaHei cho tiếng Việt/Trung).
- `--ff-mono`: SF Mono / Menlo.
- Sizes: `--fz-xs` 11px · `--fz-sm` 12px · `--fz-md` 14px · `--fz-lg` 16px · `--fz-xl` 20px · `--fz-2xl` 28px.

### Layout constants

- `--sb-w` 60px (sidebar icon rail).
- `--pn-w` 320px (chat list / friends / mgmt panel).
- `--hdr-h` 56px (header).
- `--input-h` 40px (input field).

## Component cheat sheet (đầy đủ trong docs/design.md Phần B)

- **Button**: `.btn.primary | .secondary | .ghost | .danger`.
- **Modal**: overlay `rgba(0,0,0,.5)`, card `--r-lg --sh-3`, ESC đóng.
- **Chat bubble**: outgoing `--c-brand` flex-end, incoming `--c-surface` flex-start, max-width 70%.
- **Image grid**: 1 ảnh max 320px, 2-3 ảnh 2 cột 160px vuông, 4+ ảnh 3 cột 120px vuông.
- **Status dot**: 8px circle, `on` (xanh) / `off` (xám) / `err` (cam).

## Anti-patterns cần tránh

- ❌ Hardcode `#0068ff`, `8px`, `border-radius:8px` — dùng `var(...)`.
- ❌ Padding < 8px trên touch target — khó bấm mobile.
- ❌ Box-shadow alpha > 0.2 cho element nhỏ — trông nặng.
- ❌ Color palette 1 tone — kết hợp neutral + 1 accent.
- ❌ Tự ý đổi design tokens — hỏi Sếp.

## Service shortcuts (server zcloud)

- **Restart daemon**: `systemctl restart zcloud` (KHÔNG start binary tay).
- **Tail logs**: `journalctl -u zcloud.service -f --output=cat`.
- **Build local**: `cd src/zcloud && go build -o ../../zcloudd ./cmd/zcloudd/`.
- **Smoke test API**: `curl -s http://127.0.0.1:8080/api/account/list`.
- **DB SQLite**: dùng Python `sqlite3` module (không cài sqlite3 CLI).
- **DB Postgres** (khi dùng): `PGPASSWORD=... psql -h 127.0.0.1 -U zcloud -d zcloud`.
- **Config file**: `~/.config/ductn/zcloud.yml` (override qua `ZCLOUD_CONFIG`).

## Ngôn ngữ code

- User-facing text (UI): **Tiếng Việt**.
- Identifier (CSS class, JS function, Go var): **Tiếng Anh**.
- Commit message: tiếng Việt không dấu hoặc tiếng Anh đều OK (xem git log).

## Why Separate?

Skills are shared. Your setup is yours. Keeping them apart means you can update skills without losing your notes, and share skills without leaking your infrastructure.

---

Add whatever helps you do your job. This is your cheat sheet.


## UI verify không cần browser

### `./scripts/check-js.sh`
Script extract tất cả `<script>` block từ `chat.html` / `login.html`,
ghi ra file `.js` tạm ở `/tmp` rồi chạy `node --check`. Phát hiện
**SyntaxError** (thiếu ngoặc, quote không đóng, khai báo trùng, ...) —
nguyên nhân chính gây JS chết trên browser mà curl không thấy.

```bash
./scripts/check-js.sh              # check cả chat.html + login.html
./scripts/check-js.sh chat.html    # check 1 file
```

Output khi fail:
```
[FAILED]   zcloud_check_chat_0.js:
    /tmp/zcloud_check_chat_0.js:2
    var broken=)); var ca=''...
               ^
    SyntaxError: Unexpected token ')'
```

Tự động tìm node ở `/usr/local/bin/node`, `/usr/bin/node`,
`/root/.nvm/versions/node/*/bin/node`, ... — hoạt động cả trong
systemd service (PATH bị strip).

### Tích hợp vào watch mode (`zcloudd serv watch`)

**Cách trigger watch cycle từ shell** (khi cần rebuild nhanh để test):

```bash
# Dùng HTML comment để trigger fsnotify Write event trên .html file.
# HTML comment vô hình trong DOM rendered (user không thấy) nhưng vẫn
# được browser parse (không break). Nếu lỡ quên xoá thì không hiện ra UI.
echo "<!-- watch-test: $(date +%s) -->" >> src/zcloud/internal/api/web/chat.html

# Với file .go thì dùng Go comment (chỉ ảnh hưởng AST khi compile):
echo "// touch $(date +%s)" >> src/zcloud/internal/api/handlers.go

# SAU KHI TEST → restore file về HEAD:
git restore src/zcloud/internal/api/web/chat.html src/zcloud/internal/api/handlers.go
```

**Tại sao không dùng `touch`**:
- `touch` chỉ cập nhật access time, KHÔNG tạo fsnotify Write event.
- Phải modify nội dung file (`echo ... >> file`) để watch detect.

**Sai lầm em đã mắc (12/09/2026)**:
- Dùng `echo "// x" >> chat.html` để test → quên restore → commit c993249
  phải sửa. HTML comment là lựa chọn an toàn hơn cho file HTML.
Watch loop gọi `check_js` trước khi build:
```bash
if ! check_js; then
    info "JS syntax error — KHÔNG restart, giữ binary cũ."
    continue
fi
```
Nhờ đó khi em sửa HTML mà gây SyntaxError, binary cũ vẫn chạy,
log rõ lỗi trong `journalctl -u zcloud`. Tránh được tình trạng
"commit xong browser không chạy mà không biết".

### Phạm vi phát hiện
| Bug | Phát hiện? |
|-----|-----------|
| SyntaxError (parse) | ✅ node --check |
| Reference undefined identifier (vd typo) | ⚠️ ReferenceError runtime — KHÔNG |
| Runtime exception (null deref) | ❌ Cần browser/JSDOM |
| DOM render sai | ❌ Cần browser |
| API endpoint fail | ❌ Cần curl hoặc browser |

Với lỗi runtime/DOM: dùng curl test endpoint + đọc response, hoặc
cài Playwright/Puppeteer nếu cần UI test thật (chưa có).



## Lightpanda — headless browser cho verify UI

Cài tại `/root/.local/bin/lightpanda` (binary). Dùng để thao tác / verify
UI thật mà không cần Chromium đầy đủ (~120MB vs ~500MB).

### 4 entry points

| Lệnh | Dùng cho |
|------|---------|
| `lightpanda fetch <url>` | 1 lần lấy HTML/markdown sau JS |
| `lightpanda serve` | CDP server port 9222 (Puppeteer/Playwright) |
| `lightpanda run script.js` | PandaScript — automation reproducible |
| `lightpanda mcp` | MCP stdio cho AI agent (Cursor/Claude Code/Codex) |
| `lightpanda agent` | REPL tương tác natural language |

### Verify scripts trong `scripts/`

- **`check-js.sh`** — `node --check` trên từng `<script>` block trong HTML.
  Phát hiện SyntaxError. Đã tích hợp vào watch mode (skip restart nếu fail).
- **`check-ui.sh`** — Smoke test qua Lightpanda CDP: navigate `/chat`, check
  DOM (title, CK_SCRIPT, mg-add, modal, cv count...). Tự bắt exceptionDetails
  trong response của Runtime.evaluate (Lightpanda early version không emit
  `Runtime.exceptionThrown` qua event).
- **`lp-tests/chat-basic.js`** — PandaScript 3 test case: load /chat,
  click + Thêm (QR load), switch tab Cookie.

### MCP config

- **Codex CLI**: `~/.codex/config.toml` có `[mcp_servers.lightpanda]`.
  Cần **restart session Codex mới** để pick up (session đang chạy cache config).
- **Claude Code**: `~/.claude.json` đã add. Verify `claude mcp list` → "Connected".
- **Cursor / Windsurf**: thêm vào `.cursor/mcp.json` hoặc Cascade MCP settings.

### Caveat quan trọng (Lightpanda 1.0.0-nightly)

- KHÔNG emit `Runtime.exceptionThrown` qua CDP event → phải check
  `exceptionDetails` trong response của `Runtime.evaluate`.
- `window.onerror` không bắt được uncaught error từ script khác.
- Để bắt mọi runtime error tự động → fallback Puppeteer + Chromium.

### Verify nhanh

```bash
# 1. JS syntax
./scripts/check-js.sh

# 2. UI smoke test
./scripts/check-ui.sh

# 3. PandaScript test (cần zcloudd đang chạy)
lightpanda run scripts/lp-tests/chat-basic.js

# 4. MCP đã add cho Codex/Claude Code
codex mcp list
claude mcp list
```
