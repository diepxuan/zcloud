# Zalo PC Desktop Protocol Notes

## Summary

Zalo PC 26.8.10 is an Electron desktop application. Its core chat/API surface is
the same Zalo Web protocol already documented for this project, wrapped in a
desktop shell. The main desktop-specific additions are:

- Electron main/preload/renderer/worker architecture with local SQLite.
- PC-to-mobile and backup message sync (cross-database pull + WS backup/sync
  commands).
- ZCloud/family media upload/download with cloud-viewer keys and encrypted
  headers.
- Trusted-device linking implemented with WASM.
- Local encrypted/obfuscated storage and file cache markers.

## Artifacts

Analyzed offline:

- `ZaloSetup-26.8.10.exe` - NSIS installer wrapping `$PLUGINSDIR/app-32.7z`.
- `Zalo-26.8.10/Zalo.exe` - Electron executable.
- `Zalo-26.8.10/resources/app.asar` - application bundle.
- `Zalo-26.8.10/resources/app.asar.unpacked/native/nativelibs/` - native addons.
- `pc-dist/compact-app-pc.*.js`, `shared-worker.*.js`, `trust-worker.*.js`,
  `sync-v2-sub-worker.*.js`, `libs/*.wasm`.

## Core API / Auth / Crypto

### Hosts

The bundle builds API domains from a config domain (`zaloapp.com` in
production) and from server-provided `zpw_service_map_new`/`zpw_service_map_v3`:

| Role | Default template |
|------|------------------|
| chat | `https://chat-wpa.<dm>` |
| group | `https://group-wpa.<dm>` |
| profile/friend/social | `https://profile-wpa.<dm>` / `https://friend-wpa.<dm>` |
| files/media | `https://files-wpa.<dm>` / `https://media-wpa.<dm>` |
| sticker/gif | `https://sticker-wpa.<dm>` |
| auth | `https://wpa.<dm>` |
| cloud media | `https://zcld.chat.zalo.me` |
| family | `https://zfml.chat.zalo.me` |
| zinstant | `https://zimsg.chat.zalo.me` |

### Login

- Password/QR/Facebook login returns `zpw_sek`, `uid`, `dkey`, `quest_cert`,
  `cptch_cert`, and sync flags.
- `getLoginInfo` returns config with `UIN=dkey`, `decryptKey=zpw_enk`, and
  server settings.
- Local app state keeps `z_uuid`/`imei`, `zpw_type`, `zpw_ver`,
  `viewerkey`, `zlast_uid`, recent phone/country.
- Cookies are passed to Electron session (`setAppCookie`).

### REST params

Same pattern as Zalo Web:

- `params` = `encodeURIComponent(encodeAES(JSON.stringify(obj)))`.
- Login-protected params use `zcid`/`zcid_ext`/`enc_ver` and derived
  AES-256-CBC key, with `signkey=MD5("zsecure"+type+sortedParams)`.
- Common query params include `zpw_ver`, `zpw_type`, `imei`, `computer_name`.

### WebSocket

Frame:

```text
version(1) | cmd(uint16 LE) | subCmd(1) | optional bytes
```

JSON command body:

```json
{"version":1,"cmd":...,"subCmd":...,"data":...}
```

Encrypted payloads use AES-GCM:

```text
base64(iv[16] || additionalData[16] || ciphertext+tag)
```

Key comes from the WS auth response (`cmd=1`, `key` field). Some payloads are
gzip-inflated after decrypt.

Relevant commands:

| Command | Meaning |
|---------|---------|
| 1 | auth / key exchange |
| 2 | ping |
| 4 | ping active |
| 501/521 | user/group message push |
| 510/511 | old message sync queues |
| 601/602/603 | control push/pull |
| 610/611/612/613 | reactions |
| 514-582 | E2EE signal/session commands |
| 590-592 | cross-device sync message |
| 630-634 | backup message session |

## Desktop-specific Sync

REST endpoints:

