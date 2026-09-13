// Package tui — store_helper.go: wire TUI vào Postgres.
//
// TUI không phụ thuộc trực tiếp vào Postgres driver — dùng
// `*store.Store` (interface đã wrap pgx). Tất cả load/save data đều
// qua store layer đã có sẵn (xem internal/store/queries.go).
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/diepxuan/zcloud/internal/config"
	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// Store là wrapper xung quanh *store.Store + dependency TUI cần
// (core.Client để gửi tin từ màn 3).
type Store struct {
	DB *store.Store
	// clients cache: accountID → *core.Client, dùng cho màn 3 gửi tin.
	clients map[string]*core.Client
}

// Open mở Postgres + parse config. Trả error nếu DSN rỗng hoặc DB fail.
func Open() (*Store, error) {
	cfg := config.Parse()
	dsn := cfg.PostgresDSN()
	if dsn == "" {
		return nil, fmt.Errorf("Postgres DSN rỗng — kiểm tra %s", config.DefaultConfigPath())
	}
	db, err := store.NewPostgres(dsn, cfg.MediaDirPath(),
		cfg.Database.Postgres.MaxOpenConns, cfg.Database.Postgres.MaxIdleConns)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return &Store{DB: db, clients: make(map[string]*core.Client)}, nil
}

// Close đóng DB khi TUI thoát.
func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// AccountRow là item hiển thị trong màn 1.
type AccountRow struct {
	ID          string
	DisplayName string
	Subtitle    string
	UserID      string
	noAccent    string
}

// ConversationRow là item hiển thị trong màn 2.
type ConversationRow struct {
	ID       string
	Name     string
	LastMsg  string
	Unread   int
	ConvType int
	noAccent string
}

// MessageRow là item hiển thị trong màn 3.
type MessageRow struct {
	ID        string
	FromID    string
	FromName  string
	Content   string
	Timestamp string
	MsgType   int
}

// LoadAccounts đọc tất cả account Zalo User từ Postgres, kèm subtitle
// trạng thái WS (ok / down) cho mỗi account.
func (s *Store) LoadAccounts() ([]AccountRow, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("store not opened")
	}
	accs, err := s.DB.ListAccounts(1, false)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	if len(accs) == 0 {
		return nil, nil
	}
	activeSessions, _ := s.DB.GetActiveSessionsForAccounts()
	rows := make([]AccountRow, 0, len(accs))
	for _, a := range accs {
		rows = append(rows, AccountRow{
			ID:          a.ID,
			DisplayName: a.DisplayName,
			Subtitle:    accountSubtitle(a, activeSessions[a.ID]),
			UserID:      a.UserID,
			noAccent:    stripDiacritics(a.DisplayName),
		})
	}
	return rows, nil
}

func accountSubtitle(a store.Account, hasActive bool) string {
	if !hasActive {
		return "ws: down  •  cần đăng nhập lại"
	}
	ago := time.Since(a.UpdatedAt).Truncate(time.Minute)
	if ago < time.Minute {
		return "ws: ok  •  vừa xong"
	}
	return fmt.Sprintf("ws: ok  •  %s trước", ago)
}

// LoadConversations đọc threads của 1 account.
func (s *Store) LoadConversations(accountID string) ([]ConversationRow, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("store not opened")
	}
	convs, err := s.DB.GetConversations(accountID)
	if err != nil {
		return nil, fmt.Errorf("get conversations: %w", err)
	}
	rows := make([]ConversationRow, 0, len(convs))
	for _, c := range convs {
		preview := c.LastMsgContent
		if preview == "" {
			preview = "(chưa có tin)"
		}
		rows = append(rows, ConversationRow{
			ID:       c.ID,
			Name:     c.Name,
			LastMsg:  preview,
			Unread:   c.Unread,
			ConvType: c.ConvType,
			noAccent: stripDiacritics(c.Name),
		})
	}
	return rows, nil
}

// LoadMessages đọc 50 tin gần nhất của conv (cũ → mới để hiển thị tuần tự).
func (s *Store) LoadMessages(accountID, convID string) ([]MessageRow, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("store not opened")
	}
	const maxInt64 = int64(9223372036854775807)
	msgs, err := s.DB.GetMessages(accountID, convID, maxInt64, 50)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	rows := make([]MessageRow, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, MessageRow{
			ID:        m.ID,
			FromID:    m.FromID,
			FromName:  m.FromName,
			Content:   m.Content,
			Timestamp: formatTimestamp(m.Timestamp),
			MsgType:   m.MsgType,
		})
	}
	return rows, nil
}

