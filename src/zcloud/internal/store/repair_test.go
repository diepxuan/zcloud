//go:build testdb

package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	repairSelfUID = "559609701372941728"
	repairPeerUID = "6036488311923736737"
)

// TestRepairSelfThreadMessages: tin đến bị gom vào thread mang uid của chính
// account phải được chuyển về thread người gửi, kèm media + file trên disk.
func TestRepairSelfThreadMessages(t *testing.T) {
	s := newTestStore(t)

	// Tin đến lưu sai thread (conv_id = uid của mình).
	if err := s.SaveMessage(&Message{
		ID: "in-1", AccountID: "acc-1", ConvID: repairSelfUID,
		FromID: repairPeerUID, Content: "toi gui cho ban", MsgType: 1, Timestamp: 1000,
	}); err != nil {
		t.Fatalf("save incoming: %v", err)
	}
	// Tin mình gửi đi đã đúng thread.
	if err := s.SaveMessage(&Message{
		ID: "out-1", AccountID: "acc-1", ConvID: repairPeerUID,
		FromID: repairSelfUID, Content: "reply", MsgType: 1, Timestamp: 1001,
	}); err != nil {
		t.Fatalf("save outgoing: %v", err)
	}
	// Tin tự chat với chính mình — không được đụng vào.
	if err := s.SaveMessage(&Message{
		ID: "self-1", AccountID: "acc-1", ConvID: repairSelfUID,
		FromID: repairSelfUID, Content: "ghi chu", MsgType: 1, Timestamp: 1002,
	}); err != nil {
		t.Fatalf("save self: %v", err)
	}

	// Media của tin đến nằm cùng thread sai + file thật trên disk.
	if _, err := s.SaveMedia(&MediaFile{
		ID: "in-1", AccountID: "acc-1", ConvID: repairSelfUID, MsgID: "in-1",
		FileName: "a.jpg", FileExt: "jpg", SourceURL: "https://example.com/a.jpg",
		IsDownloaded: 1,
	}); err != nil {
		t.Fatalf("save media: %v", err)
	}
	oldPath := s.MediaFilePath("acc-1", repairSelfUID, "in-1", "jpg")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("jpeg-bytes"), 0644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	if err := s.SaveMediaJob(&MediaJob{
		ID: "in-1", AccountID: "acc-1", ConvID: repairSelfUID, MsgID: "in-1",
		FileName: "a.jpg", FileExt: "jpg", SourceURL: "https://example.com/a.jpg",
	}); err != nil {
		t.Fatalf("save media job: %v", err)
	}

	n, err := s.CountSelfThreadMessages("acc-1", repairSelfUID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count=%d want 1", n)
	}

	rep, err := s.RepairSelfThreadMessages("acc-1", repairSelfUID)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if rep.Messages != 1 {
		t.Errorf("Messages=%d want 1", rep.Messages)
	}
	if rep.Media != 1 {
		t.Errorf("Media=%d want 1", rep.Media)
	}
	if rep.MediaJobs != 1 {
		t.Errorf("MediaJobs=%d want 1", rep.MediaJobs)
	}
	if rep.MovedFiles != 1 || len(rep.FileErrors) != 0 {
		t.Errorf("MovedFiles=%d errs=%v", rep.MovedFiles, rep.FileErrors)
	}

	// Thread của peer giờ có cả tin đến lẫn tin đi.
	msgs, err := s.GetMessages("acc-1", repairPeerUID, 1<<62, 50)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("peer thread=%d want 2", len(msgs))
	}

	// Thread self chỉ còn ghi chú tự gửi.
	selfMsgs, err := s.GetMessages("acc-1", repairSelfUID, 1<<62, 50)
	if err != nil {
		t.Fatalf("get self: %v", err)
	}
	if len(selfMsgs) != 1 || selfMsgs[0].ID != "self-1" {
		t.Fatalf("self thread=%+v", selfMsgs)
	}

	// File media đã sang thư mục thread mới.
	newPath := s.MediaFilePath("acc-1", repairPeerUID, "in-1", "jpg")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("media file chua chuyen: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("file cu van con: %v", err)
	}

	// Chạy lại phải idempotent.
	rep2, err := s.RepairSelfThreadMessages("acc-1", repairSelfUID)
	if err != nil {
		t.Fatalf("repair lan 2: %v", err)
	}
	if rep2.Messages != 0 || rep2.Media != 0 || rep2.MediaJobs != 0 {
		t.Errorf("khong idempotent: %+v", rep2)
	}
}

