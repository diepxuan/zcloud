# Tasks — zcloud (single source of truth)

> File này merge nội dung từ `master-plan.md` (kiến trúc + 4 mục tiêu) và
> `audit.md` (trạng thái chi tiết từng module + tồn đọng). Cập nhật
> 11/08/2026.

---

## 1. Mục tiêu dự án (4 mục tiêu chính)

1. **URL cho Sếp đăng nhập bằng QR code** — `http://zcloud.diepxuan.corp:8080`
2. **Chat real-time với user Zalo khác** — gửi/nhận qua core API + WS push
3. **Lưu lịch sử chat và media lâu dài** — PostgreSQL messages + disk media
4. **Đồng bộ lịch sử theo chuẩn Zalo** — WebSocket cmd 510/511

**Chiến lược:** Dùng Web API (chat.zalo.me), Go cho toàn bộ logic.

---

## 2. Kiến trúc tổng thể

```
┌──────────────┐     HTTP/WS     ┌──────────────────────────────────┐     Zalo API     ┌──────────────┐
│  Browser A   │ ◄─────────────►│  zcloudd (Go daemon)             │◄──────────────►│ Zalo Web     │
│  (user A)    │                 │  :8080                            │  (chat.zalo.me) │ (user A)     │
├──────────────┤                 │  ┌─────────────────────────────┐ │                 ├──────────────┤
│  Browser B   │                 │  │ Multi-User Manager          │ │                 │ Zalo Web     │
│  (user B)    │                 │  │ ├── user A → core client A  │ │                 │ (user B)     │
└──────────────┘                 │  │ ├── user B → core client B  │ │                 └──────────────┘
                                 │  │ ┌─────────────────────────┐ │ │
                                 │  │ │ Core (logic Zalo user)  │ │ │
                                 │  │ │ - encrypt, auth, chat   │ │ │
                                 │  │ │ - websocket             │ │ │
                                 │  │ │ - sync messages         │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  │ ┌─────────────────────────┐ │ │
                                 │  │ │ Store (DB + Media)      │ │ │
                                 │  │ │ - Postgres (multi-user)  │ │ │
                                 │  │ │ - media files on disk   │ │ │
                                 │  │ └─────────────────────────┘ │ │
                                 │  └─────────────────────────────┘ │
                                 └──────────────────────────────────┘
```

Xem chi tiết thiết kế tại `docs/design.md`.

---

## 3. Trạng thái 14 tasks

| ID | Tên | Trạng thái | Chi tiết |
|:--:|-----|:----------:|----------|
| 00 | Thiết lập môi trường + Go project | 🟢 Xong | [00-setup-env.md](tasks/00-setup-env.md) |
| 01 | Reverse Zalo Web API | 🟢 Xong | [01-reverse-web-api.md](tasks/01-reverse-web-api.md) |
| 02 | Reverse Android Sync | 🕐 Tạm hoãn | [02-reverse-android-sync.md](tasks/02-reverse-android-sync.md) |
| 03 | Thiết kế Core Protocol | 🟢 Xong | [03-design-core.md](tasks/03-design-core.md) |
| 04 | Xây dựng Core Library (Go) | 🟢 Xong | [04-build-core.md](tasks/04-build-core.md) |
| 05 | Xây dựng Server Daemon | 🟢 Xong | [05-build-server.md](tasks/05-build-server.md) |
| 06 | Xây dựng Web UI | 🟢 Xong | [06-build-webui.md](tasks/06-build-webui.md) |
| 07 | Multi-user Manager | 🟢 Xong | [07-multi-user.md](tasks/07-multi-user.md) |
| 08 | Zalo OA Integration | 🕐 Tạm hoãn | [08-zalo-oa.md](tasks/08-zalo-oa.md) |
| 09 | Database & Media Store | 🟢 Xong | [09-database.md](tasks/09-database.md) |
| 10 | Sync lịch sử tin nhắn cũ | 🟢 Xong | [10-sync-history.md](tasks/10-sync-history.md) |
| 11 | GetFriends + tab Liên hệ | 🟢 Xong | [11-getfriends.md](tasks/11-getfriends.md) |
| 12 | Logout / đổi tài khoản | 🟢 Xong | [12-logout.md](tasks/12-logout.md) |
| 13 | Media download | 🟢 Xong | [13-media-download.md](tasks/13-media-download.md) |
| 14 | Tách Web UI ra file tĩnh | 🟢 Xong | [14-split-webui.md](tasks/14-split-webui.md) |
| 15 | Reverse Zalo PC Desktop (static) | 🟢 Xong | [15-reverse-zalo-pc.md](tasks/15-reverse-zalo-pc.md) |
| 18 | Terminal UI (TUI) | 🟡 Mockup | [18-tui.md](tasks/18-tui.md) |
| 19 | Multi-account filter (chọn account hiển thị UI) | 🟡 Pending | [19-multi-account-filter.md](tasks/19-multi-account-filter.md) |
| 20 | Login Zalo PC (trusted-device) | 🟡 Pending | [20-zalo-pc-login.md](tasks/20-zalo-pc-login.md) |
| 21 | Fix bug parse EventNewMessage wrapper (Phase A — sync sâu) | 🟡 Pending | [21-fix-newmessage-parse.md](tasks/21-fix-newmessage-parse.md) |
| 22 | SyncV2 backup từ Zalo server (Phase B — sync sâu) | 🟡 Pending | [22-syncv2-backup.md](tasks/22-syncv2-backup.md) |
| 23 | Cross-device snapshot từ /api/message/get_crossdb (Phase C) | 🟡 Pending | [23-crossdb-snapshot.md](tasks/23-crossdb-snapshot.md) |

