# Task 20: Login Zalo PC (trusted-device protocol)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md) §5.14 (T14)
- **Trạng thái:** 🟡 Pending — chờ Sếp duyệt plan
- **Phụ thuộc:**
  - [11-getfriends.md](11-getfriends.md) — `accountID + userID`
  - [15-reverse-zalo-pc.md](15-reverse-zalo-pc.md) — reverse Zalo PC bundle
  - T11.4 (commit `d577778`) — `core.EventType` 8 type + `DesktopSyncEvent`
  - T11.6 (incomplete) — WASM reverse cho trusted-device key exchange

## Bối cảnh & yêu cầu

Hiện tại zcloud chỉ hỗ trợ **Zalo Web login** (cookie + QR). Triệu chứng
Sếp gặp 11/09/2026: login Zalo web account A (qua cookie) → zcloud listen
WS OK. Sau đó Sếp login Zalo web account A **ở browser khác** (đăng xuất
session cũ) → Zalo invalidate session cũ → zcloud WS bị **kickout** →

ngừng nhận tin nhắn.

**Zalo PC client** (ứng dụng desktop) dùng **trusted-device protocol** riêng
với **WASM key exchange** — không bị invalidate khi user login web khác.
PC client có thể coexist với web session khác trên cùng tài khoản.

Sếp yêu cầu 11/09/2026: implement login Zalo PC cho zcloud.

## Kiến trúc

### Zalo PC protocol (đã reverse 1 phần ở task 15)

Zalo PC bundle (`work/reverse-zalo-pc-20260811/app.asar`) chứa:
- `app.asar` — Electron app, framework + API/auth/crypto + desktop sync.
- WASM modules (`.wasm`) cho trusted-device key exchange.
- Native modules (.node) — ZCrypto cho encryption.

Flow login PC:
1. Client kết nối `wss://ws3-msg.chat.zalo.me` (giống web nhưng dùng
   desktop subprotocol + WASM key).
3. Client gửi cmd 501/521/510/511 như web — nhưng payload **encrypt bằng
   AES-GCM + cipherKey** từ WASM (không ph ac AES-CBC + zero IV).
4. Zalo server trust device key WASM → không kickout dù có session khác.

### Phạm vi implement

**Phase 1 (1-2 commit) — Zalo PC endpoint + WebSocket reconnect loop**:
- Thêm flag `transport: "pc" | "web"` vào Account struct.
- Thêm endpoint `POST /api/login/pc` body `{imei, computerName, signKey}`
-} → tạo session kiểu PC + start listener với WS URL khác.

**Phase 2 (1-2 commit) — AES-GCM encrypt layer**:
- `core.SendMessage/ReceiveMessage` switch transport → dùng AES-GCM
  payload thay vì AES-CBC.
- Verify bằng cách gửi/nhận tin qua PC endpoint.

**Phase 3 (3-5 commit) — WASM reverse cho trusted-device**:
- Extract `.wasm files từ `app.asar` (asar extract tool).
- Decompile bằng wabt / wasm2wat / wasm-decompile.
- Map hàm WASM → schema JSON key exchange.
- Implement key derivation + verify signature trong Go.
- Bind vào PC endpoint + WS loop.

### Files sẽ tạo / sửa

- **Mới:**
  - `internal/core/pcclient.go` — Client với transport="pc".
  - `internal/core/pccrypto.go` — AES-GCM + WASM key.
  - `internal/api/handlers.go` — `HandlePCLogin` endpoint.
  - `docs/references/zalo-pc/` — extracted WASM, decoded schema.
  - `internal/core/pccrypto_test.go` — round-trip AES-GCM.

- **Sửa:**
  - `internal/store/types.go` — Account thêm `Transport string`.
  - `internal/api/router.go` — `POST /api/login/pc`.
  - `internal/api/ws.go` — `clientFromSession` switch transport.
  - `internal/core/types.go` — `Session.Transport`.

### Công nghệ WASM reverse

Tools có sẵn:
- `npm install -g asar` → extract app.asar.
- `wasm2wat` (wabt) → WAT text.
- `wasm-decompile` → C-like pseudocode.
- `wasm-objdump -d` → disassembly.
- Optional: Ghidra + wasm plugin cho control flow graph.

Reference work đã có (`work/reverse-zalo-pc-20260811/`):
- `evidence/E-REPORT.md` — case reverse Phase 1.
- `app.asar` extract sẵn.
- `framework/` — Electron main process JS.
- `web/` — renderer JS.

### Verification

- [ ] Login qua `/api/login/pc` thành công, session stored.
- [ ] WS connect bằng PC protocol, không bị kickout.
- [ ] Gửi/nhận tin nhắn round-trip OK.
- [ ] Login web session khác song song → zcloud PC session vẫn listen.
- [ ] Đăng xuất Zalo PC (UI Zalo) → WS zcloud bị disconnect sau ~30s.
- [ ] Auto-reconnect sau disconnect (đã có sẵn từ ws.go).
- [ ] `go test ./...` pass.
- [ ] UI smoke test pass.

### Không nằm trong task này

- Persistent encrypted local store (Postgres zcloud đã có).
- Cross-device backup/sync (đã có plan riêng — T11.7).
- Trusted-device key rotation flow.

### Rủi ro

- **WASM reverse** có thể mất 2-3 ngày nếu binary phức tạp.
- Zalo có thể update PC client thay đổi schema → cần maintain.
- Key WASM có thể được verify server-side chặt — fail dù schema đúng.
- Auto-update PC client có thể break key.

### Estimate

- Phase 1: 0.5 ngày (skeleton + endpoint).
- Phase 2: 1 ngày (AES-GCM + WS loop).
- Phase 3: 3-5 ngày (WASM reverse + key exchange).

Tổng: ~1 tuần.
