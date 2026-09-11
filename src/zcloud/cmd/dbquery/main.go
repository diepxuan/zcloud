// dbquery — tiện ích nhanh để inspect Postgres mà không cần mở psql.
//
// Cách dùng:
//   go run ./cmd/dbquery -dsn "postgres://user:pass@host:5432/db?sslmode=disable" \
//       -conv 5280163336123324706
//
// Nếu không truyền -dsn, lấy từ ZCLOUD_TEST_DSN (env chung cho test/dev).
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("ZCLOUD_TEST_DSN"), "Postgres DSN (default: $ZCLOUD_TEST_DSN)")
	convID := flag.String("conv", "", "Conversation ID to count messages for")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "Thiếu DSN — truyền -dsn hoặc export ZCLOUD_TEST_DSN")
		os.Exit(2)
	}
	if *convID == "" {
		fmt.Fprintln(os.Stderr, "Thiếu -conv <convID>")
		os.Exit(2)
	}

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		os.Exit(1)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE conv_id = $1`, *convID).Scan(&n); err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("conv %s messages: %d\n", *convID, n)
}