---

## 4. Audit chi tiết (cập nhật 28/07/2026)

### 4.1 Core Library (`internal/core/`)

| Module | File | Trạng thái | Ghi chú |
|--------|------|:----------:|---------|
| AES-128-CBC encrypt/decrypt | `encrypt.go` | ✅ | Có test, zero IV, PKCS7 padding |
| AES-GCM decrypt + WS event | `encrypt.go` | ✅ | `DecodeWSEvent` |
| Key generation | `encrypt.go` | ✅ | zcid, zcid_ext, deriveEncryptKey, sign key |
| QR login flow | `auth.go` | ✅ | 2 bước poll QR + cookie login |
| Cookie login | `auth.go` | ✅ | Inject cookie → getLoginInfo |
| Send message | `chat.go` | ✅ | POST form body + AES-CBC + zpw_ver/type |
| GetConversations | `chat.go` | ✅ | Sync conversations + resolveNames |
| GetMyProfile | `chat.go` | ✅ | Tên + avatar user |
| GetGroupInfo | `chat.go` | ✅ | Tên + avatar nhóm |
| GetGroupHistory | `chat.go` | ✅ | REST fallback `/api/cm/getrecentv2` |
| GetFriends | `chat.go` | ✅ | Parse fallback `{data: [...]}` + flat |
| WebSocket client | `websocket.go` | ✅ | Connect + cmd 501/521/510/511 |
| WS cmd 510/511 | `websocket.go` | ✅ | Request + handle old messages |
| Types & errors | `types.go`, `errors.go` | ✅ | Message, Conversation, Session, Event |

### 4.2 HTTP Server (`internal/api/`)

| API | File | Trạng thái |
|-----|------|:----------:|
| `GET /` Login page | `handlers.go` | ✅ |
| `GET /chat` Chat page | `handlers.go` | ✅ |
| `GET /api/qr/create` | `handlers.go` | ✅ |
| `POST /api/qr/poll` | `handlers.go` | ✅ |
| `POST /api/login/cookie` | `handlers.go` | ✅ |
| `GET /api/account` | `handlers.go` | ✅ |
| `GET /api/account/list` | `handlers.go` | ✅ |
| `GET /api/conversations` | `handlers.go` | ✅ |
| `GET /api/conversations/sync` | `handlers.go` | ✅ |
| `GET /api/messages` | `handlers.go` | ✅ |
| `POST /api/messages/send` | `handlers.go` | ✅ |
| `POST /api/messages/sync` | `handlers.go` | ✅ |
| `GET /api/friends` | `handlers.go` | ✅ |
| `POST /api/logout` | `handlers.go` | ✅ |
| `POST /api/media/download` | `router.go` | ✅ |
| `GET /media/` | `router.go` | ✅ |
| `GET /ws` Browser WS | `ws.go` | ✅ |

### 4.3 Database & Store (`internal/store/`)

**Backend: PostgreSQL (pgx), single backend từ 11/09/2026.** SQLite code đã
bỏ hoàn toàn (`store_sqlite.go`, `NewSQLite`, `BackendSQLite`, dialect
branches trong `queries.go`). Xem commit `3f4e88c` → `0546771`.

| Bảng | Trạng thái |
|------|:----------:|
| accounts | ✅ Multi-user |
| sessions | ✅ Active/inactive, DeleteAccount |
| conversations | ✅ GetConversation |
| messages | ✅ Cursor paginate |
| media | ✅ + trường AI (ocr_text, ai_tags, ai_processed) |
| oa_configs | ✅ Schema sẵn |
| oa_webhook_logs | ✅ Schema sẵn |

### 4.4 Web UI (`internal/api/web/` — file tĩnh embed.FS)

| Tính năng | Trạng thái |
|-----------|:----------:|
| QR login | ✅ |
| Cookie login | ✅ |
| Danh sách hội thoại | ✅ |
| Load tin nhắn (cursor) | ✅ |
| Gửi tin nhắn | ✅ |
| Tên người gửi (resolveNames) | ✅ |
| Tab Liên hệ (Friends) | ✅ |
| Logout / Đổi tài khoản | ✅ |
| Sync tin nhắn cũ | ✅ |
| Search bỏ dấu tiếng Việt | ✅ |
| Logo ZCloud + favicon | ✅ |

---

## 5. Tồn đọng cần làm tiếp (theo thứ tự ưu tiên)

