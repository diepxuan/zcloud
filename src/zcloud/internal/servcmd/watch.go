// Package servcmd — watch.go (T21 refactor)
//
// Watch flow mới (T21): zcloudd serv watch là 1 PROCESS DUY NHẤT vừa chạy HTTP
// server vừa watch source. Khi source đổi → build → smoke test trên port
// random → nếu pass thì os.Exit(0) để systemd Restart=always restart process
// với binary mới. Không spawn child, không race port.
//
// Flow mỗi lần source đổi:
//   1. fsnotify phát hiện file thay đổi (filter: *.go/*.html/*.css/*.js/*.sh,
//      bỏ qua .sum, _test.go, .db, op Chmod/Access).
//   2. debounce 300ms (gom nhiều event thành 1 cycle).
//   3. cycleMu.TryLock() — bỏ qua nếu đang có cycle khác.
//   4. buildBinary → ghi ra /data/zcloud/zcloudd.new (không ghi đè binary đang chạy).
//   5. runSmokeTest(binary.new) → chạy binary mới với ZCLOUD_PORT=random, poll /api/health.
//      - PASS (200 trong 10s) → kill smoke binary, atomic rename .new → binary gốc.
//      - FAIL → kill smoke binary, xoá .new, log lỗi, KHÔNG restart.
//   6. PASS → logger.Println("restart OK") + os.Exit(0) → systemd restart.
package servcmd

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal/server"
	"github.com/fsnotify/fsnotify"
)

// buildBinary chạy `go build` ra binary path đã cho. PATH augment với
// /usr/local/go/bin (override qua env ZCLOUD_GO_BIN).
func buildBinary(binary, sourceDir string) error {
	goBin := os.Getenv("ZCLOUD_GO_BIN")
	if goBin == "" {
		goBin = "/usr/local/go/bin"
	}
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/zcloudd/")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":"+goBin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}
	return nil
}

// runSmokeTest chạy binary trên port random, poll /api/health đến khi 200 OK
// hoặc timeout 10s. Trả về nil nếu pass. Sau khi xong (pass hay fail) đều kill
// smoke binary sạch + reap child — tránh leak process, port, zombie.
func runSmokeTest(binary string, logger *log.Logger) error {
	port, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("pick free port: %w", err)
	}
	logger.Printf("smoke test: starting binary on port %d", port)

	cmd := exec.Command(binary, "serv")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("ZCLOUD_PORT=%d", port),
		"PATH="+os.Getenv("PATH")+":/usr/local/go/bin")
	cmd.Dir = ProjectRoot()
	stdout := &prefixWriter{logger: logger, prefix: "[smoke] "}
	stderr := &prefixWriter{logger: logger, prefix: "[smoke] "}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start smoke binary: %w", err)
	}

	// Reap child khi kết thúc.
	defer func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()

	// Poll /api/health với backoff 200ms, deadline 10s.
	url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				logger.Printf("smoke test PASS (%s, %d bytes)", url, len(body))
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("smoke test FAIL trên port %d (timeout 10s): %w", port, lastErr)
}

// pickFreePort chọn 1 port TCP free. Kernel chọn qua listen ":0" rồi close.
// Window race rất nhỏ nhưng acceptable cho smoke test ephemeral.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if port < 1024 {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		port = 30000 + int(binary.BigEndian.Uint16(b[:]))%(50000-30000)
	}
	return port, nil
}

// prefixWriter pipe output của subprocess ra logger với prefix.
type prefixWriter struct {
	logger *log.Logger
	prefix string
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		if line == "" {
			continue
		}
		w.logger.Print(w.prefix + line)
	}
	return len(p), nil
}

