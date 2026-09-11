//go:build testdb

package store

import (
	"os"
	"path/filepath"
	"testing"
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
