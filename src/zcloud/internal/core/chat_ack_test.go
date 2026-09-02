package core

import (
	"encoding/json"
	"testing"
)

func TestParseDeliveryAckArray(t *testing.T) {
	content := `[{"actionType":0,"clientDelMsgId":1788307651179,"destId":4866700441106275000,"globalDelMsgId":8216972229904,"type":1,"uidFrom":559609701372941700,"uidTo":4866700441106275000}]`
	ack := ParseDeliveryAck(content)
	if ack == nil {
		t.Fatal("expected ack from array, got nil")
	}
	if ack.GlobalDelMsgID != 8216972229904 {
		t.Fatalf("GlobalDelMsgID=%d", ack.GlobalDelMsgID)
	}
}

func TestParseDeliveryAckObject(t *testing.T) {
	content := `{"actionType":0,"globalDelMsgId":12345}`
	ack := ParseDeliveryAck(content)
	if ack == nil {
		t.Fatal("expected ack from object, got nil")
	}
}

func TestParseDeliveryAckPlainText(t *testing.T) {
	if a := ParseDeliveryAck("hello"); a != nil {
		t.Fatal("plain text should not be ack")
	}
	if a := ParseDeliveryAck(""); a != nil {
		t.Fatal("empty should not be ack")
	}
}

func TestMarkDeliveryAckSetsFlags(t *testing.T) {
	content := `[{"actionType":0,"globalDelMsgId":8216972229904,"type":1,"uidFrom":1,"uidTo":2,"destId":2,"clientDelMsgId":1788307651179}]`
	m := &Message{Content: content}
	if !MarkDeliveryAck(m) {
		t.Fatal("expected true")
	}
	if !m.IsDeliveryAck || m.AckStatus != "sent" {
		t.Fatalf("isAck=%v status=%q", m.IsDeliveryAck, m.AckStatus)
	}
	// JSON output phải có isAck/ackStatus
	b, _ := json.Marshal(m)
	if got := string(b); got != `{"content":"`+content+`","isAck":true,"ackStatus":"sent"}` {
		// Có thể kèm các field khác rỗng, kiểm tra substring
		if !contains(got, `"isAck":true`) || !contains(got, `"ackStatus":"sent"`) {
			t.Fatalf("missing flags: %s", got)
		}
	}
}

func TestMarkDeliveryAckPlainText(t *testing.T) {
	m := &Message{Content: "hello"}
	if MarkDeliveryAck(m) {
		t.Fatal("plain text should not set ack")
	}
	if m.IsDeliveryAck || m.AckStatus != "" {
		t.Fatalf("expected false/empty, got isAck=%v status=%q", m.IsDeliveryAck, m.AckStatus)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
