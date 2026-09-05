package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config chứa toàn bộ cấu hình cho zcloud daemon.
// Đọc theo thứ tự ưu tiên (sau đè trước):
//   1. Defaults (hard-coded trong code)
//   2. File YAML tại ~/.config/ductn/zcloud.yml (nếu tồn tại)
//   3. Env vars (ZCLOUD_*)
//   4. CLI flags
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Media    MediaConfig    `yaml:"media"`
	Log      LogConfig      `yaml:"log"`
}

// ServerConfig — HTTP server.
type ServerConfig struct {
	Port    int    `yaml:"port"`
	Domain  string `yaml:"domain"`
	DevMode bool   `yaml:"dev_mode"`
}

// DatabaseConfig — chọn backend sqlite hoặc postgres.
type DatabaseConfig struct {
	Backend   string         `yaml:"backend"` // "sqlite" hoặc "postgres"
	SQLite    SQLiteConfig   `yaml:"sqlite"`
	Postgres  PostgresConfig `yaml:"postgres"`
}

// SQLiteConfig — file path.
type SQLiteConfig struct {
	Path string `yaml:"path"`
}

// PostgresConfig — kết nối qua pgx.
// Password có thể chứa ${ZCLOUD_DB_PASSWORD} — sẽ được expand từ env.
type PostgresConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DBName       string `yaml:"dbname"`
	SSLMode      string `yaml:"sslmode"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// MediaConfig — thư mục lưu file media trên disk.
type MediaConfig struct {
	Dir string `yaml:"dir"`
}

// LogConfig — log level 0=info, 1=debug, 2=verbose.
type LogConfig struct {
	Level int `yaml:"level"`
}

// DefaultConfigPath trả về đường dẫn config mặc định theo XDG:
// $XDG_CONFIG_HOME/ductn/zcloud.yml hoặc ~/.config/ductn/zcloud.yml.
func DefaultConfigPath() string {
	if p := os.Getenv("ZCLOUD_CONFIG"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "ductn", "zcloud.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ductn", "zcloud.yml")
}

// LoadYAML đọc file YAML tại path, trả về Config với defaults nếu thiếu field.
// Trả về (nil, nil) nếu file không tồn tại — fallback sang defaults.
func LoadYAML(path string) (*Config, error) {
	cfg := defaults()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	// Expand ${VAR} trước khi parse để password/env-var chuẩn.
	expanded := os.Expand(string(data), os.Getenv)
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// defaults trả về Config với giá trị mặc định, khớp hành vi cũ.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port:    8080,
			Domain:  "zcloud.diepxuan.corp",
			DevMode: false,
		},
		Database: DatabaseConfig{
			Backend: "sqlite",
			SQLite: SQLiteConfig{
				Path: filepath.Join(".", "storages", "database", "zcloud.db"),
			},
			Postgres: PostgresConfig{
				Host:         "127.0.0.1",
				Port:         5432,
				User:         "zcloud",
				Password:     "",
				DBName:       "zcloud",
				SSLMode:      "disable",
				MaxOpenConns: 50,
				MaxIdleConns: 10,
			},
		},
		Media: MediaConfig{
			Dir: filepath.Join(".", "storages", "media"),
		},
		Log: LogConfig{Level: 0},
	}
}

// Parse đọc config theo thứ tự: defaults → YAML → env → CLI flags.
// CLI flags luôn thắng.
func Parse() *Config {
	cfg, err := LoadYAML(DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcloud] config warning: %v\n", err)
		cfg = defaults()
	}

	// CLI flags override. Bind riêng để tránh flag library ghi đè struct trực tiếp.
	flag.IntVar(&cfg.Server.Port, "port", envInt("ZCLOUD_PORT", cfg.Server.Port), "HTTP server port")
	flag.StringVar(&cfg.Server.Domain, "domain", envStr("ZCLOUD_DOMAIN", cfg.Server.Domain), "Domain name")
	flag.BoolVar(&cfg.Server.DevMode, "dev", envBool("ZCLOUD_DEV", cfg.Server.DevMode), "Development mode")
	flag.StringVar(&cfg.Database.Backend, "db-backend", envStr("ZCLOUD_DB_BACKEND", cfg.Database.Backend), "DB backend: sqlite | postgres")
	flag.StringVar(&cfg.Database.SQLite.Path, "db-path", envStr("ZCLOUD_DB_PATH", cfg.Database.SQLite.Path), "SQLite database file path")
	flag.StringVar(&cfg.Media.Dir, "media-dir", envStr("ZCLOUD_MEDIA_DIR", cfg.Media.Dir), "Media storage directory")
	flag.IntVar(&cfg.Log.Level, "log-level", envInt("ZCLOUD_LOG_LEVEL", cfg.Log.Level), "Log level: 0=info, 1=debug, 2=verbose")
	flag.StringVar(&cfg.Database.Postgres.Host, "pg-host", envStr("ZCLOUD_PG_HOST", cfg.Database.Postgres.Host), "Postgres host")
	flag.IntVar(&cfg.Database.Postgres.Port, "pg-port", envInt("ZCLOUD_PG_PORT", cfg.Database.Postgres.Port), "Postgres port")
	flag.StringVar(&cfg.Database.Postgres.User, "pg-user", envStr("ZCLOUD_PG_USER", cfg.Database.Postgres.User), "Postgres user")
	flag.StringVar(&cfg.Database.Postgres.Password, "pg-password", envStr("ZCLOUD_PG_PASSWORD", cfg.Database.Postgres.Password), "Postgres password (env ZCLOUD_DB_PASSWORD)")
	flag.StringVar(&cfg.Database.Postgres.DBName, "pg-dbname", envStr("ZCLOUD_PG_DBNAME", cfg.Database.Postgres.DBName), "Postgres database name")
	flag.StringVar(&cfg.Database.Postgres.SSLMode, "pg-sslmode", envStr("ZCLOUD_PG_SSLMODE", cfg.Database.Postgres.SSLMode), "Postgres sslmode")
	flag.Parse()

	// Tự động fallback: nếu ZCLOUD_DB_PASSWORD có nhưng password trống, dùng env.
	if cfg.Database.Postgres.Password == "" {
		if v := os.Getenv("ZCLOUD_DB_PASSWORD"); v != "" {
			cfg.Database.Postgres.Password = v
		}
	}
	if cfg.Database.Backend == "" {
		cfg.Database.Backend = "sqlite"
	}
	return cfg
}

// Addr trả về địa chỉ listen (host:port).
func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Server.Port) }

// HTTPEndpoint trả về URL HTTP đầy đủ.
func (c *Config) HTTPEndpoint() string { return fmt.Sprintf("http://%s:%d", c.Server.Domain, c.Server.Port) }

// WSEndpoint trả về URL WebSocket cho browser.
func (c *Config) WSEndpoint() string { return fmt.Sprintf("ws://%s:%d/ws", c.Server.Domain, c.Server.Port) }

// DBPath trả về đường dẫn DB (chỉ dùng khi backend=sqlite).
func (c *Config) DBPath() string { return c.Database.SQLite.Path }

// MediaDirPath trả về thư mục media.
func (c *Config) MediaDirPath() string { return c.Media.Dir }

// PostgresDSN trả về connection string cho pgx theo định dạng URL.
// Trả về chuỗi rỗng nếu thiếu trường bắt buộc.
func (c *Config) PostgresDSN() string {
	p := c.Database.Postgres
	if p.Host == "" || p.User == "" || p.DBName == "" {
		return ""
	}
	ssl := p.SSLMode
	if ssl == "" {
		ssl = "disable"
	}
	// URL-encode user/password để tránh lỗi ký tự đặc biệt.
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		urlEncode(p.User), urlEncode(p.Password), p.Host, p.Port, p.DBName, ssl)
}

// urlEncode encode ký tự đặc biệt cho URL DSN.
func urlEncode(s string) string {
	// pgx chấp nhận %XX; chỉ cần escape % và ký tự reserved.
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, " ", "%20")
	s = strings.ReplaceAll(s, "@", "%40")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, "/", "%2F")
	return s
}

// ====================================
// Env helpers
// ====================================

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
