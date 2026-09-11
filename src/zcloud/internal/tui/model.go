package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// screen là enum các màn trong wizard 3 bước:
//   screenAccounts → chọn tài khoản
//   screenConvs    → chọn thread (conv) trong account đang chọn
//   screenChat     → xem + gửi tin trong conv đang chọn
type screen int

const (
	screenAccounts screen = iota
	screenConvs
	screenChat
)

func (s screen) String() string {
	switch s {
	case screenAccounts:
		return "Chọn tài khoản"
	case screenConvs:
		return "Chọn thread"
	case screenChat:
		return "Chat"
	}
	return ""
}

// Model là state chính của TUI. Bubbletea yêu cầu Model implement
// tea.Model: Init(), Update(msg), View().
type Model struct {
	store *Store // wrapper đọc data từ Postgres; nil = chưa load

	screen   screen // màn hiện tại
	width    int    // chiều rộng terminal
	height   int    // chiều cao terminal
	quitting bool   // true khi user nhấn ESC/Ctrl+C
	err      error  // lỗi load data (hiện ở footer nếu có)

	// Màn 1: accounts
	accounts []AccountRow

	// Màn 2: conversations (của account đang chọn)
	selectedAccount int                  // index trong accounts
	convs           []ConversationRow

	// Màn 3: chat (của conv đang chọn)
	selectedConv int // index trong convs
	messages     []MessageRow
	composer     composerState
}

// composerState giữ text người dùng đang soạn (T18.3 sẽ dùng để gửi).
// T18.1 chỉ cần struct tồn tại để View không panic.
type composerState struct {
	text string
}

// Init chạy 1 lần khi chương trình bắt đầu. Hiện không cần async —
// T18.2 sẽ thêm load data tại đây.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update nhận message từ bubbletea và trả về Model mới + Cmd (nếu có).
// Phím tắt (theo docs/tasks/18-tui.md):
//   - ESC / Ctrl+C : thoát TUI (tea.Quit) — bất kỳ màn nào
//   - q            : thoát TUI (chỉ màn 1, 2; màn 3 không có để khỏi gõ nhầm)
//   - ↑/↓ / j/k   : di chuyển trong list
//   - Enter        : chọn item hiện tại (chuyển màn)
//   - /            : vào filter (T18.2)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "q":
			// q chỉ thoát ở màn 1, 2 (chọn); màn 3 để dành cho chat.
			if m.screen != screenChat {
				m.quitting = true
				return m, tea.Quit
			}

		case "up", "k":
			m.moveUp()

		case "down", "j":
			m.moveDown()

		case "enter":
			m.confirm()
		}
	}
	return m, nil
}

// moveUp di chuyển cursor lên 1 dòng trong list của màn hiện tại.
func (m *Model) moveUp() {
	switch m.screen {
	case screenAccounts:
		if m.selectedAccount > 0 {
			m.selectedAccount--
		}
	case screenConvs:
		if m.selectedConv > 0 {
			m.selectedConv--
		}
	}
}

// moveDown di chuyển cursor xuống 1 dòng.
func (m *Model) moveDown() {
	switch m.screen {
	case screenAccounts:
		if m.selectedAccount < len(m.accounts)-1 {
			m.selectedAccount++
		}
	case screenConvs:
		if m.selectedConv < len(m.convs)-1 {
			m.selectedConv++
		}
	}
}

// confirm xử lý Enter:
//   - Màn 1 → chuyển sang màn 2 (load convs).
//   - Màn 2 → chuyển sang màn 3 (load messages).
//   - Màn 3 → gửi tin (T18.3 sẽ wire).
func (m *Model) confirm() {
	switch m.screen {
	case screenAccounts:
		if m.selectedAccount >= 0 && m.selectedAccount < len(m.accounts) {
			m.screen = screenConvs
			m.selectedConv = 0
			// T18.2 sẽ load convs ở đây.
		}
	case screenConvs:
		if m.selectedConv >= 0 && m.selectedConv < len(m.convs) {
			m.screen = screenChat
			// T18.2 sẽ load messages ở đây.
		}
	case screenChat:
		// T18.3: nếu composer.text != "" thì gửi.
	}
}

// View trả về string để bubbletea in ra terminal. View() chạy mỗi khi
// state đổi hoặc terminal resize — phải pure (không side effect).
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return renderScreen(m)
}

// helper nhỏ cho debug — dùng khi cần in nhanh trạng thái.
func (m Model) debugString() string {
	return fmt.Sprintf("screen=%s selA=%d/%d selC=%d/%d msgs=%d",
		m.screen, m.selectedAccount, len(m.accounts),
		m.selectedConv, len(m.convs), len(m.messages))
}
