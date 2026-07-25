package core

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ====================================
// Zalo Web Login Flow
// ====================================

const (
	defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	// idURL    = "https://id.zalo.me"
	// wpaURL = "https://wpa.chat.zalo.me"
)

// LoginCredentials chứa thông tin đăng nhập
type LoginCredentials struct {
	Cookies   map[string]string // Cookie đã có (nếu login cookie)
	IMEI      string            // IMEI tự sinh
	UserAgent string
}

// LoginResult kết quả đăng nhập
type LoginResult struct {
	Session    *Session
	Cookies    map[string]string
	LoginInfo  map[string]any // raw login info từ API
}

// ====================================
// Cookie jar
// ====================================

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

func (j *cookieJar) Header() http.Header {
	h := http.Header{}
	for k, v := range j.cookies {
		h.Add("Cookie", k+"="+v)
	}
	return h
}

func (j *cookieJar) String() string {
	parts := make([]string, 0, len(j.cookies))
	for k, v := range j.cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// ====================================
// IMEI generation
// ====================================

func generateIMEI(ua string) string {
	sum := md5.Sum([]byte(ua))
	return strings.ToUpper(uuid.New().String()) + "-" + hex.EncodeToString(sum[:])[:12]
}

// ====================================
// QR Login Flow (từ zca-js/zcago)
// ====================================

// QRLogin thực hiện QR login hoàn chỉnh
func QRLogin(ctx context.Context) (*LoginResult, error) {
	imei := generateIMEI(defaultUA)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // không tự follow redirect
		},
	}

	jar := newCookieJar()
	ua := defaultUA

	// ====================================
	// Bước 1: Load login page → lấy version
	// ====================================

	req1, _ := http.NewRequestWithContext(ctx, "GET", "https://id.zalo.me/account?continue=https%3A%2F%2Fchat.zalo.me", nil)
	req1.Header.Set("User-Agent", ua)
	resp1, err := client.Do(req1)
	if err != nil {
		return nil, fmt.Errorf("load login page: %w", err)
	}
	defer resp1.Body.Close()

	jar.SetFromHeader(resp1.Header)

	// Đọc body để tìm version
	bodyBytes, _ := io.ReadAll(resp1.Body)
	bodyStr := string(bodyBytes)

	// Tìm version từ JS bundle URL: main-{version}.js
	version := extractVersion(bodyStr)
	_ = version // sẽ dùng sau nếu cần

	// ====================================
	// Bước 2: Get login info
	// ====================================

	loginInfoData := url.Values{}
	loginInfoData.Set("imei", imei)
	loginInfoData.Set("type", "30")
	loginInfoData.Set("version", version)

	req2, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/logininfo", strings.NewReader(loginInfoData.Encode()))
	req2.Header.Set("User-Agent", ua)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Referer", "https://id.zalo.me/")
	for k, v := range jar.cookies {
		req2.Header.Add("Cookie", k+"="+v)
	}

	resp2, err := client.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("login info: %w", err)
	}
	defer resp2.Body.Close()
	jar.SetFromHeader(resp2.Header)

	// ====================================
	// Bước 3: Verify client
	// ====================================

	verifyData := url.Values{}
	verifyData.Set("imei", imei)
	verifyData.Set("type", "30")

	req3, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/verify-client", strings.NewReader(verifyData.Encode()))
	req3.Header.Set("User-Agent", ua)
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.Header.Set("Referer", "https://id.zalo.me/")
	req3.Header.Set("Cookie", jar.String())

	resp3, err := client.Do(req3)
	if err != nil {
		return nil, fmt.Errorf("verify client: %w", err)
	}
	defer resp3.Body.Close()
	jar.SetFromHeader(resp3.Header)

	// ====================================
	// Bước 4: Generate QR code
	// ====================================

	qrData := url.Values{}
	qrData.Set("imei", imei)
	qrData.Set("type", "30")

	req4, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/generate", strings.NewReader(qrData.Encode()))
	req4.Header.Set("User-Agent", ua)
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req4.Header.Set("Referer", "https://id.zalo.me/")
	req4.Header.Set("Cookie", jar.String())

	resp4, err := client.Do(req4)
	if err != nil {
		return nil, fmt.Errorf("generate qr: %w", err)
	}
	defer resp4.Body.Close()
	jar.SetFromHeader(resp4.Header)

	var qrResult struct {
		Data struct {
			Code  int    `json:"code"`
			Token string `json:"token"`
			Image string `json:"image"` // Base64 PNG
		} `json:"data"`
		ErrorCode int `json:"error_code"`
	}
	if err := json.NewDecoder(resp4.Body).Decode(&qrResult); err != nil {
		return nil, fmt.Errorf("parse qr response: %w", err)
	}

	if qrResult.ErrorCode != 0 {
		return nil, fmt.Errorf("qr generate error %d", qrResult.ErrorCode)
	}

	qrToken := qrResult.Data.Token

	// ====================================
	// Bước 5: Poll waiting-scan
	// ====================================

	pollData := url.Values{}
	pollData.Set("token", qrToken)
	pollData.Set("imei", imei)
	pollData.Set("type", "30")

	scanOK := false
	for i := 0; i < 120; i++ { // 120 lần x 2 giây = 4 phút tối đa
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req5, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/waiting-scan", strings.NewReader(pollData.Encode()))
		req5.Header.Set("User-Agent", ua)
		req5.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req5.Header.Set("Referer", "https://id.zalo.me/")
		req5.Header.Set("Cookie", jar.String())

		resp5, err := client.Do(req5)
		if err != nil {
			return nil, fmt.Errorf("poll scan: %w", err)
		}

		var scanResult struct {
			ErrorCode int `json:"error_code"`
		}
		json.NewDecoder(resp5.Body).Decode(&scanResult)
		resp5.Body.Close()
		jar.SetFromHeader(resp5.Header)

		if scanResult.ErrorCode == 0 || scanResult.ErrorCode == 1 {
			scanOK = true
			break
		}
		// ErrorCode 8 = chưa scan, retry
		time.Sleep(2 * time.Second)
	}

	if !scanOK {
		return nil, fmt.Errorf("qr scan timeout")
	}

	// ====================================
	// Bước 6: Poll waiting-confirm
	// ====================================

	for i := 0; i < 60; i++ { // 60 lần x 2 giây = 2 phút
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req6, _ := http.NewRequestWithContext(ctx, "POST", "https://id.zalo.me/account/authen/qr/waiting-confirm", strings.NewReader(pollData.Encode()))
		req6.Header.Set("User-Agent", ua)
		req6.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req6.Header.Set("Referer", "https://id.zalo.me/")
		req6.Header.Set("Cookie", jar.String())

		resp6, err := client.Do(req6)
		if err != nil {
			return nil, fmt.Errorf("poll confirm: %w", err)
		}

		var confirmResult struct {
			ErrorCode int    `json:"error_code"`
			Message   string `json:"error_message"`
		}
		json.NewDecoder(resp6.Body).Decode(&confirmResult)
		resp6.Body.Close()
		jar.SetFromHeader(resp6.Header)

		if confirmResult.ErrorCode == 0 {
			break
		}
		if confirmResult.ErrorCode == -13 {
			return nil, fmt.Errorf("qr login declined on phone")
		}
		if confirmResult.ErrorCode == -14 {
			return nil, fmt.Errorf("qr expired")
		}
		time.Sleep(2 * time.Second)
	}

	// ====================================
	// Bước 7: Check session + get user info
	// ====================================

	req7, _ := http.NewRequestWithContext(ctx, "GET", "https://id.zalo.me/account/checksession", nil)
	req7.Header.Set("User-Agent", ua)
	req7.Header.Set("Cookie", jar.String())

	resp7, err := client.Do(req7)
	if err != nil {
		return nil, fmt.Errorf("check session: %w", err)
	}
	defer resp7.Body.Close()
	jar.SetFromHeader(resp7.Header)

	// ====================================
	// Bước 8: Cookie login → getLoginInfo + getServerInfo
	// ====================================

	return cookieLogin(ctx, jar.cookies, imei, ua)
}

