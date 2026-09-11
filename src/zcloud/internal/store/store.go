package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// Backend xác định loại DB đang dùng. Hiện tại zcloud chỉ hỗ trợ Postgres.
type Backend string

const (
	BackendPostgres Backend = "postgres"
)

// Store quản lý toàn bộ persistent data. Hiện chỉ hỗ trợ Postgres thông qua
// database/sql + pgx driver; schema/migrations trong store_postgres.go.
type Store struct {
	db        *sql.DB
	backend   Backend
	mediaPath string
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

// Backend trả về backend đang dùng (hiện chỉ có postgres).
func (s *Store) Backend() Backend { return s.backend }

// DB trả về *sql.DB (cho caller nào cần truy cập trực tiếp).
func (s *Store) DB() *sql.DB { return s.db }

// Path trả về "postgres" — giữ API cũ cho log/UI ("Database sẵn sàng — %s").
func (s *Store) Path() string { return "postgres" }

// MediaPath trả về thư mục media trên disk.
func (s *Store) MediaPath() string { return s.mediaPath }

// Close đóng connection pool.
func (s *Store) Close() error { return s.db.Close() }

// ====================================
// MediaFile path helpers — media luôn trên disk, DB chỉ lưu metadata.
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
