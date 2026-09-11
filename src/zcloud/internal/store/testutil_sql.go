//go:build testdb

package store

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// sqlOpenAdmin mở connection tạm để tạo/drop schema — không dùng pool của Store.
func sqlOpenAdmin(dsn string) (*sql.DB, error) {
	return sql.Open("pgx", dsn)
}
