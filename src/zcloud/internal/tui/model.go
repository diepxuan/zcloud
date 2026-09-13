// Package tui — model.go: bubbletea Model cho wizard 3 màn.
package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
	selectedConvID    string
	messages          []MessageRow
	composer          composerState
	lastSeenConvAt    int64 // UpdatedAt của conv lần cuối (Unix ms) — để check realtime
	chatRefreshActive bool  // true khi đang bật tick refresh ở màn 3
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
		m.filteringConv = false
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
		// Sau khi load, bật tick refresh 3s/lần + fetch conv UpdatedAt lần đầu.
		if m.screen == screenChat && !m.chatRefreshActive {
			m.chatRefreshActive = true
			return m, tea.Batch(
				m.store.getConvUpdatedAtCmd(m.selectedAccountID, m.selectedConvID),
				TickCmd(3*time.Second),
			)
		}
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
		// Refresh messages ngay để pick up SaveMessage từ WS broadcast.
		return m, m.store.loadMessagesCmd(m.selectedAccountID, m.selectedConvID)

	case ConvUpdatedAtMsg:
		// Tick check: conv UpdatedAt đã đổi → reload messages.
		// Nếu không đổi → skip (optimization).
		if msg.err != nil {
			// Lỗi DB im lặng — không hiện footer để tránh flicker.
			return m, TickCmd(3 * time.Second)
		}
		if m.screen == screenChat && msg.updatedAtMs > m.lastSeenConvAt {
			m.lastSeenConvAt = msg.updatedAtMs
			return m, m.store.loadMessagesCmd(m.selectedAccountID, m.selectedConvID)
		}
		// Tiếp tục tick.
		if m.chatRefreshActive {
			return m, TickCmd(3 * time.Second)
		}
		return m, nil

	case tickMsg:
		// Mỗi 3s — check conv.UpdatedAt để quyết định reload.
		if m.screen != screenChat || !m.chatRefreshActive {
			return m, nil
		}
		return m, m.store.getConvUpdatedAtCmd(m.selectedAccountID, m.selectedConvID)

	case tea.KeyMsg:
		mm := m; return mm.handleKey(msg)
	}
	return m, nil
}

// handleKey xử lý KeyMsg theo màn hiện tại.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ESC / Ctrl+C luôn thoát (kể cả khi đang loading).
	if key == "esc" || key == "ctrl+c" {
		m.chatRefreshActive = false // stop tick
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
func (m Model) handleKeyAccounts(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtering := m.filteringAcc
	switch {
	case filtering && key == "enter":
		filtered := filteredAccounts(m.accounts, m.filterAcc)
		if len(filtered) > 0 {
			acc := filtered[0]
			m.selectedAccount = indexOfAccount(m.accounts, acc.ID)
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
		// Backspace xoá 1 rune (không phải 1 byte) — đủ cho emoji + dấu VN.
		_, size := utf8.DecodeLastRuneInString(m.filterAcc)
		if size > 0 {
			m.filterAcc = m.filterAcc[:len(m.filterAcc)-size]
		}
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
	switch {
	case key == "q":
		m.quitting = true
		return m, tea.Quit
	case key == "/" || (len(key) > 1 && key[0] == '/'):
		// "/" để bật filter. Nếu user gõ nhanh "/tran" 1 phát → match
		// HasPrefix('/') + lấy phần sau làm filter text.
		m.filteringAcc = true
		if len(key) > 1 {
			m.filterAcc = key[1:]
		} else {
			m.filterAcc = ""
		}
		return m, nil
	case key == "up" || key == "k":
		m.moveUpAccFiltered(m.accounts, &m.selectedAccount)
	case key == "down" || key == "j":
		m.moveDownAccFiltered(m.accounts, &m.selectedAccount)
	case key == "enter":
		filtered := filteredAccounts(m.accounts, m.filterAcc)
		if m.selectedAccount >= 0 && m.selectedAccount < len(filtered) {
			acc := filtered[m.selectedAccount]
			m.selectedAccountID = acc.ID
			m.screen = screenConvs
			m.selectedConv = 0
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
			m.filteringConv = false
			m.filterConv = ""
			m.err = nil
			if m.store != nil {
				m.loading = true
				return m, m.store.loadMessagesCmd(m.selectedAccountID, conv.ID)
			}
			return m, nil
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
		_, size := utf8.DecodeLastRuneInString(m.filterConv)
		if size > 0 {
			m.filterConv = m.filterConv[:len(m.filterConv)-size]
		}
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
	switch {
	case key == "q":
		m.quitting = true
		return m, tea.Quit
	case key == "/" || (len(key) > 1 && key[0] == '/'):
		m.filteringConv = true
		if len(key) > 1 {
			m.filterConv = key[1:]
		} else {
			m.filterConv = ""
		}
		return m, nil
	case key == "up" || key == "k":
		m.moveUpConvFiltered(m.convs, &m.selectedConv)
	case key == "down" || key == "j":
		m.moveDownConvFiltered(m.convs, &m.selectedConv)
	case key == "enter":
		filtered := filteredConvs(m.convs, m.filterConv)
		if m.selectedConv >= 0 && m.selectedConv < len(filtered) {
			conv := filtered[m.selectedConv]
			m.selectedConvID = conv.ID
			m.screen = screenChat
			m.composer = composerState{}
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
		// Strip 1 rune (không phải 1 byte) để Backspace trên emoji (4 bytes)
		// hoặc dấu combining bị corrupt không xảy ra.
		_, size := utf8.DecodeLastRuneInString(m.composer.text)
		if size > 0 {
			m.composer.text = m.composer.text[:len(m.composer.text)-size]
		}
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
		m.composer.text += string(msg.Runes)
	}
	return m, nil
}

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

// View trả string để bubbletea in ra terminal.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return renderScreen(m)
}
