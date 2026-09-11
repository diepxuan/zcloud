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

	// Footer (keymap hint)
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(errStyle.Render("Lỗi: " + m.err.Error()))
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
		return "↑/↓ chọn  •  Enter xác nhận  •  ESC thoát"
	case screenConvs:
		return "↑/↓ chọn  •  / filter  •  Enter mở chat  •  ESC thoát"
	case screenChat:
		return "↑/↓ cuộn  •  gõ tin + Enter gửi  •  ESC thoát"
	}
	return ""
}

// renderAccounts vẽ màn 1.
func renderAccounts(b *strings.Builder, m Model) {
	if len(m.accounts) == 0 {
		b.WriteString(dimStyle.Render("(chưa có account nào — đăng nhập QR ở web trước)"))
		b.WriteString("\n")
		return
	}
	for i, a := range m.accounts {
		line := fmt.Sprintf("  %s  %s  %s", a.ID, a.DisplayName, dimStyle.Render(a.Subtitle))
		if i == m.selectedAccount {
			b.WriteString(selectedStyle.Render("> " + fmt.Sprintf("%s  %s", a.DisplayName, a.Subtitle)))
		} else {
			b.WriteString(itemStyle.Render(line))
		}
		b.WriteString("\n")
	}
}

// renderConvs vẽ màn 2.
func renderConvs(b *strings.Builder, m Model) {
	if len(m.convs) == 0 {
		accName := ""
		if m.selectedAccount >= 0 && m.selectedAccount < len(m.accounts) {
			accName = m.accounts[m.selectedAccount].DisplayName
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("(account %s chưa có thread nào)", accName)))
		b.WriteString("\n")
		return
	}
	for i, c := range m.convs {
		if i == m.selectedConv {
			b.WriteString(selectedStyle.Render("> " + fmt.Sprintf("%s  %s", c.Name, dimStyle.Render(c.LastMsg))))
		} else {
			b.WriteString(itemStyle.Render(fmt.Sprintf("  %s  %s", c.Name, dimStyle.Render(c.LastMsg))))
		}
		b.WriteString("\n")
	}
}

// renderChat vẽ màn 3 (T18.1 chỉ khung; messages sẽ load ở T18.2).
func renderChat(b *strings.Builder, m Model) {
	convName := ""
	if m.selectedConv >= 0 && m.selectedConv < len(m.convs) {
		convName = m.convs[m.selectedConv].Name
	}
	b.WriteString(dimStyle.Render("Thread: " + convName))
	b.WriteString("\n\n")

	if len(m.messages) == 0 {
		b.WriteString(dimStyle.Render("(T18.1 chưa load messages — sẽ làm ở T18.2)"))
		b.WriteString("\n")
	} else {
		for _, msg := range m.messages {
			b.WriteString(itemStyle.Render(fmt.Sprintf("[%s] %s: %s",
				msg.Timestamp, msg.FromName, msg.Content)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("─────────────────────────────────────"))
	b.WriteString("\n")
	b.WriteString(itemStyle.Render("> " + m.composer.text))
}
