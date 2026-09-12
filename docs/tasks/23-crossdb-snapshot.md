# Task 23: Cross-device snapshot từ /api/message/get_crossdb (Phase C — sync sâu)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.17 (T17 — mới)
- **Trạng thái:** 🟡 Pending — Sếp duyệt ngày 12/09/2026
- **Phụ thuộc:**
  - [21-fix-newmessage-parse.md](21-fix-newmessage-parse.md) — Phase A
  - [22-syncv2-backup.md](22-syncv2-backup.md) — Phase B (cần trước)
  - [15-reverse-zalo-pc.md](15-reverse-zalo-pc.md) — reverse notes
  - `docs/protocol/pc-desktop.md` § Local SQLite + DB-cross addon
  - `work/reverse-zalo-pc-20260811/evidence/E-005.md,E-011.md` — SQLCipher + db-cross

## Bối cảnh

Phase B (SyncV2) chỉ pull messages qua encrypted batch (~100 msg/lần). Để
lấy **toàn bộ lịch sử** (potentially thousands) cần dùng **cross-device
DB snapshot** — flow Zalo PC dùng khi sync từ mobile.

Theo `docs/protocol/pc-desktop.md` + `E-005/E-011`:

| Step | Cmd / Path | Mục đích |
|------|------------|----------|
| 1 | `POST /api/message/get_crossdb` (12412) | Request snapshot `{pc_name, sync_session}` |
| 2 | Server push DB encrypted qua WS event590-592 | Snapshot data được wrap + AES-CBC mã hóa |
| 3 | `db-cross-v4-native.node` giải mã bằng SQLCipher key | Key derivation từ `dkey` + `UIN` + WASM |
| 4 | Restore vào local SQLite (`_production/*.db`) | Migration script |

## Phạm vi Phase C

**Mục tiêu**: lấy 1 lần snapshot đầy đủ lịch sử từ Zalo server.

**Sub-task**:

### T23.1 — Capture crossdb payload (cần Sếp + bundle gốc)
- Sếp cung cấp bundle `ZaloSetup-26.8.10.exe` (em hiện không có file gốc,
  chỉ có notes/evidence).
- Em extract `.wasm` + `db-cross-v4-native.node` từ bundle → decompile.
- Sếp login PC client → trigger cross-device sync → capture:
  - Request/response `get_crossdb` (cmd 12412).
  - WS event 590-592 payload.
  - Snapshot binary dump (lưu vào `work/crossdb-capture-20260912/`).
- Cần `db-cross-v4-native.node` strings dump (xem E-011) để biết:
  - SQLCipher key derivation: `cipher_compatibility`, `hexrekey`, `textrekey`.
  - Decrypt: `decompressAndDecryptDb` / `decompressAndDecryptDb_V2`.
- **Không thể làm nếu không có bundle gốc**.

### T23.2 — Implement CrossDB parser (sau T23.1)
File mới: `src/zcloud/internal/core/crossdb.go`

```go
type CrossDBClient struct {
    session    *Session
    pcName     string
    syncSession string
}

// RequestSnapshot gọi /api/message/get_crossdb.
// Trả snapshot binary URL + checksum.
func (c *CrossDBClient) RequestSnapshot(ctx context.Context) (*SnapshotInfo, error)

// DownloadSnapshot tải snapshot từ URL trả về.
func (c *CrossDBClient) DownloadSnapshot(ctx context.Context, url string) ([]byte, error)

// DecompressAndDecrypt dùng SQLCipher key (derive từ session.dkey).
func (c *CrossDBClient) DecompressAndDecrypt(ct []byte) (*sql.DB, error)

// ExtractMessages query SQLite đã giải mã → []Message.
func (c *CrossDBClient) ExtractMessages(db *sql.DB, accountID string) ([]Message, error)
```

### T23.3 — Hook vào admin flow (sau T23.2)
- Endpoint mới: `POST /api/sync/snapshot` (admin only).
- Trigger: Sếp bấm nút "Force full sync" trong panel Quản lý (UI thêm 1 nút).
- Background goroutine: pull snapshot → decrypt → SaveMessage batch.
- Hiển thị progress qua WS broadcast `sync_progress` event.

### T23.4 — Persist + resume state
- Mỗi batch 1000 rows → SaveMessage → update `accounts.sync_progress` JSONB.
- Restart giữa chừng → resume từ `last_processed_id`.

## Partial-progress strategy (chống session bị ngắt)

Phase C là phase **lớn nhất và rủi ro nhất**. Chia nhỏ để dễ resume:

