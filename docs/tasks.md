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
| T9 | Review host/config từ server thay vì hardcode | 🟡 Medium | PC bundle dùng `zpw_service_map_new` + server domains; cần đọc session/config nếu muốn chống đổi host |
| T10 | WS AES-GCM + desktop command set | 🟢 Optional | PC bundle xác nhận AES-GCM layout và cmd 590-592/630-634 nếu làm cross-device/backup sync |
| T11 | Auto-sync media đầy đủ + cross-device/backup sync | 🟢 Deferred | Làm sau khi T1+T2+T3 (verify + integration test, auto-sync nền, đồng bộ media kèm tin nhắn) hoàn thành. Xem chi tiết tại [tasks/16-auto-sync-media.md](tasks/16-auto-sync-media.md) |

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

