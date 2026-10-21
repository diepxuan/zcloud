package api

import (
	"database/sql"
	"testing"
	"time"

	"github.com/diepxuan/zcloud/internal/store"
)

func TestMergeConversationForSyncPreservesResolvedIdentity(t *testing.T) {
	lastAt := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	existing := &store.Conversation{
		ID: "group-1", Name: "Nhóm đã resolve", Avatar: "old-avatar",
		LastMsgID: "msg-old", LastMsgAt: sql.NullTime{Time: lastAt, Valid: true}, Unread: 7,
	}
	incoming := &store.Conversation{
		ID: "group-1", Name: "group-1", Avatar: "", LastMsgID: "",
		LastMsgAt: sql.NullTime{}, Unread: 0,
	}

	mergeConversationForSync(incoming, existing)

	if incoming.Name != existing.Name {
		t.Fatalf("name = %q, want %q", incoming.Name, existing.Name)
	}
	if incoming.Avatar != existing.Avatar {
		t.Fatalf("avatar = %q, want %q", incoming.Avatar, existing.Avatar)
	}
	if incoming.LastMsgID != existing.LastMsgID {
		t.Fatalf("lastMsgId = %q, want %q", incoming.LastMsgID, existing.LastMsgID)
	}
	if !incoming.LastMsgAt.Valid || !incoming.LastMsgAt.Time.Equal(lastAt) {
		t.Fatalf("lastMsgAt = %+v, want %s", incoming.LastMsgAt, lastAt)
	}
	if incoming.Unread != existing.Unread {
		t.Fatalf("unread = %d, want %d", incoming.Unread, existing.Unread)
	}
}

func TestMergeConversationForSyncAcceptsNewIdentity(t *testing.T) {
	existing := &store.Conversation{
		ID: "group-1", Name: "Tên cũ", Avatar: "old-avatar",
	}
	incoming := &store.Conversation{
		ID: "group-1", Name: "Tên mới", Avatar: "new-avatar",
	}

	mergeConversationForSync(incoming, existing)

	if incoming.Name != "Tên mới" {
		t.Fatalf("name = %q, want Tên mới", incoming.Name)
	}
	if incoming.Avatar != "new-avatar" {
		t.Fatalf("avatar = %q, want new-avatar", incoming.Avatar)
	}
}

func TestMergeConversationForSyncNilExisting(t *testing.T) {
	incoming := &store.Conversation{ID: "group-1", Name: "group-1"}
	mergeConversationForSync(incoming, nil)
	if incoming.Name != "group-1" {
		t.Fatalf("name = %q, want group-1", incoming.Name)
	}
}

func TestConversationSyncLockScopedByAccount(t *testing.T) {
	s := &Server{}
	unlockA, ok := s.tryLockConversationSync("account-a")
	if !ok {
		t.Fatal("first sync lock for account-a was rejected")
	}

	if _, ok := s.tryLockConversationSync("account-a"); ok {
		t.Fatal("second sync lock for account-a should be rejected")
	}

	unlockB, ok := s.tryLockConversationSync("account-b")
	if !ok {
		t.Fatal("sync lock for account-b should not be blocked by account-a")
	}
	unlockB()

	unlockA()
	unlockA2, ok := s.tryLockConversationSync("account-a")
	if !ok {
		t.Fatal("sync lock for account-a was not released")
	}
	unlockA2()
}
