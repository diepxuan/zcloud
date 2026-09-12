// zcloudd — Zalo Cloud Service.
//
// CLI shape:
//
//	zcloudd              chạy HTTP server (foreground, không watch)
//	zcloudd serv         quản lý systemd service (start/stop/restart/status/...)
//	zcloudd serv watch   watch + auto-rebuild + restart server
//	zcloudd tui          terminal UI (mockup, sắp triển khai)
package main

import (
	"fmt"
	"os"

	"github.com/diepxuan/zcloud/internal"
	"github.com/diepxuan/zcloud/internal/servcmd"
	"github.com/diepxuan/zcloud/internal/server"
	"github.com/diepxuan/zcloud/internal/tui"
)

// init in banner sớm để user biết binary đã load. In cả version để user
// xác nhận binary đang chạy (vd khi debug restart).
func init() {
	fmt.Println("zcloudd — Zalo Cloud Service")
	fmt.Println(internal.Full())
	fmt.Println()
}

func main() {
	if len(os.Args) < 2 {
		// Không có subcommand → chạy server trực tiếp (backward compat).
		if err := server.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[zcloud] server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "serv":
		// Subcommand quản lý service: serv [start|stop|restart|status|logs|watch|install|help]
		// Mặc định (không có sub-subcommand) → watch mode.
		if err := servcmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "[zcloud] serv error: %v\n", err)
			os.Exit(1)
		}
	case "tui":
		// Terminal UI (mockup).
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[zcloud] tui error: %v\n", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Printf("zcloudd %s\n", internal.Full())
		return
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  zcloudd              chạy HTTP server")
		fmt.Fprintln(os.Stderr, "  zcloudd version       in version")
		fmt.Fprintln(os.Stderr, "  zcloudd serv         quản lý systemd service (xem `zcloudd serv help`)")
		fmt.Fprintln(os.Stderr, "  zcloudd serv watch   watch + auto-rebuild")
		fmt.Fprintln(os.Stderr, "  zcloudd tui          terminal UI (mockup)")
		os.Exit(1)
	}
}
