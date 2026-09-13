// Package tui — model.go: bubbletea Model cho wizard 3 màn.
package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/text/unicode/norm"
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

// composerState giữ text người dùng đang soạn + cờ sending
// để chặn Enter kép khi gửi chưa xong.
type composerState struct {
	text    string
	sending bool
}

// Model là state chính của TUI.
type Model struct {
	store    *Store // wrapper đọc data từ Postgres
	storeErr error  // lỗi Open store lúc khởi động

	screen    screen
	width     int
	height    int
	quitting  bool
	err       error // lỗi load runtime (hiện ở footer)
	loading   bool  // true khi đang chờ load async
	lastInput string // text composer cuối cùng (để echo "[sent]" khi gửi OK)

	// Màn 1: accounts
	accounts        []AccountRow
	selectedAccount int
	filterAcc       string // filter text cho màn 1
	filteringAcc    bool   // true khi đang trong filter mode

	// Màn 2: conversations
	selectedAccountID string // ID account đang chọn (giữ qua màn 2, 3)
	convs             []ConversationRow
	selectedConv      int
	filterConv        string
	filteringConv     bool

	// Màn 3: chat
	selectedConvID string
	messages       []MessageRow
	composer       composerState
}

// newModel khởi tạo Model mặc định (chưa load data — đợi Init).
func newModel() Model {
	return Model{
		screen:          screenAccounts,
		selectedAccount: 0,
		selectedConv:    0,
	}
}

// Init chạy 1 lần lúc khởi động — return tea.Cmd để bubbletea execute
// (async load để không block UI).
func (m Model) Init() tea.Cmd {
	if m.store == nil {
		return errCmd(fmt.Errorf("store chưa mở"))
	}
	m.loading = true
	return m.store.loadAccountsCmd()
}

// Update nhận message từ bubbletea và trả về Model mới + Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case loadAccountsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.accounts = msg.rows
		m.err = nil
		if m.selectedAccount >= len(m.accounts) {
			m.selectedAccount = 0
		}
		return m, nil

	case loadConvsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.convs = msg.rows
		m.selectedConv = 0
		m.filterConv = ""
		m.err = nil
		return m, nil

	case loadMessagesMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.messages = msg.rows
		m.err = nil
		return m, nil

	case sendMessageMsg:
		m.composer.sending = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Echo optimistic: thêm row "[đã gửi] <text>" rồi refresh.
		m.messages = append(m.messages, MessageRow{
			FromName:  "→ Bạn",
			Content:   msg.text + "  [đã gửi]",
			Timestamp: "now",
			MsgType:   1,
		})
		m.lastInput = msg.text
		m.composer.text = ""
		m.err = nil
		// Refresh messages để lấy tin thật từ DB (qua WS broadcast SaveMessage
		// hoặc qua REST fallback nếu WS miss).
		return m, m.store.loadMessagesCmd(m.selectedAccountID, m.selectedConvID)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey xử lý KeyMsg theo màn hiện tại.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ESC / Ctrl+C luôn thoát (kể cả khi đang loading).
	if key == "esc" || key == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	// Đang loading → khoá input khác (tránh race với async load).
	if m.loading {
		return m, nil
	}

	switch m.screen {
	case screenAccounts:
		return m.handleKeyAccounts(key, msg)
	case screenConvs:
		return m.handleKeyConvs(key, msg)
	case screenChat:
		return m.handleKeyChat(key, msg)
	}
	return m, nil
}

