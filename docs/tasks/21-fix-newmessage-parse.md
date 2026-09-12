# Task 21: Fix bug parse EventNewMessage wrapper (Phase A — sync sâu)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.15 (T15 — mới)
- **Trạng thái:** 🟡 Pending — Sếp duyệt ngày 12/09/2026
- **Phụ thuộc:**
  - [04-build-core.md](04-build-core.md) — `internal/core/websocket.go`
  - [10-sync-history.md](10-sync-history.md) — `handleOldMessages` đã làm đúng
  - `docs/references/zca-js/src/apis/listen.ts:259` — Zalo JS reference

## Vấn đề

Bug phát hiện ngày 12/09/2026: WS cmd 501/521 (realtime new message) đến, code
`parseMessages` chạy nhưng `SaveMessage` không bao giờ được gọi.

Verify bằng debug log:
```
DEBUG: handleNewMessages tt=0 payload_len=769 data_len=896
       first16=7b226572726f725f636f6465223a302c
DEBUG: handleNewMessages unmarshal OK msgs_len=0 groupMsgs_len=0
```

`first16` = `{"error_code":0,` → Zalo wrap payload trong
`{error_code: 0, data: {msgs: [...]}}` (2 lớp wrapper).

`handleNewMessages` chỉ parse 1 lớp:
```go
type rawData struct {
    Msgs      json.RawMessage `json:"msgs"`
    GroupMsgs json.RawMessage `json:"groupMsgs"`
}
```
→ cả 2 field không có trong root → len=0 → drop.

So sánh với `handleOldMessages` (đã đúng từ trước):
```go
type rawData struct {
    Msgs      json.RawMessage `json:"msgs"`
    GroupMsgs json.RawMessage `json:"groupMsgs"`
    Data      json.RawMessage `json:"data"`   // ← unwrap layer 2
}
```
+ code block `if len(msgsRaw) == 0 && len(rawData.Data) > 0` unwrap `data.msgs`.

## Root cause

Tham chiếu `zca-js/src/apis/listen.ts:259`:
```ts
const parsedData = (await decodeEventData(parsed, this.cipherKey)).data;
const { msgs } = parsedData;
```

→ Zalo cmd 501/521 **luôn** wrap `{error_code, data: {msgs | groupMsgs}}`.
Code Go hiện tại unwrap 1 lớp (JSON outer), nhưng msgs nằm trong `data`
(lớp 2).

## Phạm vi

**Phạm vi Phase A** (~30 phút):
- Fix `internal/core/websocket.go:handleNewMessages` — unwrap `data.msgs` /
  `data.groupMsgs` giống `handleOldMessages`.
- Bỏ debug log `DEBUG: handleNewMessages` đã inject lúc debug.
- Smoke test: gửi tin từ Trần Ngọc Đức theo §5.4 → verify DB có row mới
  trong <1s.

**Không thuộc phase này**:
- Cross-device/backup sync (xử lý ở task 22, 23).
- Trusted-device WASM (không cần).
- AES-GCM encrypt mode cho send (chỉ cần khi login PC transport=pc).

## Files sẽ sửa

- **Sửa**:
  - `src/zcloud/internal/core/websocket.go:handleNewMessages` — thêm field
  `Data` vào struct, unwrap giống `handleOldMessages`.
  - `src/zcloud/internal/core/websocket.go:parseMessages` — không đổi.

## Partial-progress strategy (chống session bị ngắt)

Phòng trường hợp session bị ngắt giữa chừng khi code/sửa:

1. **Backup code trước khi sửa**: copy `internal/core/websocket.go` vào
   `tmp/scratch/pre-phase-a-websocket.go.bak` (gitignored, an toàn).
2. **Commit atomic**: 1 commit duy nhất cho cả phase A.
3. **Revert command dự phòng**: `git revert HEAD --no-edit` nếu break.
4. **Build verify**: `cd src/zcloud && go build ./...` phải PASS trước commit.
5. **Test verify**: `go test ./internal/core/... -run TestDecodeWSEvent -v` PASS.
6. **Smoke test** với Trần Ngọc Đức (conv 4866700441106275565, marker `[T15-...]`)
   theo quy ước §5.4 trước khi declare xong.

Nếu bị ngắt giữa chừng sau khi đã inject debug log: revert file từ
`.bak`, hoặc `git checkout HEAD -- src/zcloud/internal/core/websocket.go`.

## Verification

- [ ] `go build ./...` PASS.
- [ ] `go test ./internal/core/... -run TestDecodeWSEvent` PASS.
- [ ] Gửi `[T15-1789123456] phase A verify` từ Trần Ngọc Đức → DB có row
      mới (count tăng 1) trong <2s.
- [ ] WS broadcast `new_message` event đến browser <1s.
- [ ] `journalctl -u zcloud --since '1 min ago' | grep 'new msg from'` có log
      cho message mới (trước đây log này im lặng trong nhiều giờ).
- [ ] Không có log `unmarshal err` cho cmd 501/521.

## Không nằm trong phase này

- Lịch sử cũ hơn ~6h (Zalo WS cmd 510/511 chỉ sync giới hạn) → Phase B/C.
- Sync media từ backup → Phase B/C.
- Multi-account filter UI → task 19.

## Rủi ro

- **Thấp**: chỉ thêm field `Data` + unwrap block, pattern đã có sẵn ở
  `handleOldMessages` (đã chạy production nhiều tháng).
- Nếu Zalo đổi schema (bỏ wrapper hoặc thêm lớp 3), phải capture payload
  thật để xác nhận lại.

## Estimate

~30 phút (sửa + test + commit + push).