| # | Tính năng | Mức độ | Ghi chú |
|:-:|-----------|:------:|---------|
| T1 | WS AES-GCM decrypt hoàn chỉnh | 🟡 Medium | Có cipherKey nhưng chưa dùng |
| T2 | Auto-download media khi WS nhận event | ✅ Done (đợt 09/2026, polish 11/09) | `enqueueMessageMediaJobs` tự động vào `media_jobs` khi WS nhận new/old msg có attachment. `MediaWorker` (5s interval, batch 20) tải về disk + retry 3 + dedupe + skip nếu file đã có. UI fallback `/api/media/download` qua `imgErr` khi URL Zalo CDN die. Xem chi tiết T2.7–T2.10 dưới. |
| T3 | Integration test (chat/store/api) | 🟡 Medium | Chỉ có `encrypt_test.go` |
| T4 | Zalo OA webhook (task 08) | 🟢 Optional | Schema sẵn, thiếu handler |
| T5 | Logging tập trung | 🔵 Low | `fmt.Printf` lẫn `log.Printf` |
| T6 | Dọn `zcloudd` binary trong git history | 🔵 Low | Đã ignore, history cũ |
| T7 | ~~Re-login QR cho account hiện tại~~ | ✅ Resolved | Lỗi `zpw_sek không đúng` xảy ra 11/08/2026 khi test SendMessage. Sau đó session được refresh qua background (commit đợt 29/07 cập nhật `secret_key bZMgG6RLiSa/DrYbIotXIg==`), từ 01/09/2026 trở đi không còn lỗi. Verify 11/09/2026: `listening=true`, `hasActiveSession=true`, WS connect `wss://ws3-msg.chat.zalo.me?zpw_ver=688` OK, ping/pong đều, sync old messages nhiều conv. Ghi chú cũ trong audit có thể bỏ. |
| T8 | Verify end-to-end (Sep ↔ Trần Ngọc Đức) | ✅ Verified 11/09/2026 | `/api/messages/send` (Sep → Trần Ngọc Đức) → `sent: true`; DB có row (15→16); WS broadcast `new_message` <1s; Zalo echo cmd 501 parse + dedupe OK. Xem chi tiết §5.4. |
| T9 | Review host/config từ server thay vì hardcode | ✅ Done (đợt 11/09) | Sub-task x t §5.7: T9.1 fix hardcode `GetConversations` ✅, T9.2 host fallback chain qua ServiceMap list khi host fail ✅, T9.3 thêm ServiceKey constants (10 key) ✅, T9.4 tests 28 case cho serviceBaseURL/isHostError/HostRetry ✅. WS URL đã wire từ trước qua session.WSURLs[0]. |
| T10 | WS AES-GCM + desktop command set | ✅ Done (đợt 11/09) | AES-GCM layout đã có + cipherKey wire vào DecodeWSEvent từ trước (cmd 590-592/630-634 dispatch thành DesktopSyncEvent qua cmdToEventType). Test round-trip AES-GCM encrypt=2/3 + base64 key + missing key. Schema payload từng cmd cần reverse thêm trusted-device WASM. |
| T11 | Auto-sync media đầy đủ + cross-device/backup sync | 🟡 Partial (đợt 11/09) | Sub-task xem §5.6: T11.1 (TTL handling — fail fast non-media) ✅, T11.2 (rate-limit 200ms) ✅, T11.3 (AES-GCM = T10) ✅, T11.4 (parse desktop sync cmd) ✅, **T11.5 (parse schema payload 590-592/630-634 + hook backup flow) 🟡 In Progress**. Còn: T11.6 WASM reverse cho trusted-device, integration test media end-to-end (cần Sếp gửi data thật từ Trần Ngọc Đức). |

## 5.1. Đã hoàn thành trong đợt này (29/07/2026)

- **Boot listener tự động**: `main.go` quét `sessions.is_active=1` lúc khởi động, gọi `StartZaloListener` cho từng account. Watcher goroutine 30s quét account mới (sau login QR/cookie).
- **api_version đọc từ session**: bỏ hardcode `688` trong `HandlePollQR`/`HandleCreateAccountFromProfile`/`HandleCookieLogin`, thay bằng `session.APIVersion` (đã được auth.go set = 688 mặc định).
- **SendMessage debug + autoRefresh**: thêm log `error_message` + raw body khi fail. Thêm `autoRefresh` vào `HandleSendMessage` để session hết hạn được refresh tự động trước khi gửi.
- **DB session refresh**: phát hiện session cũ (`api_version=665`) đã được refresh nhiều lần qua background, secret_key cập nhật (`bZMgG6RLiSa/DrYbIotXIg==`).

## 5.2. Đã hoàn thành trong đợt này (11/08/2026)

- **Reverse tĩnh Zalo PC 26.8.10**: tải/verify/extract installer Windows, giải
  `app.asar`, map framework Electron + API/auth/crypto + desktop sync.
- **Tài liệu**: `docs/protocol/pc-desktop.md` + case
  `work/reverse-zalo-pc-20260811/evidence/E-REPORT.md`.
- **Kết luận**: Web API core khớp zcloud; desktop thêm cross-device/backup sync,
  ZCloud/family media, trusted-device WASM, Postgres encrypted local store.

---

### T9.7 — Chi tiết T9 (đợt 11/09)

**T9.1 — Fix hardcode `GetConversations`**:

`internal/core/chat.go:171` thay hardcode `https://tt-convers-wpa.chat.zalo.me/api/preloadconvers/get-last-msgs?...` bằng `serviceBaseURL(c.Session, "chat", "https://tt-convers-wpa.chat.zalo.me") + path`. Khi Zalo rotate host mà session.ServiceMap còn trỏ host cũ, endpoint vẫn hoạt động qua URL mới server cung cấp.

**T9.2 — Host fallback chain** (`internal/core/chat.go`):

3 helper mới:
- `isHostError(err)`: nhận diện DNS/connection errors qua chuỗi marker (`no such host`, `connection refused`, `connection reset`, `i/o timeout`, `network is unreachable`, `tls:`, `certificate`, `EOF`) + `*os.SyscallError` wrap.
- `fallbackHost(session, key, idx)`: trả URL kế tiếp trong ServiceMap list (vd idx=0 lấy URL đầu, idx=1 lấy URL backup).
- `Client.HostRetry(ctx, method, url, body, headers)`: thử các host trong ServiceMap[key] theo thứ tự, chain qua nhiều service key (vd ['chat', 'profile']) khi hết list. Trả error cuối nếu tất cả fail.