1. **T23.1 capture** chia làm 3 sub-step:
   - `T23.1a`: Em extract bundle (cần Sếp cung cấp `.exe`).
   - `T23.1b`: Sếp login + trigger sync + capture WS traffic.
   - `T23.1c`: Em phân tích capture, viết `docs/protocol/crossdb.md`.
   
   Nếu ngắt giữa `T23.1a` và `T23.1b`: extract vẫn còn trong
   `work/extract-zalo-pc-20260912/`, không mất.

2. **T23.2 implementation** chia 4 commit độc lập:
   - `23.2a`: `CrossDBClient` struct + `RequestSnapshot` skeleton.
   - `23.2b`: `DownloadSnapshot` + checksum verify.
   - `23.2c`: `DecompressAndDecrypt` (cần reverse SQLCipher key).
   - `23.2d`: `ExtractMessages` + SaveMessage batch.
   
   Mỗi commit có unit test riêng, có thể commit/push độc lập.

3. **T23.3 hook** chỉ phụ thuộc `RequestSnapshot` (đã có ở 23.2a):
   - UI + endpoint có thể làm trước khi T23.2c xong.
   - Nếu 23.2c fail → endpoint return error rõ ràng cho UI.

4. **T23.4 state machine** giống Phase B:
   - Persist `last_processed_id` sau mỗi batch.
   - Resume state từ DB khi restart.

5. **Fallback quan trọng**: nếu SQLCipher key reverse fail (không giải mã
   được snapshot), Phase C dừng ở T23.2c. Phase A + B đã giải quyết 80%
   vấn đề sync (realtime + backup qua SyncV2). Snapshot chỉ là "nice to have"
   cho lịch sử >1 năm.

## Files sẽ tạo / sửa

- **Mới**:
  - `src/zcloud/internal/core/crossdb.go` — CrossDB client.
  - `src/zcloud/internal/core/crossdb_test.go` — unit test với mock snapshot.
  - `src/zcloud/internal/api/snapshot_scheduler.go` — background worker.
  - `work/crossdb-capture-20260912/` — capture từ Sếp (gitignored).
  - `work/extract-zalo-pc-20260912/` — bundle extracted (gitignored).
  - `docs/protocol/crossdb.md` — reverse notes.
- **Sửa**:
  - `src/zcloud/internal/store/queries.go` — thêm cột `sync_progress JSONB`
    vào `accounts`.
  - `src/zcloud/internal/store/types.go` — `Account.SyncProgress`.
  - `src/zcloud/internal/api/handlers.go` — `HandleForceSnapshot` endpoint.
  - `src/zcloud/internal/api/router.go` — `POST /api/sync/snapshot`.
  - `src/zcloud/internal/api/web/chat.html` — nút "Force full sync" ở panel 
    Quản lý.

## Verification

- [ ] T23.1: bundle extracted, capture có snapshot binary + keys.
- [ ] T23.2: `TestCrossDB_DecompressAndDecrypt` PASS với mock snapshot.
- [ ] T23.3: `POST /api/sync/snapshot` trigger đúng worker, không crash.
- [ ] T23.4: end-to-end 1 snapshot có 5000 messages → DB có 5000 rows mới,
      đúng `conv_id` + `from_id` + `timestamp` (khớp với Zalo mobile app).
- [ ] Restart daemon giữa chừng (sau 2500 rows) → resume từ 2501.
- [ ] UI progress bar hiển thị % sync, kết thúc hiển thị "X messages added".
- [ ] `go test ./internal/core/... -run TestCrossDB` PASS.
- [ ] `go build ./...` PASS.

## Không nằm trong phase này

- Trusted-device WASM linking (chỉ cho companion app).
- AES-GCM encrypt mode (chỉ khi login PC transport=pc).
- Restore local SQLite của PC — zcloud dùng Postgres riêng.
- Giải mã media/file cache (XOR-obfuscated local files — không cần).

## Rủi ro

- **Rất cao**: cần bundle gốc `.exe` từ Sếp. Không có → dừng.
- **Cao**: SQLCipher key derivation. E-005 + E-011 chỉ cho thấy **pragmas**
  (`cipher_compatibility`, `rekey`), không cho thấy **key derivation logic**.
  Có thể mất 2-3 ngày chỉ để reverse.
- **Trung bình**: snapshot binary schema có thể thay đổi giữa các version 
  Zalo PC (26.8.10 có thể khác 26.9.x).
- **Thấp**: nếu fail → Phase A + B đã đủ dùng (sync được ~6h + SyncV2
  pull được vài nghìn messages gần nhất).

## Estimate

- T23.1: 1 ngày (extract + capture + analyze).
- T23.2: 2-3 ngày (đặc biệt nếu phải reverse SQLCipher key).
- T23.3: 0.5 ngày.
- T23.4: 0.5 ngày.
- Tổng: **~4-5 ngày** (không tính Sếp cung cấp bundle).

