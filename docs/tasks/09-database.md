# Task 09: Database & Media Store

## Liên kết
- **Task list:** [../tasks.md](../tasks.md)
- **Phụ thuộc:** [03-design-core.md](03-design-core.md)
- **Trạng thái:** Xong (Postgres-only từ 11/09/2026)

## Mục tiêu
Lưu trữ persistent: PostgreSQL (accounts, sessions, conversations, messages,
media, OA configs, OA webhook logs) + media files trên disk.

## Tech
- **Postgres driver:** `github.com/jackc/pgx/v5` qua `database/sql`.
- **Migrations** trong `internal/store/store_postgres.go` dạng `const migration*PG`.
- **Schema-per-test** qua `internal/store/testutil.go` cho integration test
  (`-tags testdb`, env `ZCLOUD_TEST_DSN`).

## Bảng chính
- `accounts` — tài khoản người dùng, multi-user, FK từ sessions.
- `sessions` — phiên Zalo, JSON cookies, secret_key base64, ws_urls JSON array.
- `conversations` — hội thoại, PK `(id, account_id)`, conv_type 0/1/2.
- `messages` — tin nhắn, PK `(id, account_id)`, index `(conv_id, ts DESC)`.
- `media` — file media + trường AI (ocr_text, ai_tags, ai_processed).
- `media_jobs` — hàng đợi tải media bền vững (pending/running/done/failed).
- `oa_configs` — cấu hình OA, schema sẵn.
- `oa_webhook_logs` — log webhook OA, schema sẵn.

## Path
- DB: server Postgres theo `database.postgres.*` trong YAML (host/port/user/password/dbname).
- Media: `./storages/media/{accountID}/{convID}/{fileID}.{ext}`.

## Files
- `internal/store/store.go` — `Store` struct + `NewPostgres`.
- `internal/store/queries.go` — Postgres CRUD queries (package-level const).
- `internal/store/store_postgres.go` — migrations + `ensureSessionServiceMapPG`,
  `ensureAccountUserIDPG` (idempotent ALTER).
- `internal/store/testutil.go` — testdb-only helper (`newTestStore`, schema-per-test).
- `docs/design.md` §A4.6 — quy ước schema.
- `docs/database/schema.sql` — schema reference (sync từ store_postgres.go).

## Verification
- [x] Migration tự chạy khi `NewPostgres`.
- [x] Index phục vụ query `messages(account_id, conv_id, timestamp DESC)`.
- [x] Foreign key hoạt động.
- [x] Integration test (`-tags testdb`) tạo schema riêng + drop khi xong.
- [x] Drop SQLite backend — không còn `store_sqlite.go`, `NewSQLite`,
      `BackendSQLite`, dialect branches trong queries.

## Tạo user/db lần đầu

```bash
sudo -u postgres createuser -s zcloud
sudo -u postgres createdb -O zcloud zcloud
```

Sau đó set password qua env `ZCLOUD_DB_PASSWORD` (tham chiếu trong YAML qua
`${ZCLOUD_DB_PASSWORD}`).
