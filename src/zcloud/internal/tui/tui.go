// Package tui là terminal UI cho zcloud — hiện tại chỉ là placeholder/mockup.
// Mục tiêu cuối cùng: dashboard TUI để xem accounts, conversations, recent
// messages, restart service, v.v. — tương tác trực tiếp từ terminal mà
// không cần mở browser.
//
// Trạng thái: MOCKUP. Hàm Run chỉ in banner + giải thích các phím tắt dự
// kiến, rồi thoát. Sẽ được thay thế bằng Bubble Tea / tview khi triển khai.
package tui

import (
	"fmt"
)

// Run khởi động TUI. Hiện tại là mockup — in banner và các phím dự kiến rồi
// thoát ngay (return nil). Khi implement sẽ block cho đến khi user nhấn q/Ctrl-C.
func Run() error {
	fmt.Println(`╔════════════════════════════════════════════╗
║   zcloud TUI — MOCKUP (chưa triển khai)   ║
╚════════════════════════════════════════════╝

  Phím tắt dự kiến (sẽ có trong bản thực):

    ↑/↓         di chuyển giữa accounts / conversations
    Tab         chuyển panel (accounts ↔ messages)
    Enter       mở conversation / xem chi tiết
    n           soạn tin nhắn mới
    r           reload dữ liệu
    s           mở panel service (start/stop/watch)
    /           tìm kiếm
    ?           trợ giúp
    q           thoát

  Dữ liệu hiện không có TUI runtime — bản mockup chỉ để xác nhận CLI shape.

  Tạm thời dùng:
    zcloudd           chạy HTTP server
    zcloudd serv      quản lý systemd service
    zcloudd serv watch  watch + auto-rebuild khi sửa code

  Xem docs/tasks.md §5 cho kế hoạch triển khai TUI.
`)
	return nil
}
