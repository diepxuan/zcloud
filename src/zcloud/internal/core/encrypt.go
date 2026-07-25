package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// ====================================
// AES-128-CBC (zero IV) — encryption cho Zalo API
// ====================================

var zeroIV = make([]byte, aes.BlockSize) // 16 bytes 0x00

// PKCS7 padding
func pkcs7Pad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		return nil, fmt.Errorf("pkcs7: invalid block size %d", blockSize)
	}
	padLen := blockSize - len(data)%blockSize
	pad := byte(padLen)
	padded := make([]byte, len(data)+padLen)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = pad
	}
	return padded, nil
}

// PKCS7 unpadding
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("pkcs7: invalid data length %d", len(data))
	}
	padLen := int(data[len(data)-1])
	if padLen <= 0 || padLen > blockSize {
		return nil, fmt.Errorf("pkcs7: invalid padding length %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if int(data[i]) != padLen {
			return nil, fmt.Errorf("pkcs7: invalid padding byte at %d", i)
		}
	}
	return data[:len(data)-padLen], nil
}

// EncodeAESCBC encrypts plaintext using AES-128-CBC with zero IV
// key: 16-byte AES key (raw bytes, not base64)
func EncodeAESCBC(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes: new cipher: %w", err)
	}

	padded, err := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	if err != nil {
		return "", fmt.Errorf("aes: padding: %w", err)
	}

	iv := make([]byte, aes.BlockSize) // zero IV
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecodeAESCBC decrypts ciphertext using AES-128-CBC with zero IV
// key: 16-byte AES key (raw bytes)
func DecodeAESCBC(key []byte, ciphertext string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("aes: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: new cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize) // zero IV
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("aes: unpadding: %w", err)
	}

	return plain, nil
}

// ====================================
// Zalo key generation
// ====================================

const (
	zcidKey         = "3FC4F0D2AB50057BCE0D90D9187A22B1"
	defaultEncVer   = "1"
	defaultCompName = "zcloud"
)

// EncryptParams generates encrypted parameters for Zalo API login requests
type EncryptParamResult struct {
	Params map[string]any
	Enk    *string // encryptKey, nil nếu không encrypt
}

// generateZCID creates the zcid field
func generateZCID(apiType uint, imei string, firstLaunch int64) (string, error) {
	plain := fmt.Sprintf("%d,%s,%d", apiType, imei, firstLaunch)
	enc, err := EncodeAESCBC([]byte(zcidKey), plain)
	if err != nil {
		return "", err
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	return strings.ToUpper(hex.EncodeToString(raw)), nil
}

// randomHex generates random hex string with length between minLen and maxLen
func randomHex(minLen, maxLen int) string {
	length := minLen
	if maxLen > minLen {
		length = minLen + rand.Intn(maxLen-minLen+1)
	}
	b := make([]byte, (length+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// processStr splits string into even-index and odd-index characters
func processStr(s string) (even, odd []string) {
	runes := []rune(s)
	for i, r := range runes {
		if i%2 == 0 {
			even = append(even, string(r))
		} else {
			odd = append(odd, string(r))
		}
	}
	return
}

// joinFirst joins first n elements of parts
func joinFirst(parts []string, n int) string {
	if n > len(parts) {
		n = len(parts)
	}
	return strings.Join(parts[:n], "")
}

// reverseCopy returns reversed copy of slice
func reverseCopy[T any](in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// deriveEncryptKey derives the final encrypt key from zcid_ext and zcid
func deriveEncryptKey(zcidExt, zcid string) string {
	// MD5(zcid_ext) → uppercase
	sum := md5.Sum([]byte(zcidExt))
	nUpper := strings.ToUpper(hex.EncodeToString(sum[:]))

	evenE, _ := processStr(nUpper)
	evenI, oddI := processStr(zcid)

	var b strings.Builder
	b.WriteString(joinFirst(evenE, 8))
	b.WriteString(joinFirst(evenI, 12))
	b.WriteString(joinFirst(reverseCopy(oddI), 12))
	return b.String()
}

// NewEncryptParam generates encrypted parameters for a Zalo API call
func NewEncryptParam(apiType uint, imei string, data map[string]any) (*EncryptParamResult, error) {
	now := time.Now().UnixMilli()

	// Tạo zcid
	zcid, err := generateZCID(apiType, imei, now)
	if err != nil {
		return nil, fmt.Errorf("zcid: %w", err)
	}

	// Tạo zcid_ext (random 6-12 hex chars)
	zcidExt := randomHex(6, 12)

	// Tạo encryptKey
	encKey := deriveEncryptKey(zcidExt, zcid)

	// Encrypt data
	jsonData, _ := json.Marshal(data) // sẽ dùng encoding/json
	if err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}

	cipher, err := EncodeAESCBC([]byte(encKey), string(jsonData))
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	params := map[string]any{
		"zcid":     zcid,
		"enc_ver":  defaultEncVer,
		"zcid_ext": zcidExt,
		"params":   cipher,
	}

	return &EncryptParamResult{
		Params: params,
		Enk:    &encKey,
	}, nil
}

// GenerateSignKey generates MD5 sign key for Zalo API requests
// signKey = MD5("zsecure" + typeStr + sorted param values)
func GenerateSignKey(typeStr string, params map[string]any) string {
	// Sắp xếp keys theo alphabet
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("zsecure")
	b.WriteString(typeStr)
	for _, k := range keys {
		val := fmt.Sprintf("%v", params[k])
		b.WriteString(val)
	}

	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// encryptParamsForLogin encrypts params map for login API calls
func encryptParamsForLogin(sc SessionContext, encrypt bool, typeStr string) (*EncryptParamResult, error) {
	data := map[string]any{
		"computer_name": defaultCompName,
		"imei":          sc.GetIMEI(),
		"language":      sc.GetLanguage(),
		"ts":            time.Now().UnixMilli(),
	}

	var enc *EncryptParamResult
	if encrypt {
		var err error
		enc, err = NewEncryptParam(sc.GetAPIType(), sc.GetIMEI(), data)
		if err != nil {
			return nil, err
		}
	}

	params := make(map[string]any, 8)
	if enc == nil {
		for k, v := range data {
			params[k] = v
		}
	} else {
		for k, v := range enc.Params {
			params[k] = v
		}
	}

	params["type"] = sc.GetAPIType()
	params["client_version"] = sc.GetAPIVersion()

	if typeStr == "getserverinfo" {
		params["signkey"] = GenerateSignKey(typeStr, map[string]any{
			"imei":           sc.GetIMEI(),
			"type":           sc.GetAPIType(),
			"client_version": sc.GetAPIVersion(),
			"computer_name":  defaultCompName,
		})
	} else {
		params["signkey"] = GenerateSignKey(typeStr, params)
	}

	return &EncryptParamResult{Params: params, Enk: enc.Enk}, nil
}
