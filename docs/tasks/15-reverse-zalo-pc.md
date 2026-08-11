# Task 15: Reverse Zalo PC Desktop (static)

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [01-reverse-web-api.md](01-reverse-web-api.md), [10-sync-history.md](10-sync-history.md)
- **Trạng thái:** Xong

## Mục tiêu
Reverse tĩnh installer Windows Zalo PC để xác minh sự khác biệt giữa desktop và
Zalo Web API, đồng thời ghi lại desktop-specific sync/cloud/trust/local storage.
Không cài đặt, không chạy app, không tương tác live account.

## Artifact
- `tmp/zalo-pc/ZaloSetup-26.8.10.exe`
- `tmp/zalo-pc/app/Zalo-26.8.10/resources/app.asar`
- `tmp/zalo-pc/asar/`

## Output
- `docs/protocol/pc-desktop.md`
- `work/reverse-zalo-pc-20260811/evidence/E-REPORT.md`

## Kết luận
- Zalo PC là Electron wrapper quanh Zalo Web API core; REST/AES/signkey/WS giữ
  nguyên chuẩn Web.
- Desktop thêm:
  - Cross-device/backup sync: `get_crossdb`, `pull_mobile_msg`,
    `transfer-sync-v2/request-sync`, WS 590-592/630-634.
  - ZCloud/family media: `zcld.chat.zalo.me` / `zfml.chat.zalo.me` +
    `cloud-viewer-key` / `zfamily-viewer-key` / `x-zl-*`.
  - Trusted-device linking: WASM `trusted_device_protocol_bg`.
  - SQLCipher-style local SQLite + SecureLocalStorage XOR.

## Verification
- [x] Installer download + SHA-256.
- [x] Giải NSIS + app-32.7z + app.asar.
- [x] Static map API/auth/crypto/WS.
- [x] Static map desktop sync/cloud/trust/local storage.
- [x] Evidence + report.
