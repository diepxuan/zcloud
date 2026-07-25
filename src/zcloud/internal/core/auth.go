package core

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ====================================
// Zalo Authentication — QR + Cookie
// ====================================

const (
	defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// QRLoginSession lưu trạng thái QR login
type QRLoginSession struct {
	Cookies   map[string]string
	Token     string
	IMEI      string
	UserAgent string
	ImageB64  string // QR code image (base64 PNG)
	ExpiresAt time.Time
}

// ====================================
// IMEI generation
// ====================================

func generateIMEI(ua string) string {
	b := make([]byte, 16)
	rand.Read(b)
	uuid := hex.EncodeToString(b)
	uuid = uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:]

	sum := md5.Sum([]byte(ua))
	return strings.ToUpper(uuid) + "-" + hex.EncodeToString(sum[:])[:12]
}

// ====================================
// Step 1: Create QR code
// ====================================

// CreateQRLogin thực hiện các bước tạo QR code:
// 1. Load login page → lấy version
// 2. Get login info + verify client
// 3. Generate QR
// Trả về QRLoginSession (chứa image QR + token + cookies)
func CreateQRLogin(ctx context.Context) (*QRLoginSession, error) {
	imei := generateIMEI(defaultUA)
	jar := newCookieJar()
	client := &http.Client{Timeout: 30 * time.Second}

	// ====================================
	// Bước 1: Load login page
	// ====================================
	req1, _ := http.NewRequestWithContext(ctx, "GET", "https://id.zalo.me/account?continue=https%3A%2F%2Fchat.zalo.me", nil)
	req1.Header.Set("User-Agent", defaultUA)
	resp1, err := client.Do(req1)
	if err != nil {
		return nil, fmt.Errorf("load page: %w", err)
	}
	bodyBytes, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	jar.SetFromHeader(resp1.Header)

	// Tìm version từ JS bundle
	version := extractVersion(string(bodyBytes))
	_ = version

	// ====================================
	// Bước 2: Get login info
	// ====================================
	infoData := url.Values{"imei": {imei}, "type": {"30"}, "version": {version}}
	req2, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/logininfo", strings.NewReader(infoData.Encode()))
	setQRHeaders(req2, defaultUA, jar)
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("login info: %w", err)
	}
	resp2.Body.Close()
	jar.SetFromHeader(resp2.Header)

	// ====================================
	// Bước 3: Verify client
	// ====================================
	verifyData := url.Values{"imei": {imei}, "type": {"30"}}
	req3, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/verify-client", strings.NewReader(verifyData.Encode()))
	setQRHeaders(req3, defaultUA, jar)
	resp3, err := client.Do(req3)
	if err != nil {
		return nil, fmt.Errorf("verify client: %w", err)
	}
	resp3.Body.Close()
	jar.SetFromHeader(resp3.Header)

	// ====================================
	// Bước 4: Generate QR
	// ====================================
	qrData := url.Values{"imei": {imei}, "type": {"30"}}
	req4, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/generate", strings.NewReader(qrData.Encode()))
	setQRHeaders(req4, defaultUA, jar)
	resp4, err := client.Do(req4)
	if err != nil {
		return nil, fmt.Errorf("generate qr: %w", err)
	}
	defer resp4.Body.Close()
	jar.SetFromHeader(resp4.Header)

	var qrResp struct {
		Data struct {
			Code  interface{} `json:"code"` // có thể là int hoặc string
			Token string      `json:"token"`
			Image string      `json:"image"`
		} `json:"data"`
		ErrorCode int `json:"error_code"`
	}
	if err := json.NewDecoder(resp4.Body).Decode(&qrResp); err != nil {
		return nil, fmt.Errorf("parse qr: %w", err)
	}
	if qrResp.ErrorCode != 0 {
		return nil, fmt.Errorf("qr generate error %d", qrResp.ErrorCode)
	}

	session := &QRLoginSession{
		Cookies:   jar.cookies,
		Token:     qrResp.Data.Token,
		IMEI:      imei,
		UserAgent: defaultUA,
		ImageB64:  qrResp.Data.Image,
		ExpiresAt: time.Now().Add(3 * time.Minute),
	}

	return session, nil
}

