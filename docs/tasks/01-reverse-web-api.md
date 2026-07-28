# Task 01: Reverse Zalo Web API

## Liên kết
- **Master plan:** [tasks.md](../tasks.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** [00-setup-env.md](00-setup-env.md) ✅
- **Kế tiếp:** [03-design-core.md](03-design-core.md), [04-build-core.md](04-build-core.md)
- **Tham khảo:** zca-js, zcago (AES-128-CBC, key gen), Za-go

## Mục tiêu
Capture + document toàn bộ Web API chat.zalo.me: login flow, encryption, REST endpoints, WebSocket.
**Không capture DevTools thủ công** — dùng source tham khảo để biết encryption + API flow.

## Encryption (đã xác nhận từ zca-js/zcago)

- **Thuật toán:** AES-128-CBC (zero IV) — **KHÔNG phải AES-ECB**
- **Key:** Base64-decode `zpw_enk` từ login response → bytes
- **IV:** 16 bytes 0x00
- **Padding:** PKCS7
- **Signing:** MD5("zsecure" + type + sorted params values)

## Các bước

### 01.1 — Tài liệu encryption
- Đọc source zca-js `src/utils.ts` (ParamsEncryptor, encodeAES, decodeAES)
- Đọc source zcago `internal/cryptox/aes.go`, `internal/httpx/encryption.go`
- Ghi thành `docs/protocol/web-encryption.md`

### 01.2 — Tài liệu login flow
- Đọc source zcago `session/auth/login.go` + `session/auth/login_qr.go`
- Đọc source zca-js `src/apis/login.ts` + `src/apis/loginQR.ts`
- Ghi thành `docs/protocol/web-login.md`

**Login flow:**
1. GET `id.zalo.me/account?continue=...` → lấy JS version
2. POST `id.zalo.me/account/logininfo` → login info
3. POST `id.zalo.me/account/verify-client` → verify device
4. POST `id.zalo.me/account/authen/qr/generate` → QR code
5. Poll `account/authen/qr/waiting-scan` → chờ scan
6. Poll `account/authen/qr/waiting-confirm` → chờ confirm
7. GET `account/checksession` → validate session
8. GET `jr.chat.zalo.me/jr/userinfo` → user info
9. GET `wpa.chat.zalo.me/api/login/getLoginInfo` → zpw_enk + zpw_ws (cookie login)
10. GET `wpa.chat.zalo.me/api/login/getServerInfo` → settings

### 01.3 — Tài liệu REST API
- Đọc source zcago `api/` directory (55+ endpoints)
- Đọc source zca-js `src/apis/` directory
- Ghi thành `docs/protocol/web-api.md`

**Core endpoints:**
- `/api/conversation/getsmsreq` — conversations list
- `/api/message/sendreq` — send message
- `/api/friend/getfriends` — friend list
- `/api/group/getgroupinfo` — group info
- `/api/group/history` — group message history
- `/api/profile/getprofile` — user profile

### 01.4 — Tài liệu WebSocket
- Đọc source zcago `listener/` directory
- Đọc source zca-js `src/apis/listen.ts`
- Ghi thành `docs/protocol/web-websocket.md`

**WS commands:**
- CMD 1,1: key exchange (cipherKey)
- CMD 501,0: new user messages
- CMD 521,0: new group messages
- CMD 510/511,1: request old messages (history)
- CMD 601: control events
- CMD 612: reactions
- CMD 3000: duplicate connection

**Ping:** binary header [0x01][cmd=2][0x01][JSON {"eventId": timestamp}]

### 01.5 — Node.js test script (tạm thời)
- `scripts/re/test-flow.js` — test decrypt/encrypt với dữ liệu mẫu từ source tham khảo
- Script này **không phải source chính**, sẽ bỏ sau khi Go implement xong

## Output
- `docs/protocol/web-encryption.md` — AES-128-CBC spec + key generation
- `docs/protocol/web-login.md` — login flow + QR + cookies
- `docs/protocol/web-api.md` — REST endpoints + params format
- `docs/protocol/web-websocket.md` — WS protocol + commands + decrypt
- `scripts/re/test-flow.js` — test script (temporary)

## Verification
- [ ] Tất cả spec docs đã ghi đủ, chính xác
- [ ] Encrypt/decrypt flow có thể implement trong Go
- [ ] Login flow đầy đủ từ QR → secret_key
- [ ] WebSocket protocol đã hiểu: cmd, subCmd, encrypt types
- [ ] References source đọc kỹ, ghi chú đầy đủ

## Ghi chú
- Không cần capture DevTools thủ công — source tham khảo đã reverse sẵn
- Tập trung vào hiểu encryption và API flow để implement Go
- Android sync (task 02) không cần thiết — Web API có history qua WS cmd 510/511
