# Task 19: Multi-account filter (chọn account hiển thị trong UI)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.13 (T13)
- **Trạng thái:** 🟡 Pending
- **Phụ thuộc:**
  - [07-multi-user.md](07-multi-user.md) — accounts + sessions
  - [11-getfriends.md](11-getfriends.md) — friends + contacts
  - [09-database.md](09-database.md) — `accounts` table

## Bối cảnh & yêu cầu

Hiện tại (sau task 07 + bug fix `/api/qr/poll` GET): zcloud hỗ trợ nhiều
account, mỗi account có 1 WS listener riêng, lưu session + messages + convs
riêng trong DB. Web UI có panel **Quản lý tài khoản** (`#pn-mg`) liệt kê
account, mỗi account có nút Restart + Xoá.

**Tuy nhiên UI chỉ "xem được" 1 account tại 1 thời điểm** (`ca` — current
account). Conversation list + friends list chỉ load cho `ca`. Các account
khác listen WS bình thường, có data trong DB nhưng không hiển thị ở đâu.

Sếp yêu cầu 11/09/2026: thêm checkbox cho mỗi account để chọn subset
hiển thị. Account không check vẫn listen + lưu data nhưng UI ẩn.

## Thiết kế

### Backend

**Schema** — thêm cột `enabled BOOLEAN NOT NULL DEFAULT TRUE` vào
`accounts` table.

```sql
ALTER TABLE accounts ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT TRUE;
```

Migrate tự động trong `store.NewPostgres` khi khởi động (dùng
`INFORMATION_SCHEMA.COLUMNS` check, an toàn với idempotent).

**Store API** (mới — `internal/store/queries.go`):
- `SetAccountEnabled(accountID string, enabled bool) error`
- Modify `Account` struct: thêm `Enabled bool \`json:"enabled"\``.
- Modify `ListAccounts(accountType int)`: thêm query param `enabledOnly bool`
  → chỉ trả account có `enabled = true`. (UI header có thể gọi
  `enabledOnly=true` để lấy subset hiển thị; management panel gọi
  `enabledOnly=false` để thấy tất cả kèm trạng thái on/off.)

**HTTP API** (mới — `internal/api/handlers.go`):
- `POST /api/account/enabled` body `{accountId, enabled}` → gọi
  `SetAccountEnabled`. Trả `{ok: true}`.
- `GET /api/account/list?enabledOnly=true|false` (default false cho backward
  compat — management panel vẫn thấy tất cả).
- Existing `GET /api/account/list` không thay đổi.

**WS listener & sync scheduler** — KHÔNG thay đổi. Tất cả account đều
listen + sync bình thường bất kể `enabled`. Flag `enabled` chỉ ảnh hưởng
UI rendering.

### Frontend

**`internal/api/web/chat.html`** — Quản lý panel (`#pn-mg`):

Mỗi `.mg-item` thêm cột checkbox:
```html
<input type="checkbox" class="mg-enabled" 
       data-id="${a.id}" 
       ${a.enabled?'checked':''}
       onchange="toggleAccountEnabled('${a.id}', this.checked)">
```

CSS: checkbox ở `.mg-row1` đầu mỗi item, căn trái, click riêng không
trigger `renderAccounts`.

`renderAccounts()` — gắn event cho checkbox + hiển thị trạng thái:
- enabled + listening → "🟢 đang dùng"
- enabled + !listening → "🟡 enabled, listener off"
- disabled → "⚪ tắt (vẫn listen, UI ẩn)"

`toggleAccountEnabled(id, enabled)` — gọi API:
 +```js
async function toggleAccountEnabled(id, enabled){
    await fetch('/api/account/enabled',{
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({accountId:id, enabled:enabled})
    });
    // Reload lại list + reload conversations nếu cần
    await loadAccounts();
    if(!enabled){
        // Nếu đang xem account này mà tắt → chuyển sang account enabled đầu tiên
        if(ca===id){
            var first=accounts.find(function(a){return a.id!==id && a.enabled});
            if(first) switchAcc(first.id);
        }
    }
    sy(); // reload conv list với subset mới
}
```

`renderStats()` — thêm số liệu enabled:
```
5 tài khoản • 3 hiển thị • 4 đang nghe WS • 5 có session
```

Toolbar có thêm "Bật/Tắt tất cả":
```html
<button onclick="toggleAllEnabled()">Bật/Tắt tất cả</button>
```
(ví dụ toggleAllEnabled: nếu tất cả enabled → tắt hết; ngược lại bật hết.)

