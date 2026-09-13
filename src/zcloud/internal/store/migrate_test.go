//go:build testdb

package store

// Test cho accounts.transport / accounts.syncv2_state — regression
// 13/09/2026: T22.3 thêm field Transport vào Account struct + SELECT
// trong GetAccount/ListAccounts nhưng QUÊN tạo migration. Restart dẫn
// đến `column "transport" does not exist` ở mọi ListAccounts → UI list
// account trả 500.
//
// Lưu ý: các ensure*PG hiện tại có pre-existing bug — query
// information_schema KHÔNG filter theo schema nên nếu public.accounts đã
// có cột, ALTER TABLE bị skip trên schema mới (xem doc bên dưới).
// Test này dùng schema sạch để verify migration idempotent.

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// newCleanSchema tạo schema sạch (không có public.accounts leak) + tự
// chạy lại migrationAccountsPG + ensure*PG để tái tạo bug. Trả về
// *sql.DB trỏ thẳng vào schema đó.
func newCleanSchema(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSNOrSkip(t)
	schema := testSchemaName(t)

	admin, err := sqlOpenAdmin(dsn)
	if err != nil {
		t.Fatalf("admin open: %v", err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})

	db, err := sqlOpenAdmin(dsnWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_ = filepath.Separator // keep import for go vet
	return db
}

// hasColumnInSchema kiểm tra cột có tồn tại trong schema hiện tại
// (KHÔNG dùng pattern ensure*PG cũ vì cross-schema leak).
func hasColumnInSchema(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatalf("check %s.%s: %v", table, column, err)
	}
	return exists
}

// TestMigration_AccountTransport đảm bảo cột accounts.transport tồn tại
// sau khi NewPostgres chạy migration (regression 13/09/2026).
//
// Hạn chế: do pre-existing bug ở ensureAccount*PG (information_schema
// không filter theo schema), test này chỉ pass khi public.accounts
// CHƯA có cột transport — tức là trên DB sạch. Trong production, bug
// này không lộ vì public.accounts được tạo sớm với schema đầy đủ.
func TestMigration_AccountTransport(t *testing.T) {
	db := newCleanSchema(t)
	_ = db
	st, err := NewPostgres(dsnWithSchema(testDSNOrSkip(t), testSchemaName(t)), filepath.Join(t.TempDir(), "media"), 4, 2)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if !hasColumnInSchema(t, st.db, "accounts", "transport") {
		t.Fatal("accounts.transport không tồn tại trong schema test (regression 13/09/2026)")
	}
	// GetAccount/ListAccounts cũng phải chạy được.
	if _, err := st.ListAccounts(0, false); err != nil {
		t.Fatalf("ListAccounts fail: %v", err)
	}
}

// TestMigration_AccountSyncV2State tương tự cho cột syncv2_state +
// helper Get/Set state.
func TestMigration_AccountSyncV2State(t *testing.T) {
	db := newCleanSchema(t)
	_ = db
	st, err := NewPostgres(dsnWithSchema(testDSNOrSkip(t), testSchemaName(t)), filepath.Join(t.TempDir(), "media"), 4, 2)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if !hasColumnInSchema(t, st.db, "accounts", "syncv2_state") {
		t.Fatal("accounts.syncv2_state không tồn tại (regression T22.3)")
	}

	// Insert raw (CreateAccount có pre-existing bug ở INSERT user_id).
	if _, err := st.db.Exec(`
		INSERT INTO accounts (id, display_name, account_type)
		VALUES ('acc-syncv2-test', 'S', 1)
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	raw, err := st.GetAccountSyncV2State("acc-syncv2-test")
	if err != nil {
		t.Fatalf("GetAccountSyncV2State: %v", err)
	}
	if raw != "{}" {
		t.Fatalf("raw=%q want {}", raw)
	}

	const newState = `{"phase":"pulling","lastSeqId":42}`
	if err := st.SetAccountSyncV2State("acc-syncv2-test", newState); err != nil {
		t.Fatalf("SetAccountSyncV2State: %v", err)
	}
	got, err := st.GetAccountSyncV2State("acc-syncv2-test")
	if err != nil {
		t.Fatalf("GetAccountSyncV2State #2: %v", err)
	}
	// Postgres JSONB thêm space sau dấu : — so sánh linh hoạt.
	if !strings.Contains(got, `"phase"`) || !strings.Contains(got, `"pulling"`) || !strings.Contains(got, `"lastSeqId"`) || !strings.Contains(got, `42`) {
		t.Fatalf("state không khớp: %s", got)
	}

	// accountID không tồn tại: Set phải err, Get trả '{}' (an toàn).
	if err := st.SetAccountSyncV2State("acc-not-exist", newState); err == nil {
		t.Fatal("SetAccountSyncV2State(accountID không tồn tại) phải err")
	}
	raw2, err := st.GetAccountSyncV2State("acc-not-exist")
	if err != nil || raw2 != "{}" {
		t.Fatalf("Get(not exist): raw=%q err=%v", raw2, err)
	}
}