| Command ID | Path | Notes |
|-----------|------|-------|
| 12412 | `/api/message/get_crossdb` | `{pc_name,sync_session}` |
| 12000 | `/api/message/pull_mobile_msg` | `{pc_name,public_key,from_seq_id,is_retry,min_seq_id,temp_key,imei}` |
| 12700 | `/api/message/cancel_pull_mobile_msg` | `{pc_name,public_key,imei}` |
| 12003 | `/api/message/delete_snapshot_mobile_msg` | cleanup |
| 12096 | `/api/message/get_backupmsginfo` | backup info |
| 12888 | `/api/transfer-sync-v2/request-sync` | `{reqId,data}` |

WS commands:

- `SYNC_MESSAGE.REQUEST=590`
- `SYNC_MESSAGE.ACK_DELETE_SYNC_SESSION=591`
- `SYNC_MESSAGE.REQUEST_MOBILE_WAKE_UP=592`
- `BACKUP_MSG.CREATE_SESSION=631`
- `BACKUP_MSG.INIT=630`
- `BACKUP_MSG.GET_METADATA=632`
- `BACKUP_MSG.SIGNAL_RESTORE=633`
- `BACKUP_MSG.GET_CONFIGS=634`

Transfer state machine events include `transfer_after_login`, `user_confirm`,
`user_reject`, `mobile_restoring`, `mobile_active`, `mobile_idle`,
`syncmsg_info`, `transfer_error`, and use `temp_key`, `public_key`,
`pc_name`, `user_action`.

## ZCloud / Family Media

Service map:

- `zcloud` -> `https://zcld.chat.zalo.me`
- `zfamily` -> `https://zfml.chat.zalo.me`
- `zimsg` -> `https://zimsg.chat.zalo.me`

Upload/download headers:

- `cloud-viewer-key`
- `zfamily-viewer-key`
- `x-zl-mdck`
- `x-zl-eci`
- `x-zl-ex-inf`
- `x-zl-ex-eci`
- `x-zl-msi`

`x-zl-msi` is AES-CBC encrypted message metadata. `x-zl-eci` / `x-zl-ex-eci`
carry encrypted cloud keys (`v=...; i=...`). Personal cloud URL parameters
include `pcloudsession`, `algoversion=1|2`, and key/iv/tag data.

## Local SQLite

Paths:

```text
<userData>/<db>/_production/*.db
<userData>/<db>/_production_bu/*.db
<userData>/<db>/_production_temp_bu/*.db
<userData>/<db>/_production_rej_bu/*.db
```

The app ships `node_sqlite3.node` built with SQLCipher-like pragmas
(`cipher_compatibility`, `rekey`, WAL). The `db-cross-v4-native.node` addon
contains `decompressAndDecryptDb` / `decompressAndDecryptDb_V2` and encrypt DB
strings. Exact SQLCipher key derivation was not recovered from static JS.

## Trusted Device

`trust-worker.js` loads `trusted_device_protocol_bg.*.wasm`. Exports cover:

- ed25519 keypair generation / derive public key / agreement / sign / verify
- linking secret key and linking QR
- companion linking proof
- device list creation/verification and trusted identity verification
- expiry checks

This appears to be the QR device-link / companion flow, separate from the
normal Web session used by zcloud.

## Local Cache / File Noise

Download/thumb modules use:

```text
XOR_KEY=147
Z_SIGN="077a416c4f07"
Z_SIGN_BUFFER=[0x07,0x7a,0x41,0x6c,0x4f,0x07]
XOR_LEN=100
```

First 100 bytes of local media cache are XOR-obfuscated and a marker is
appended. This is local file handling, not transport encryption.

## Relevance to zcloud

- Existing Go Web API implementation remains correct for normal chat,
  conversations, friends, history, and real-time WS.
- Desktop-specific sync/backup endpoints and WASM flows are not needed for the
  current Web API cloud service unless cross-device sync becomes a feature.
- The main actionable correction is to avoid hardcoding one `wpa`/`chat` host;
  production should consume session/server info and service maps.
