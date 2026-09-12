// Package servcmd implements the `zcloudd serv` subcommand for managing
// the zcloud daemon as a systemd service. It replaces the legacy
// scripts/zcloud.sh and scripts/zcloudd.sh bash wrappers.
//
// Subcommands:
//
//	zcloudd serv                  chạy HTTP server (foreground, không watch) — used by systemd
//	zcloudd serv start            systemctl start zcloud (install unit if missing)
//	zcloudd serv stop             systemctl stop zcloud
//	zcloudd serv restart          systemctl restart zcloud (also rebuilds binary)
//	zcloudd serv status           show systemd status + listening port
//	zcloudd serv logs [-f]        journalctl -u zcloud
//	zcloudd serv watch            foreground watch mode (fsnotify + auto-rebuild)
//	zcloudd serv install          (re)write /etc/systemd/system/zcloud.service + daemon-reload
//	zcloudd serv version          in version (semver + git short hash)
package servcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diepxuan/zcloud/internal/server"
	"github.com/diepxuan/zcloud/internal"
)

// ServiceName là tên systemd unit.
const ServiceName = "zcloud"

// ServiceFile là đường dẫn tới unit file.
const ServiceFile = "/etc/systemd/system/zcloud.service"

// BinaryPath trả về đường dẫn tới binary hiện tại.
func BinaryPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "/data/zcloud/zcloudd"
}

