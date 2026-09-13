package core

// Package pccrypto cung cấp các helper ed25519 + HKDF-SHA256 + AES-GCM
// cho Zalo PC protocol (trusted-device, syncv2, backup). Pure-Go dùng
// stdlib + golang.org/x/crypto + filippo.io/edwards25519 + math/big —
// không load WASM binary.
//
// Thiết kế theo docs/protocol/syncv2.md §6-7:
//   - ed25519 keypair: tạo mỗi account, persist public_key trong DB.
//   - session key: HKDF-SHA256(ed25519_agreement(local_priv, remote_pub),
//     salt, info) → AES-256 key cho AES-GCM message encryption.
//   - Message format: [12B nonce][ciphertext+tag].
//
// Mapping với Zalo PC WASM exports:
//   zprotoEd25519{Generate,Derive,Calculate,Verify} ↔ stdlib ed25519.
//   zprotoEd25519CalculateAgreement ↔ ed25519 → X25519 ECDH (RFC 7748).

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// PCKeyPair là ed25519 keypair cho 1 account.
type PCKeyPair struct {
	Private ed25519.PrivateKey // 64B
	Public  ed25519.PublicKey  // 32B
}

// GeneratePCKeyPair tạo ed25519 keypair mới. Tương đương
// zprotoEd25519GenerateKeyPair() trong WASM.
func GeneratePCKeyPair() (PCKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PCKeyPair{}, fmt.Errorf("ed25519.GenerateKey: %w", err)
	}
	return PCKeyPair{Private: priv, Public: pub}, nil
}

// PCPublicKey returns derived public key (32 bytes). Tương đương
// zprotoEd25519DerivePublicKey(priv).
func (k PCKeyPair) PCPublicKey() ed25519.PublicKey {
	return k.Private.Public().(ed25519.PublicKey)
}

// prime25519 = 2^255 - 19 (Edwards curve field prime, cũng là Montgomery curve field).
var prime25519 = func() *big.Int {
	p := new(big.Int).Lsh(big.NewInt(1), 255)
	p.Sub(p, big.NewInt(19))
	return p
}()

// edwardsPubToMontgomery converts ed25519 public key (32B compressed
// Edwards y, sign bit ở bit 255) sang X25519 Montgomery u-coord.
//
// Formula: u = (1 + y) / (1 - y) mod p,   p = 2^255 - 19.
//
// Sign bit của Edwards được encode vào bit 254 của Montgomery (X25519
// chấp nhận arbitrary bit 254 vì nó nằm ngoài field, nên set/unset đều
// OK — ta clear bit này để deterministic output).
//
// Reference:
//   https://libsodium.gitbook.io/doc/advanced/ed25519-curve25519
func edwardsPubToMontgomery(edPub ed25519.PublicKey) ([]byte, error) {
	if len(edPub) != 32 {
		return nil, fmt.Errorf("invalid ed25519 pub size %d", len(edPub))
	}

	// Parse y từ little-endian 32B, clear sign bit (bit 255 = bit 7 của byte 31).
	yBytes := make([]byte, 32)
	copy(yBytes, edPub)
	yBytes[31] &= 0x7F // clear sign bit
	yInt := new(big.Int).SetBytes(yBytes)

	// u = (1 + y) * modinv(1 - y, p) mod p
	one := big.NewInt(1)
	yn := new(big.Int).Sub(one, yInt)
	if yn.Sign() == 0 {
		return nil, errors.New("edwards pub: y = 1 (point at infinity)")
	}
	ynInv := new(big.Int).ModInverse(yn, prime25519)
	if ynInv == nil {
		return nil, errors.New("mod inverse failed")
	}
	num := new(big.Int).Add(one, yInt)
	u := new(big.Int).Mod(new(big.Int).Mul(num, ynInv), prime25519)

	// Encode u as little-endian 32 bytes (X25519 format).
	out := make([]byte, 32)
	uBytes := u.Bytes()
	// uBytes là big-endian, có thể < 32B. Copy từ phải sang trái.
	if len(uBytes) > 32 {
		return nil, fmt.Errorf("u too large: %d bytes", len(uBytes))
	}
	// Reverse copy: big-endian → little-endian.
	for i, b := range uBytes {
		out[len(uBytes)-1-i] = b
	}
	return out, nil
}

