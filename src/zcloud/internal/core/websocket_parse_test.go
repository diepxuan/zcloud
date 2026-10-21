package core

import (
	"testing"
	"time"
)

// TestParseNewMessagesWrapped đảm bảo handleNewMessages unwrap layer 2
// (`{error_code, data: {msgs|groupMsgs}}`) đúng theo zca-js tham chiếu
// (src/apis/listen.ts:259). Bug 12/09/2026: payload bị wrap mà code chỉ parse
// 1 lớp nên msgs_len=0 và SaveMessage không bao giờ được gọi.
func TestParseNewMessagesWrapped(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}

	// Layer 2 wrapper, kiểu user thread (501).
	payload := []byte(`{"error_code":0,"data":{"msgs":[
		{"msgId":"n1","uidFrom":"other","dName":"Other","idTo":"me","content":"hi","ts":1700000000000,"msgType":1}
	]}}`)
	w.handleNewMessages(payload, ThreadUser)

	got := drainEventsAll(w.msgChan, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != EventNewMessage {
		t.Fatalf("type=%v want EventNewMessage", got[0].Type)
	}
	if got[0].Message == nil || got[0].Message.ID != "n1" {
		t.Fatalf("msg=%+v", got[0].Message)
	}
	if got[0].Message.Content != "hi" {
		t.Fatalf("content=%q", got[0].Message.Content)
	}
}

// TestParseNewMessagesGroupWrapped: group thread (521) cũng bị wrap layer 2.
func TestParseNewMessagesGroupWrapped(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	payload := []byte(`{"error_code":0,"data":{"groupMsgs":[
		{"msgId":"g1","uidFrom":"u1","dName":"U1","grid":"grp-1","content":"hello","ts":1700000010000,"msgType":1}
	]}}`)
	w.handleNewMessages(payload, ThreadGroup)

	got := drainEventsAll(w.msgChan, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Message.ConvID != "grp-1" {
		t.Fatalf("ConvID=%q", got[0].Message.ConvID)
	}
	if got[0].Message.FromID != "u1" {
		t.Fatalf("FromID=%q", got[0].Message.FromID)
	}
}

// TestParseNewMessagesGroupWithoutGridUsesIDTo: một số payload groupMsgs
// không có grid. GroupMessage của zca-js dùng idTo làm threadId; không được
// fallback conversation về uid người gửi.
func TestParseNewMessagesGroupWithoutGridUsesIDTo(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	payload := []byte(`{"error_code":0,"data":{"groupMsgs":[
		{"msgId":"g2","uidFrom":"member-1","dName":"Member","idTo":"group-1","content":"hello","ts":1700000010000,"msgType":1}
	]}}`)
	w.handleNewMessages(payload, ThreadGroup)

	got := drainEventsAll(w.msgChan, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Message.ConvID != "group-1" {
		t.Fatalf("ConvID=%q want group-1", got[0].Message.ConvID)
	}
	if got[0].Message.FromID != "member-1" {
		t.Fatalf("FromID=%q want member-1", got[0].Message.FromID)
	}
}

// TestParseNewMessagesGroupUnknownConversationSkipped: không được đoán group
// conversation từ uid người gửi rồi ghi vào chat riêng.
func TestParseNewMessagesGroupUnknownConversationSkipped(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	payload := []byte(`{"groupMsgs":[
		{"msgId":"g3","uidFrom":"member-1","content":"missing thread","ts":1700000010000,"msgType":1}
	]}`)
	w.handleNewMessages(payload, ThreadGroup)

	got := drainEventsAll(w.msgChan, 0, 200*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected no event, got %+v", got)
	}
}

// TestParseOldMessagesGroupWithoutGridUsesIDTo: nhánh 511 cũng phải giữ
// conversation group khi payload thiếu grid.
func TestParseOldMessagesGroupWithoutGridUsesIDTo(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	payload := []byte(`{"groupMsgs":[
		{"msgId":"g4","uidFrom":"member-1","idTo":"group-2","content":"old","ts":1700000010000,"msgType":1}
	]}`)
	w.handleOldMessages(payload, ThreadGroup)

	got := drainEventsAll(w.msgChan, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Message.ConvID != "group-2" {
		t.Fatalf("ConvID=%q want group-2", got[0].Message.ConvID)
	}
}

// TestParseNewMessagesPlainRoot: tương thích ngược — payload không wrap vẫn parse.
func TestParseNewMessagesPlainRoot(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 4),
		errChan:   make(chan error, 2),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	payload := []byte(`{"msgs":[
		{"msgId":"p1","uidFrom":"other","dName":"Other","idTo":"me","content":"plain","ts":1700000000000,"msgType":1}
	]}`)
	w.handleNewMessages(payload, ThreadUser)

	got := drainEventsAll(w.msgChan, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Message.Content != "plain" {
		t.Fatalf("content=%q", got[0].Message.Content)
	}
}

// TestParseNewMessagesEmpty: payload rỗng không panic, không emit.
func TestParseNewMessagesEmpty(t *testing.T) {
	w := &WSClient{
		msgChan:   make(chan Event, 2),
		errChan:   make(chan error, 1),
		closeChan: make(chan CloseEvent, 1),
		session:   &Session{UserID: "me"},
	}
	w.handleNewMessages([]byte(`{}`), ThreadUser)
	w.handleNewMessages([]byte(`{"error_code":0,"data":{}}`), ThreadUser)
	w.handleNewMessages(nil, ThreadUser)
	got := drainEventsAll(w.msgChan, 0, 100*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected no events, got %d", len(got))
	}
}

// drainEventsAll giống drainEvents trong sync_test.go (file đó có build tag
// testdb nên không truy cập được từ đây). Drain tối đa n event, timeout.
func drainEventsAll(ch <-chan Event, n int, timeout time.Duration) []Event {
	out := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(timeout):
			return out
		}
	}
	return out
}
