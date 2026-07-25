package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Config chứa toàn bộ cấu hình cho zcloud daemon
type Config struct {
	// Server
	Port    int    // Cổng HTTP (mặc định: 8080)
	Domain  string // Domain (mặc định: zcloud.diepxuan.corp)
	HostURL string // URL đầy đủ cho WebSocket

	// Session
	SessionDir string // Thư mục lưu session

	// DB (tùy chọn — SQLite path, mặc định tạo zcloud.db)
	DBPath string

	// Media
	MediaDir string // Thư mục lưu media files (ảnh, video, voice)

	// Log
	LogLevel int // 0=info, 1=debug, 2=verbose

	// Dev
	DevMode bool // Dev mode (cho phép CORS, v.v.)
}

// Parse đọc config từ CLI flags và env vars
// Thứ tự ưu tiên: CLI flag > env var > mặc định
func Parse() *Config {
	cfg := &Config{}

	// Mặc định
	cfg.Port = 8080
	cfg.Domain = "zcloud.diepxuan.corp"
	cfg.SessionDir = defaultSessionDir()
	cfg.LogLevel = 0

	// CLI flags
	flag.IntVar(&cfg.Port, "port", envInt("ZCLOUD_PORT", cfg.Port), "HTTP server port")
	flag.StringVar(&cfg.Domain, "domain", envStr("ZCLOUD_DOMAIN", cfg.Domain), "Domain name")
	flag.StringVar(&cfg.SessionDir, "session-dir", envStr("ZCLOUD_SESSION_DIR", cfg.SessionDir), "Session storage directory")
	flag.StringVar(&cfg.DBPath, "db-path", envStr("ZCLOUD_DB_PATH", ""), "Database path (default: zcloud.db)")
	flag.StringVar(&cfg.MediaDir, "media-dir", envStr("ZCLOUD_MEDIA_DIR", "./zcloud-media"), "Media storage directory")
	flag.IntVar(&cfg.LogLevel, "log-level", envInt("ZCLOUD_LOG_LEVEL", cfg.LogLevel), "Log level: 0=info, 1=debug, 2=verbose")
	flag.BoolVar(&cfg.DevMode, "dev", envBool("ZCLOUD_DEV", false), "Development mode")
	flag.Parse()

	// Host URL cho WebSocket client
	cfg.HostURL = fmt.Sprintf("%s:%d", cfg.Domain, cfg.Port)

	return cfg
}

// Addr trả về địa chỉ listen (host:port)
func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// HTTPEndpoint trả về URL HTTP đầy đủ
func (c *Config) HTTPEndpoint() string {
	return fmt.Sprintf("http://%s:%d", c.Domain, c.Port)
}

// WSEndpoint trả về URL WebSocket
func (c *Config) WSEndpoint() string {
	return fmt.Sprintf("ws://%s:%d/ws", c.Domain, c.Port)
}

// ====================================
// Helpers
// ====================================

func defaultSessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".zcloud", "sessions")
	}
	return filepath.Join(home, ".zcloud", "sessions")
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscanf(v, "%d", &i)
		return i
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}