// handleKeyAccounts xử lý phím ở màn 1 (chọn account + filter).
// Khi filter active: Enter = confirm + move; Up/Down = navigate filtered;
// Backspace = xoá filter char; Esc = clear filter (lần 2 = thoát).
// Khi không filter: / = bật filter, các phím khác navigate.
func (m Model) handleKeyAccounts(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtering := m.filteringAcc
	switch {
	case filtering && key == "enter":
		// Enter trong filter mode = chọn row đầu tiên khớp filter vào màn 2.
		filtered := filteredAccounts(m.accounts, m.filterAcc)
		if len(filtered) > 0 {
			acc := filtered[0]
			m.selectedAccount = indexOfAccount(m.accounts, acc.ID)
			m.selectedAccountID = acc.ID
			m.screen = screenConvs
			m.selectedConv = 0
			m.err = nil
			if m.store != nil {
				m.loading = true
				return m, m.store.loadConvsCmd(acc.ID)
			}
		}
		return m, nil
	case filtering && (key == "up" || key == "k"):
		m.moveUpAccFiltered(filteredAccounts(m.accounts, m.filterAcc), &m.selectedAccount)
		return m, nil
	case filtering && (key == "down" || key == "j"):
		m.moveDownAccFiltered(filteredAccounts(m.accounts, m.filterAcc), &m.selectedAccount)
		return m, nil
	case filtering && key == "backspace":
		if m.filterAcc == "" {
			return m, nil
		}
		m.filterAcc = popLastGrapheme(m.filterAcc)
		return m, nil
	case filtering && key == "esc":
		// Esc trong filter = clear filter (không thoát). Nếu filter đã rỗng
		// thì handleKey đã return từ trước rồi (early ESC check).
		m.filterAcc = ""
		return m, nil
	case filtering && key == "q":
		// q trong filter = quit (giống non-filter).
		m.quitting = true
		return m, tea.Quit
	case filtering:
		// Ký tự khác (Rune) → thêm vào filter.
		if len(msg.Runes) > 0 {
			m.filterAcc += string(msg.Runes)
		}
		return m, nil
	}

	// Không filter.
	switch key {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "/":
		m.filteringAcc = true
		m.filterAcc = ""
		return m, nil
	case "up", "k":
		m.moveUpAccFiltered(m.accounts, &m.selectedAccount)
	case "down", "j":
		m.moveDownAccFiltered(m.accounts, &m.selectedAccount)
	case "enter":
		filtered := filteredAccounts(m.accounts, m.filterAcc)
		if m.selectedAccount >= 0 && m.selectedAccount < len(filtered) {
			acc := filtered[m.selectedAccount]
			m.selectedAccountID = acc.ID
			m.screen = screenConvs
			m.selectedConv = 0
			m.filteringAcc = false
			m.filterAcc = ""
			m.err = nil
			if m.store != nil {
				m.loading = true
				return m, m.store.loadConvsCmd(acc.ID)
			}
			return m, nil
		}
	}
	return m, nil
}

// handleKeyConvs xử lý phím ở màn 2 (chọn conv + filter).
// Cùng pattern với handleKeyAccounts.
func (m Model) handleKeyConvs(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtering := m.filteringConv
	switch {
	case filtering && key == "enter":
		filtered := filteredConvs(m.convs, m.filterConv)
		if len(filtered) > 0 {
			conv := filtered[0]
			m.selectedConv = indexOfConv(m.convs, conv.ID)
			m.selectedConvID = conv.ID
			m.screen = screenChat
			m.composer = composerState{}
			m.err = nil
			if m.store != nil {
				m.loading = true
				return m, m.store.loadMessagesCmd(m.selectedAccountID, conv.ID)
			}
		}
		return m, nil
	case filtering && (key == "up" || key == "k"):
		m.moveUpConvFiltered(filteredConvs(m.convs, m.filterConv), &m.selectedConv)
		return m, nil
	case filtering && (key == "down" || key == "j"):
		m.moveDownConvFiltered(filteredConvs(m.convs, m.filterConv), &m.selectedConv)
		return m, nil
	case filtering && key == "backspace":
		if m.filterConv == "" {
			return m, nil
		}
		m.filterConv = popLastGrapheme(m.filterConv)
		return m, nil
	case filtering && key == "esc":
		m.filterConv = ""
		return m, nil
	case filtering && key == "q":
		m.quitting = true
		return m, tea.Quit
	case filtering:
		if len(msg.Runes) > 0 {
			m.filterConv += string(msg.Runes)
		}
		return m, nil
	}

	// Không filter.
	switch key {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "/":
		m.filteringConv = true
		m.filterConv = ""
		return m, nil
	case "up", "k":
		m.moveUpConvFiltered(m.convs, &m.selectedConv)
	case "down", "j":
		m.moveDownConvFiltered(m.convs, &m.selectedConv)
	case "enter":
		filtered := filteredConvs(m.convs, m.filterConv)
		if m.selectedConv >= 0 && m.selectedConv < len(filtered) {
			conv := filtered[m.selectedConv]
			m.selectedConvID = conv.ID
			m.screen = screenChat
			m.composer = composerState{}
			m.filteringConv = false
			m.filterConv = ""
			m.err = nil
			if m.store != nil {
				m.loading = true
				return m, m.store.loadMessagesCmd(m.selectedAccountID, conv.ID)
			}
			return m, nil
		}
	}
	return m, nil
}