Chưa wire HostRetry vào callsites (SendMessage/GetProfile/GetGroup/...) — sẽ làm bước tiếp nếu gặp host fail thực tế. Hiện helper đã sẵn sàng.

**T9.3 — ServiceKey constants** (`internal/core/chat.go`):

Định nghĩa 10 constant thay string literal:
- `ServiceKeyChat`, `ServiceKeyProfile`, `ServiceKeyGroup`, `ServiceKeyFile` (đang dùng)
- `ServiceKeyMediaStore`, `ServiceKeyZFamily`, `ServiceKeyZCloudUpFile` (cho T11.7 ZCloud)
- `ServiceKeySticker`, `ServiceKeyAlias`, `ServiceKeyZimsg` (chưa dùng)

Refactor callsites ở `chat.go` (2 chat + 3 profile + 3 group + 1 convers) và `desktop_sync.go` (5 file). Tổng cộng 14 call site.

**T9.4 — Tests** (`internal/core/service_test.go`, `host_test.go`):

28 test case:
- `TestServiceBaseURL`: 8 case (nil session, empty map, no key, list rỗng, URL rỗng, URL đầy đủ, trailing slash, nhiều URL → lấy URL đầu)
- `TestIsHostError`: 11 case (nil, plain, no such host, connection refused/reset, i/o timeout, network unreachable, TLS handshake, certificate, syscall wrap, plain string contains marker)
- `TestFallbackHost`: 7 case (nil, empty, no key, idx 0/1, out of range, trailing slash)
- `TestMultiKeyChooser`: chain ['chat', 'profile'] qua 3 host
- `TestHostRetryChainFallback`: host đầu fail → fallback OK
- `TestHostRetryAllHostsFail`: trả error `exhausted`
- `TestHostRetryNonHostError`: lỗi 500 trả về ngay, không chain

Tổng T9: 4 commit (T9.1+T9.4 `2469af6`, T9.2 `d3fc5c2`, T9.3 `d577778`, T9.4 final `649eb42`).

### T11.6 — Chi tiết T11 (đợt 11/09)

**T11.1 — TTL / fail-fast non-media response**: `internal/api/media_worker.go:download` trả error ngay khi response không phải media binary (HTML/text 200 OK thay vì binary). Retry 3 lần cho non-media response là lãng phí — URL Zalo CDN không thể "hồi sinh".

**T11.2 — Rate-limit giữa các job**: `internal/api/media_worker.go:processAccount` thêm 200ms sleep giữa các job (~5 req/s). Đủ nhanh cho batch=20 (mỗi tick xử lý ~4s), đủ chậm để không trigger Zalo rate limit (~10 req/s).

**T11.3 = T10 — AES-GCM layout + cipherKey wire**: cipherKey đã được `handleDesktopSync`/`decryptPayload` sử dụng. Test `TestDecodeWSEventAESGCM`/`AESGCMRaw`/`AESGCMBase64Key`/`AESGCMMissingKey` cover 4 case encrypt=2 (AES-GCM+gzip), encrypt=3 (AES-GCM raw), cipherKey truyền dạng base64 string, cipherKey rỗng → error.

**T11.4 — Parse cmd 590-592/630-634 as DesktopSyncEvent**:
- `core.EventType` thêm 8 type: `EventRequestSync`, `EventAckDeleteSession`, `EventMobileWakeUp`, `EventInitBackup`, `EventCreateBackup`, `EventBackupMeta`, `EventRestoreMobile`, `EventBackupConfigs`.
- `core.CmdToEventType(cmd, subCmd)` ánh WS cmd → EventType.
- `core.EventType.String()` trả tên event để log + broadcast.
- `core.DesktopSyncEvent{Cmd, SubCmd, Type, RawData json.RawMessage}` — payload thô để downstream parse khi reverse thêm WASM.
- `core.Event.DesktopSync *DesktopSyncEvent` — non-nil cho 8 cmd trên.
- `core.handleDesktopSync` dispatch Event vào `msgChan` (non-blocking; drop nếu channel đầy để không block WS read loop).
- `api.handleZaloEvent` default case: log + broadcast `desktop_sync` cho browser với `{cmd, subCmd, event, rawData}`.
- Test `TestCmdToEventType`/`TestEventTypeString`/`TestMsgTypeIsLink` đảm bảo mapping không trùng + Link type có helper riêng.

**T11.5 — Parse schema payload cmd 590-592/630-634 + hook backup flow** (🟡 In Progress):

Hiện tại (sau T11.4): worker nhận `DesktopSyncEvent{RawData json.RawMessage}` nhưng chỉ log payload, chưa parse thành struct có schema cụ thể. T11.5 làm tiếp:

1. **Capture payload thật từ Zalo PC bundle** — login PC, chạy backup/sync, capture WS frame cmd 590-592/630-634 (wireshark hoặc custom logger).
2. **Reverse schema** cho 8 cmd:
   - 590 RequestSync: `{data: ...}` — server yêu cầu client gửi msg cross-device
   - 591 AckDeleteSession: `{data: ...}` — xác nhận xoá session sync
   - 592 MobileWakeUp: `{data: ...}` — đánh thức mobile để sync
   - 630 InitBackup: `{data: {...}}` — seq_id, pc_name, public_key
   - 631 CreateBackup: (no data)
   - 632 BackupMeta: `{backup_data: [...], total_count: N}` — metadata backup từ PC
   - 633 RestoreMobile: `{data: ...}` — báo mobile khôi phục
   - 634 BackupConfigs: `{configs: {...}}` — lấy cấu hình backup
