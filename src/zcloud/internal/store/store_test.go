package store

import (
	"path/filepath"
	"testing"
)

// newTestStore tạo SQLite store trong thư mục tạm, an toàn cho test song song.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	mediaPath := filepath.Join(dir, "media")
	s, err := NewSQLite(dbPath, mediaPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Tạo account mặc định để thỏa FK từ messages/account_id.
	if err := s.CreateAccount("acc-1", "Test Account", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return s
}

// newTestStoreFull mở SQLite ở dir chỉ định (dùng cho test ở package khác).
func newTestStoreFull(dir string) (*Store, error) {
	return NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
}

func TestSaveMessageDedupe(t *testing.T) {
	s := newTestStore(t)
	m := &Message{
		ID: "msg-1", AccountID: "acc-1", ConvID: "c-1",
		FromID: "u-1", Content: "hello", MsgType: 1, Timestamp: 1000,
	}
	if err := s.SaveMessage(m); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	// Lưu lại cùng (id, account_id) phải được ignore, không lỗi.
	m.Content = "hello edited"
	if err := s.SaveMessage(m); err != nil {
		t.Fatalf("save dup: %v", err)
	}
	msgs, err := s.GetMessages("acc-1", "c-1", 1<<62, 50)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after dedupe, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Fatalf("content overwritten: got %q", msgs[0].Content)
	}
}

func TestSaveMessageDistinctAccounts(t *testing.T) {
	s := newTestStore(t)
	for _, acc := range []string{"a", "b"} {
		if err := s.CreateAccount(acc, acc, 1); err != nil {
			t.Fatalf("CreateAccount %s: %v", acc, err)
		}
		m := &Message{ID: "msg-1", AccountID: acc, ConvID: "c-1", Content: "hi-" + acc, Timestamp: 1}
		if err := s.SaveMessage(m); err != nil {
			t.Fatalf("save %s: %v", acc, err)
		}
	}
	for _, acc := range []string{"a", "b"} {
		msgs, _ := s.GetMessages(acc, "c-1", 1<<62, 10)
		if len(msgs) != 1 {
			t.Fatalf("acc %s: expected 1, got %d", acc, len(msgs))
		}
	}
}
