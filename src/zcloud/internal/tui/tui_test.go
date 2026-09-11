package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestESC luôn thoát bất kể đang ở màn nào.
func TestESC_QuitsFromAnyScreen(t *testing.T) {
	screens := []screen{screenAccounts, screenConvs, screenChat}
	for _, s := range screens {
		t.Run(s.String(), func(t *testing.T) {
			m := newModel()
			m.screen = s

			// Bấm ESC → Update trả về tea.Quit
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			mm := updated.(Model)
			if !mm.quitting {
				t.Errorf("expected quitting=true sau ESC, got false")
			}
			if cmd == nil {
				t.Errorf("expected tea.Quit cmd, got nil")
			}
		})
	}
}

// TestCtrlC tương tự ESC — cũng thoát.
func TestCtrlC_QuitsFromAnyScreen(t *testing.T) {
	for _, s := range []screen{screenAccounts, screenConvs, screenChat} {
		m := newModel()
		m.screen = s
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		mm := updated.(Model)
		if !mm.quitting {
			t.Errorf("expected quitting=true sau Ctrl+C, got false")
		}
		if cmd == nil {
			t.Errorf("expected tea.Quit cmd, got nil")
		}
	}
}

// TestQ chỉ thoát ở màn 1, 2 — không thoát ở màn 3 (chat) để khỏi gõ nhầm.
func TestQ_QuitsExceptChat(t *testing.T) {
	cases := []struct {
		screen   screen
		wantQuit bool
	}{
		{screenAccounts, true},
		{screenConvs, true},
		{screenChat, false}, // chat: q là ký tự text
	}
	for _, tc := range cases {
		m := newModel()
		m.screen = tc.screen
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		mm := updated.(Model)
		if mm.quitting != tc.wantQuit {
			t.Errorf("screen=%s: want quitting=%v, got %v",
				tc.screen, tc.wantQuit, mm.quitting)
		}
	}
}

// TestArrowKeys di chuyển cursor đúng.
func TestArrowKeys_MoveCursor(t *testing.T) {
	m := newModel()
	m.accounts = []AccountRow{
		{DisplayName: "A"},
		{DisplayName: "B"},
		{DisplayName: "C"},
	}
	m.selectedAccount = 0

	// Down 2 lần → cursor = 2
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.selectedAccount != 2 {
		t.Errorf("sau 2 lần Down: want sel=2, got %d", m.selectedAccount)
	}

	// Down quá giới hạn → không vượt
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.selectedAccount != 2 {
		t.Errorf("Down quá giới hạn: want sel=2, got %d", m.selectedAccount)
	}

	// Up 1 lần → cursor = 1
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.selectedAccount != 1 {
		t.Errorf("sau Up: want sel=1, got %d", m.selectedAccount)
	}

	// Up quá giới hạn → không xuống âm
	for i := 0; i < 5; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(Model)
	}
	if m.selectedAccount != 0 {
		t.Errorf("Up quá giới hạn: want sel=0, got %d", m.selectedAccount)
	}
}

// TestEnter_SwitchScreen: Enter ở màn 1 → màn 2, Enter ở màn 2 → màn 3.
func TestEnter_SwitchScreen(t *testing.T) {
	m := newModel()
	m.accounts = []AccountRow{
		{ID: "a1", DisplayName: "Sep"},
		{ID: "a2", DisplayName: "Anh"},
	}
	m.selectedAccount = 1
	m.convs = []ConversationRow{
		{Name: "Conv1"},
		{Name: "Conv2"},
		{Name: "Conv3"},
	}

	// Enter ở màn 1 → màn 2
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.screen != screenConvs {
		t.Errorf("sau Enter màn 1: want screenConvs, got %s", m.screen)
	}

	m.selectedConv = 0

	// Enter ở màn 2 → màn 3
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.screen != screenChat {
		t.Errorf("sau Enter màn 2: want screenChat, got %s", m.screen)
	}
}

// TestWindowSize cập nhật width/height.
func TestWindowSize(t *testing.T) {
	m := newModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := updated.(Model)
	if mm.width != 120 || mm.height != 40 {
		t.Errorf("got w=%d h=%d, want 120x40", mm.width, mm.height)
	}
}
