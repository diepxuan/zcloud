package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// Backend xác định loại DB đang dùng. Một số query khác nhau giữa 2 dialect.
type Backend string

const (
	BackendSQLite   Backend = "sqlite"
	BackendPostgres Backend = "postgres"
)

// Store quản lý toàn bộ persistent data. Dùng được với cả SQLite và Postgres
// thông qua database/sql — dialect-specific SQL được tách trong store_sqlite.go
// và store_postgres.go.
type Store struct {
	db        *sql.DB
	backend   Backend
	dbPath    string // chỉ dùng cho SQLite (để hiển thị/log)
	mediaPath string
}

// NewSQLite mở hoặc tạo SQLite database. dbPath trống → mặc định
// ./storages/database/zcloud.db.
func NewSQLite(dbPath, mediaPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = filepath.Join(".", "storages", "database", "zcloud.db")
	}
	if mediaPath == "" {
		mediaPath = filepath.Join(".", "storages", "media")
	}
	for _, dir := range []string{filepath.Dir(dbPath), mediaPath} {
		if dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
			}
		}
	}
	// Pure-Go SQLite driver, không CGO.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("store: wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("store: busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("store: fk: %w", err)
	}
	s := &Store{db: db, backend: BackendSQLite, dbPath: dbPath, mediaPath: mediaPath}
	if err := s.migrateSQLite(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

// NewPostgres mở kết nối Postgres qua pgx. DSN theo định dạng
// postgres://user:pass@host:port/db?sslmode=...
func NewPostgres(dsn, mediaPath string, maxOpen, maxIdle int) (*Store, error) {
	if mediaPath == "" {
		mediaPath = filepath.Join(".", "storages", "media")
	}
	if err := os.MkdirAll(mediaPath, 0755); err != nil {
		return nil, fmt.Errorf("store: mkdir media: %w", err)
	}
	// pgx driver đăng ký qua "pgx" (native protocol) hoặc "postgres" (qua database/sql).
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open postgres: %w", err)
	}
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	// Verify connection trước khi migrate.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}
	s := &Store{db: db, backend: BackendPostgres, mediaPath: mediaPath}
	if err := s.migratePostgres(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate postgres: %w", err)
	}
	return s, nil
}

// Backend trả về backend đang dùng (sqlite/postgres).
func (s *Store) Backend() Backend { return s.backend }

// DB trả về *sql.DB (cho caller nào cần truy cập trực tiếp).
func (s *Store) DB() *sql.DB { return s.db }

// Path trả về đường dẫn DB (SQLite) hoặc DSN (Postgres).
func (s *Store) Path() string {
	if s.backend == BackendPostgres {
		return "postgres"
	}
	return s.dbPath
}

// MediaPath trả về thư mục media trên disk.
func (s *Store) MediaPath() string { return s.mediaPath }

// Close đóng connection pool.
func (s *Store) Close() error { return s.db.Close() }

// ====================================
// MediaFile path helpers (chỉ dùng cho SQLite; Postgres thì media vẫn trên disk)
// ====================================

// MediaFilePath trả về đường dẫn đầy đủ cho file media.
// Cấu trúc: {mediaPath}/{accountID}/{convID}/{fileID}.{ext}
func (s *Store) MediaFilePath(accountID, convID, fileID, ext string) string {
	return filepath.Join(s.mediaPath, accountID, convID, fileID+"."+ext)
}

// MediaDir tạo và trả về thư mục chứa media
func (s *Store) MediaDir(accountID, convID string) string {
	dir := filepath.Join(s.mediaPath, accountID, convID)
	_ = os.MkdirAll(dir, 0755)
	return dir
}
