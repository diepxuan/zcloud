package core

// TestSyncV2_CipherRoundTrip: build cipher session + encrypt/decrypt.
// Stub Phase B dùng ed25519 self-agreement + HKDF + AES-256-GCM. Test chỉ
// đảm bảo round-trip OK với cùng key. Phase C/WASM sẽ dùng key derivation
// thật từ WASM zprotoSync2*.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestSyncV2_CipherRoundTrip(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	// 64 hex chars (= 32B) — phù hợp cả hex lẫn base64.
	tempKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	sess, err := BuildCipherSession(priv, tempKey)
	if err != nil {
		t.Fatalf("BuildCipherSession: %v", err)
	}
	pt := `{"msgId":"m1","uidFrom":"u1","content":"hello","ts":1700000000000,"msgType":1}`
	ct, err := sess.EncryptMessage(pt)
	if err != nil {
		t.Fatalf("EncryptMessage: %v", err)
	}
	dec, err := sess.DecryptMessage(ct)
	if err != nil {
		t.Fatalf("DecryptMessage: %v", err)
	}
	if dec != pt {
		t.Fatalf("roundtrip mismatch:\nwant=%q\ngot =%q", pt, dec)
	}
}

func TestSyncV2_BuildCipher_DifferentTempKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s1, _ := BuildCipherSession(priv, "aaaa")
	s2, _ := BuildCipherSession(priv, "bbbb")
	if string(s1.key) == string(s2.key) {
		t.Fatal("temp_key khác nhau mà ra cùng AES key")
	}
}

func TestSyncV2_BuildCipher_DifferentPriv(t *testing.T) {
	_, p1, _ := ed25519.GenerateKey(rand.Reader)
	_, p2, _ := ed25519.GenerateKey(rand.Reader)
	s1, _ := BuildCipherSession(p1, "same")
	s2, _ := BuildCipherSession(p2, "same")
	if string(s1.key) == string(s2.key) {
		t.Fatal("priv khác nhau mà ra cùng AES key")
	}
}

func TestSyncV2_BuildCipher_EmptyTempKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := BuildCipherSession(priv, ""); err == nil {
		t.Fatal("temp_key rỗng phải fail")
	}
}

// TestSyncV2_HandleEvent: state machine transitions cho Phase B (T22.3).
// 1) user_confirm user_action=1 → phase=pulling, temp_key lưu.
// 2) user_confirm user_action=0 → phase=error.
// 3) transfer_error error_code=42 → phase=error.
// 4) transfer_after_login (no temp_key) → phase giữ nguyên.
func TestSyncV2_HandleEvent(t *testing.T) {
	c, err := NewSyncV2Client(&Session{IMEI: "imei-1"}, &SyncV2State{
		PCName:    "zcloud",
		IMEI:      "imei-1",
		PublicKey: "00",
		Phase:     "waiting_confirm",
	})
	if err != nil {
		t.Fatalf("NewSyncV2Client: %v", err)
	}
	// user_confirm user_action=1
	err = c.HandleEvent([]byte(`{"act":"user_confirm","data":{"user_action":1,"pc_name":"zcloud","public_key":"00","temp_key":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}}`))
	if err != nil {
		t.Fatalf("user_confirm err: %v", err)
	}
	if c.State().Phase != "pulling" {
		t.Fatalf("phase=%q want pulling", c.State().Phase)
	}
	if c.State().TempKey == "" {
		t.Fatal("temp_key chưa lưu")
	}

	// user_confirm user_action=0 (reject) — phải lỗi + phase=error.
	c2, _ := NewSyncV2Client(&Session{IMEI: "i"}, &SyncV2State{PCName: "z", IMEI: "i", PublicKey: "00", Phase: "waiting_confirm"})
	err = c2.HandleEvent([]byte(`{"act":"user_confirm","data":{"user_action":0,"pc_name":"z","public_key":"00"}}`))
	if err == nil {
		t.Fatal("user_action=0 phải fail")
	}
	if c2.State().Phase != "error" {
		t.Fatalf("phase=%q want error", c2.State().Phase)
	}

	// transfer_error error_code=42
	c3, _ := NewSyncV2Client(&Session{IMEI: "i"}, &SyncV2State{PCName: "z", IMEI: "i", PublicKey: "00", Phase: "pulling"})
	err = c3.HandleEvent([]byte(`{"act":"transfer_error","data":{"error_code":42}}`))
	if err == nil {
		t.Fatal("transfer_error code != 0 phải fail")
	}
	if c3.State().LastError == "" {
		t.Fatal("LastError rỗng")
	}

	// transfer_after_login (no temp_key) — phase giữ nguyên.
	c4, _ := NewSyncV2Client(&Session{IMEI: "i"}, &SyncV2State{PCName: "z", IMEI: "i", PublicKey: "00", Phase: "init"})
	if err := c4.HandleEvent([]byte(`{"act":"transfer_after_login","data":{"imei":"i"}}`)); err != nil {
		t.Fatalf("transfer_after_login err: %v", err)
	}
	if c4.State().Phase != "init" {
		t.Fatalf("phase=%q want init", c4.State().Phase)
	}
}

// TestSyncV2_DecryptMessages_MockBatch: giả lập batch 2 messages,
// encrypt/decrypt qua cipher session rồi parse JSON wsMessage.
func TestSyncV2_DecryptMessages_MockBatch(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	tempKey := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	sess, err := BuildCipherSession(priv, tempKey)
	if err != nil {
		t.Fatalf("BuildCipherSession: %v", err)
	}

	c1, _ := sess.EncryptMessage(`{"msgId":"a","uidFrom":"u1","content":"hi","ts":1,"msgType":1}`)
	c2, _ := sess.EncryptMessage(`{"msgId":"b","uidFrom":"u2","content":"reply","ts":2,"msgType":1}`)
	batch := &PullBatchResponse{
		Messages: []PulledMessage{
			{SessionID: "s1", Cipher: c1},
			{SessionID: "s2", Cipher: c2},
		},
		NextSeqID: 2,
		Done:      true,
	}
	c := &SyncV2Client{
		session: &Session{UserID: "me"},
		state: &SyncV2State{
			TempKey:    tempKey,
			PublicKey:  hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
			PrivateKey: hex.EncodeToString(priv.Seed()),
		},
	}
	msgs, err := c.DecryptMessages(batch)
	if err != nil {
		t.Fatalf("DecryptMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "a" || msgs[1].ID != "b" {
		t.Fatalf("ids=%q,%q", msgs[0].ID, msgs[1].ID)
	}
}