// ProjectRoot trả về thư mục project (parent của binary nếu binary ở /data/zcloud).
func ProjectRoot() string {
	bin := BinaryPath()
	dir := bin
	for i := 0; i < 4; i++ {
		dir = parent(dir)
		if dir == "" {
			break
		}
		if _, err := os.Stat(dir + "/src/zcloud"); err == nil {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
}

func parent(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return ""
	}
	return p[:i]
}

// Run dispatch `zcloudd serv <subcommand>`. Nếu subcommand rỗng → chạy HTTP server
// (giống `zcloudd` không-arg). Systemd ExecStart=zcloudd serv dùng nhánh này.
func Run(args []string) error {
	if len(args) == 0 {
		return server.Run()
	}
	switch args[0] {
	case "start":
		return start()
	case "stop":
		return stop()
	case "restart":
		return restart()
	case "status":
		return status()
	case "logs":
		return logs(args[1:])
	case "watch":
		return watchForeground()
	case "version", "--version", "-v":
		fmt.Printf("zcloudd %s\n", internal.Full())
		return nil
	case "install":
		watch := false
		for _, a := range args[1:] {
			switch a {
			case "--watch", "-w":
				watch = true
			case "-h", "--help":
				fmt.Println("Usage: zcloudd serv install [--watch]")
				fmt.Println("  --watch, -w  ExecStart=zcloudd serv watch (auto-rebuild + restart)")
				return nil
			default:
				return fmt.Errorf("install: flag không hợp lệ: %s", a)
			}
		}
		return install(watch)
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		printHelp()
		return fmt.Errorf("serv: subcommand không hợp lệ: %s", args[0])
	}
}

func printHelp() {
	fmt.Println(`zcloudd serv — quản lý service zcloud daemon

Usage:
  zcloudd serv                  chạy HTTP server (foreground, không watch)
  zcloudd serv start            systemctl start zcloud (cài unit nếu thiếu)
  zcloudd serv stop             systemctl stop zcloud
  zcloudd serv restart          systemctl restart + rebuild binary
  zcloudd serv status           in trạng thái systemd + port listener
  zcloudd serv logs [-f]        xem journalctl -u zcloud
  zcloudd serv watch            foreground watch + auto-rebuild (dev)
  zcloudd serv install          (re)generate systemd unit + daemon-reload
  zcloudd serv version           in version (semver + git short hash)
  zcloudd serv help             in trợ giúp này`)
}

// systemctl là helper chạy systemctl và trả về (stdout, exitCode).
func systemctl(subargs ...string) (string, int) {
	cmd := exec.Command("systemctl", subargs...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return string(out), code
}

// systemctlRun chạy systemctl và in output. Trả về error nếu exit != 0.
func systemctlRun(label string, subargs ...string) error {
	out, code := systemctl(subargs...)
	if out != "" {
		fmt.Print(out)
	}
	if code != 0 {
		return fmt.Errorf("%s thất bại (exit=%d)", label, code)
	}
	return nil
}

// install tạo/cập nhật /etc/systemd/system/zcloud.service rồi daemon-reload.
// Mặc định ExecStart=zcloudd serv (foreground); truyền watch=true để dùng `serv watch`.
func install(watch bool) error {
	bin := BinaryPath()
	root := ProjectRoot()
	unit := fmt.Sprintf(`[Unit]
Description=ZCloud Daemon — Zalo Cloud Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
ExecStart=%s serv%s
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal
Environment=PATH=/usr/local/go/bin:/usr/bin:/bin
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
`, root, bin, watchArg(watch))

	// Đọc file hiện tại để so sánh — không ghi đè nếu không đổi.
	existing, err := os.ReadFile(ServiceFile)
	if err == nil && string(existing) == unit {
		fmt.Printf("[zcloud] %s đã đúng phiên bản, skip.\n", ServiceFile)
	} else {
		tmp := ServiceFile + ".tmp"
		if err := os.WriteFile(tmp, []byte(unit), 0644); err != nil {
			return fmt.Errorf("ghi %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, ServiceFile); err != nil {
			return fmt.Errorf("rename %s → %s: %w", tmp, ServiceFile, err)
		}
		fmt.Printf("[zcloud] Đã cập nhật %s\n", ServiceFile)
	}
	if err := systemctlRun("daemon-reload", "daemon-reload"); err != nil {
		return err
	}
	if out, code := systemctl("is-enabled", ServiceName); code != 0 || !strings.Contains(out, "enabled") {
		_ = systemctlRun("enable "+ServiceName, "enable", ServiceName)
	}
	return nil
}

func start() error {
	if _, err := os.Stat(ServiceFile); os.IsNotExist(err) {
		fmt.Println("[zcloud] Service file chưa tồn tại — cài đặt...")
		if err := install(false); err != nil {
			return err
		}
	}
	if err := systemctlRun("enable "+ServiceName, "enable", ServiceName); err != nil {
		return err
	}
	if err := systemctlRun("start "+ServiceName, "start", ServiceName); err != nil {
		return err
	}
	fmt.Printf("[zcloud] %s đã start — http://localhost:8080\n", ServiceName)
	return nil
}

func stop() error {
	out, code := systemctl("is-active", ServiceName)
	if code != 0 || !strings.Contains(out, "active") {
		fmt.Printf("[zcloud] %s không chạy\n", ServiceName)
		return nil
	}
	return systemctlRun("stop "+ServiceName, "stop", ServiceName)
}

func restart() error {
	// Rebuild trước khi restart để code mới được load.
	if err := buildBinary(BinaryPath(), filepath.Join(ProjectRoot(), "src", "zcloud")); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	if _, err := os.Stat(ServiceFile); os.IsNotExist(err) {
		if err := install(false); err != nil {
			return err
		}
	}
	if err := systemctlRun("restart "+ServiceName, "restart", ServiceName); err != nil {
		return err
	}
	fmt.Printf("[zcloud] %s đã restart\n", ServiceName)
	return nil
}

func status() error {
	fmt.Println()
	fmt.Println("=== zcloud service ===")
	out, _ := systemctl("is-active", ServiceName)
	if strings.Contains(out, "active") {
		fmt.Println("  Trạng thái: ACTIVE")
	} else {
		fmt.Println("  Trạng thái: INACTIVE")
	}
	systemctl("status", ServiceName, "--no-pager", "-l")
	fmt.Println()
	fmt.Println("=== Port 8080 ===")
	portOut, _ := exec.Command("sh", "-c", "ss -tlnp 2>/dev/null | grep 8080 || echo '  Không có process nào'").CombinedOutput()
	fmt.Print(string(portOut))
	return nil
}

func logs(args []string) error {
	subargs := []string{"-u", ServiceName, "--no-pager", "-o", "cat"}
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			subargs = []string{"-u", ServiceName, "-f", "--no-pager", "-o", "cat"}
		}
	}
	cmd := exec.Command("journalctl", subargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func watchForeground() error {
	return Watch(context.Background(), BinaryPath(), ProjectRoot()+"/src/zcloud")
}
// watchArg trả về " watch" nếu watch=true, "" nếu false — dùng cho template ExecStart.
func watchArg(w bool) string {
	if w {
		return " watch"
	}
	return ""
}
