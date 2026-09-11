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
	"github.com/diepxuan/zcloud/internal/servcmd"
	"github.com/diepxuan/zcloud/internal/store"
)

// init được gọi trước main — in banner và check subcommand `serv` để dispatch
// sớm, tránh phải parse flags toàn cục khi user chỉ muốn quản lý service.
func init() {
	fmt.Println("zcloudd — Zalo Cloud Service")
	fmt.Println("Phiên bản phát triển")
	fmt.Println()
}

func main() {
	// Dispatch: nếu argv[1] == "serv" → chạy subcommand quản lý service.
	// Đây là entry point cho systemd ExecStart=zcloudd serv, không phải server.
	if len(os.Args) > 1 && os.Args[1] == "serv" {
		if err := runServ(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "[zcloud] serv error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Parse config
	cfg := config.Parse()

	// Logger
	logger := log.New(os.Stdout, "[zcloud] ", log.LstdFlags|log.Lshortfile)
	logger.Printf("Khởi động zcloud daemon — %s", cfg.HTTPEndpoint())
	logger.Printf("Media dir: %s", cfg.MediaDirPath())

	// ====================================
	// Initialize database (Postgres-only)
	// ====================================

	dsn := cfg.PostgresDSN()
	if dsn == "" {
		logger.Fatalf("Postgres DSN rỗng — kiểm tra host/user/dbname trong config")
	}
	logger.Printf("Postgres: %s@%s:%d/%s", cfg.Database.Postgres.User, cfg.Database.Postgres.Host, cfg.Database.Postgres.Port, cfg.Database.Postgres.DBName)
	db, err := store.NewPostgres(dsn, cfg.MediaDirPath(),
		cfg.Database.Postgres.MaxOpenConns, cfg.Database.Postgres.MaxIdleConns)
	if err != nil {
		logger.Fatalf("Database init error: %v", err)
	}
	defer db.Close()

	// Tạo media directory nếu chưa có
	if err := os.MkdirAll(cfg.MediaDirPath(), 0755); err != nil {
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
	if cfg.Server.DevMode {
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

	// Dọn tin 1-1 bị gom nhầm vào thread mang uid của chính account (bug cũ ở
	// wsMessage.toMessage). Chạy trước listener để lịch sử đúng thread ngay từ
	// lần render đầu; idempotent nên lần khởi động sau không làm gì.
	repairSelfThreads(db, logger)

	// Boot Zalo listener cho mọi account đang active — listener chạy độc lập,
	// luôn kết nối Zalo để nhận tin nhắn real-time và lưu lịch sử.
	activeIDs, err := db.ListActiveAccountIDs()
	if err != nil {
		logger.Printf("zalo-ws: boot list err=%v", err)
	} else if len(activeIDs) == 0 {
		logger.Printf("zalo-ws: boot — no active account")
	} else {
		logger.Printf("zalo-ws: boot — starting listeners for %d account(s)", len(activeIDs))
		for _, accID := range activeIDs {
			api.StartZaloListener(db, accID, logger)
		}
	}

	// Watcher quét account active mới (login QR/cookie) — start listener nếu chưa có.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ids, err := db.ListActiveAccountIDs()
			if err != nil { continue }
			for _, accID := range ids {
				api.StartZaloListener(db, accID, logger)
			}
		}
	}()

	// Background session refresh — mỗi 30 phút
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			logger.Println("autoRefresh: background refresh start")
			s.RefreshAllSessions()
		}
	}()

	// Auto-sync scheduler — định kỳ kéo tin nhắn cũ cho mọi conversation.
	// Tần suất: env ZC_AUTOSYNC_INTERVAL (Go duration), mặc định 10 phút.
	syncInterval := 10 * time.Minute
	if v := os.Getenv("ZC_AUTOSYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 30*time.Second {
			syncInterval = d
		}
	}
	scheduler := api.NewSyncScheduler(db, logger, syncInterval)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	scheduler.Start(schedulerCtx)
	defer scheduler.Stop()
	logger.Printf("auto-sync: scheduler started (interval=%s)", syncInterval)

	// Media worker — quét pending media jobs và tải file về disk theo queue.
	mediaWorker := api.NewMediaWorker(db, logger, 5*time.Second, 20)
	mediaCtx, mediaCancel := context.WithCancel(context.Background())
	mediaWorker.Start(mediaCtx)
	defer mediaWorker.Stop()
	logger.Printf("media-worker: started (interval=5s)")

	<-done
	logger.Println("Đang tắt server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Shutdown error: %v", err)
	}
	schedulerCancel()
	mediaCancel()

	logger.Println("Server đã tắt.")
}

// runServ dispatch sang package servcmd. Tách ra để giữ main.go gọn.
func runServ(args []string) error {
	return servcmd.Run(args)
}
// ====================================
// Middleware
// ====================================

// repairSelfThreads chuyển tin đến bị lưu nhầm vào thread mang uid của chính
// account về đúng thread người gửi. Chỉ chạy cho account có user_id (uid thuần
// lấy từ session), và im lặng khi không còn gì để dọn.
func repairSelfThreads(db *store.Store, logger *log.Logger) {
	accounts, err := db.ListAccounts(0)
	if err != nil {
		logger.Printf("repair-thread: list accounts err=%v", err)
		return
	}
	for _, a := range accounts {
		if a.UserID == "" {
			continue
		}
		n, err := db.CountSelfThreadMessages(a.ID, a.UserID)
		if err != nil {
			logger.Printf("repair-thread: count %s err=%v", a.ID, err)
			continue
		}
		if n == 0 {
			continue
		}
		rep, err := db.RepairSelfThreadMessages(a.ID, a.UserID)
		if err != nil {
			logger.Printf("repair-thread: %s err=%v", a.ID, err)
			continue
		}
		logger.Printf("repair-thread: %s — messages=%d media=%d jobs=%d files=%d",
			a.ID, rep.Messages, rep.Media, rep.MediaJobs, rep.MovedFiles)
		for _, e := range rep.FileErrors {
			logger.Printf("repair-thread: %s file err=%s", a.ID, e)
		}
	}
}

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
