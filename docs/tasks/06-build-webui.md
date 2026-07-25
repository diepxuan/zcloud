# Task 06: Build Web UI

## Liên kết
- **Master plan:** [master-plan.md](../master-plan.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** [05-build-server.md](05-build-server.md) ✅
- **Kế tiếp:** — (task cuối)

## Mục tiêu
Single Page App — vanilla JS, responsive desktop + mobile, real-time WebSocket.

## Cấu trúc files

```
src/zcloud/web/
├── index.html       # Chat page
├── login.html       # Login page (hoặc gộp vào app.js routing)
├── style.css        # Styles (responsive)
└── app.js           # SPA logic (routing, API calls, WebSocket)
```

## Thứ tự implement

### 06.1 — Login page
- QR code từ `/api/login/qr`
- Poll `/api/login/check` → check status
- States: `waiting` / `scanning` / `confirmed` / `success` / `error`
- Auto-redirect → chat page khi login OK

### 06.2 — Chat page (layout + sidebar)
**Layout:**
```
┌──────────────┬──────────────────────────────┐
│  Sidebar     │  Chat area                   │
│ ┌──────────┐ │ ┌──────────────────────────┐ │
│ │ Search   │ │ │ Header: name + avatar    │ │
│ ├──────────┤ │ ├──────────────────────────┤ │
│ │ Conv 1   │ │ │ Message list             │ │
│ │ Conv 2   │ │ │ • load more (cursor)     │ │
│ │ Conv 3   │ │ │ • real-time push         │ │
│ │  ...     │ │ │ • auto-scroll            │ │
│ └──────────┘ │ ├──────────────────────────┤ │
│              │ │ Input + Send button      │ │
└──────────────┴ └──────────────────────────┘ ┘
```

**Sidebar features:**
- Fetch `/api/conversations` → render list
- Click conversation → load messages
- Search filter
- Unread badge

### 06.3 — Message area
- Load messages: `GET /api/conversations/{id}/messages?cursor=...`
- "Load older" button / infinite scroll
- Send message: `POST /api/messages` {to, content, type}
- WebSocket client: `ws://host/ws` → push new messages
- Auto-scroll to bottom khi có tin mới
- Message timestamp (relative: "2 phút trước", "hôm qua", ...)

### 06.4 — WebSocket real-time
- Connect khi login thành công
- Auto-reconnect khi mất kết nối (hiển thị indicator)
- `onmessage` → parse JSON → append to message list
- Event types: `new_message`, `delivered`, `seen`, `error`

### 06.5 — Polish
- **Responsive:** desktop sidebar+chat, mobile overlay (sidebar trượt vào)
- **Notifications:** Web Notification API khi tab không active
- **Online/offline:** indicator trên header
- **Error toast:** khi API call fail
- **Message status:** sending → sent → delivered (optional)

## API calls (fetch)

```javascript
// QR login
const qrResp = await fetch('/api/login/qr');
const qrBlob = await qrResp.blob();
// Poll check
const checkResp = await fetch('/api/login/check', {method: 'POST'});
// Conversations
const convs = await (await fetch('/api/conversations')).json();
// Messages
const msgs = await (await fetch(`/api/conversations/${id}/messages?cursor=${c}`)).json();
// Send
await fetch('/api/messages', {method: 'POST', body: JSON.stringify({to, content})});
// WebSocket
const ws = new WebSocket(`ws://${host}/ws`);
```

## Output
- `src/zcloud/web/index.html`
- `src/zcloud/web/login.html` (hoặc gộp)
- `src/zcloud/web/style.css`
- `src/zcloud/web/app.js`

## Verification
- [ ] Login QR hiển thị + scan → redirect OK
- [ ] Conversations list render
- [ ] Click conversation → load messages
- [ ] Send message → hiện trên UI
- [ ] Real-time push từ WebSocket → hiện ngay
- [ ] "Load more" hoạt động (cursor pagination)
- [ ] Responsive: desktop + mobile browser
- [ ] Notification khi tab inactive
- [ ] Reconnect indicator khi mất kết nối
- [ ] Error toast khi API fail

## Ghi chú
- Không dùng framework — vanilla JS cho đơn giản, dễ deploy
- ES6+ modules (type="module")
- CSS custom properties cho theme (dễ đổi màu)
- WebSocket URL từ `window.location.host` — tự động đúng
