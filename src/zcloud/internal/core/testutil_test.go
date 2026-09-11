//go:build testdb

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/diepxuan/zcloud/internal/store"
)

// newTestStore mở Postgres + schema riêng cho test, trả về *Store sẵn dùng.
// Skip nếu ZCLOUD_TEST_DSN chưa được set.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("ZCLOUD_TEST_DSN")
	if dsn == "" {
		t.Skip("ZCLOUD_TEST_DSN chưa được set — chạy với -tags testdb và set env.")
	}
	name := strings.ReplaceAll(t.Name(), "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	schema := "zcloud_t_" + name

	dir := t.TempDir()
	admin, err := sql.Open("pgx", dsn)
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

	st, err := store.NewPostgres(dsn, filepath.Join(dir, "media"), 4, 2)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	if _, err := st.DB().Exec("SET search_path TO " + schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateAccount("acc-1", "Test", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return st
}