// ====================================
// Cookie-based login
// ====================================

// LoginWithCookies đăng nhập bằng cookie có sẵn
func LoginWithCookies(ctx context.Context, cookies map[string]string, imei, ua string) (*LoginResult, error) {
	if ua == "" {
		ua = defaultUA
	}
	if imei == "" {
		imei = generateIMEI(ua)
	}
	return cookieLogin(ctx, cookies, imei, ua)
}

func cookieLogin(ctx context.Context, cookies map[string]string, imei, ua string) (*LoginResult, error) {
	// Tạo session context
	sess := &Session{
		IMEI:       imei,
		UserAgent:  ua,
		Language:   "vi",
		APIType:    30,
		APIVersion: 665,
	}

	// ====================================
	// GET getLoginInfo
	// ====================================

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
	// Set cookies
	cookieStr := ""
	for k, v := range cookies {
		if cookieStr != "" {
			cookieStr += "; "
		}
		cookieStr += k + "=" + v
	}
	req.Header.Set("Cookie", cookieStr)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getLoginInfo request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
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

	// Decrypt response data
	decrypted, err := DecodeAESCBC([]byte(*encResult.Enk), string(*loginResp.Data))
	if err != nil {
		return nil, fmt.Errorf("decrypt login data: %w", err)
	}

	var innerResp struct {
		ErrorCode    int              `json:"error_code"`
		ErrorMessage string           `json:"error_message"`
		Data         *json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &innerResp); err != nil {
		return nil, fmt.Errorf("parse inner login: %w", err)
	}

	type loginDataRaw struct {
		UID        string `json:"uid"`
		ZPWEnk     string `json:"zpw_enk"`
		ZPWSEK     string `json:"zpw_sek"`
		ZPWS       string `json:"zpw_ws"`
		ZPWService string `json:"zpw_service_map_v3"`
	}

	var data loginDataRaw
	if innerResp.Data != nil {
		if err := json.Unmarshal(*innerResp.Data, &data); err != nil {
			return nil, fmt.Errorf("parse login data: %w", err)
		}
	}

	sess.Cookies = cookies
	sess.SecretKey = data.ZPWEnk
	sess.UserID = data.UID

	// Parse zpw_ws (JSON array hoặc string)
	if data.ZPWS != "" {
		var wsURLs []string
		if err := json.Unmarshal([]byte(data.ZPWS), &wsURLs); err != nil {
			// Có thể là string đơn
			sess.WSURLs = []string{data.ZPWS}
		} else {
			sess.WSURLs = wsURLs
		}
	}

	sess.ExpiresAt = time.Now().Add(24 * time.Hour)

	return &LoginResult{
		Session: sess,
		Cookies: cookies,
	}, nil
}

// ====================================
// Helpers
// ====================================

// extractVersion tìm version từ HTML body
func extractVersion(body string) string {
	// Tìm main-{version}.js
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
