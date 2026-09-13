// Package tui — tui.go: entry point + lifecycle.
//
// Workflow yêu cầu bởi Sếp 11/09/2026:
//   - ./zcloudd  hoặc  ./zcloudd tui  mở TUI
//   - Màn 1: chọn account (↑/↓, /, Enter)
//   - Màn 2: chọn thread (/ filter, Enter)
//   - Màn 3: xem + gửi tin
//   - ESC ở bất kỳ màn nào cũng thoát hẳn (exit 0)
//
// Xem chi tiết tại docs/tasks/18-tui.md.
package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run khởi động TUI: mở Postgres, tạo Model, chạy bubbletea program.
// Trả về error nếu terminal không hỗ trợ TUI (không có TTY) hoặc store fail.
func Run() error {
	if !isTTY() {
		fmt.Fprintln(os.Stderr, "zcloud TUI cần terminal thật (TTY).")
		fmt.Fprintln(os.Stderr, "Chạy tương tác: ./zcloudd tui")
		fmt.Fprintln(os.Stderr, "Hoặc dùng web UI: http://zcloud.diepxuan.corp:8080")
		return fmt.Errorf("no TTY")
	}

	st, err := Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcloud-tui] không mở được store: %v\n", err)
		fmt.Fprintln(os.Stderr, "Kiểm tra Postgres config trong ~/.config/ductn/zcloud.yml")
		return err
	}
	defer st.Close()

	m := newModel()
	m.store = st
	// storeErr không set ở đây (Open() đã fail trên rồi).

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// isTTY kiểm tra stdout có phải terminal thật không.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
