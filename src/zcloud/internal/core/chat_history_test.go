package core

import (
	"encoding/json"
	"testing"
)

func TestParseGroupHistoryPayloadNestedString(t *testing.T) {
	inner := `{"groupMsgs":[{"msgId":"m1","grid":"g1","uidFrom":"u1","ts":1690000000000,"msgType":1,"content":"xin chao"}]}`
	payload, err := json.Marshal(map[string]string{"data": inner})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	items, err := parseGroupHistoryPayload(payload)
	if err != nil {
		t.Fatalf("parseGroupHistoryPayload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	msgs, skipped := groupHistoryMessagesFromItems(items)
	if skipped != 0 || len(msgs) != 1 {
		t.Fatalf("messages=%d skipped=%d, want 1,0", len(msgs), skipped)
	}
	if msgs[0].ID != "m1" || msgs[0].ConvID != "g1" || msgs[0].Timestamp != 1690000000000 {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestDecodeGroupHistoryDataNestedEnvelope(t *testing.T) {
	key := []byte("0123456789abcdef")
	inner, err := EncodeAESCBC(key, `{"groupMsgs":[{"msgId":"m1","grid":"g1","ts":1690000000000,"msgType":1,"content":"ok"}]}`)
	if err != nil {
		t.Fatalf("EncodeAESCBC inner: %v", err)
	}
	envelope, err := json.Marshal(map[string]any{
		"error_code": 0, "error_message": "", "data": inner,
	})
	if err != nil {
		t.Fatalf("Marshal envelope: %v", err)
	}
	outer, err := EncodeAESCBC(key, string(envelope))
	if err != nil {
		t.Fatalf("EncodeAESCBC outer: %v", err)
	}
	decoded, err := decodeGroupHistoryData(key, outer)
	if err != nil {
		t.Fatalf("decodeGroupHistoryData: %v", err)
	}
	items, err := parseGroupHistoryPayload(decoded)
	if err != nil {
		t.Fatalf("parseGroupHistoryPayload: %v", err)
	}
	if len(items) != 1 || items[0]["msgId"] != "m1" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestGroupHistoryMessagesSkipInvalidTimestamp(t *testing.T) {
	items := []map[string]any{
		{"msgId": "missing-ts", "grid": "g1", "content": "photo"},
		{"msgId": "zero-ts", "grid": "g1", "ts": float64(0), "content": "photo"},
		{"msgId": "valid", "grid": "g1", "ts": "1700000000000", "msgType": "1", "content": "ok"},
	}
	msgs, skipped := groupHistoryMessagesFromItems(items)
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
	if len(msgs) != 1 || msgs[0].ID != "valid" {
		t.Fatalf("messages = %+v, want only valid", msgs)
	}
	if msgs[0].Timestamp != 1700000000000 || msgs[0].Type != MsgTypeText {
		t.Fatalf("unexpected parsed message: %+v", msgs[0])
	}
}

func TestGroupHistoryMessagesParseStructuredPhoto(t *testing.T) {
	items := []map[string]any{{
		"msgId": "photo", "grid": "g1", "ts": float64(1700000000000),
		"content": map[string]any{
			"href":   "https://example.com/a.jpg",
			"thumb":  "https://example.com/a.jpg",
			"params": `{"width":720,"height":1280}`,
		},
	}}
	msgs, skipped := groupHistoryMessagesFromItems(items)
	if skipped != 0 || len(msgs) != 1 {
		t.Fatalf("messages=%d skipped=%d, want 1,0", len(msgs), skipped)
	}
	if msgs[0].Content != "" {
		t.Fatalf("content = %q", msgs[0].Content)
	}
	if msgs[0].Type != MsgTypeImage || len(msgs[0].Attachments) != 1 {
		t.Fatalf("unexpected photo message: %+v", msgs[0])
	}
	attachment := msgs[0].Attachments[0]
	if attachment.URL != "https://example.com/a.jpg" || attachment.Width != 720 || attachment.Height != 1280 {
		t.Fatalf("unexpected attachment: %+v", attachment)
	}
}
