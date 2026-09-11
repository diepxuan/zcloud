package servcmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// rebuildBinary chạy `go build` từ sourceDir ra binary. Trả về nil nếu build OK.
func rebuildBinary() error {
	sourceDir := filepath.Join(ProjectRoot(), "src", "zcloud")
	cmd := exec.Command("go", "build", "-o", BinaryPath(), "./cmd/zcloudd/")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/usr/local/go/bin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}
	return nil
}

// runCheckJS chạy scripts/check-js.sh trong project. Trả về nil nếu pass.
func runCheckJS() error {
	script := filepath.Join(ProjectRoot(), "scripts", "check-js.sh")
	if _, err := os.Stat(script); err != nil {
		return nil // không có script thì skip
	}
	cmd := exec.Command(script)
	cmd.Dir = ProjectRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("check-js: %w\n%s", err, string(out))
	}
	return nil
}

// runCheckUI chạy scripts/check-ui.sh (Lightpanda smoke test). Trả về nil nếu pass
// hoặc nếu lightpanda chưa cài (non-blocking warning).
func runCheckUI() error {
	script := filepath.Join(ProjectRoot(), "scripts", "check-ui.sh")
	if _, err := os.Stat(script); err != nil {
		return nil
	}
	cmd := exec.Command(script)
	cmd.Dir = ProjectRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("check-ui: %w\n%s", err, string(out))
	}
	return nil
}

// lightpandaAvailable kiểm tra binary lightpanda có trên PATH hay không.
func lightpandaAvailable() bool {
	if _, err := exec.LookPath("lightpanda"); err == nil {
		return true
	}
	if _, err := os.Stat("/root/.local/bin/lightpanda"); err == nil {
		return true
	}
	return false
}

// Watch là watch mode giống scripts/zcloudd.sh cũ:
//   1. Build binary lần đầu, start binary
//   2. inotify trên sourceDir
//   3. Khi file thay đổi → check JS → build → restart → (nếu lightpanda có) check UI
//   4. Khi ctx bị cancel → tắt binary, thoát
//
// sourceDir thường là /data/zcloud/src/zcloud.
func Watch(ctx context.Context, binary string, sourceDir string) error {
	if !filepath.IsAbs(binary) {
		binary = filepath.Join(ProjectRoot(), "zcloudd")
	}
	logger := log.New(os.Stdout, "[zcloud] ", log.LstdFlags)

	// Step 1: build lần đầu
	logger.Println("Build binary lần đầu...")
	if err := buildTo(binary, sourceDir); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	if err := os.Chmod(binary, 0755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}

	// Step 2: start binary
	binCmd, err := startBinary(binary, logger)
	if err != nil {
		return err
	}
	defer stopBinary(binCmd, logger)

	// Step 3: watcher
	logger.Printf("Watch source code: %s", sourceDir)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer w.Close()

	if err := w.Add(sourceDir); err != nil {
		return fmt.Errorf("watch %s: %w", sourceDir, err)
	}
	// Walk để bắt cả các subdir.
	_ = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		// Bỏ qua các thư mục rác.
		name := info.Name()
		if name == ".git" || strings.HasSuffix(path, ".db") {
			return filepath.SkipDir
		}
		_ = w.Add(path)
		return nil
	})

	// Debounce: gom nhiều event trong 300ms thành 1 lần rebuild.
	var pending *time.Timer
	coalesce := func() {
		if pending != nil {
			pending.Stop()
		}
		pending = time.AfterFunc(300*time.Millisecond, func() {
			logger.Println("Code thay đổi → check JS + build + restart + check UI")
			// 1. Check JS syntax trước. Nếu fail → giữ binary cũ, cảnh báo.
			if err := runCheckJS(); err != nil {
				logger.Printf("JS syntax error — KHÔNG restart, giữ binary cũ: %v", err)
				return
			}
			// 2. Stop binary cũ.
			stopBinary(binCmd, logger)
			// 3. Build binary mới.
			if err := buildTo(binary, sourceDir); err != nil {
				logger.Printf("Build FAIL — giữ binary cũ: %v", err)
				// Khởi lại binary cũ.
				binCmd, _ = startBinary(binary, logger)
				return
			}
			// 4. Start binary mới.
			binCmd, err = startBinary(binary, logger)
			if err != nil {
				logger.Printf("Start binary mới thất bại: %v", err)
				return
			}
			// 5. Đợi server ready.
			time.Sleep(2 * time.Second)
			// 6. UI smoke test nếu lightpanda có.
			if lightpandaAvailable() {
				if err := runCheckUI(); err != nil {
					logger.Printf("UI smoke test FAILED: %v", err)
				}
			}
		})
	}

	// Loop: drain watcher cho đến khi ctx done.
	for {
		select {
		case <-ctx.Done():
			logger.Println("Context done — thoát watch mode.")
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			// Bỏ qua file rác: test, sum, db
			if isIgnored(ev.Name) {
				continue
			}
			logger.Printf("event: %s %s", ev.Op, ev.Name)
			coalesce()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			logger.Printf("watcher error: %v", err)
		}
	}
}

// isIgnored bỏ qua file test/temporary không cần rebuild.
var ignoreRe = regexp.MustCompile(`(_test\.go|\.sum$|\.db$)`)

func isIgnored(path string) bool {
	base := filepath.Base(path)
	return ignoreRe.MatchString(base)
}

// buildTo chạy go build từ sourceDir ra binary. Khác với rebuildBinary ở chỗ cho
// phép override đường dẫn output (dùng cho watch mode truyền binary qua tham số).
func buildTo(binary, sourceDir string) error {
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/zcloudd/")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/usr/local/go/bin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}
	return nil
}

// startBinary chạy binary với các flag mặc định cho watch mode.
func startBinary(binary string, logger *log.Logger) (*exec.Cmd, error) {
	cmd := exec.Command(binary, "--port", "8080", "--dev")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = ProjectRoot()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start binary: %w", err)
	}
	logger.Printf("zcloudd chạy (PID %d)", cmd.Process.Pid)
	return cmd, nil
}

// stopBinary tắt binary và đợi nó exit.
func stopBinary(cmd *exec.Cmd, logger *log.Logger) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil {
		return // đã exit
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	logger.Printf("zcloudd đã dừng (PID %d)", cmd.Process.Pid)
}
