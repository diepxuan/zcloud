# Task 32: Review tên group và tên tài khoản Zalo

## Liên kết

- **Task list:** [../tasks.md](../tasks.md) §3 và §5.27
- **Trạng thái:** ✅ Done (21/10/2026)
- **Mức độ:** Trung bình — sai hiển thị identity, không ảnh hưởng dữ liệu tin nhắn
- **Liên quan:**
  - [31-fix-group-thread-mix.md](31-fix-group-thread-mix.md) — repair và scoping conversation
  - `internal/api/handlers.go` — sync conversation, profile, group
  - `internal/store/queries.go` — cập nhật identity conversation
  - `internal/api/web/chat.js` — tránh gọi sync dồn dập từ browser WS

## Hiện tượng

Sau khi Task 31 sửa việc trộn tin nhắn nhóm, một số conversation nhóm vẫn
hiển thị tên là ID số. Tên tài khoản Zalo cũng cần được đối chiếu lại giữa
`accounts` và `zalo_accounts`, đặc biệt sau khi sync conversation.

## Nguyên nhân

1. `clearUnreads` có thể trả `userName` rỗng hoặc bằng ID. Bước
   `SaveConversation` sau đó ghi đè tên group đã resolve trước đó.
2. Group cũ chỉ còn trong DB không được đưa vào batch `GetGroupInfo` nếu Zalo
   không trả group đó trong danh sách conversation hiện tại.
3. `GetGroupInfo` đôi lúc bỏ sót một group trong batch lớn; lỗi này trước đây
   không được retry hoặc log ID cụ thể.
4. Một request sync có thể mất 30-90 giây. Timeout HTTP cũ 15 giây làm
   response rỗng dù DB đã cập nhật; browser WS cũng có thể tạo nhiều request
   sync chồng nhau.
5. Tên tài khoản chỉ được cập nhật ở một phần identity, chưa đồng bộ cả
   `accounts` và `zalo_accounts`.

## Phạm vi fix

### T32.1 — Giữ identity đã resolve

- Thêm `mergeConversationForSync`: giữ tên/avatar/last message/unread từ DB
  khi payload Zalo thiếu field.
- Không ghi đè tên thật bằng chuỗi ID hoặc tên rỗng.
- Chỉ cập nhật `name`/`avatar` sau khi resolve group qua
  `UpdateConversationIdentity`, không đụng trạng thái tin nhắn.

### T32.2 — Resolve group đầy đủ

- Đưa group stale còn trong DB vào batch resolve.
- Retry riêng từng group bị thiếu sau batch lớn.
- Log `conversation_group_info_incomplete` kèm `missing_ids`.
- Chặn map nil trước bước retry.
- Không bịa tên và không xoá conversation khi Zalo không trả metadata.

### T32.3 — Đồng bộ tên tài khoản

- Lấy profile qua `GetMyProfile` với timeout riêng.
- Cập nhật cả `accounts` và `zalo_accounts`.
- Giữ avatar cũ nếu profile không trả avatar; log lỗi profile thay vì bỏ qua
  hoàn toàn.

### T32.4 — Ổn định sync và UI

- Timeout riêng: conversation 45 giây, profile 10 giây, group 30 giây.
- Tăng HTTP `WriteTimeout` lên 3 phút để response không bị đóng giữa sync.
- Khóa sync theo `accountId`, không chặn các account khác.
- Nhả khóa ngay sau khi ghi DB xong, trước khi ghi response JSON.
- Browser chỉ gọi server sync khi mở trang; event WS chỉ reload conversation
  từ DB.

Không thay đổi schema.

## Kiểm chứng dữ liệu live

Account `acc_2291602426651082808`:

- 86 conversation nhóm.
- 84 group có tên.
- 2 group chưa resolve: `2927187516150146410`, `6654693590337934511`.

Account `acc_559609701372941728`:

- 9 conversation nhóm.
- 8 group có tên.
- 1 group chưa resolve: `1816136005206752080`.

Ba ID trên không được `GetGroupInfo` trả metadata, kể cả khi retry riêng.
Không có nguồn tên hợp lệ khác nên giữ nguyên ID thay vì đoán.

Tên tài khoản đã đối chiếu:

- `acc_2291602426651082808`: `Trần Ngọc Đức`.
- `acc_559609701372941728`: `Chăn Ga Gối Nệm Điệp Xuân`.

Sync live sau fix:

- Account chính: HTTP 200, 59,3 giây, 246 conversation.
- Account phụ: HTTP 200, 9,6 giây, 99 conversation.

## Test hồi quy

- `TestMergeConversationForSyncPreservesResolvedIdentity`.
- `TestMergeConversationForSyncAcceptsNewIdentity`.
- `TestMergeConversationForSyncNilExisting`.
- `TestConversationSyncLockScopedByAccount`.
- `TestUpdateConversationIdentityPreservesMessageState`.
- `go test ./...` pass.
- `go build ./...` pass.
- `./scripts/check-js.sh` pass.
- `./scripts/check-ui.sh` pass.

## Rủi ro và kiểm soát

- Group removed hoặc không còn quyền truy cập có thể không bao giờ resolve
  được tên. Giữ ID số và log rõ thay vì xoá dữ liệu.
- `GetGroupInfo` có thể trả map nil mà không báo lỗi; handler khởi tạo map
  trước retry để tránh panic.
- Sync lâu hơn timeout cũ; đã tách timeout theo từng bước và khóa theo
  account để không tạo request chồng nhau.

## Không nằm trong task này

- Xoá conversation stale hoặc group đã bị Zalo gỡ.
- Thay đổi schema.
- Suy đoán tên group từ nguồn không chính thức.