// ====================================
// Step 2: Poll QR + Login
// ====================================

// PollQRLogin poll QR status và hoàn thành login khi user scan
// Trả về LoginResult khi thành công
func PollQRLogin(ctx context.Context, session *QRLoginSession) (*LoginResult, error) {
	jar := newCookieJar()
	jar.Set(session.Cookies)
	client := &http.Client{Timeout: 30 * time.Second}
	ua := session.UserAgent

	pollData := url.Values{
		"token": {session.Token},
		"imei":  {session.IMEI},
		"type":  {"30"},
	}

	// ====================================
	// Poll waiting-scan (tối đa 120 lần = 4 phút)
	// ====================================
	scanned := false
	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/waiting-scan", strings.NewReader(pollData.Encode()))
		setQRHeaders(req, ua, jar)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll scan: %w", err)
		}

		var result struct {
			ErrorCode int `json:"error_code"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		jar.SetFromHeader(resp.Header)

		if result.ErrorCode == 0 || result.ErrorCode == 1 {
			scanned = true
			break
		}
		// ErrorCode 8 = chưa scan
		time.Sleep(2 * time.Second)
	}

	if !scanned {
		return nil, fmt.Errorf("qr scan timeout — không thấy quét QR")
	}

	// ====================================
	// Poll waiting-confirm (tối đa 60 lần = 2 phút)
	// ====================================
	confirmed := false
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/waiting-confirm", strings.NewReader(pollData.Encode()))
		setQRHeaders(req, ua, jar)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll confirm: %w", err)
		}

		var result struct {
			ErrorCode int `json:"error_code"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		jar.SetFromHeader(resp.Header)

		if result.ErrorCode == 0 {
			confirmed = true
			break
		}
		if result.ErrorCode == -13 {
			return nil, fmt.Errorf("qr declined — từ chối trên điện thoại")
		}
		if result.ErrorCode == -14 {
			return nil, fmt.Errorf("qr expired — QR hết hạn")
		}
		time.Sleep(2 * time.Second)
	}

	if !confirmed {
		return nil, fmt.Errorf("qr confirm timeout — không xác nhận trên điện thoại")
	}

	// ====================================
	// Check session + get user info
	// ====================================
	req7, _ := http.NewRequestWithContext(ctx, "GET", "https://id.zalo.me/account/checksession", nil)
	setQRHeaders(req7, ua, jar)
	resp7, err := client.Do(req7)
	if err != nil {
		return nil, fmt.Errorf("check session: %w", err)
	}
	resp7.Body.Close()
	jar.SetFromHeader(resp7.Header)

	// ====================================
	// Cookie login → getLoginInfo + getServerInfo
	// ====================================
	return CookieLogin(ctx, jar.cookies, session.IMEI, ua)
}

// ====================================
// Cookie-based login
// ====================================

