# Task 22: SyncV2 backup từ Zalo server (Phase B — sync sâu)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.16 (T16 — mới)
- **Trạng thái:** 🟡 Pending — Sếp duyệt ngày 12/09/2026
- **Phụ thuộc:**
  - [21-fix-newmessage-parse.md](21-fix-newmessage-parse.md) — Phase A (parse đúng)
  - [16-auto-sync-media.md](16-auto-sync-media.md) — T11 đang in-progress
  - `docs/protocol/pc-desktop.md` § Desktop-specific Sync + WASM
  - `work/reverse-zalo-pc-20260811/evidence/E-008.md` — WASM exports
  - `work/reverse-zalo-pc-20260811/evidence/E-010.md` — SyncV2 sub-worker

## Bối cảnh

WS cmd 510/511 chỉ sync được ~50 tin gần nhất / conv (Zalo giới hạn). Để
lấy lịch sử sâu hơn, cần dùng **SyncV2 backup flow** — flow mà Zalo PC
dùng để pull messages từ mobile/cloud.

Theo `docs/protocol/pc-desktop.md`:

| Cmd | Path / WS | Mục đích |
|-----|-----------|----------|
| 12888 | `POST /api/transfer-sync-v2/request-sync` | Mở session SyncV2 |
| 12412 | `GET /api/message/get_crossdb` | Lấy cross-device DB snapshot |
| 12000 | `POST /api/message/pull_mobile_msg` | Pull từng batch messages |
| 590-592 | WS | Event điều phối transfer state machine |
| 630-634 | WS | Backup session |

## Phạm vi Phase B

**Mục tiêu**: pull được lịch sử sâu (>6h) qua SyncV2 backup flow.

**Sub-task**:

### T22.1 — Capture payload SyncV2 (cần Sếp)
- Sếp login Zalo PC client 1 lần với account Trần Ngọc Đức.
- Dùng Wireshark/Charles Proxy capture:
  - Request + response `/api/transfer-sync-v2/request-sync` (cmd 12888).
  - Event WS 590-592 (payload `{reqId, data}` AES-CBC encoded).
  - Response `/api/message/pull_mobile_msg` (cmd 12000) cho 2-3 batch.
- Lưu capture vào `work/syncv2-capture-20260912/` (gitignored).
- **Không thể làm nếu không có capture thật** — em không reverse được
  schema đầy đủ từ bundle tĩnh (AES-CBC key derivation cần runtime data).

### T22.2 — Implement SyncV2 client (sau T22.1)
File mới: `src/zcloud/internal/core/syncv2.go`

```go
type SyncV2Client struct {
    session    *Session
    pcName     string
    publicKey  []byte  // ed25519 từ WASM zprotoSync2CreateMetadataCipher
    tempKey    []byte  // từ WS event 590-592
    syncSession string  // từ response request-sync
}

// RequestSync gọi /api/transfer-sync-v2/request-sync với AES-CBC params.
// Trả về syncSession + tempKey encrypted.
func (c *SyncV2Client) RequestSync(ctx context.Context) (*SyncV2Init, error)

// PullBatch gọi /api/message/pull_mobile_msg với from_seq_id.
// Trả về next_seq_id + encrypted batch messages.
func (c *SyncV2Client) PullBatch(ctx context.Context, fromSeq int64) (*SyncV2Batch, error)

// DecryptMessages giải mã batch bằng public_key + temp_key (WASM logic).
func (c *SyncV2Client) DecryptMessages(ct []byte) ([]Message, error)
```

### T22.3 — Hook vào WS listener (sau T22.2)
- Khi WS nhận `BackupMeta` (cmd 632) → trigger `SyncV2Client.RequestSync`.
- Lưu `syncSession` + `tempKey` vào `accounts` row (cột mới `syncv2_state`).
- Background goroutine loop gọi `PullBatch` cho đến khi `done=true`.

### T22.4 — Persist vào messages table
- Mỗi message pull được → `SaveMessage` (giống Phase A).
- Update `conversations.last_msg_at` cho UI hiển thị.
- SyncScheduler tích hợp: nếu account có `syncv2_state` chưa hoàn thành → 
  resume pull thay vì skip.

## Partial-progress strategy (chống session bị ngắt)

