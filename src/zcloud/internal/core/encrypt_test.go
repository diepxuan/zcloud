package core

import (
	"encoding/hex"
	"testing"
)

func TestPKCS7Pad(t *testing.T) {
	tests := []struct {
		input    string
		block    int
		wantLen  int
		wantErr  bool
	}{
		{"hello", 16, 16, false},
		{"0123456789abcdef", 16, 32, false}, // exactly block size → add full block
		{"", 16, 16, false},
	}

	for _, tt := range tests {
		padded, err := pkcs7Pad([]byte(tt.input), tt.block)
		if (err != nil) != tt.wantErr {
			t.Errorf("pkcs7Pad(%q, %d) error = %v, wantErr %v", tt.input, tt.block, err, tt.wantErr)
		}
		if len(padded) != tt.wantLen {
			t.Errorf("pkcs7Pad(%q, %d) = %d bytes, want %d", tt.input, tt.block, len(padded), tt.wantLen)
		}
		// Kiểm tra unpadding
		unpadded, err := pkcs7Unpad(padded, tt.block)
		if err != nil {
			t.Errorf("pkcs7Unpad error: %v", err)
		}
		if string(unpadded) != tt.input {
			t.Errorf("pkcs7Unpad = %q, want %q", string(unpadded), tt.input)
		}
	}
}

func TestAESCBCCrypto(t *testing.T) {
	key := []byte("3FC4F0D2AB50057B") // 16-byte key
	plaintext := `{"imei":"test","computer_name":"zcloud"}`

	ciphertext, err := EncodeAESCBC(key, plaintext)
	if err != nil {
		t.Fatalf("EncodeAESCBC error: %v", err)
	}
	if ciphertext == "" {
		t.Fatal("ciphertext is empty")
	}

	decrypted, err := DecodeAESCBC(key, ciphertext)
	if err != nil {
		t.Fatalf("DecodeAESCBC error: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Errorf("DecodeAESCBC = %q, want %q", string(decrypted), plaintext)
	}
}

func TestGenerateZCID(t *testing.T) {
	imei := "test-imei-12345"
	firstLaunch := int64(1700000000000)

	zcid, err := generateZCID(30, imei, firstLaunch)
	if err != nil {
		t.Fatalf("generateZCID error: %v", err)
	}
	if len(zcid) == 0 {
		t.Fatal("zcid is empty")
	}
	// ZCID should be hex string (uppercase)
	if _, err := hex.DecodeString(zcid); err != nil {
		t.Errorf("zcid is not valid hex: %v", err)
	}
	t.Logf("zcid = %s (len=%d)", zcid, len(zcid))
}

func TestDeriveEncryptKey(t *testing.T) {
	// zcid thực tế từ AES-CBC: 48 bytes = 96 hex chars
	// Lấy từ TestGenerateZCID output
	testCases := []struct {
		ext string
		id  string
	}{
		{"a1b2c3", "4F64BD29470A9DC69C4ABD49735481C3FC2FD05F588204C64F4CBE3B6D0ED3C13B1D2EBA2B2766D9CD579D9682F09A44"},
		{"abcdef", "089321A95D04EF222DCB6621144D2F9111CE689917BFF19284B86051C605EEB4"},
		{"", "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"},
	}

	for _, tc := range testCases {
		key := deriveEncryptKey(tc.ext, tc.id)
		if len(key) == 0 {
			t.Errorf("deriveEncryptKey(%q, zcid[0:16]) returned empty key", tc.ext)
			continue
		}
		// 8 + 12 + 12 = 32 chars (khi zcid đủ dài)
		if len(key) != 32 {
			t.Errorf("deriveEncryptKey(%q, zcid[0:16]) = %q (len=%d), want 32 chars", tc.ext, key, len(key))
		}
		t.Logf("deriveEncryptKey(ext=%q) = %s (len=%d)", tc.ext, key, len(key))
	}
}

func TestGenerateSignKey(t *testing.T) {
	params := map[string]any{
		"imei":           "test-imei",
		"type":           30,
		"client_version": 665,
		"computer_name":  "zcloud",
	}

	// getserverinfo sign key
	signkey1 := GenerateSignKey("getserverinfo", params)
	if len(signkey1) != 32 {
		t.Errorf("signkey len = %d, want 32 (MD5 hex)", len(signkey1))
	}
	t.Logf("signkey(getserverinfo) = %s", signkey1)

	// Chạy 2 lần phải cho kết quả giống nhau
	signkey2 := GenerateSignKey("getserverinfo", params)
	if signkey1 != signkey2 {
		t.Error("signkey not deterministic")
	}
}

func TestFullEncryptFlow(t *testing.T) {
	session := &Session{
		IMEI:       "test-imei-full",
		Language:   "vi",
		APIType:    30,
		APIVersion: 665,
	}

	data := map[string]any{
		"computer_name": "zcloud",
		"imei":          session.GetIMEI(),
		"language":      session.GetLanguage(),
		"ts":            int64(1700000000000),
	}

	result, err := NewEncryptParam(session.GetAPIType(), session.GetIMEI(), data)
	if err != nil {
		t.Fatalf("NewEncryptParam error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Enk == nil {
		t.Fatal("Enk is nil")
	}
	if len(*result.Enk) != 32 {
		t.Errorf("Enk len = %d, want 32", len(*result.Enk))
	}
	// params["params"] phải có
	if _, ok := result.Params["params"]; !ok {
		t.Error("params key not found in result")
	}
	if _, ok := result.Params["zcid"]; !ok {
		t.Error("zcid key not found in result")
	}
	t.Logf("Enk = %s", *result.Enk)
	t.Logf("Params = %v", result.Params)
}

func BenchmarkEncodeAESCBC(b *testing.B) {
	key := []byte("0123456789abcdef")
	plaintext := `{"imei":"test-imei","computer_name":"zcloud","ts":1700000000000}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeAESCBC(key, plaintext)
	}
}
