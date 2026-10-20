# Task 30: Fix realtime load tin nhắn trên Web UI

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §3 và §5.25
- **Trạng thái:** 🟢 Done (20/10/2026) — Sếp báo ngày 20/10/2026
- **Mức độ:** Cao — ảnh hưởng trực tiếp mục tiêu chat real-time
- **Liên quan:**
  - [06-build-webui.md](06-build-webui.md) — Web UI ban đầu
  - [14-split-webui.md](14-split-webui.md) — tách `chat.html` thành `chat.js`
  - [19-multi-account-filter.md](19-multi-account-filter.md) — nhiều account enabled

## Hiện tượng

Trong `/chat`, tin nhắn mới không tự xuất hiện. Sếp phải:

- F5 trang, hoặc
- chuyển sang conversation khác rồi quay lại conversation đang xem.

Sau khi chuyển conversation, `sc()` gọi `lm()` nên dữ liệu mới được đọc lại
từ DB. Điều này giải thích vì sao đổi conv lại thấy tin mới.

## Nguyên nhân đã xác định

Frontend hiện **không mở browser WebSocket**:

- `internal/api/web/chat.js` dòng 4-7 ghi rõ browser WS đã bỏ và chỉ giữ
  biến `wsr=null`.
- Không có lời gọi `new WebSocket(...)` cho endpoint `/ws?accountId=...`.
- `DOMContentLoaded` chỉ chạy `loadAccounts()`, `sy()` và health-version
  polling; không subscribe sự kiện realtime.
- `sc()` gọi `/api/messages/sync` rồi `lm()` đúng một lần. Không có cơ chế
  reload khi tin mới được ghi vào DB.

Backend vẫn hoạt động đúng:

- `handleZaloEvent` lưu tin nhắn và gọi `WSManager.Broadcast`.
- `/ws` đã có handler, room theo `accountId`, gửi event `new_message`,
  `old_message`, `media_downloaded`, `typing`, `reaction`, `seen`,
  `delivered`.

Vì frontend không kết nối `/ws`, broadcast hiện không có browser connection
nào để nhận.

## Phạm vi fix

### 1. Browser WebSocket manager

- Sau khi `loadAccounts()` xong, mở một WebSocket cho mỗi account enabled.
- Map theo `accountId` để không tạo socket trùng.
- URL dùng `ws:` hoặc `wss:` theo `location.protocol`.
- Khi checkbox enabled thay đổi:
  - account bật → mở socket;
  - account tắt → đóng socket và clear reconnect timer.
- Reconnect có backoff, tránh loop khi server restart.

### 2. Dispatch event realtime

- `new_message`:
  - Nếu `convId === cc`: append bubble ngay, dedupe theo message ID,
    `regroupMessages()`, cập nhật `cvCache`.
  - Nếu đang ở gần cuối thread thì auto-scroll; nếu Sếp đang đọc tin cũ
    thì giữ vị trí scroll.
  - Nếu tin thuộc conversation khác: debounce gọi `sy()` để cập nhật
    sidebar, thêm unread badge cho conversation đó.
- `old_message`: append nếu đúng conversation hiện tại, dedupe theo ID.
- `media_downloaded`: gọi `applyMediaDownloaded()`.
- `typing`, `reaction`, `seen`, `delivered`: xử lý tối thiểu hoặc bỏ qua
  có log, không được throw.

### 3. Dedupe và optimistic send

- Mọi bubble từ DB/WS phải dedupe theo `message.id`.
- Tin tự gửi hiện được render optimistic nhưng không có `data-msg`; khi
  `new_message` echo về có thể bị lặp.
- Fix một trong hai cách:
  - dùng `msgId` server trả về trong response gửi tin để gắn `data-msg`;
  - hoặc đánh dấu bubble optimistic bằng `clientId/tmpId` và thay thế khi
    broadcast echo tương ứng.

### 4. Sửa lỗi phụ trong `scrollLastMessage`

- Nhánh fallback đang dùng biến không tồn tại `ms.querySelectorAll`.
- Đổi thành `ml.querySelectorAll` và thêm test cho trường hợp element cuối
  là button/spinner/end marker.

## Files dự kiến sửa

- `src/zcloud/internal/api/web/chat.js`
- `src/zcloud/internal/api/web/chat.css` nếu thêm unread badge
- `src/zcloud/internal/api/web/chat.html` nếu cần container trạng thái WS
- `src/zcloud/internal/api/ws.go` nếu cần bổ sung payload/registry
- `src/zcloud/internal/api/handlers_test.go` hoặc test WS mới
- `scripts/check-ui.sh` hoặc `scripts/lp-tests/` nếu thêm smoke test

## Verification

- [x] `node --check src/zcloud/internal/api/web/chat.js` pass.
- [x] `./scripts/check-ui.sh` pass 14/14 — có test dispatch `new_message`
  + dedupe theo `msgId`.
- [x] `go build ./...` pass.
- [ ] Live: gửi tin từ Zalo phone vào đúng thread Trần Ngọc Đức (chờ Sếp
  xác nhận trên trình duyệt thật).

## Kết quả triển khai

- `chat.js`: WS manager 1 socket/account enabled, reconnect backoff
  (1s → 30s), đóng socket khi account tắt.
- Dispatch `new_message`/`old_message` dedupe theo `msgId`;
  `media_downloaded` swap media local.
- Optimistic send dùng `data-tmp`; WS echo về trước response được adopt
  (gắn `data-msg`) thay vì render bubble thứ hai.
- Conv khác có tin mới → unread badge + refresh sidebar debounce 600ms;
  conv đang mở giữ scroll nếu user đang đọc tin cũ.
- Fix `ms.querySelectorAll` → `ml.querySelectorAll` trong
  `scrollLastMessage`.
- Thêm khai báo `var cc=''` — trước đây `cc` là implicit global, đọc từ
  `sy()` trước khi mở conv gây ReferenceError.

## Không nằm trong task này

- Push notification khi tab trình duyệt đóng.
- Sync lịch sử sâu >6h; việc đó thuộc Task 22/23.
- Thay WebSocket bằng SSE hoặc polling toàn bộ.

## Rủi ro

- Reconnect storm nếu server restart hoặc session Zalo lỗi.
- Multi-account có thể tạo nhiều socket nếu không dedupe theo account ID.
- Race giữa optimistic send và WS echo có thể gây bubble trùng.
- Scroll có thể giật nếu event đến khi user đang đọc tin cũ.

## Estimate

- WebSocket manager + dispatch: 0.5 ngày.
- Dedupe optimistic send + unread state: 0.5 ngày.
- Test live và edge cases: 0.5 ngày.
- Tổng: khoảng 1-1.5 ngày.
