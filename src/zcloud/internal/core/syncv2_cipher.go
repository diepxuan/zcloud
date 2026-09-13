package core

// SyncV2 cipher — Phase B (Task 22). Stub pure-Go dựa trên ed25519 agreement
// + HKDF-SHA256 + AES-GCM. Build tag `syncv2_wasm` sẽ override bằng
// WASM thật (libzproto_wasm_bg.*.wasm) sau khi reverse xong 13 args của
// `zprotoSync2CreateMetadataCipher`.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Sync2Cipher là opaque cipher session cho decrypt batch messages.
// Phase B stub dùng AES-256-GCM với key derive từ ed25519 agreement +
// temp_key qua HKDF. Pattern này dựa trên Noise/NoiseIK (X25519) nhưng
// dùng ed25519 ECDH (chỉ 1 chiều vì ed25519 → X25519 conversion cần
// SHA-512 hash trick; tạm thời dùng SHA-256 để đơn giản).
type Sync2Cipher struct {
	key []byte // 32B AES-256 key
}

// BuildCipherSession tạo cipher session từ:
//   priv: ed25519 private key (32B seed → expand to 64B)
//   tempKeyHex: temp_key từ user_confirm event (hex string)
//
// Derive:
//   shared = SHA256(priv.Public().Bytes() || priv.Seed())  // ed25519 self-agreement
//   salt = SHA256(tempKey || "syncv2-salt")
//   key  = HKDF-SHA256(shared, salt, "syncv2-msg-key", 32)
func BuildCipherSession(priv ed25519.PrivateKey, tempKeyHex string) (*Sync2Cipher, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("priv key size %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	if tempKeyHex == "" {
		return nil, fmt.Errorf("temp_key empty")
	}
	tempKey, err := hex.DecodeString(tempKeyHex)
	if err != nil {
		// Thử base64.
		tempKey, err = base64.StdEncoding.DecodeString(tempKeyHex)
		if err != nil {
			return nil, fmt.Errorf("temp_key not hex/base64: %w", err)
		}
	}
	// Self-agreement: SHA256(pub || seed) — stub cho ed25519 shared secret.
	// (WASM thật dùng X25519 convert; tạm stub bằng hash trong Phase B.)
	pub := priv.Public().(ed25519.PublicKey)
	seed := priv.Seed()
	h := sha256.New()
	h.Write(pub)
	h.Write(seed)
	shared := h.Sum(nil)

	// Salt = SHA256(tempKey || "syncv2-salt")
	sh := sha256.New()
	sh.Write(tempKey)
	sh.Write([]byte("syncv2-salt"))
	salt := sh.Sum(nil)

	// Key = HKDF-SHA256(shared, salt, info, 32)
	key, err := hkdfSHA256(shared, salt, []byte("syncv2-msg-key"), 32)
	if err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return &Sync2Cipher{key: key}, nil
}

// hkdfSHA256 implement HKDF-Expand-Len(SHA-256) — đủ cho output ≤ 32 bytes.
// Phase B stub: chỉ support len ≤ hashLen (32B cho SHA-256). Đủ cho AES-256 key.
func hkdfSHA256(secret, salt, info []byte, length int) ([]byte, error) {
	if length > 32 {
		return nil, fmt.Errorf("hkdf stub: len %d > 32", length)
	}
	// PRK = HMAC-SHA256(salt, secret)
	prk := hmac.New(sha256.New, salt)
	prk.Write(secret)
	k := prk.Sum(nil)
	// OKM = HMAC-SHA256(k, info || 0x01)
	mac := hmac.New(sha256.New, k)
	mac.Write(info)
	mac.Write([]byte{0x01})
	out := mac.Sum(nil)
	return out[:length], nil
}

// DecryptMessage giải mã 1 ciphertext (base64) → plaintext (string).
// Layout: [nonce 12B] || [ciphertext+tag] (AES-256-GCM).
func (c *Sync2Cipher) DecryptMessage(cipherB64 string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", fmt.Errorf("decode b64: %w", err)
	}
	if len(ct) < 12+16 {
		return "", fmt.Errorf("cipher too short: %d", len(ct))
	}
	nonce, payload := ct[:12], ct[12:]
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(pt), nil
}

// EncryptMessage encrypt 1 plaintext → base64 cipher (helper cho test).
func (c *Sync2Cipher) EncryptMessage(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, 12)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	ct := aead.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}
