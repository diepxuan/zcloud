// Package tui — screens.go: render từng màn.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5F5FFF")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5F5FFF"))

	itemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DDDDDD"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555")).
			Bold(true)

	composerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#222222")).
			Padding(0, 1)

	composerLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#5F5FFF"))

	ownStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7FD3FF"))
)

// renderScreen dispatch theo màn hiện tại.
func renderScreen(m Model) string {
	var b strings.Builder

	// Header
	b.WriteString(titleStyle.Render(fmt.Sprintf(" zcloud TUI — %s ", m.screen)))
	b.WriteString("\n\n")

	switch m.screen {
	case screenAccounts:
		renderAccounts(&b, m)
	case screenConvs:
		renderConvs(&b, m)
	case screenChat:
		renderChat(&b, m)
	}

	// Footer
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(errStyle.Render("Lỗi: " + m.err.Error()))
		b.WriteString("\n")
	}
	if m.loading {
		b.WriteString(dimStyle.Render("(đang tải...)"))
		b.WriteString("\n")
	}
	b.WriteString(footerStyle.Render(footerHint(m.screen)))
	b.WriteString("\n")

	return b.String()
}

// footerHint trả keymap hint cho màn hiện tại.
func footerHint(s screen) string {
	switch s {
	case screenAccounts:
		return "↑/↓ chọn  •  / filter  •  Enter xác nhận  •  q/ESC thoát"
	case screenConvs:
		return "↑/↓ chọn  •  / filter  •  Enter mở chat  •  q/ESC thoát"
	case screenChat:
		return "gõ text + Enter gửi  •  Backspace xoá  •  ESC thoát"
	}
	return ""
}

// renderAccounts vẽ màn 1 (có filter).
func renderAccounts(b *strings.Builder, m Model) {
	filterDisp := m.filterAcc
	if filterDisp == " " {
		filterDisp = ""
	}
	if m.filteringAcc {
		b.WriteString(composerLabelStyle.Render("/"))
		b.WriteString(composerStyle.Render(m.filterAcc + "█"))
		b.WriteString("\n\n")
	}
	rows := filteredAccounts(m.accounts, m.filterAcc)
	if len(rows) == 0 {
		if len(m.accounts) == 0 {
			b.WriteString(dimStyle.Render("(chưa có account nào — đăng nhập QR ở web trước)"))
		} else {
			b.WriteString(dimStyle.Render("(không có account khớp filter)"))
		}
		b.WriteString("\n")
		return
	}
	// Duyệt rows đã filter, NHƯNG highlight theo ID khớp selectedAccount gốc.
	for _, r := range rows {
		isSel := r.ID == m.accounts[m.selectedAccount].ID
		line := fmt.Sprintf("  %s  %s  %s",
			r.DisplayName,
			dimStyle.Render("• "+r.Subtitle),
			dimStyle.Render("["+r.UserID+"]"))
		if isSel {
			b.WriteString(selectedStyle.Render("> " + fmt.Sprintf("%s  %s",
				r.DisplayName, dimStyle.Render("• "+r.Subtitle))))
		} else {
			b.WriteString(itemStyle.Render(line))
		}
		b.WriteString("\n")
	}
}

// renderConvs vẽ màn 2 (có filter).
func renderConvs(b *strings.Builder, m Model) {
	filterDisp := m.filterConv
	if filterDisp == " " {
		filterDisp = ""
	}
	if m.filteringConv {
		b.WriteString(composerLabelStyle.Render("/"))
		b.WriteString(composerStyle.Render(m.filterConv + "█"))
		b.WriteString("\n\n")
	}
	rows := filteredConvs(m.convs, m.filterConv)
	if len(rows) == 0 {
		accName := ""
		if m.selectedAccount >= 0 && m.selectedAccount < len(m.accounts) {
			accName = m.accounts[m.selectedAccount].DisplayName
		}
		if len(m.convs) == 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("(account %s chưa có thread nào)", accName)))
		} else {
			b.WriteString(dimStyle.Render("(không có thread khớp filter)"))
		}
		b.WriteString("\n")
		return
	}
	// Highlight theo ID conv hiện tại (nếu có).
	currentConvID := ""
	if m.selectedConv >= 0 && m.selectedConv < len(m.convs) {
		currentConvID = m.convs[m.selectedConv].ID
	}
	for _, c := range rows {
		isSel := c.ID == currentConvID
		preview := dimStyle.Render("  " + truncate(c.LastMsg, 50))
		prefix := "  "
		if isSel {
			prefix = "> "
			b.WriteString(selectedStyle.Render(prefix + fmt.Sprintf("%s", c.Name)))
		} else {
			b.WriteString(itemStyle.Render(prefix + c.Name))
		}
		b.WriteString("\n")
		b.WriteString(preview)
		b.WriteString("\n")
	}
}

// renderChat vẽ màn 3 (chat).
func renderChat(b *strings.Builder, m Model) {
	// Header thread
	convName := ""
	if m.selectedConv >= 0 && m.selectedConv < len(m.convs) {
		convName = m.convs[m.selectedConv].Name
	}
	b.WriteString(dimStyle.Render("Thread: " + convName + "  •  " + m.selectedConvID))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")

	// Messages
	if len(m.messages) == 0 {
		b.WriteString(dimStyle.Render("(chưa có tin nhắn)"))
		b.WriteString("\n")
	} else {
		// Hiển thị tối đa ~20 tin cuối (để còn chỗ cho composer).
		start := 0
		if len(m.messages) > 20 {
			start = len(m.messages) - 20
			b.WriteString(dimStyle.Render(fmt.Sprintf("... (%d tin cũ hơn, dùng ↑/↓ xem sau) ...", start)))
			b.WriteString("\n")
		}
		for i := start; i < len(m.messages); i++ {
			msg := m.messages[i]
			renderOneMessage(b, msg)
		}
	}

	// Composer (đáy màn hình).
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")
	b.WriteString(composerLabelStyle.Render("Soạn tin nhắn: "))
	cursor := "█"
	if m.composer.sending {
		cursor = " …"
	}
	b.WriteString(composerStyle.Render(m.composer.text + cursor))
	b.WriteString("\n")
}

// renderOneMessage format 1 dòng tin nhắn theo loại.
func renderOneMessage(b *strings.Builder, msg MessageRow) {
	prefix := fmt.Sprintf("[%s] %s", msg.Timestamp, msg.FromName)
	content := msg.Content
	switch msg.MsgType {
	case 2:
		content = "📷 Ảnh"
	case 3:
		content = "🟡 Sticker"
	case 4:
		content = "📎 File"
	case 5:
		content = "🎤 Voice"
	case 6:
		content = "🔗 " + content
	case 7:
		content = "🎬 Video"
	}
	// Tin của mình → màu xanh nhạt.
	if strings.HasPrefix(msg.FromName, "→") || strings.Contains(msg.FromName, "Bạn") {
		b.WriteString(ownStyle.Render(prefix + ": "))
	} else {
		b.WriteString(itemStyle.Render(prefix + ": "))
	}
	b.WriteString(content)
	b.WriteString("\n")
}

// truncate cắt ngắn string + "…" nếu quá dài.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n > 1 {
		return s[:n-1] + "…"
	}
	return s[:n]
}
