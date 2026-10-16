// Package server chứa logic khởi động HTTP server + background workers
// của zcloud. Được tách ra từ main.go để có thể gọi từ nhiều entry point
// (zcloudd trực tiếp, zcloudd serv, zcloudd serv watch).
//
// runServer khởi động đầy đủ: Postgres → HTTP server → repair thread →
// Zalo listener → auto-sync → media worker. Block cho đến khi nhận SIGINT/SIGTERM.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/diepxuan/zcloud/internal/api"
	"github.com/diepxuan/zcloud/internal/config"
	"github.com/diepxuan/zcloud/internal/logging"
	"github.com/diepxuan/zcloud/internal/store"
)

// Run khởi động zcloud HTTP server với đầy đủ background workers.
// Trả về khi server đã shutdown hoàn toàn.
func Run() error {
	cfg := config.Parse()
	logger := logging.New(cfg.Log.Level)
	slog.SetDefault(logger.Logger)
	logger.Info("daemon_starting", "endpoint", cfg.HTTPEndpoint(), "media_dir", cfg.MediaDirPath())

	// ====================================
	// Initialize database (Postgres-only)
	// ====================================
	dsn := cfg.PostgresDSN()
	if dsn == "" {
		err := fmt.Errorf("postgres DSN is empty; check host, user, and dbname")
		logger.Error("database_config_invalid", "error", err)
		return err
	}
	logger.Info("database_connecting",
		"user", cfg.Database.Postgres.User,
		"host", cfg.Database.Postgres.Host,
		"port", cfg.Database.Postgres.Port,
		"database", cfg.Database.Postgres.DBName,
	)
	db, err := store.NewPostgres(dsn, cfg.MediaDirPath(),
		cfg.Database.Postgres.MaxOpenConns, cfg.Database.Postgres.MaxIdleConns)
	if err != nil {
		logger.Error("database_init_failed", "error", err)
		return err
	}
	defer db.Close()

	if err := os.MkdirAll(cfg.MediaDirPath(), 0755); err != nil {
		logger.Error("media_dir_create_failed", "error", err)
		return err
	}
	logger.Info("database_ready", "backend", db.Path())

	// ====================================
	// Setup HTTP server
	// ====================================
	mux := http.NewServeMux()
	s := api.NewServer(db, logger)
	api.SetupRouter(mux, s, db)

	var handler http.Handler = mux
	if cfg.Server.DevMode {
		handler = corsMiddleware(mux)
	}
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
		logger.Info("http_listening", "addr", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http_server_failed", "error", err)
			os.Exit(1)
		}
	}()

	// Dọn tin 1-1 bị gom nhầm vào thread mang uid của chính account.
	repairSelfThreads(db, logger)

	// Boot Zalo listener cho mọi account đang active.
	activeIDs, err := db.ListActiveAccountIDs()
	if err != nil {
		logger.Error("zalo_listener_boot_list_failed", "error", err)
	} else if len(activeIDs) == 0 {
		logger.Info("zalo_listener_boot_empty")
	} else {
		logger.Info("zalo_listener_boot", "account_count", len(activeIDs))
		for _, accID := range activeIDs {
			s.StartZaloListener(accID)
		}
	}

	// Watcher quét account active mới.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ids, err := db.ListActiveAccountIDs()
			if err != nil {
				continue
			}
			for _, accID := range ids {
				s.StartZaloListener(accID)
			}
		}
	}()

	// Background session refresh — mỗi 30 phút.
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			logger.Info("session_refresh_started")
			s.RefreshAllSessions()
		}
	}()

	// Auto-sync scheduler.
	syncInterval := 10 * time.Minute
	if v := os.Getenv("ZC_AUTOSYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 30*time.Second {
			syncInterval = d
		}
	}
	scheduler := api.NewSyncSchedulerForServer(s, syncInterval)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	scheduler.Start(schedulerCtx)
	defer scheduler.Stop()
	defer schedulerCancel()
	logger.Info("auto_sync_started", "interval", syncInterval.String())

	// Media worker.
	mediaWorker := api.NewMediaWorker(db, logger, s.WSManager, 5*time.Second, 20)
	mediaCtx, mediaCancel := context.WithCancel(context.Background())
	mediaWorker.Start(mediaCtx)
	defer mediaWorker.Stop()
	defer mediaCancel()
	logger.Info("media_worker_started", "interval", "5s")

	// Friends worker — cache GetFriends vào DB contacts theo lịch
	// acc1 → 3s → acc2 → 3s → ... → accN → interval (mặc định 1 phút).
	friendsInterval := 1 * time.Minute
	if v := os.Getenv("ZC_FRIENDS_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 10*time.Second {
			friendsInterval = d
		}
	}
	friendsStagger := 3 * time.Second
	if v := os.Getenv("ZC_FRIENDS_STAGGER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			friendsStagger = d
		}
	}
	friendsWorker := api.NewFriendsWorker(db, logger, friendsInterval, friendsStagger)
	friendsCtx, friendsCancel := context.WithCancel(context.Background())
	friendsWorker.Start(friendsCtx)
	defer friendsWorker.Stop()
	defer friendsCancel()
	logger.Info("friends_worker_started", "interval", friendsInterval.String(), "stagger", friendsStagger.String())

	<-done
	logger.Info("daemon_stopping")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http_shutdown_failed", "error", err)
		return err
	}

	logger.Info("daemon_stopped")
	return nil
}

// repairSelfThreads chuyển tin đến bị lưu nhầm vào thread mang uid của chính
// account về đúng thread người gửi. Chỉ chạy cho account có user_id (uid thuần
// lấy từ session), và im lặng khi không còn gì để dọn.
func repairSelfThreads(db *store.Store, logger *logging.Logger) {
	accounts, err := db.ListAccounts(0, false)
	if err != nil {
		logger.Error("self_thread_account_list_failed", "error", err)
		return
	}
	for _, a := range accounts {
		if a.UserID == "" {
			continue
		}
		n, err := db.CountSelfThreadMessages(a.ID, a.UserID)
		if err != nil {
			logger.Error("self_thread_count_failed", "account_id", a.ID, "error", err)
			continue
		}
		if n == 0 {
			continue
		}
		rep, err := db.RepairSelfThreadMessages(a.ID, a.UserID)
		if err != nil {
			logger.Error("self_thread_repair_failed", "account_id", a.ID, "error", err)
			continue
		}
		logger.Info("self_thread_repaired",
			"account_id", a.ID,
			"messages", rep.Messages,
			"media", rep.Media,
			"jobs", rep.MediaJobs,
			"files", rep.MovedFiles,
		)
		for _, e := range rep.FileErrors {
			logger.Error("self_thread_file_repair_failed", "account_id", a.ID, "error", e)
		}
	}
}

func loggingMiddleware(next http.Handler, logger *logging.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http_request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
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
