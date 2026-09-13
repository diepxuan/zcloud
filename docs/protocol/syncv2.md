# SyncV2 Protocol — Phase B (Task 22)

> Reverse notes từ `tmp/zalo-pc/asar/pc-dist/sync-v2-sub-worker.*.js`
> (SHA-256 81cb71b707f4f4074c9aa751c4f4e6fb584be5c0039ffb6d7ac2f3fd2c7110bc)
> + `tmp/zalo-pc/asar/pc-dist/shared-worker.*.js` (wrapper quanh WASM
> `libzproto_wasm_bg.ecc0555ba9d0f8867a4699ce943ec745.wasm`).
> Ngày reverse: 2026-09-13.

## 1. Mục đích

Pull lịch sử chat sâu (>6h, khoảng 6h-30 ngày) từ Zalo server. WS cmd
510/511 (cũ) chỉ sync ~50 tin gần nhất/conv. SyncV2 dùng flow "transfer
after login" tương tự Zalo PC dùng khi sync từ mobile — kéo encrypted
batch về rồi giải mã bằng session key dẫn xuất từ ed25519 keypair +
`temp_key` mà mobile gửi.

## 2. Endpoints

### 2.1 REST

| Cmd | Path | Params (raw, AES-CBC sau đó) |
|-----|------|------------------------------|
| 12888 | `POST /api/transfer-sync-v2/request-sync` | `{reqId, data}` — `data` là JSON-stringified inner |
| 12412 | `GET /api/message/get_crossdb` | `{pc_name, sync_session}` |
| 12000 | `GET /api/message/pull_mobile_msg` | `{pc_name, public_key, from_seq_id, is_retry, min_seq_id, temp_key, imei}` |
| 12700 | `GET /api/message/cancel_pull_mobile_msg` | `{pc_name, public_key, imei}` |
| 12003 | `GET /api/message/delete_snapshot_mobile_msg` | `{public_key, imei}` |
| 12096 | `GET /api/message/get_backupmsginfo` | (no params) |

Service: `cross_setting.useDevDomain ? getDevDomain() : getFileDomain()`.
Default prod: `https://files-wpa.<dm>` (xem `pc-desktop.md` Hosts).

### 2.2 WebSocket commands

```
SYNC_MESSAGE = {REQUEST: 590, ACK_DELETE_SYNC_SESSION: 591, REQUEST_MOBILE_WAKE_UP: 592}
BACKUP_MSG   = {CREATE_SESSION: 631, INIT: 630, GET_METADATA: 632, SIGNAL_RESTORE: 633, GET_CONFIGS: 634}
```

Tất cả `requestAsyncV2({cmd, subCmd:0, data: {...}})` — payload wrap trong
`{data: ...}` (cùng pattern 2 lớp wrapper như cmd 501/521 đã fix ở task 21).

## 3. State machine

State machine trong `sync-v2-sub-worker.js`, 8 event filters:

```js
isTransferAfterLoginControlEvent: e => e.act === "transfer_after_login"
                                    && e.data.temp_key?.length > 0
                                    && e.data.imei === self.imei
isUserConfirm:    e => e.act === "user_confirm"
                       && (e.data.user_action === 1 || e.data.user_action === 3)
                       && e.hostName === e.data.pc_name
                       && e.publicKey === e.data.public_key
isUserReject:     e => e.act === "user_confirm" && e.data.user_action === 0
isMobileRestoring: e => e.act === "user_confirm" && e.data.user_action === 2
isMobileActive:   e => e.act === "transfer_error" && e.data.status === 1
isMobileIdle:     e => e.act === "transfer_error" && e.data.status === 2
isBackupSuccess:  e => e.act === "syncmsg_info" && e.publicKey === e.data.public_key
isBackupFail:     e => e.act === "transfer_error" && e.data.error_code !== 0
```

### 3.1 Flow happy path

```
[Client]                                   [Zalo Server]                [Mobile]
   |                                              |                        |
   | 1. generate ed25519 keypair                  |                        |
   | 2. REST request-sync (cmd 12888)             |                        |
   |    body: {pc_name, public_key, imei}         |                        |
   |--------------------------------------------->|                        |
   |                                              | WS push event 590       |
   |                                              | (transfer_after_login)  |
   |                                              | payload: {temp_key, ...}|
   |                                              |----------------------->|
   |                                              |   (mobile wakes up)     |
   |                                              |<-----------------------|
   |                                              | WS push event 590/591   |
   |                                              | (user_confirm)          |
   |                                              | payload: {user_action: 1,|
   |                                              |   pc_name, public_key}  |
   | <--------------------------------------------|                        |
   | 3. nhận user_confirm + temp_key              |                        |
   | 4. REST pull_mobile_msg loop:                |                        |
   |    from_seq_id = last_seq_id + 1             |                        |
   |    {pc_name, public_key, from_seq_id,        |                        |
   |     is_retry:0, min_seq_id:0,                |                        |
   |     temp_key, imei}                          |                        |
   |--------------------------------------------->|                        |
   |                                              | response: {messages:   |
   |                                              |  [{sessionId, cipher}],|
   |                                              |  next_seq_id, done}    |
   | 5. DecryptMessages với zprotoSync2DecryptMessage(session, cipher)
   | 6. SaveMessage mỗi tin                       |                        |
   | 7. lặp đến khi done=true                     |                        |
```

