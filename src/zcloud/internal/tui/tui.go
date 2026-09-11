// Package tui là terminal UI cho zcloud — wizard 3 màn tuần tự
// (chọn account → chọn thread → chat) dùng charmbracelet/bubbletea.
//
// Workflow yêu cầu bởi Sếp 11/09/2026:
//   - ./zcloudd tui  hoặc  ./zcloudd  (no-arg)  mở TUI
//   - Màn 1: chọn account (↑/↓, Enter)
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

// Run khởi động TUI. Trả về error nếu terminal không hỗ trợ TUI
// (không có TTY) — bubbletea tự lo việc restore alternate buffer
// khi Run() trả về.
func Run() error {
	// Không có TTY (vd chạy trong pipe / ssh không có PTY) → in hướng dẫn
	// thay vì để bubbletea crash.
	if !isTTY() {
		fmt.Fprintln(os.Stderr, "zcloud TUI cần terminal thật (TTY).")
		fmt.Fprintln(os.Stderr, "Chạy tương tác: ./zcloudd tui")
		fmt.Fprintln(os.Stderr, "Hoặc dùng web UI: http://zcloud.diepxuan.corp:8080")
		return fmt.Errorf("no TTY")
	}

	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// newModel tạo Model khởi đầu — T18.1 hardcode 1-2 account mẫu, T18.2
// sẽ load từ Postgres.
func newModel() Model {
	return Model{
		store:           &Store{},
		screen:          screenAccounts,
		selectedAccount: 0,
		selectedConv:    0,
		accounts:        dummyAccounts(),
		convs:           nil, // màn 2 load khi user chọn account
		messages:        nil, // màn 3 load khi user chọn conv
	}
}

// isTTY kiểm tra stdin/stdout có phải terminal thật không.
// Dùng os.Stat + mode char device.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
