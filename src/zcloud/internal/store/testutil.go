//go:build testdb

package store

// Helper test mở Postgres thật. Chỉ build khi tag "testdb" được bật, nên
// `go test ./...` mặc định vẫn pass mà không cần Postgres chạy.
//
// Cách dùng:
//   ZCLOUD_TEST_DSN="postgres://user:pass@host:5432/db?sslmode=disable" \
//       go test -tags testdb ./...
//
// Mỗi test tạo schema riêng (zcloud_test_<TestName>) để chạy song song an
// toàn. Schema được drop khi test kết thúc.

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDSNOrSkip(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ZCLOUD_TEST_DSN")
	if dsn == "" {
		t.Skip("ZCLOUD_TEST_DSN chưa được set — bỏ qua test cần Postgres. Set env rồi chạy lại với -tags testdb.")
	}
	return dsn
}

func testSchemaName(t *testing.T) string {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return fmt.Sprintf("zcloud_t_%s", name)
}

// dsnWithSchema thêm ?search_path=<schema> vào DSN để mọi
// connection trong pool tự dùng schema đó. Tránh bug SET search_path chỉ
// áp dụng cho 1 connection trong pool (pgx5 hỗ trợ qua RuntimeParams).
func dsnWithSchema(dsn, schema string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// newTestStore mở Postgres, tạo schema riêng cho test, chạy migration,
// trả về *Store sẵn dùng. Tự cleanup schema khi test xong.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSNOrSkip(t)
	schema := testSchemaName(t)
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "media")

	// Dùng NewPostgres như bình thường; trước đó tạo schema riêng qua 1
	// admin connection.
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

	st, err := NewPostgres(dsnWithSchema(dsn, schema), mediaPath, 4, 2)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	// NewPostgres đã migrate với search_path đúng (từ DSN). Tạo test account.
	t.Cleanup(func() { _ = st.Close() })

	if err := st.CreateAccount("acc-1", "Test Account", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return st
}

// newTestStoreFull: giống newTestStore nhưng cho package khác (api, core).
func newTestStoreFull(t *testing.T) (*Store, error) {
	t.Helper()
	dsn := testDSNOrSkip(t)
	schema := testSchemaName(t)
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "media")

	admin, err := sqlOpenAdmin(dsn)
	if err != nil {
		return nil, err
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		return nil, err
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})

	st, err := NewPostgres(dsnWithSchema(dsn, schema), mediaPath, 4, 2)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, nil
}