**Conversation list** (sidebar `#cl`) — merge từ enabled accounts:

Hiện chỉ gọi `/api/conversations/sync?accountId=ca`. Em cần:

1. Frontend lấy **danh sách enabled accounts** từ cache `accounts.filter(a=>a.enabled)`.
2. Loop qua từng enabled account, gọi `/api/conversations?accountId=...` (đã có).
3. Merge kết quả, sort theo `lastMsgAt DESC`.
4. Render mỗi conv kèm badge account (chỉ hiện nếu >1 enabled):
```html
<div class="cv-row">
  <img class="cv-a" src="${c.avatar}">
  <div class="cv-b">
    <div class="cv-n">${c.name}</div>
    <div class="cv-p">${c.lastMsgContent}</div>
    <div class="cv-acc">${accountLabel(a)}</div>  <!-- vd "🔵 Sep" -->
  </div>
</div>
```

CSS: badge nhỏ, màu nhạt, góc dưới phải của conv.

**`switchAcc(id)`** — đổi tên thành `switchAcc` vẫn dùng cho "đang chat"
(account mà message composer gửi đi). Mặc định `ca = accounts[0].enabled.id`.
Khi click conv, tự động set `ca = c.accountId`.

**Header `#header`** — thêm dropdown "Đang chat" để Sếp đổi account chủ
động (khi muốn gửi tin từ account khác):
```html
<select onchange="switchAcc(this.value)">
  <option value="acc_1">Sep Duc</option>
  <option value="acc_2">Phạm Hồng</option>
</select>
```
Ẩn nếu chỉ có 1 enabled account.

**Friends list (`#pn-co`)** — tương tự convs: merge từ enabled accounts,
mỗi friend gắn badge account.

**Auto-refresh** — khi WS `new_message` event đến từ 1 account không
trong enabled list → vẫn lưu DB + broadcast `new_message` qua WS, nhưng
UI cập nhật conv list đúng subset enabled.

## Files sẽ tạo / sửa

- **Sửa:**
  - `internal/store/types.go` — thêm `Enabled bool` vào `Account`.
  - `internal/store/queries.go` — `SetAccountEnabled` + modify
    `ListAccounts`.
  - `internal/store/store.go` — migrate cột `enabled` lúc khởi động.
  - `internal/api/handlers.go` — `HandleAccountList` đọc `?enabledOnly`,
    thêm `HandleAccountSetEnabled`.
  - `internal/api/router.go` — thêm `POST /api/account/enabled`.
  - `internal/api/web/chat.html` — checkbox trong management panel,
    merge convs/friends, dropdown header.
- **Mới:**
  - `internal/store/queries_test.go` — test SetAccountEnabled + ListAccounts
    với enabledOnly.

## Verification

- [ ] Migrate cột `enabled` chạy đúng (Postgres test schema).
- [ ] `POST /api/account/enabled` flip được giá trị, persist qua DB.
- [ ] Reload trang web, panel Quản lý hiện checkbox đúng trạng thái.
- [ ] Bỏ check 1 account → conv list biến mất account đó.
- [ ] Tắt account `ca` đang chat → tự switch sang account enabled đầu tiên.
- [ ] Multi-account enabled → conv list gộp + badge tên account rõ ràng.
- [ ] Header dropdown "Đang chat" chuyển được account, composer gửi từ đúng account.
- [ ] Account disabled vẫn listen WS (kiểm tra journalctl) + DB có data mới.
- [ ] `go test ./...` pass.

## Không nằm trong task này

- Persist enabled state qua server restart (đã có vì lưu DB).
- Filter theo trạng thái khác (vd chỉ hiện account online) — có thể làm
  sau nếu Sếp cần.
- Mobile app (chỉ làm web hiện tại).

## Rủi ro

- **Performance**: nếu merge convs từ 5+ account, list có thể >500 items.
  Cần virtual scroll hoặc pagination. Phase đầu merge đơn giản,
  optimize sau nếu cần.
- **Race condition** giữa WS event từ account đang tắt và UI update:
  nếu Sếp vừa tắt enabled → UI ẩn → WS broadcast `new_message` cho
  account đó → UI có thể hiện thoáng qua rồi ẩn. Acceptable.
- **Composer** gửi nhầm account nếu Sếp không chú ý `ca`. Dropdown
  header hiển thị rõ `ca` đang chat sẽ giảm rủi ro.