3. **Tạo struct Go** cho mỗi cmd (vd `InitBackupPayload{SeqID, PcName, PublicKey}`).
4. **Decode thay vì log**: `handleDesktopSync` switch theo `evType` parse `RawData` thành struct.
5. **Hook backup flow**: khi nhận `BackupMeta` → trigger download backup messages qua `pull_mobile_msg`.

Phụ thuộc: T11.6 (WASM reverse cho trusted-device key exchange).

**T11.6 — Reverse trusted-device WASM flow** (🟢 Optional, multi-week):

Zalo PC bundle (`work/reverse-zalo-pc-20260811/app.asar`) dùng WASM cho trusted-device key exchange khi sync/backup. Có thể cần reverse `.wasm` modules để biết schema chính xác của payload cmd 590-592/630-634.

Approach:
1. Extract `.wasm` files từ `app.asar`
3. Decompile (wabt / wasm2wat / radare2 / Ghidra WASM plugin)
4. Map hàm WASM → schema JSON
5. Implement key exchange + verify signature

**Còn lại của T11** (sau T11.5 + T11.6):
- ZCloud media + family album (chưa reverse).
- Integration test end-to-end (cần Sếp gửi data thật từ Trần Ngọc Đức theo §5.4).

## 5.12. T12 — Terminal UI (TUI)

Chi tiết: [tasks/18-tui.md](tasks/18-tui.md).

Workflow yêu cầu của Sếp (11/09/2026): `./zcloudd` hoặc `./zcloudd tui`
→ màn chọn account → màn chọn conv (filter được tên/ID) → màn chat
(xem + gửi tin). ESC ở bất kỳ màn nào cũng thoát hẳn (exit 0).

| # | Tính năng | Mức độ | Ghi chú |
|:-:|-----------|:------:|---------|
| T18.1 | Bubbletea + 3 màn tuần tự (wizard) | 🟡 Pending | Hiện `./zcloudd tui` chỉ in banner. Khi implement thật dùng `charmbracelet/bubbletea` + `lipgloss` + `bubbles`. **Không được đổi `./zcloudd` no-arg** — watch fork `./zcloudd` no-arg để lấy HTTP server (xem commit `6019140`). |
| T18.2 | Load data từ Postgres + filter `/` | 🟡 Pending | Màn 1: load accounts. Màn 2: load convs của account đang chọn. Filter `/` lọc theo displayName (case-insensitive contains) + parse ID nếu text toàn số. |
| T18.3 | Composer + gửi tin nhắn (màn 3) | 🟡 Pending | Textinput ở bottom panel, Enter để gửi qua `core.Client.SendMessage(text, convID)`. Echo optimistic + marker `[sent]` khi nhận WS ack. Text >2000 char → chia nhỏ. Smoke test với Trần Ngọc Đức theo §5.4. |
| T18.5 | Cookie login bằng 2 field zpsid + zpw_sek (bỏ script) | 🟡 Pending | Modal hiện tại dùng script DevTools bị `NotAllowedError: Document is not focused`. Thay bằng 2 ô input thủ công copy từ DevTools → Application → Cookies → chat.zalo.me. `zpw_sek` dùng `type="password"` để che value. Server validate 2 field bắt buộc. |
| T18.6 | Polish + resize + cleanup | 🟡 Pending | Detect không có TTY → in hướng dẫn `zcloudd serv` thay vì crash. Resize 20 dòng không vỡ. Thoát alternate buffer sạch. |

## 5.13. T13 — Multi-account filter (chọn account hiển thị trong UI)

Chi tiết: [tasks/19-multi-account-filter.md](tasks/19-multi-account-filter.md).

Sếp yêu cầu 11/09/2026: trong panel "Quản lý tài khoản" thêm checkbox cho
mỗi account để chọn subset hiển thị. Account không check vẫn listen WS
+ lưu data bình thường nhưng UI ẩn convs/contacts. Khi multi-account
enabled, conv list gộp + gắn badge tên account.

| # | Tính năng | Mức độ | Ghi chú |
|:-:|-----------|:------:|---------|
| T19.1 | Backend: cột `enabled` + `SetAccountEnabled` + endpoint `POST /api/account/enabled` | 🟡 Pending | Schema migration thêm cột `enabled BOOLEAN NOT NULL DEFAULT TRUE` vào `accounts`. Store API + HTTP handler mới. WS listener KHÔNG thay đổi. |
| T19.2 | Frontend: checkbox trong management panel + merge convs/friends | 🟡 Pending | Checkbox per-account trong `#pn-mg`. Conv list + friends list gộp từ enabled accounts, mỗi item kèm tên account. |
| T19.3 | Header dropdown "Đang chat" + auto-switch khi tắt account đang chat | 🟡 Pending | Dropdown chọn account composer gửi từ. Nếu tắt `ca` → auto-switch sang enabled account đầu tiên. |

## 5.14. T14 — Login Zalo PC (trusted-device protocol)

Chi tiết: [tasks/20-zalo-pc-login.md](tasks/20-zalo-pc-login.md).

**Vấn đề Sếp gặp 11/09/2026**: zcloud chỉ hỗ trợ Zalo Web login. Khi user
login Zalo web account A ở browser khác (đăng xuất session cũ) → Zalo
invalidate session cũ → zcloud WS bị kickout → ngừng nhận tin.

**Giải pháp**: Zalo PC client dùng trusted-device protocol riêng (WASM
key exchange) — không bị kickout khi có session khác.