// clampPrivateKey clamps ed25519 seed thành X25519 scalar theo RFC 7748:
//   k[0]  &= 248   (clear bottom 3 bits)
//   k[31] &= 127   (clear bit 254)
//   k[31] |= 64    (set bit 255 — make scalar divisible by cofactor 8)
func clampPrivateKey(seed []byte) []byte {
	if len(seed) != 32 {
		panic("clampPrivateKey: seed must be 32 bytes")
	}
	out := make([]byte, 32)
	copy(out, seed)
	out[0] &= 248
	out[31] &= 127
	out[31] |= 64
	return out
}

// PCAgreement computes shared secret từ ed25519 priv + remote ed25519
// pub (32B each). Convert sang X25519 + chạy ECDH. Trả 32B shared secret.
//
// Tương đương zprotoEd25519CalculateAgreement(priv, pub).
func PCAgreement(localPriv ed25519.PrivateKey, remotePub ed25519.PublicKey) ([]byte, error) {
	if len(localPriv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid priv size %d", len(localPriv))
	}
	if len(remotePub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid pub size %d", len(remotePub))
	}

	// ed25519 priv = seed(32B) + pub(32B). Lấy seed.
	seed := localPriv[:32]

	// Clamp seed → X25519 scalar.
	xPriv := clampPrivateKey(seed)

	// Convert ed25519 pub → X25519 Montgomery u-coord.
	xPub, err := edwardsPubToMontgomery(remotePub)
	if err != nil {
		return nil, fmt.Errorf("convert ed25519 pub → montgomery: %w", err)
	}

	// ECDH.
	return curve25519.X25519(xPriv, xPub)
}

// SignEd25519 tương đương zprotoEd25519CalculateSignature(priv, msg).
func SignEd25519(priv ed25519.PrivateKey, msg []byte) []byte {
	return ed25519.Sign(priv, msg)
}

// VerifyEd25519 tương đương zprotoEd25519VerifySignature(pub, msg, sig).
func VerifyEd25519(pub ed25519.PublicKey, msg, sig []byte) bool {
	return ed25519.Verify(pub, msg, sig)
}

// DeriveSessionKey computes 32B AES-256 key từ agreement + salt + info
// qua HKDF-SHA256. Tương đương zprotoSync2GetSessionId hoặc nội bộ
// zprotoSync2EncryptMessage.
func DeriveSessionKey(agreement, salt, info []byte) ([]byte, error) {
	if len(agreement) != 32 {
		return nil, fmt.Errorf("invalid agreement size %d", len(agreement))
	}
	r := hkdf.New(sha256.New, agreement, salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return key, nil
}

// AESEncryptMessage encrypts plaintext với AES-256-GCM.
// Output: [12B nonce][ciphertext+tag]. Tương đương zprotoSync2EncryptMessage.
func AESEncryptMessage(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key size %d (need 32 for AES-256)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize()) // 12B
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("rand nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// AESDecryptMessage decrypts [12B nonce][ciphertext+tag].
// Tương đương zprotoSync2DecryptMessage.
func AESDecryptMessage(key, message, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key size %d (need 32 for AES-256)", len(key))
	}
	if len(message) < 12 {
		return nil, errors.New("message too short (need >= 12B nonce)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := message[:12]
	ciphertext := message[12:]
	return gcm.Open(nil, nonce, ciphertext, aad)
}

// pcKeypairFromBytes reconstruct PCKeyPair từ 32B private seed + 32B
// public key (dùng cho persist/load từ DB). ed25519.PrivateKey = seed||pub
// theo RFC 8032 (Hachi: hash seed → 32B SHA-512 left half → scalar;
// toàn bộ priv = seed||pub).
//
// Lưu ý: RFC 8032 nói priv = seed||pub với pub derived from seed.
// ed25519.NewKeyFromSeed(seed) trả về priv 64B theo format này.
func pcKeypairFromBytes(privSeed, pub []byte) (PCKeyPair, error) {
	if len(privSeed) != ed25519.SeedSize {
		return PCKeyPair{}, fmt.Errorf("invalid seed size %d", len(privSeed))
	}
	if len(pub) != ed25519.PublicKeySize {
		return PCKeyPair{}, fmt.Errorf("invalid pub size %d", len(pub))
	}
	priv := ed25519.NewKeyFromSeed(privSeed)
	// Verify pub khớp.
	if !pubEqual(priv.Public().(ed25519.PublicKey), ed25519.PublicKey(pub)) {
		return PCKeyPair{}, errors.New("pub mismatch with derived from seed")
	}
	return PCKeyPair{Private: priv, Public: ed25519.PublicKey(pub)}, nil
}

func pubEqual(a, b ed25519.PublicKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Helper không dùng nhưng compile-time guard cho binary package.
var _ = binary.LittleEndian