// Watch là watch mode mới (T21):
//   - Chạy HTTP server (server.Run) + fsnotify source trong cùng 1 process.
//   - Mỗi event trigger cycle build + smoke + (nếu pass) atomic rename + os.Exit(0).
//   - cycleMu bảo vệ: chỉ 1 cycle chạy tại 1 thời điểm.
//   - Khi ctx cancel → graceful shutdown server.
func Watch(ctx context.Context, binary string, sourceDir string) error {
	if !filepath.IsAbs(binary) {
		binary = filepath.Join(ProjectRoot(), "zcloudd")
	}
	logger := log.New(os.Stdout, "[zcloud] ", log.LstdFlags)

	// 1. Initial build (đảm bảo binary đang chạy là mới nhất).
	logger.Println("Build binary lần đầu...")
	if err := buildBinary(binary, sourceDir); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	if err := os.Chmod(binary, 0755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}

	// 2. Setup fsnotify.
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer w.Close()

	if err := w.Add(sourceDir); err != nil {
		return fmt.Errorf("watch %s: %w", sourceDir, err)
	}
	_ = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		name := info.Name()
		if name == ".git" || strings.HasSuffix(path, ".db") {
			return filepath.SkipDir
		}
		_ = w.Add(path)
		return nil
	})
	logger.Printf("Watch source code: %s (initial build OK, chmod 0755)", sourceDir)

	// 3. Channel signal: fsnotify → debounce → cycle.
	var cycleMu sync.Mutex
	var pending *time.Timer
	coalesce := func(path string) {
		if pending != nil {
			pending.Stop()
		}
		pending = time.AfterFunc(300*time.Millisecond, func() {
			if !cycleMu.TryLock() {
				logger.Printf("cycle đang chạy — skip trigger từ %s", path)
				return
			}
			defer cycleMu.Unlock()

			newBin := binary + ".new"
			logger.Printf("Code thay đổi (%s) → build → smoke → atomic rename → restart", path)
			if err := buildBinary(newBin, sourceDir); err != nil {
				logger.Printf("Build FAIL — giữ binary cũ: %v", err)
				_ = os.Remove(newBin)
				return
			}
			if err := runSmokeTest(newBin, logger); err != nil {
				logger.Printf("Smoke test FAIL — giữ binary cũ, KHÔNG restart: %v", err)
				_ = os.Remove(newBin)
				return
			}
			// Atomic rename: chỉ thay đổi inode khi smoke đã pass.
			if err := os.Chmod(newBin, 0755); err != nil {
				logger.Printf("chmod .new FAIL: %v", err)
				_ = os.Remove(newBin)
				return
			}
			if err := os.Rename(newBin, binary); err != nil {
				logger.Printf("rename .new → binary FAIL: %v", err)
				_ = os.Remove(newBin)
				return
			}
			logger.Printf("atomic rename OK — os.Exit(0) để systemd restart với binary mới")
			// Exit ngay — systemd Restart=always sẽ boot lại process này.
			os.Exit(0)
		})
	}

	// 4. Goroutine: chạy server.Run() trong cùng process.
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run()
	}()

	// 5. Loop: drain watcher.
	for {
		select {
		case <-ctx.Done():
			logger.Println("Context done — graceful shutdown server.")
			// Gửi SIGTERM đến chính mình để server.Run() shutdown sạch.
			p, _ := os.FindProcess(os.Getpid())
			if p != nil {
				_ = p.Signal(os.Interrupt)
			}
			// Đợi server.Run return (timeout 12s).
			select {
			case <-serverErr:
			case <-time.After(12 * time.Second):
				logger.Println("server shutdown timeout")
			}
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !isBuildableEvent(ev) {
				continue
			}
			logger.Printf("event: %s %s", ev.Op, ev.Name)
			coalesce(ev.Name)
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			logger.Printf("watcher error: %v", err)
		case err := <-serverErr:
			// server.Run return (ví dụ signal nhận) → thoát watch.
			if err != nil {
				logger.Printf("server.Run returned: %v", err)
			}
			return err
		}
	}
}

// buildableRe match các file extension cần rebuild.
var buildableRe = regexp.MustCompile(`\.(go|html|css|js|sh)$`)

// isBuildableEvent: op Write/Create/Remove + extension trong buildableRe,
// bỏ qua _test.go, *.sum, *.db.
func isBuildableEvent(ev fsnotify.Event) bool {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return false
	}
	base := filepath.Base(ev.Name)
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".sum") || strings.HasSuffix(base, ".db") {
		return false
	}
	return buildableRe.MatchString(base)
}