**Phạm vi**:
- Phase 1: endpoint `POST /api/login/pc` + WS reconnect loop (~0.5 ngày).
- Phase 2: AES-GCM payload layer (~1 ngày).
- Phase 3: WASM reverse cho trusted-device key exchange (~3-5 ngày).

**Tổng estimate**: ~1 tuần. Đợi Sếp duyệt plan trước khi code.

## 5.15. T15 — Fix bug parse EventNewMessage wrapper (Phase A — sync sâu)

Chi tiết: [tasks/21-fix-newmessage-parse.md](tasks/21-fix-newmessage-parse.md).

**Bug phát hiện 12/09/2026**: WS cmd 501/521 (realtime new message) parse OK
nhưng `SaveMessage` không bao giờ được gọi. Verify bằng debug log:
```
DEBUG: handleNewMessages tt=0 payload_len=769 data_len=896
       first16=7b226572726f725f636f6465223a302c
DEBUG: handleNewMessages unmarshal OK msgs_len=0 groupMsgs_len=0
```
`first16` = `{"error_code":0,` → Zalo wrap trong `{error_code, data: {msgs}}`.
`handleNewMessages` chỉ parse 1 lớp → msgs không tìm thấy → drop.

**Fix**: thêm field `Data` vào struct, unwrap `data.msgs` / `data.groupMsgs`
giống `handleOldMessages` đã làm đúng.

**Hậu quả nếu không fix**:
- `journalctl -u zcloud --since '7 hours ago' | grep 'new msg from'` = **0 log**.
- Mọi tin nhắn realtime bị drop trong nhiều giờ (chỉ sync cũ chạy được).
- DB chỉ có tin từ WS cmd 510/511 (sync history), không có tin realtime.

**Estimate**: ~30 phút.

## 5.16. T22 — SyncV2 backup từ Zalo server (Phase B — sync sâu)

Chi tiết: [tasks/22-syncv2-backup.md](tasks/22-syncv2-backup.md).

**Trạng thái 13/09/2026** — Infrastructure xong, **BLOCKED bởi 3 thứ cần Sếp**:

| Sub | Trạng thái | Commit |
|:--:|:--:|:--:|
| T22.2a skeleton | ✅ | `77ab314` |
| T22.2b PullBatch | ✅ | `77ab314` |
| T22.3 WS hook | ✅ | `ebaabe9` |
| T22.3 followup (transport col) | ✅ | `f088277`, `f795e5a` |
| T22.4 scheduler resume | ✅ | `d196a03` |
| T22.5 admin endpoints | ✅ | `a43f47b` |
| T22.6 UI nút SyncV2 | ✅ | `ba9e221` |
| **T22.7 WASM cipher thật** | 🟡 Blocked | cần Sếp capture live |
| **T22.8 end-to-end smoke** | 🟡 Blocked | cần account transport=pc |
| **T23 crossdb snapshot** | 🟡 Blocked | cần bundle Zalo + SQLCipher |

**3 blockers cần Sếp cung cấp** (xem `memory/2026-09-13-phase3.md`):

1. **Account `transport=pc`**: Sep hiện `transport=web` → server từ chối
   `request-sync`. Cần Sếp login Zalo PC client thật (có GUI) + extract
   `zpsid`/`zpw_sek` qua `POST /api/login/pc`.
2. **WASM cipher args**: pure-Go stub (HKDF + AES-GCM) chỉ là best guess
   dựa trên Noise pattern. Cần 1 capture WS event 590/591/592 + 1
   `pull_mobile_msg` batch response để reverse đúng cipher layout.
3. **Phase C (T23)**: cần bundle `ZaloSetup-26.8.10.exe` (chỉ có notes)
   + SQLCipher key derivation (không thể reverse từ static JS).

**Mục tiêu**: pull được lịch sử sâu (>6h, hiện tại WS cmd 510/511 chỉ sync
~50 tin gần nhất) qua SyncV2 backup flow mà Zalo PC dùng.

**Endpoints theo `docs/protocol/pc-desktop.md`**:
- `POST /api/transfer-sync-v2/request-sync` (cmd 12888) — mở session.
- `POST /api/message/pull_mobile_msg` (cmd 12000) — pull từng batch.
- WS cmd 590-592/630-634 — transfer state machine.

**Phụ thuộc bắt buộc**:
- Sếp login Zalo PC client 1 lần để capture payload thật (cần cho AES-CBC 
  key derivation + payload schema).
- Không có capture → dừng task.

**Estimate**: ~2-3 ngày (không tính Sếp capture).

## 5.17. T17 — Cross-device snapshot từ /api/message/get_crossdb (Phase C)

Chi tiết: [tasks/23-crossdb-snapshot.md](tasks/23-crossdb-snapshot.md).

**Mục tiêu**: lấy 1 lần toàn bộ lịch sử qua DB snapshot — flow Zalo PC
dùng khi sync từ mobile qua SQLCipher-encrypted SQLite.

**Endpoints theo `docs/protocol/pc-desktop.md`**:
- `POST /api/message/get_crossdb` (cmd 12412) — request snapshot.
- WS event 590-592 — nhận snapshot binary encrypted.
- `db-cross-v4-native.node` (E-011) — `decompressAndDecryptDb` + SQLCipher.

**Phụ thuộc bắt buộc**:
- Sếp cung cấp bundle `ZaloSetup-26.8.10.exe` (em hiện không có file gốc).
- Sếp capture snapshot binary + keys khi sync.
- **Không có bundle + capture → dừng task** (SQLCipher key reverse quá khó).

**Estimate**: ~4-5 ngày (không tính Sếp cung cấp bundle).

## 6. References