// handleKeyChat xử lý phím ở màn 3 (xem + gửi tin).
// Mọi phím (trừ ESC/Ctrl+C) đều thêm vào composer text trừ khi là
// phím đặc biệt.
func (m Model) handleKeyChat(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Phím đặc biệt trước.
	switch key {
	case "backspace":
		if m.composer.text == "" {
			return m, nil
		}
		// Strip 1 grapheme cluster (bao gồm combining marks) cuối cùng.
		// Backspace trên "ầ" (NFD = 3 runes) chỉ xoá 1 cluster,
		// không strip 1 byte (sẽ corrupt UTF-8 multi-byte).
		m.composer.text = popLastGrapheme(m.composer.text)
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.composer.text)
		if text == "" || m.composer.sending {
			return m, nil
		}
		m.composer.sending = true
		m.err = nil
		return m, m.store.sendMessageCmd(m.selectedAccountID, m.selectedConvID, text)
	}

	// Nhận text vào composer.
	if len(msg.Runes) > 0 {
		m.composer.text = appendGrapheme(m.composer.text, msg.Runes)
	}
	return m, nil
}

// appendFilterAcc thêm ký tự vào filterAcc (màn 1).
func (m *Model) appendFilterAcc(key string) tea.Model {
	if key == "backspace" {
		if len(m.filterAcc) > 0 {
			m.filterAcc = m.filterAcc[:len(m.filterAcc)-1]
		}
	} else if len(key) == 1 {
		m.filterAcc += key
	}
	return m
}

// appendFilterConv thêm ký tự vào filterConv (màn 2).
func (m *Model) appendFilterConv(key string) tea.Model {
	if key == "backspace" {
		if len(m.filterConv) > 0 {
			m.filterConv = m.filterConv[:len(m.filterConv)-1]
		}
	} else if len(key) == 1 {
		m.filterConv += key
	}
	return m
}

// moveUpFiltered/moveDownFiltered: di chuyển cursor trong list đã filter.
// Trả về index trong list GỐC (không phải filtered) để Update không phá index.
// Vì list gốc không đổi khi filter thay đổi, ta ánh xạ index đơn giản:
// chọn row thứ N trong filtered → tìm row đó trong accounts[].
// moveUpAccFiltered/moveDownAccFiltered: di chuyển cursor trong list accounts.
// Nếu filter rỗng → logic đơn giản sel +/- (tối ưu + khớp test cũ).
// Nếu filter có → ánh xạ index qua ID.
func (m *Model) moveUpAccFiltered(filtered []AccountRow, sel *int) {
	if m.filterAcc == "" {
		if *sel > 0 {
			*sel--
		}
		return
	}
	if len(filtered) == 0 || *sel <= 0 {
		return
	}
	for i, r := range filtered {
		if r.ID == m.accounts[*sel].ID && i > 0 {
			*sel = indexOfAccount(m.accounts, filtered[i-1].ID)
			return
		}
	}
	if *sel > 0 {
		*sel--
	}
}

