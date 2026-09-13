//go:build testdb

package store

import (
	"testing"
	"time"
)

func TestUpsertZaloAccount(t *testing.T) {
	s := newTestStore(t)
	id, err := s.UpsertZaloAccount("2291602426651082808", "Sep", "avatar1.png", "0901")
	if err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if id != "za_2291602426651082808" {
		t.Fatalf("expected id za_..., got %s", id)
	}
	id2, err := s.UpsertZaloAccount("2291602426651082808", "Tran Ngoc Duc", "avatar2.png", "")
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if id2 != id {
		t.Fatalf("id changed after upsert: %s vs %s", id, id2)
	}
	za, err := s.GetZaloAccountByUserID("2291602426651082808")
	if err != nil || za == nil {
		t.Fatalf("GetZaloAccountByUserID: %v", err)
	}
	if za.DisplayName != "Tran Ngoc Duc" {
		t.Fatalf("display_name not updated: got %q", za.DisplayName)
	}
	if za.Avatar != "avatar2.png" {
		t.Fatalf("avatar not updated: got %q", za.Avatar)
	}
	if za.Phone != "0901" {
		t.Fatalf("phone bi overwrite vi upsert truyen rong: got %q", za.Phone)
	}
}

func TestFindOrCreateAccountByZaloAccountID_New(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("111", "Alice", "", "")
	accID, err := s.FindOrCreateAccountByZaloAccountID(zaID, "111", "Alice", "")
	if err != nil {
		t.Fatalf("find/create: %v", err)
	}
	if accID != "acc_111" {
		t.Fatalf("expected acc_111, got %s", accID)
	}
	acc, _ := s.GetAccount(accID)
	if acc == nil {
		t.Fatalf("account not found")
	}
	if acc.ZaloAccountID != zaID {
		t.Fatalf("FK not set: got %q", acc.ZaloAccountID)
	}
	if acc.UserID != "111" {
		t.Fatalf("user_id not set: got %q", acc.UserID)
	}
}

func TestFindOrCreateAccountByZaloAccountID_Reuse(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("222", "Bob", "", "")
	accID1, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "222", "Bob", "")
	accID2, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "222", "Bob Doi Ten", "new.png")
	if accID1 != accID2 {
		t.Fatalf("reuse failed: %s vs %s", accID1, accID2)
	}
	acc, _ := s.GetAccount(accID1)
	if acc.DisplayName != "Bob Doi Ten" {
		t.Fatalf("display_name not refreshed: got %q", acc.DisplayName)
	}
	if acc.Avatar != "new.png" {
		t.Fatalf("avatar not refreshed: got %q", acc.Avatar)
	}
}

func TestLogoutAccount_PreservesMessages(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("333", "Carol", "", "")
	accID, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "333", "Carol", "")
	if err := s.SaveSession(&Session{ID: "sess1", AccountID: accID, UserID: "333", IsActive: 1}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	_ = s.SaveMessage(&Message{ID: "m1", AccountID: accID, ConvID: "c1", Content: "hi", Timestamp: 1})
	if err := s.SaveConversation(&Conversation{ID: "c1", AccountID: accID, Name: "Test"}); err != nil {
		t.Fatalf("upsert conv: %v", err)
	}
	if err := s.LogoutAccount(accID); err != nil {
		t.Fatalf("logout: %v", err)
	}
	sessions, _ := s.GetActiveSessionsForAccounts()
	if sessions[accID] {
		t.Fatalf("session still active after logout")
	}
	msgs, _ := s.GetMessages(accID, "c1", 1<<62, 10)
	if len(msgs) != 1 {
		t.Fatalf("messages bi xoa sau logout: got %d", len(msgs))
	}
	convs, _ := s.GetConversations(accID)
	if len(convs) != 1 || convs[0].ID != "c1" {
		t.Fatalf("conversations bi xoa: %+v", convs)
	}
	acc, _ := s.GetAccount(accID)
	if acc == nil {
		t.Fatalf("account row bi xoa")
	}
	za, _ := s.GetZaloAccount(zaID)
	if za == nil {
		t.Fatalf("zalo_account row bi xoa")
	}
}

func TestDeleteAccountByZaloAccountID_Cascade(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("444", "Dave", "", "")
	accID, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "444", "Dave", "")
	_ = s.SaveSession(&Session{ID: "sess1", AccountID: accID, UserID: "444", IsActive: 1})
	_ = s.SaveMessage(&Message{ID: "m1", AccountID: accID, ConvID: "c1", Content: "hi", Timestamp: 1})
	_ = s.SaveConversation(&Conversation{ID: "c1", AccountID: accID, Name: "Test"})
	if err := s.DeleteAccountByZaloAccountID(zaID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	za, _ := s.GetZaloAccount(zaID)
	if za != nil {
		t.Fatalf("zalo_account con")
	}
	acc, _ := s.GetAccount(accID)
	if acc != nil {
		t.Fatalf("account con")
	}
	msgs, _ := s.GetMessages(accID, "c1", 1<<62, 10)
	if len(msgs) != 0 {
		t.Fatalf("messages con: %d", len(msgs))
	}
}

func TestLoginFlow_ReusesAccount(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("555", "Eve", "", "")
	accID1, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "555", "Eve", "")
	accID2, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "555", "Eve", "")
	if accID1 != accID2 {
		t.Fatalf("reuse failed: %s vs %s", accID1, accID2)
	}
	_ = s.SaveSession(&Session{ID: "sess-pc", AccountID: accID1, UserID: "555", Transport: "pc", IsActive: 1})
	_ = s.SaveSession(&Session{ID: "sess-web", AccountID: accID1, UserID: "555", Transport: "web", IsActive: 1})
	sessions, _ := s.GetActiveSessionsForAccounts()
	if !sessions[accID1] {
		t.Fatalf("account khong co active session")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE account_id = $1`, accID1).Scan(&n)
	if n != 2 {
		t.Fatalf("expected 2 sessions (multi-session), got %d", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s := newTestStore(t)
	zaID, _ := s.UpsertZaloAccount("666", "Frank", "", "")
	accID, _ := s.FindOrCreateAccountByZaloAccountID(zaID, "666", "Frank", "")
	_, _ = s.db.Exec(`INSERT INTO sessions (id, account_id, user_id, cookies, secret_key, imei, created_at, expires_at, is_active) VALUES ('old', $1, '666', '', '', '', NOW() - INTERVAL '60 days', NOW() + INTERVAL '1 day', 0)`, accID)
	_, _ = s.db.Exec(`INSERT INTO sessions (id, account_id, user_id, cookies, secret_key, imei, created_at, expires_at, is_active) VALUES ('new', $1, '666', '', '', '', NOW(), NOW() + INTERVAL '1 day', 0)`, accID)
	_, _ = s.db.Exec(`INSERT INTO sessions (id, account_id, user_id, cookies, secret_key, imei, created_at, expires_at, is_active) VALUES ('active', $1, '666', '', '', '', NOW() - INTERVAL '60 days', NOW() + INTERVAL '1 day', 1)`, accID)
	n, err := s.DeleteExpiredSessions(30 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted (chi 'old' thoa dieu kien), got %d", n)
	}
	var remaining int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE account_id = $1`, accID).Scan(&remaining)
	if remaining != 2 {
		t.Fatalf("expected 2 remaining (new + active), got %d", remaining)
	}
}