| Lib | Lang | Stars | Path |
|-----|------|-------|------|
| zca-js | TypeScript | 567★ | `docs/references/zca-js/` |
| zcago | Go | 8★ | `docs/references/zcago/` |
| Za-go | Go | 64★ | `docs/references/za-go/` |

---
## 5.3 Đang thực hiện (đợt 09/2026) — Đồng bộ dữ liệu (T1+T2+T3)

✅ **Hoàn thành đợt này (08/09/2026):**

- **T1 — Verify + integration test**: test parse WS 510/511 (cá nhân + nhóm), test SaveMessage dedupe qua Postgres (schema riêng mỗi test), test end-to-end parse → SaveMessage (`internal/core/sync_test.go`, `internal/store/store_test.go`). Chạy với `ZCLOUD_TEST_DSN=... go test -tags testdb ./...`.
- **T2 — Auto-sync scheduler** (`internal/api/sync_scheduler.go`): goroutine quét account active mỗi 10 phút (env `ZC_AUTOSYNC_INTERVAL`, min 30s), throttle 500ms/conv, idempotent lock tránh overlap, truyền `lastId` từ `conv.LastMsgID` cho WS cmd 510/511. `RequestOldMessagesViaListener` nhận thêm `lastID`; `HandleSyncMessages` nhận `lastId` từ client (fallback `conv.LastMsgID`).
- **T3 — Đồng bộ media đầy đủ** (`internal/api/ws.go`): `MsgType.IsMedia()` helper, `extractAllMedia` (dedupe URL), `downloadOneMedia` (skip nếu file tồn tại, retry 3 lần cho cả network error và HTTP 5xx, lưu meta qua `SaveMedia`, broadcast `media_downloaded`).

**Smoke test còn lại** (optional): gửi tin thật từ Zalo client khác → DB có row + media file trong `/storages/media/`. T7 đã resolve, không còn chặn.

Sau khi 3 phần trên ổn định → làm T11 (PC desktop / cross-device sync).

---

## 5.4 Quy ước test gửi/nhận

**Mọi test gửi/nhận (cả end-to-end live lẫn smoke) PHẢI thực hiện với Trần Ngọc Đức** (conv_id `4866700441106275565`, tên hiển thị `Trần Ngọc Đức`). Lý do:

- Là Sếp (Duc Tran) — đối tượng test duy nhất Sếp cho phép dùng để smoke mà không sợ ảnh hưởng khách hàng.
- Là thread 1-1 (convType=0), đã có lịch sử test trước đó (Sep đã gửi tin test UI ack ngày 02/09).
- Tin nhận/gửi có thể verify ngay trên điện thoại Sếp nếu cần đối chiếu cuối.

**Các thread KHÔNG dùng cho test** (giữ nguyên hành vi khách hàng):

- `7957954460977268756` Linh Bui — khách hàng gối.
- `5280163336123324706` Phandaitrang — khách hàng ảnh.
- Mọi thread còn lại (ThangMT, Cam Tu, Phan Xuan, Tran Thi Le Thuy, Nga Tran, Tran Cong Diep, …) đều không được spam bằng tin test.

**Marker cho tin test**: prefix `[T8-...]`, `[T2-...]`, `[smoke-...]` + epoch timestamp để dễ lọc lại khi cần dọn DB.

**Verify T8 (11/09/2026)** — Sep gửi `[T8-1789096483] end-to-end toi Tran Ngoc Duc` tới conv `4866700441106275565`:

- `POST /api/messages/send` → `sent: true`, msgId `1789096483961`.
- DB: messages=15 → 16, content match, fromId `559609701372941728`, convId `4866700441106275565`.
- WS `/ws`: nhận `new_message` event với payload đầy đủ trong <1s.
- Zalo server echo: frame `01f50100...` (cmd 501 subCmd 01) → zcloud parse qua `EventNewMessage` + dedupe (cùng msgId, INSERT OR IGNORE không lưu row mới).
- Server response: `error_code: 0, error_message: Successful`.

---

## 5.5 T2 chi tiết (Auto-download media)

T2 xử lý 5 sub-task kỹ thuật liên quan đến media pipeline.

### T2.7 — Reject non-media response (HTML-as-media)

- **File**: `internal/core/mediafile.go` (helper `IsMediaContent`, `MediaContentPrefix`), wire vào `internal/api/media_worker.go:download` và `internal/api/router.go:HandleMediaDownload`.
- **Vấn đề**: URL Zalo CDN die (photo không tồn tại, token hết hạn, …) trả về HTML/text 200 OK thay vì binary media. Code cũ save file HTML thành `.bin` 185KB + đánh `done`. → DB báo done nhưng browser không hiển thị được.
- **Fix**: sniff 8 byte đầu (JPEG/PNG/GIF/WebP/MP4/MOV/M4A/WebM/OGG/MP3) — nếu không match thì fail download và retry hết maxAttempts → `MarkFailed`.
- **Cleanup**: 3 file `.bin` 185KB HTML đã xoá khỏi disk 11/09/2026.

### T2.8 — Variant selection (prefer longest URL)

- **File**: `internal/api/ws.go:extractAllMedia` + helper `baseAttachmentID`, `extFromFileOrURL`.
- **Vấn đề cũ**: dedupe theo MsgID, giữ variant đầu tiên — thường là thumb (-1). Sync từ WS hay bị ảnh thumb thay vì HD.
- **Fix mới**: dedupe theo base ID (strip `-N` nếu N là số), giữ variant URL dài nhất (thường là original HD, vì URL original chứa path + size metadata dài hơn thumb).
- **Test**: `TestExtractAllMediaPreferLongestVariant`, `TestExtractAllMediaDedupVariantSameBase`, `TestExtractAllMediaDistinctBases`, `TestBaseAttachmentID`.