func (m *Model) moveDownAccFiltered(filtered []AccountRow, sel *int) {
	if m.filterAcc == "" {
		if *sel < len(m.accounts)-1 {
			*sel++
		}
		return
	}
	if len(filtered) == 0 {
		return
	}
	for i, r := range filtered {
		if r.ID == m.accounts[*sel].ID && i < len(filtered)-1 {
			*sel = indexOfAccount(m.accounts, filtered[i+1].ID)
			return
		}
	}
	if *sel < len(m.accounts)-1 {
		*sel++
	}
}

// moveUpConvFiltered/moveDownConvFiltered: tương tự cho conversations.
func (m *Model) moveUpConvFiltered(filtered []ConversationRow, sel *int) {
	if len(filtered) == 0 || *sel <= 0 {
		return
	}
	for i, r := range filtered {
		if r.ID == m.convs[*sel].ID && i > 0 {
			*sel = indexOfConv(m.convs, filtered[i-1].ID)
			return
		}
	}
	if *sel > 0 {
		*sel--
	}
}

func (m *Model) moveDownConvFiltered(filtered []ConversationRow, sel *int) {
	if len(filtered) == 0 {
		return
	}
	for i, r := range filtered {
		if r.ID == m.convs[*sel].ID && i < len(filtered)-1 {
			*sel = indexOfConv(m.convs, filtered[i+1].ID)
			return
		}
	}
	if *sel < len(m.convs)-1 {
		*sel++
	}
}

func indexOfAccount(rows []AccountRow, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return 0
}

func indexOfConv(rows []ConversationRow, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return 0
}

// filteredAccounts trả accounts khớp filter (case + accent insensitive).
func filteredAccounts(rows []AccountRow, filter string) []AccountRow {
	if filter == "" || filter == " " {
		return rows
	}
	// Filter text cũng bỏ dấu để so sánh với noAccent precomputed.
	f := stripDiacritics(strings.ToLower(filter))
	out := make([]AccountRow, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.noAccent), f) {
			out = append(out, r)
		} else if r.UserID == filter {
			out = append(out, r)
		}
	}
	return out
}

// filteredConvs tương tự cho conversations.
func filteredConvs(rows []ConversationRow, filter string) []ConversationRow {
	if filter == "" || filter == " " {
		return rows
	}
	f := stripDiacritics(strings.ToLower(filter))
	out := make([]ConversationRow, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.noAccent), f) {
			out = append(out, r)
		} else if r.ID == filter {
			out = append(out, r)
		}
	}
	return out
}

// appendGrapheme thêm 1 grapheme cluster (base char + combining marks)
// vào text. Normalize thành NFC để:
//   - NFD input ("a" + "̂" + "̀" = 3 runes) trở thành 1 cluster "ầ"
//   - Hiển thị trong terminal ổn định
//   - Backspace xoá đúng 1 grapheme visible
func appendGrapheme(text string, runes []rune) string {
	if len(runes) == 0 {
		return text
	}
	cluster := string(runes)
	// Nếu cluster đã là NFC form (1 rune) → append trực tiếp.
	// Nếu là NFD (base + combining marks) → normalize → NFC.
	normalized := norm.NFC.String(cluster)
	return text + normalized
}

// popLastGrapheme xoá 1 grapheme cluster (NFC) cuối cùng của text.
// Trả về text mới + số bytes đã xoá.
func popLastGrapheme(text string) string {
	if text == "" {
		return text
	}
	// Normalize cả text thành NFC để cluster boundary rõ ràng.
	nfc := norm.NFC.String(text)
	// Tìm cluster cuối: iterate rune ngược, nếu gặp combining mark
	// → tiếp tục lùi cho đến khi gặp base char (Mark nonspacing = Mn).
	runes := []rune(nfc)
	if len(runes) == 0 {
		return text
	}
	// Pop combining marks trước (cuối → base).
	lastBase := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		if !unicode.Is(unicode.Mn, runes[i]) {
			lastBase = i + 1
			break
		}
	}
	if lastBase == 0 {
		// Toàn combining marks — giữ lại 1.
		lastBase = 1
	}
	return nfc[:lastBase-1]
}

// View trả string để bubbletea in ra terminal.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return renderScreen(m)
}