Phòng trường hợp session bị ngắt giữa chừng khi code/sửa:

1. **T22.1 capture**: dữ liệu capture lưu vào `work/syncv2-capture-20260912/`
   (gitignored). Nếu session ngắt giữa lúc capture đang chạy, dữ liệu đã
   có vẫn dùng được cho T22.2.

2. **T22.2 implementation** chia thành 3 sub-commit:
   - `22.2a`: `SyncV2Client` struct + `RequestSync` skeleton (mock data).
   - `22.2b`: `PullBatch` + error handling.
   - `22.2c`: `DecryptMessages` (sau khi biết AES-CBC key derivation).
   Mỗi commit đều `go build ./...` PASS + có unit test riêng.

3. **T22.3 hook** độc lập với T22.2:
   - Có thể commit + push WS event handler riêng.
   - Nếu T22.2 chưa xong → handler vẫn trigger nhưng `SyncV2Client` 
     return error → graceful log, không crash.

4. **T22.4 persist** độc lập:
   - Có thể chạy end-to-end test với mock data trước.
   - Nếu sync giữa chừng bị ngắt → `syncv2_state.last_seq_id` persist
     trong DB → resume từ `last_seq_id + 1` khi restart.

5. **State machine** quan trọng — mỗi sub-task phải:
   - Persist `last_seq_id` vào DB sau mỗi batch thành công.
   - Khi restart → đọc `last_seq_id` từ DB → tiếp tục.
   - Tránh pull lại từ đầu (rate-limit + tốn bandwidth).

## Files sẽ tạo / sửa

- **Mới**:
  - `src/zcloud/internal/core/syncv2.go` — SyncV2 client.
  - `src/zcloud/internal/core/syncv2_test.go` — unit test với mock data.
  - `src/zcloud/internal/api/syncv2_scheduler.go` — background worker.
  - `work/syncv2-capture-20260912/` — capture từ Sếp (gitignored).
  - `docs/protocol/syncv2.md` — reverse notes sau khi có capture.
- **Sửa**:
  - `src/zcloud/internal/store/queries.go` — thêm cột `syncv2_state JSONB`
    vào `accounts` (migration an toàn qua `INFORMATION_SCHEMA`).
  - `src/zcloud/internal/store/types.go` — `Account.SyncV2State`.
  - `src/zcloud/internal/api/ws.go` — handler `BackupMeta` trigger 
    SyncV2 request.

## Verification

- [ ] T22.1: capture có đầy đủ request/response/event cho 1 sync round-trip.
- [ ] T22.2: `TestSyncV2_RequestSync` PASS với mock capture.
- [ ] T22.3: WS event 632 → log `[syncv2] request-sync sent` không crash.
- [ ] T22.4: end-to-end pull 10 batch messages → 10 rows mới trong DB,
      đúng `conv_id` + `from_id`.
- [ ] Restart daemon giữa chừng → resume từ `last_seq_id` đúng vị trí.
- [ ] `journalctl -u zcloud --since '1 hour ago' | grep syncv2` có log 
      cho từng batch.
- [ ] `go test ./internal/core/... -run TestSyncV2` PASS.
- [ ] `go build ./...` PASS.

## Không nằm trong phase này

- Snapshot DB nguyên (`get_crossdb`) → Phase C (task 23).
- Trusted-device WASM linking → không cần (chỉ dùng cho companion app).
- AES-GCM encrypt mode (chỉ khi login PC transport=pc).

## Rủi ro

- **Cao**: cần Sếp capture payload thật. Không có capture → dừng task.
- **Trung bình**: AES-CBC key derivation cần `public_key` + `temp_key` +
  WASM logic (`zprotoSync2CreateMetadataCipher`). Nếu không reverse được 
  WASM → không decrypt được batch → không có message.
- **Thấp**: schema payload đã có trong `docs/protocol/pc-desktop.md` + 
  `E-005.md` (cmd 12412/12000/12003/12096/12700/12888 + paths).

## Estimate

- T22.1: phụ thuộc Sếp (capture 30 phút).
- T22.2: 1-2 ngày (sau capture).
- T22.3: 0.5 ngày.
- T22.4: 0.5 ngày.
- Tổng: ~2-3 ngày (không tính Sếp capture).