// SendMessageText gửi text thuần tới convID qua core.Client.
func (s *Store) SendMessageText(accountID, convID, text string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("store not opened")
	}
	if text == "" {
		return nil
	}
	client, err := s.clientFor(accountID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = client.SendMessage(ctx, convID, text, core.MsgTypeText)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// clientFor trả *core.Client cho accountID, cache trong map.
func (s *Store) clientFor(accountID string) (*core.Client, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("store not opened")
	}
	if c, ok := s.clients[accountID]; ok && c != nil && c.Session != nil {
		return c, nil
	}
	sessRec, err := s.DB.GetActiveSession(accountID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if sessRec == nil {
		return nil, fmt.Errorf("account chưa có active session — đăng nhập lại trên web")
	}
	sess, err := sessionRecordToCore(sessRec)
	if err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	c := core.NewClient(sess)
	s.clients[accountID] = c
	return c, nil
}

func sessionRecordToCore(sr *store.Session) (*core.Session, error) {
	if sr == nil {
		return nil, fmt.Errorf("nil session record")
	}
	s := &core.Session{
		SecretKey:  sr.SecretKey,
		IMEI:       sr.IMEI,
		UserID:     sr.UserID,
		UserAgent:  sr.UserAgent,
		Language:   sr.Language,
		APIType:    sr.APIType,
		APIVersion: sr.APIVersion,
		Transport:  sr.Transport,
	}
	if sr.Cookies != "" {
		if err := json.Unmarshal([]byte(sr.Cookies), &s.Cookies); err != nil {
			return nil, fmt.Errorf("parse cookies: %w", err)
		}
	}
	if sr.WSURLs != "" {
		_ = json.Unmarshal([]byte(sr.WSURLs), &s.WSURLs)
	}
	if sr.ServiceMap != "" {
		_ = json.Unmarshal([]byte(sr.ServiceMap), &s.ServiceMap)
	}
	if !sr.ExpiresAt.IsZero() {
		s.ExpiresAt = sr.ExpiresAt
	}
	return s, nil
}

// stripDiacritics bỏ dấu tiếng Việt để filter không phụ thuộc cách gõ.
func stripDiacritics(s string) string {
	repl := map[rune]rune{
		'ầ': 'a', 'ấ': 'a', 'ậ': 'a', 'ẩ': 'a', 'ẫ': 'a',
		'ằ': 'a', 'ắ': 'a', 'ặ': 'a', 'ẳ': 'a', 'ẵ': 'a',
		'ề': 'e', 'ế': 'e', 'ệ': 'e', 'ể': 'e', 'ẽ': 'e',
		'ồ': 'o', 'ố': 'o', 'ộ': 'o', 'ổ': 'o', 'ỗ': 'o',
		'ờ': 'o', 'ớ': 'o', 'ợ': 'o', 'ở': 'o', 'ỡ': 'o',
		'ừ': 'u', 'ứ': 'u', 'ự': 'u', 'ử': 'u', 'ữ': 'u',
		'ì': 'i', 'í': 'i', 'ị': 'i', 'ỉ': 'i', 'ĩ': 'i',
		'ỳ': 'y', 'ý': 'y', 'ỵ': 'y', 'ỷ': 'y', 'ỹ': 'y',
		'đ': 'd', 'Đ': 'D',
	}
	runes := []rune(s)
	for i, r := range runes {
		if rep, ok := repl[r]; ok {
			runes[i] = rep
		}
	}
	return string(runes)
}

// formatTimestamp convert unix-ms → "HH:MM" hoặc "DD/MM HH:MM" nếu khác ngày.
func formatTimestamp(ms int64) string {
	if ms == 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("02/01 15:04")
	}
	return t.Format("02/01/2006 15:04")
}

// logger trả *log.Logger dùng cho TUI.
func logger() *log.Logger {
	return log.New(log.Writer(), "[zcloud-tui] ", log.LstdFlags|log.Lshortfile)
}

// ====================================
// Tea commands (async wrapper cho bubbletea)
// ====================================

type loadAccountsMsg struct {
	rows []AccountRow
	err  error
}

func (s *Store) loadAccountsCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := s.LoadAccounts()
		return loadAccountsMsg{rows: rows, err: err}
	}
}

type loadConvsMsg struct {
	rows []ConversationRow
	err  error
}

func (s *Store) loadConvsCmd(accountID string) tea.Cmd {
	return func() tea.Msg {
		rows, err := s.LoadConversations(accountID)
		return loadConvsMsg{rows: rows, err: err}
	}
}

type loadMessagesMsg struct {
	rows []MessageRow
	err  error
}

func (s *Store) loadMessagesCmd(accountID, convID string) tea.Cmd {
	return func() tea.Msg {
		rows, err := s.LoadMessages(accountID, convID)
		return loadMessagesMsg{rows: rows, err: err}
	}
}

type sendMessageMsg struct {
	err  error
	text string
}

func (s *Store) sendMessageCmd(accountID, convID, text string) tea.Cmd {
	return func() tea.Msg {
		err := s.SendMessageText(accountID, convID, text)
		return sendMessageMsg{err: err, text: text}
	}
}

func errCmd(err error) tea.Cmd {
	return func() tea.Msg { return loadAccountsMsg{err: err} }
}