### T2.9 — Auto-download link OG preview image

- **File**: `internal/core/types.go` (thêm `IsLink()`), `internal/api/ws.go:hasImageAttachment`, hook vào `enqueueMessageMediaJobs`.
- **Vấn đề**: MsgTypeLink (chat.link) bị `IsMedia()` bỏ qua hoàn toàn → OG preview image (qua field `thumb`) không được tải về disk.
- **Fix**: trong `enqueueMessageMediaJobs`, thêm nhánh `msg.Type.IsLink() && hasImageAttachment(msg.Attachments)` → cho qua nếu attachment có URL ảnh (jpg/jpeg/png/gif/webp). Link thuần không ảnh thì bỏ qua (không tải nhầm HTML).
- **Test**: `TestHasImageAttachment` cover jpg/jpeg-upper/png/gif/webp/mp4/pdf/empty/mixed.

### T2.10 — Dọn dead code

- **File**: `internal/api/ws.go` xoá `maybeAutoDownloadMedia` (không ai gọi, chỉ định nghĩa). Giữ `downloadOneMedia` (còn test dùng + có thể fallback).

### T2.11 — Verify với data thật (pending — cần Sếp thao tác)

Chưa verify được vì DB hiện chỉ có image (type=2). Cần Sếp gửi từ Trần Ngọc Đức:
- 1 sticker (type=3) → confirm IsMedia() pick up + tải thành công
- 1 voice (type=5) → confirm retry + ext detect đúng (m4a/mp3)
- 1 file (type=4) → confirm ext lấy từ FileName
- 1 video (type=7) → confirm mp4 ext + retry với file lớn
- 1 link có OG image (type=6) → confirm hasImageAttachment + tải thumb

Script verify: `/tmp/t2_verify_media.sh` (đếm type + jobs + disk).

### T2.12 — Test thật phải dùng thread Trần Ngọc Đức

Theo §5.4, mọi test gửi/nhận phải dùng conv `4866700441106275565`. Sếp gửi các loại media vào thread này (không cần mở app khác — Zalo phone có sẵn account Sep), em sẽ chạy `bash /tmp/t2_verify_media.sh` để verify.
## Phần A đã xong — 13/09/2026

Commit `7239e0f`: unwrap layer 2 (`data.msgs` / `data.groupMsgs`) trong
`handleNewMessages` (cmd 501/521). Thêm 4 test parse ở
`websocket_parse_test.go` (wrapped user, wrapped group, plain root, empty).
Build pass, test mới pass. Test cũ `TestEncodeDecodeAESGCM_RoundTrip/mode_2`
vẫn fail nhưng là pre-existing (xác nhận bằng `git stash` trên HEAD).

Smoke test live (gửi `[T21-...]` từ Trần Ngọc Đức) chưa chạy trong session
này — Sếp tự verify hoặc chờ session kế tiếp.
| 22 | SyncV2 backup từ Zalo server (Phase B — sync sâu) | 🟡 T22.2a xong | [22-syncv2-backup.md](tasks/22-syncv2-backup.md) |

## Phase B da co skeleton — 13/09/2026

Commit `77ab314` push len main:
- `docs/protocol/syncv2.md` reverse state machine + REST/WS command map
  (khong can capture: doc source JS truc tiep).
- `internal/core/syncv2.go` `SyncV2Client` (ed25519 keypair, RequestSync,
  PullBatch, HandleEvent, AES-CBC REST wrapper).
- `internal/core/syncv2_cipher.go` `BuildCipherSession` stub (ed25519
  self-agreement + HKDF-SHA256 + AES-256-GCM) — Phase B stub, build tag
  `syncv2_wasm` se dung WASM that sau.
- `internal/core/syncv2_test.go` 5 unit test pass.

Con lai (can Sếp live test):
- T22.2b: PullBatch + DecryptMessages end-to-end voi Zalo PC.
- T22.3: Hook WS 590/591/592/632 + persist state JSONB.
- T22.4: Scheduler resume.

LXC khong capture duoc traffic nen cipher session dung HKDF stub. Khi
Sếp live test, neu giai ma fail → reverse them 13 args cua
`zprotoSync2CreateMetadataCipher` (xem `docs/protocol/syncv2.md` §4,§9).

## Phase B T22.3 (WS hook + state JSONB) xong — 13/09/2026

Commit `ebaabe9` push len main:
- `store.Account.SyncV2State` (JSONB) + migration `ensureAccountSyncV2PG`.
- `store.GetAccountSyncV2State` / `SetAccountSyncV2State` (load/persist).
- `api/syncv2_handler.go` (moi): `syncV2Registry` + `handleSyncV2Event`
  feed WS 590/591/592/632 vao `core.SyncV2Client`. Khi phase=init tu
  trigger `RequestSync`. Khi phase=pulling tu goi `PullBatch` loop,
  decrypt, SaveMessage, persist `last_seq_id` cho restart resume.
- `api/ws.go`: hook `EventRequestSync/AckDelete/MobileWakeUp/BackupMeta`
  vao handler. Them `Transport/CipherKey` vao `clientFromSession`.
- `core/syncv2_test`: them `TestSyncV2_HandleEvent` (4 transitions).

Verify: build PASS, 6/6 test pass, migration tu chay, cot `syncv2_state`
JSONB da co trong DB (psql \d accounts).
