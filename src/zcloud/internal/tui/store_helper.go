package tui

import "github.com/diepxuan/zcloud/internal/store"

// Store là wrapper xung quanh *store.Store để TUI không phụ thuộc trực
// tiếp vào struct Postgres. Khi T18.2 wire thật, các method LoadAccounts/
// LoadConversations/LoadMessages sẽ gọi store.Store.
type Store struct {
	// db *store.Store // T18.2 sẽ uncomment.
}

// AccountRow là item hiển thị trong màn 1.
type AccountRow struct {
	ID          string // Account.ID
	DisplayName string
	Subtitle    string // vd "ws: ok  •  last seen 2m"
}

// ConversationRow là item hiển thị trong màn 2.
type ConversationRow struct {
	ID       string
	Name     string
	LastMsg  string // preview
	Unread   int
	ConvType int // 0: cá nhân, 1: nhóm, 2: OA
}

// MessageRow là item hiển thị trong màn 3.
type MessageRow struct {
	ID        string
	FromID    string
	FromName  string
	Content   string
	Timestamp string // human-readable, vd "16:32"
	MsgType   int
}

// dummyAccounts trả data mẫu cho T18.1 (để test layout + keymap trước
// khi wire Postgres ở T18.2).
func dummyAccounts() []AccountRow {
	return []AccountRow{
		{ID: "acc_559609701372941728", DisplayName: "Sep Duc", Subtitle: "ws: ok  •  last seen 2m"},
		{ID: "acc_test_002", DisplayName: "Phạm Hồng", Subtitle: "ws: stale  •  last seen 1h"},
		{ID: "acc_test_003", DisplayName: "Test account", Subtitle: "ws: down  •  last seen 3d"},
	}
}

// dummyConvs trả data mẫu cho T18.1.
func dummyConvs() []ConversationRow {
	return []ConversationRow{
		{ID: "4866700441106275565", Name: "Duc Tran", LastMsg: "hello sep!"},
		{ID: "3270622634384316423", Name: "Cam Tu", LastMsg: "📷 Ảnh"},
		{ID: "916680095426814684", Name: "Phandaitrang", LastMsg: "ok ảnh"},
	}
}

// unused: tránh warning khi T18.2 chưa wire store. Biên dịch check rằng
// store.Account tương thích với AccountRow (cùng ID field).
var _ store.Account = store.Account{}
