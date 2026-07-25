package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/diepxuan/zcloud/internal/api"
	"github.com/diepxuan/zcloud/internal/config"
	"github.com/diepxuan/zcloud/internal/store"
)

func main() {
	// Parse config
	cfg := config.Parse()

	// Logger
	logger := log.New(os.Stdout, "[zcloud] ", log.LstdFlags|log.Lshortfile)
	logger.Printf("Khởi động zcloud daemon — %s", cfg.HTTPEndpoint())
	logger.Printf("Database: %s", cfg.DBPath)
	logger.Printf("Media dir: %s", cfg.MediaDir)

	// ====================================
	// Initialize database
	// ====================================

	db, err := store.New(cfg.DBPath, cfg.MediaDir)
	if err != nil {
		logger.Fatalf("Database init error: %v", err)
	}
	defer db.Close()

	// Tạo media directory nếu chưa có
	if err := os.MkdirAll(cfg.MediaDir, 0755); err != nil {
		logger.Fatalf("Media dir error: %v", err)
	}
	logger.Printf("Database sẵn sàng — %s", db.Path())

	// ====================================
	// Setup HTTP server
	// ====================================

	mux := http.NewServeMux()

	// Tạo server handler và gắn tất cả routes
	s := api.NewServer(db, logger)
	api.SetupRouter(mux, s, db)

	// CORS middleware (dev mode)
	var handler http.Handler = mux
	if cfg.DevMode {
		handler = corsMiddleware(mux)
	}

	// Logging middleware
	handler = loggingMiddleware(handler, logger)

	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ====================================
	// Graceful shutdown
	// ====================================

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Printf("Listen on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	logger.Println("Đang tắt server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Shutdown error: %v", err)
	}

	logger.Println("Server đã tắt.")
}

// ====================================
// Middleware
// ====================================

func loggingMiddleware(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Printf("%s %s — %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func init() {
	fmt.Println("zcloudd — Zalo Cloud Service")
	fmt.Println("Phiên bản phát triển")
	fmt.Println()
}
