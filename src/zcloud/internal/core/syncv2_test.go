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