### 3.2 user_action

| Value | Ý nghĩa |
|-------|---------|
| 0 | user reject |
| 1 | user accept (chỉ transfer) |
| 2 | mobile restoring (không cần pull thêm) |
| 3 | user accept + restore |

## 4. WASM API (`libzproto_wasm_bg.*.wasm`)

Exports (E-008):

```
zprotoSync2GetSessionId
zprotoSync2CreateMetadataCipher
zprotoSync2EncryptMessage
zprotoSync2DecryptMessage
zprotoSync2DecryptMetadata
zprotoEd25519GenerateKeyPair
zprotoEd25519DerivePublicKey
zprotoEd25519CalculateAgreement
zprotoEd25519CalculateSignature
zprotoEd25519VerifySignature
```

Lưu ý: `zprotoSync2CreateMetadataCipher` arg list trong source JS:
```js
zprotoSync2CreateMetadataCipher(u,h,g,y,f,S,w,_,C,T,E,o,c)
```
Tương ứng 13 args → dùng `wasm-bindgen-export-*` indexing. Có thể sẽ
cần test bằng capture thật để biết chính xác từng arg. Xem
`docs/tasks/22-syncv2-backup.md` §Phase B verification.

## 5. PC Name & IMEI

- `pc_name` = hostname máy (vd `ZALO-PC\john` hoặc qua helper
  `getHostName()`) — bundle dùng `os.hostname()` (Node) hoặc
  `chrome.platform.os.hostname` (browser).
- `imei` = `M.a.getZaloClientID()` — Zalo random client ID, lưu local
  như z_uuid/imei trong session.

Trong zcloud, `imei` map tới `Session.IMEI` (đã có), `pc_name` lấy từ
`os.Hostname()` (Linux LXC: `zcloud.diepxuan.corp`).

## 6. ed25519 keypair

Generate mỗi account, persist:
- `public_key` (32B hex/base64) gửi kèm request.
- `private_key` (32B hex/base64) lưu local (chưa cần gửi đi — server
  dùng để derive agreement khi handshake).

Trong Go: `crypto/ed25519` (stdlib). Lưu vào Postgres JSONB
`accounts.syncv2_state` (cột mới, migration an toàn).

## 7. Sequence encryption (pull_mobile_msg response)

Response mỗi batch:
```json
{
  "messages": [
    {"sessionId": "<hex>", "cipher": "<base64>"},
    ...
  ],
  "next_seq_id": 12345,
  "done": false
}
```

Mỗi message được encrypt bằng cipher session tương ứng với `sessionId`.
Decrypt qua `sync2Cipher.decryptMessage(cipher)` → JSON message Zalo
chuẩn (giống `wsMessage` trong task 21).

## 8. Cấu trúc file sẽ tạo

- `src/zcloud/internal/core/syncv2.go` — `SyncV2Client` struct.
- `src/zcloud/internal/core/syncv2_wasm.go` — wrapper cho WASM
  (build-tag `//go:build syncv2_wasm` để dùng thật; mặc định dùng pure-Go
  stub dựa trên ed25519 + AES-GCM).
- `src/zcloud/internal/core/syncv2_test.go` — unit test (mock data).
- `docs/protocol/syncv2.md` — file này.

## 9. Chưa rõ (cần capture thật nếu có)

- Chính xác 13 args của `zprotoSync2CreateMetadataCipher` (mapping từ
  self/peer keys + salt/aad/info).
- Format `temp_key` (hex? base64? raw bytes? — string 32-64 char).
- Có phải `isBackupSuccess` event cũng mang `messages` payload hay phải
  gọi `pull_mobile_msg` riêng (source có vẻ là gọi riêng).

Nếu LXC không capture được, em sẽ:
1. Implement bằng ed25519 stdlib + tự derive session key qua HKDF-SHA256
   từ agreement + salt + info (best guess dựa trên pattern Noise/NoiseIK).
2. Smoke test với 1 message thật (nếu Sếp có thể gửi từ Zalo PC sau khi
   chạy zcloudd watch + cùng imei).
3. Nếu fail decrypt → xem lại arg list qua wasm export dump.
