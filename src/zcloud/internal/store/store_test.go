//go:build testdb

package store

import (
	"database/sql"
	"testing"
	"time"
)

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

func TestMediaJobLifecycle(t *testing.T) {
	s := newTestStore(t)
	job := &MediaJob{
		ID: "job-1", AccountID: "acc-1", ConvID: "c-1", MsgID: "m-1",
		FileName: "a.jpg", FileExt: "jpg", SourceURL: "https://example.com/a.jpg",
	}
	if err := s.SaveMediaJob(job); err != nil {
		t.Fatalf("SaveMediaJob: %v", err)
	}
	if err := s.SaveMediaJob(job); err != nil {
		t.Fatalf("SaveMediaJob duplicate should be ignored: %v", err)
	}
	pending, err := s.ListPendingMediaJobs("acc-1", 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 || pending[0].Status != MediaJobPending {
		t.Fatalf("pending = %+v", pending)
	}
	if err := s.MarkMediaJobRunning(job.ID, job.AccountID, 1, ""); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	got, err := s.GetMediaJob(job.ID, job.AccountID)
	if err != nil || got == nil {
		t.Fatalf("GetMediaJob: %v", err)
	}
	if got.Status != MediaJobRunning || got.Attempts != 1 {
		t.Fatalf("running = %+v", got)
	}
	if err := s.MarkMediaJobDone(job.ID, job.AccountID, "acc-1/c-1/job-1.jpg"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	jobs, err := s.ListMediaJobs("acc-1", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != MediaJobDone {
		t.Fatalf("jobs = %+v", jobs)
	}
}

// TestSetAccountEnabled kiểm tra SetAccountEnabled + ListAccounts với
// enabledOnly — verify flag persist qua DB.
func TestSetAccountEnabled(t *testing.T) {
	s, err := newTestStoreFull(t)
	if err != nil {
		t.Fatalf("newTestStoreFull: %v", err)
	}

	// Tạo 2 accounts
	if err := s.CreateAccount("acc-en-1", "Test 1", 1); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if err := s.CreateAccount("acc-en-2", "Test 2", 1); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	// Mặc định enabled = true
	all, err := s.ListAccounts(1, false)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(all))
	}
	for _, a := range all {
		if !a.Enabled {
			t.Errorf("account %s: want Enabled=true by default, got false", a.ID)
		}
	}

	// Tắt acc-en-1
	if err := s.SetAccountEnabled("acc-en-1", false); err != nil {
		t.Fatalf("set false: %v", err)
	}

	// ListAccounts với enabledOnly=true chỉ trả acc-en-2
	enabled, err := s.ListAccounts(1, true)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 1 {
		t.Fatalf("want 1 enabled, got %d", len(enabled))
	}
	if enabled[0].ID != "acc-en-2" {
		t.Errorf("want acc-en-2, got %s", enabled[0].ID)
	}

	// ListAccounts với enabledOnly=false trả cả 2
	all2, err := s.ListAccounts(1, false)
	if err != nil {
		t.Fatalf("list all 2: %v", err)
	}
	if len(all2) != 2 {
		t.Fatalf("want 2 total, got %d", len(all2))
	}
	for _, a := range all2 {
		want := a.ID == "acc-en-2"
		if a.Enabled != want {
			t.Errorf("account %s: want Enabled=%v, got %v", a.ID, want, a.Enabled)
		}
	}

	// Bật lại acc-en-1
	if err := s.SetAccountEnabled("acc-en-1", true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	enabled2, err := s.ListAccounts(1, true)
	if err != nil {
		t.Fatalf("list enabled 2: %v", err)
	}
	if len(enabled2) != 2 {
		t.Fatalf("want 2 enabled after re-enable, got %d", len(enabled2))
	}
}

func TestUpdateConversationIdentityPreservesMessageState(t *testing.T) {
	s, err := newTestStoreFull(t)
	if err != nil {
		t.Fatalf("newTestStoreFull: %v", err)
	}
	if err := s.CreateAccount("acc-identity", "Test", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	conv := &Conversation{
		ID: "group-identity", AccountID: "acc-identity",
		Name: "group-identity", Avatar: "old-avatar", ConvType: 1,
		LastMsgID: "msg-1", LastMsgAt: sql.NullTime{Time: time.UnixMilli(1000), Valid: true},
		Unread: 9,
	}
	if err := s.SaveConversation(conv); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	if err := s.UpdateConversationIdentity("acc-identity", "group-identity", "Tên nhóm", "new-avatar"); err != nil {
		t.Fatalf("UpdateConversationIdentity: %v", err)
	}
	got, err := s.GetConversation("acc-identity", "group-identity")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Name != "Tên nhóm" || got.Avatar != "new-avatar" {
		t.Fatalf("identity = %q/%q", got.Name, got.Avatar)
	}
	if got.LastMsgID != "msg-1" || !got.LastMsgAt.Valid || got.Unread != 9 {
		t.Fatalf("state overwritten: %+v", got)
	}
}

func TestCountAndRefreshConversationLastMessage(t *testing.T) {
	s, err := newTestStoreFull(t)
	if err != nil {
		t.Fatalf("newTestStoreFull: %v", err)
	}
	if err := s.CreateAccount("acc-history", "Test", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	conv := &Conversation{
		ID: "group-history", AccountID: "acc-history",
		Name: "Nhóm lịch sử", ConvType: 1,
	}
	if err := s.SaveConversation(conv); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	for _, m := range []*Message{
		{ID: "msg-old", AccountID: "acc-history", ConvID: "group-history", Timestamp: 1000},
		{ID: "msg-new", AccountID: "acc-history", ConvID: "group-history", Timestamp: 2000},
	} {
		if err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}
	if got, err := s.CountMessages("acc-history", "group-history"); err != nil || got != 2 {
		t.Fatalf("CountMessages() = %d, %v; want 2, nil", got, err)
	}
	if err := s.RefreshConversationLastMessage("acc-history", "group-history"); err != nil {
		t.Fatalf("RefreshConversationLastMessage: %v", err)
	}
	got, err := s.GetConversation("acc-history", "group-history")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.LastMsgID != "msg-new" {
		t.Fatalf("lastMsgId = %q, want msg-new", got.LastMsgID)
	}
	if !got.LastMsgAt.Valid || got.LastMsgAt.Time.UnixMilli() != 2000 {
		t.Fatalf("lastMsgAt = %+v, want 2000ms", got.LastMsgAt)
	}
	if got.Name != "Nhóm lịch sử" {
		t.Fatalf("name = %q, want Nhóm lịch sử", got.Name)
	}
}

func TestSaveGroupHistoryMessageMovesMisclassifiedMessage(t *testing.T) {
	s, err := newTestStoreFull(t)
	if err != nil {
		t.Fatalf("newTestStoreFull: %v", err)
	}
	if err := s.CreateAccount("acc-group-history", "Test", 1); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	privateConv := &Conversation{
		ID: "private-thread", AccountID: "acc-group-history", Name: "Private",
		LastMsgID: "misassigned", LastMsgAt: sql.NullTime{Time: time.UnixMilli(3000), Valid: true},
	}
	groupConv := &Conversation{
		ID: "group-thread", AccountID: "acc-group-history", Name: "Group", ConvType: 1,
	}
	for _, conv := range []*Conversation{privateConv, groupConv} {
		if err := s.SaveConversation(conv); err != nil {
			t.Fatalf("SaveConversation(%s): %v", conv.ID, err)
		}
	}
	for _, m := range []*Message{
		{ID: "private-old", AccountID: "acc-group-history", ConvID: "private-thread", Timestamp: 1000},
		{ID: "misassigned", AccountID: "acc-group-history", ConvID: "private-thread", Timestamp: 3000},
	} {
		if err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}

	history := &Message{
		ID: "misassigned", AccountID: "acc-group-history", ConvID: "group-thread",
		Timestamp: 3000, MsgType: 2,
	}
	if err := s.SaveGroupHistoryMessage(history); err != nil {
		t.Fatalf("SaveGroupHistoryMessage: %v", err)
	}
	if err := s.SaveGroupHistoryMessage(history); err != nil {
		t.Fatalf("SaveGroupHistoryMessage idempotent: %v", err)
	}

	for convID, want := range map[string]int{"group-thread": 1, "private-thread": 1} {
		got, err := s.CountMessages("acc-group-history", convID)
		if err != nil || got != want {
			t.Fatalf("CountMessages(%s) = %d, %v; want %d", convID, got, err, want)
		}
	}
	gotPrivate, err := s.GetConversation("acc-group-history", "private-thread")
	if err != nil {
		t.Fatalf("GetConversation(private): %v", err)
	}
	if gotPrivate.LastMsgID != "private-old" || !gotPrivate.LastMsgAt.Valid ||
		gotPrivate.LastMsgAt.Time.UnixMilli() != 1000 {
		t.Fatalf("private last message not restored: %+v", gotPrivate)
	}
}
