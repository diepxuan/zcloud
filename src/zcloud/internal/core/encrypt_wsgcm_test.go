package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// buildWSAESGCMEnvelope wraps an AES-GCM ciphertext into the Zalo WS event
// envelope {"data":"<base64(ct)>","encrypt":<mode>}.
func buildWSAESGCMEnvelope(t *testing.T, ct []byte, mode int) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"data":    base64.StdEncoding.EncodeToString(ct),
		"encrypt": mode,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// TestEncodeDecodeWSRoundTrip phủ end-to-end: EncodeAESGCM → wrap envelope →
// DecodeWSEvent cho cả mode 2 (AES-GCM + gzip) và mode 3 (AES-GCM raw).
// Chạy thêm biến thể cipherKey dạng raw vs base64 string.
func TestEncodeDecodeWSRoundTrip(t *testing.T) {
	// Key nhị phân 16 byte (không phải base64) → decryptAESGCMWithKey đi nhánh raw key.
	keyRaw := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	}
	keyB64 := []byte(base64.StdEncoding.EncodeToString(keyRaw))
	plain := []byte(`{"cmd":501,"data":{"msg":"xin chào WS","n":42}}`)
	aad := []byte("0123456789abcdef")

	keys := []struct {
		name      string
		cipherKey []byte
	}{
		{"raw_key", keyRaw},
		{"base64_key", keyB64},
	}

	for _, mode := range []byte{WSEncryptAESGCM, WSEncryptAESGCMRaw} {
		for _, k := range keys {
			t.Run(fmt.Sprintf("mode_%d/%s", mode, k.name), func(t *testing.T) {
				ct, err := EncodeAESGCM(keyRaw, aad, plain, mode)
				if err != nil {
					t.Fatalf("encode: %v", err)
				}
				envelope := buildWSAESGCMEnvelope(t, ct, int(mode))
				out, err := DecodeWSEvent(envelope, k.cipherKey)
				if err != nil {
					t.Fatalf("DecodeWSEvent: %v", err)
				}
				if !bytes.Equal(out, plain) {
					t.Errorf("round-trip mismatch: want %q, got %q", plain, out)
				}
			})
		}
	}
}

// TestGunzipIfNeeded phủ các biên: passthrough (không có magic gzip), rỗng/1
// byte không panic, gzip hợp lệ giải nén đúng, magic 0x1f8b nhưng hỏng → error.
func TestGunzipIfNeeded(t *testing.T) {
	notGzip := []byte("plain text, không phải gzip")
	gzPayload := []byte("payload cần nén")
	gz, err := gzipBytes(gzPayload)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}

	tests := []struct {
		name    string
		in      []byte
		wantErr bool
		want    []byte
	}{
		{"non_gzip_passthrough", notGzip, false, notGzip},
		{"empty_passthrough", []byte{}, false, []byte{}},
		{"single_byte_passthrough", []byte{0x1f}, false, []byte{0x1f}},
		{"valid_gzip", gz, false, gzPayload},
		{"corrupt_magic", []byte{0x1f, 0x8b, 0xff, 0xff, 0xff, 0xff}, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gunzipIfNeeded(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeWSBase64 phủ các biên: base64 sai → error, dữ liệu < 32 byte →
// error "short data", base64 hợp lệ ≥ 32 byte → OK.
func TestDecodeWSBase64(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string // substring kỳ vọng; "" = không lỗi
		wantLen int
	}{
		{"invalid_base64", "not valid base64!!!", "ws base64 decode", 0},
		{"short_data", base64.StdEncoding.EncodeToString([]byte("hello")), "short data", 0},
		{"valid", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xAB}, 32)), "", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeWSBase64(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(got) != tt.wantLen {
					t.Errorf("len = %d, want %d", len(got), tt.wantLen)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

// TestDecryptAESGCMWithKey phủ: raw key (không phải base64) → OK, base64 key →
// OK, key rỗng → error "cipher key is required".
func TestDecryptAESGCMWithKey(t *testing.T) {
	plain := []byte(`{"ok":true}`)
	aad := []byte("0123456789abcdef")

	// raw key nhị phân 16 byte → không parse được base64, đi nhánh raw key.
	keyRaw := []byte{
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}
	rawForKeyRaw, err := EncodeAESGCM(keyRaw, aad, plain, WSEncryptAESGCMRaw)
	if err != nil {
		t.Fatalf("encode raw key: %v", err)
	}

	// base64 key: 16 byte ASCII.
	keyAscii := []byte("0123456789abcdef")
	rawForKeyB64, err := EncodeAESGCM(keyAscii, aad, plain, WSEncryptAESGCMRaw)
	if err != nil {
		t.Fatalf("encode base64 key: %v", err)
	}

	tests := []struct {
		name      string
		raw       []byte
		cipherKey []byte
		wantErr   string
	}{
		{"raw_key", rawForKeyRaw, keyRaw, ""},
		{"base64_key", rawForKeyB64, []byte(base64.StdEncoding.EncodeToString(keyAscii)), ""},
		{"empty_key", rawForKeyRaw, nil, "cipher key is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := decryptAESGCMWithKey(tt.raw, tt.cipherKey)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(out, plain) {
				t.Errorf("got %q, want %q", out, plain)
			}
		})
	}
}
