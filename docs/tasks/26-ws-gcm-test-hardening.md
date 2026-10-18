# Task 26: Test hardening WS AES-GCM (T1) — fix test đỏ + tăng coverage

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5 (tồn đọng T1 "WS AES-GCM decrypt
  hoàn chỉnh" + T3 "integration test") + §5.21
- **Trạng thái:** 🟡 In Progress
- **Phụ thuộc:** [04-build-core.md](04-build-core.md), `internal/core/encrypt.go`

## Bối cảnh

`go test ./internal/core/...` đang **FAIL** ở subtest
`TestEncodeDecodeAESGCM_RoundTrip/mode_2` (`internal/core/encrypt_test.go`).

**Root cause (đã điều tra):** đây là **test sai**, không phải bug production.

- `EncodeAESGCM(key, aad, plain, mode)`: mode `2` (WSEncryptAESGCM) **gzip
  trước** khi mã hoá; mode `3` (WSEncryptAESGCMRaw) mã hoá raw.
- `DecodeAESGCM(key, iv, aad, ct)`: primitive thô — **KHÔNG** gunzip.
- Gunzip nằm ở tầng `DecodeWSEvent` (hàm `gunzipIfNeeded`, chỉ gunzip khi
  payload có magic byte `0x1f 0x8b`).
- Test cũ gọi thẳng `DecodeAESGCM` cho cả mode 2 và 3 rồi so với plaintext →
  mode 2 nhận bytes gzip → mismatch.

Production path (mode 2/3 qua `DecodeWSEvent`) đã được phủ bởi
`TestDecodeWSEventAESGCM` / `...Raw` / `...Base64Key`. Caller production của
`DecodeAESGCM` duy nhất là `decryptAESGCMWithKey` (không gunzip) — xác nhận
qua `rg "DecodeAESGCM\("`.

## Mục tiêu (định nghĩa hoàn thành)

1. `go test ./internal/core/...` **PASS, 0 fail** (test đỏ chuyển xanh).
2. Sửa `TestEncodeDecodeAESGCM_RoundTrip` cho khớp contract thật.
3. Bổ sung test round-trip **end-to-end qua `DecodeWSEvent`** (encode → wrap
   envelope base64 → decode) cho mode 2 và 3.
4. Bổ sung test biên cho `gunzipIfNeeded`, `decodeWSBase64`,
   `decryptAESGCMWithKey`.
5. **KHÔNG đổi hành vi production.** Chỉ sửa/thêm `_test.go`.

## Ràng buộc

- Chỉ sửa file test trong `internal/core/` (`encrypt_test.go` hoặc file test mới
  `encrypt_wsgcm_test.go`).
- **KHÔNG** sửa `internal/core/encrypt.go` trừ khi phát hiện bug thật (khi đó
  báo cáo, KHÔNG tự đổi ngầm).
- Giữ phong cách test hiện có (table-driven khi phù hợp, tên test rõ ràng).

## Thay đổi chi tiết

### 1. Fix `TestEncodeDecodeAESGCM_RoundTrip` (`internal/core/encrypt_test.go`)
- Tách kỳ vọng theo mode:
  - mode `3` (raw): `DecodeAESGCM(...)` == plaintext.
  - mode `2` (gzip): `DecodeAESGCM(...)` == gzip(plaintext) (tức bytes gzip),
    và sau `gunzipIfNeeded` == plaintext.
- Giữ kiểm tra layout `[iv 16][aad 16][ct+tag]` và aad match.

### 2. Thêm `TestEncodeDecodeWSRoundTrip` (mới)
Với mỗi mode ∈ {2, 3}:
- `ct := EncodeAESGCM(key, aad, plain, mode)`.
- Wrap envelope: `{"data":"<base64(ct)>","encrypt":<mode>}`.
- `out, err := DecodeWSEvent(envelope, key)` → `err == nil` và `out == plain`.
- Chạy thêm biến thể `cipherKey` dạng base64 string (raw key vs base64 key).

### 3. Test biên (mới, table-driven)
- `gunzipIfNeeded`:
  - non-gzip bytes → trả nguyên (passthrough).
  - bytes rỗng / 1 byte → passthrough, không panic.
  - magic `0x1f 0x8b` nhưng nội dung hỏng → trả error.
- `decodeWSBase64`:
  - base64 không hợp lệ → error.
  - base64 của dữ liệu < 32 byte → error "short data".
  - base64 hợp lệ ≥ 32 byte → OK.
- `decryptAESGCMWithKey`:
  - raw key (không phải base64) → decrypt OK.
  - base64 key → decrypt OK.
  - key rỗng → error "cipher key is required".

## Kiểm chứng (chạy, ghi output thật)

1. `cd src/zcloud && go test ./internal/core/... -run 'AESGCM|WSEvent|Gunzip|WSBase64|WSRoundTrip' -v` → PASS.
2. `go test ./internal/core/...` → **PASS, 0 fail** (điểm chốt).
3. `go build ./...` PASS; `go vet ./...` sạch.
4. `go test -cover ./internal/core/...` → ghi lại % coverage (so với 31.0% trước).

## Ngoài phạm vi

- Không đổi `encrypt.go`, `websocket.go`.
- Không viết integration test HTTP client (`chat.go` mock server) — task riêng sau.
- Không đụng `syncv2`, `desktop_sync` test.

## Rủi ro

- Thấp — chỉ test. Rủi ro chính là "sửa test cho xanh" mà không phản ánh đúng
  contract → đã chốt contract ở mục Bối cảnh (primitive thô + gunzip ở
  `DecodeWSEvent`), test mới phải phản ánh đúng contract đó.