func TestRepairMisclassifiedGroupMessages(t *testing.T) {
	s := newTestStore(t)
	const accountID = "acc-1"
	const privateConvID = "private-1"
	const groupConvID = "group-1"
	const groupMsgID = "group-last-1"

	if err := s.SaveConversation(&Conversation{
		ID: privateConvID, AccountID: accountID, Name: "Private",
		ConvType: 0, LastMsgID: groupMsgID,
	}); err != nil {
		t.Fatalf("save private conv: %v", err)
	}
	if err := s.SaveConversation(&Conversation{
		ID: groupConvID, AccountID: accountID, Name: "Group",
		ConvType: 1, LastMsgID: groupMsgID,
	}); err != nil {
		t.Fatalf("save group conv: %v", err)
	}
	if err := s.SaveMessage(&Message{
		ID: "private-last", AccountID: accountID, ConvID: privateConvID,
		FromID: privateConvID, Content: "private hello", MsgType: 1, Timestamp: 1000,
	}); err != nil {
		t.Fatalf("save private msg: %v", err)
	}
	// Group message bị parser cũ ghi nhầm vào chat riêng.
	if err := s.SaveMessage(&Message{
		ID: groupMsgID, AccountID: accountID, ConvID: privateConvID,
		FromID: "member-1", Content: "group hello", MsgType: 1, Timestamp: 2000,
	}); err != nil {
		t.Fatalf("save group msg wrong conv: %v", err)
	}

	if _, err := s.SaveMedia(&MediaFile{
		ID: groupMsgID, AccountID: accountID, ConvID: privateConvID, MsgID: groupMsgID,
		FileName: "a.jpg", FileExt: "jpg", SourceURL: "https://example.com/a.jpg",
		IsDownloaded: 1,
	}); err != nil {
		t.Fatalf("save media: %v", err)
	}
	oldPath := s.MediaFilePath(accountID, privateConvID, groupMsgID, "jpg")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("jpeg-bytes"), 0644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	if err := s.SaveMediaJob(&MediaJob{
		ID: groupMsgID, AccountID: accountID, ConvID: privateConvID, MsgID: groupMsgID,
		FileName: "a.jpg", FileExt: "jpg", SourceURL: "https://example.com/a.jpg",
	}); err != nil {
		t.Fatalf("save media job: %v", err)
	}

	n, err := s.CountMisclassifiedGroupMessages(accountID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count=%d want 1", n)
	}

	rep, err := s.RepairMisclassifiedGroupMessages(accountID)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if rep.Messages != 1 || rep.Media != 1 || rep.MediaJobs != 1 || rep.MovedFiles != 1 {
		t.Fatalf("repair result=%+v", rep)
	}
	if len(rep.FileErrors) != 0 {
		t.Fatalf("file errors=%v", rep.FileErrors)
	}

	groupMsgs, err := s.GetMessages(accountID, groupConvID, 1<<62, 10)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if len(groupMsgs) != 1 || groupMsgs[0].ID != groupMsgID {
		t.Fatalf("group messages=%+v", groupMsgs)
	}
	privateMsgs, err := s.GetMessages(accountID, privateConvID, 1<<62, 10)
	if err != nil {
		t.Fatalf("get private: %v", err)
	}
	if len(privateMsgs) != 1 || privateMsgs[0].ID != "private-last" {
		t.Fatalf("private messages=%+v", privateMsgs)
	}
	privateConv, err := s.GetConversation(accountID, privateConvID)
	if err != nil {
		t.Fatalf("get private conv: %v", err)
	}
	if privateConv.LastMsgID != "private-last" {
		t.Fatalf("private last_msg_id=%q want private-last", privateConv.LastMsgID)
	}
	if !privateConv.LastMsgAt.Valid || privateConv.LastMsgAt.Time.UnixMilli() != 1000 {
		t.Fatalf("private last_msg_at=%+v want 1000", privateConv.LastMsgAt)
	}

	groupConv, err := s.GetConversation(accountID, groupConvID)
	if err != nil {
		t.Fatalf("get group conv: %v", err)
	}
	if !groupConv.LastMsgAt.Valid {
		t.Fatalf("group last_msg_at not set: %+v", groupConv.LastMsgAt)
	}
	wantTime := time.UnixMilli(2000)
	if groupConv.LastMsgAt.Time.UnixMilli() != wantTime.UnixMilli() {
		t.Fatalf("last_msg_at=%v want %v", groupConv.LastMsgAt.Time, wantTime)
	}

	newPath := s.MediaFilePath(accountID, groupConvID, groupMsgID, "jpg")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("media file chua chuyen: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("file cu van con: %v", err)
	}

	n, err = s.CountMisclassifiedGroupMessages(accountID)
	if err != nil {
		t.Fatalf("count after repair: %v", err)
	}
	if n != 0 {
		t.Fatalf("count after repair=%d want 0", n)
	}
	rep2, err := s.RepairMisclassifiedGroupMessages(accountID)
	if err != nil {
		t.Fatalf("repair lan 2: %v", err)
	}
	if rep2.Messages != 0 || rep2.Media != 0 || rep2.MediaJobs != 0 ||
		rep2.PrivateThreads != 0 || rep2.MovedFiles != 0 {
		t.Fatalf("khong idempotent: %+v", rep2)
	}
}

func TestGetConversationsPreviewScopedByConv(t *testing.T) {
	s := newTestStore(t)
	const accountID = "acc-1"

	if err := s.SaveConversation(&Conversation{
		ID: "private-1", AccountID: accountID, Name: "Private",
		ConvType: 0, LastMsgID: "shared-id",
	}); err != nil {
		t.Fatalf("save private conv: %v", err)
	}
	if err := s.SaveConversation(&Conversation{
		ID: "group-1", AccountID: accountID, Name: "Group",
		ConvType: 1, LastMsgID: "shared-id",
	}); err != nil {
		t.Fatalf("save group conv: %v", err)
	}
	if err := s.SaveMessage(&Message{
		ID: "shared-id", AccountID: accountID, ConvID: "private-1",
		FromID: "peer-1", Content: "private content", MsgType: 1, Timestamp: 1000,
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}

	convs, err := s.GetConversations(accountID)
	if err != nil {
		t.Fatalf("get conversations: %v", err)
	}
	for _, c := range convs {
		switch c.ID {
		case "private-1":
			if c.LastMsgContent != "private content" {
				t.Fatalf("private preview=%q", c.LastMsgContent)
			}
		case "group-1":
			if c.LastMsgContent != "" {
				t.Fatalf("group leaked private preview=%q", c.LastMsgContent)
			}
		}
	}
}