func CookieLogin(ctx context.Context, cookies map[string]string, imei, ua string) (*LoginResult, error) {
	if ua == "" {
		ua = defaultUA
	}
	if imei == "" {
		imei = generateIMEI(ua)
	}

	sess := &Session{
		IMEI:       imei,
		UserAgent:  ua,
		Language:   "vi",
		APIType:    30,
		APIVersion: 665,
		Cookies:    cookies,
	}

	// GET getLoginInfo
	encResult, err := encryptParamsForLogin(sess, true, "getlogininfo")
	if err != nil {
		return nil, fmt.Errorf("encrypt login params: %w", err)
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}
	if encResult.Enk != nil {
		query.Set("enk", *encResult.Enk)
	}

	loginURL := "https://wpa.chat.zalo.me/api/login/getLoginInfo?" + query.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", loginURL, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.zalo.me")
	req.Header.Set("Referer", "https://chat.zalo.me/")
	req.Header.Set("Cookie", cookiesToString(cookies))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getLoginInfo request: %w", err)
	}
	defer resp.Body.Close()

	var loginResp struct {
		ErrorCode    int              `json:"error_code"`
		ErrorMessage string           `json:"error_message"`
		Data         *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("parse login info: %w", err)
	}
	if loginResp.ErrorCode != 0 {
		return nil, fmt.Errorf("login error %d: %s", loginResp.ErrorCode, loginResp.ErrorMessage)
	}
	if loginResp.Data == nil || encResult.Enk == nil {
		return nil, fmt.Errorf("no login data returned")
	}

	decrypted, err := DecodeAESCBC([]byte(*encResult.Enk), string(*loginResp.Data))
	if err != nil {
		return nil, fmt.Errorf("decrypt login data: %w", err)
	}

	type loginDataRaw struct {
		UID    string `json:"uid"`
		ZPWEnk string `json:"zpw_enk"`
		ZPWSEK string `json:"zpw_sek"`
		ZPWS   string `json:"zpw_ws"`
	}
	var innerResp struct {
		ErrorCode int `json:"error_code"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &innerResp); err != nil {
		return nil, fmt.Errorf("parse inner login: %w", err)
	}
	var data loginDataRaw
	if innerResp.Data != nil {
		if err := json.Unmarshal(innerResp.Data, &data); err != nil {
			return nil, fmt.Errorf("parse login data: %w", err)
		}
	}

	sess.SecretKey = data.ZPWEnk
	sess.UserID = data.UID
	sess.ExpiresAt = time.Now().Add(24 * time.Hour)

	if data.ZPWS != "" {
		var wsURLs []string
		if err := json.Unmarshal([]byte(data.ZPWS), &wsURLs); err == nil {
			sess.WSURLs = wsURLs
		} else {
			sess.WSURLs = []string{data.ZPWS}
		}
	}

	// Get getServerInfo để lấy thêm cấu hình
	serverInfo, err := getServerInfo(ctx, sess, cookies)
	if err == nil && serverInfo != nil {
		_ = serverInfo
	}

	return &LoginResult{
		Session: sess,
		Cookies: cookies,
	}, nil
}

// ====================================
// getServerInfo
// ====================================

func getServerInfo(ctx context.Context, sess *Session, cookies map[string]string) (map[string]any, error) {
	encResult, err := encryptParamsForLogin(sess, false, "getserverinfo")
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}

	urlStr := "https://wpa.chat.zalo.me/api/login/getServerInfo?" + query.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	req.Header.Set("User-Agent", sess.UserAgent)
	req.Header.Set("Cookie", cookiesToString(cookies))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ====================================
// Helpers
// ====================================

func setQRHeaders(req *http.Request, ua string, jar *cookieJar) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://id.zalo.me/")
	req.Header.Set("Origin", "https://id.zalo.me")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Cookie", jar.String())
}

func cookiesToString(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

type cookieJar struct {
	cookies map[string]string
}

func newCookieJar() *cookieJar {
	return &cookieJar{cookies: make(map[string]string)}
}

func (j *cookieJar) Set(cookies map[string]string) {
	for k, v := range cookies {
		j.cookies[k] = v
	}
}

func (j *cookieJar) SetFromHeader(header http.Header) {
	for _, c := range header.Values("Set-Cookie") {
		if parts := strings.SplitN(c, "=", 2); len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			value := strings.SplitN(parts[1], ";", 2)[0]
			j.cookies[name] = value
		}
	}
}

func (j *cookieJar) String() string {
	parts := make([]string, 0, len(j.cookies))
	for k, v := range j.cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func extractVersion(body string) string {
	idx := strings.Index(body, "main-")
	if idx < 0 {
		return "665"
	}
	body = body[idx+5:]
	end := strings.IndexAny(body, ".\"")
	if end < 0 {
		return "665"
	}
	return body[:end]
}
