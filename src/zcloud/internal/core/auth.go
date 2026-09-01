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
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

type QRLoginSession struct {
	Code     string
	ImageB64 string
	Version  string
	IMEI     string
	client   *http.Client
	jar      *cookiejar.Jar
}

type LoginResult struct {
	Session *Session
	Cookies map[string]string
}

// ServerInfo chứa cấu hình server trả về từ /api/login/getServerInfo.
// Zalo có thể trả key "settings" hoặc "setttings", nên parse linh hoạt.
type ServerInfo struct {
	Settings  json.RawMessage `json:"settings"`
	Setttings json.RawMessage `json:"setttings"`
	ExtraVer  json.RawMessage `json:"extra_ver"`
}

func generateIMEI(ua string) string {
	b := make([]byte, 16)
	rand.Read(b)
	uuid := hex.EncodeToString(b)
	uuid = uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:]
	sum := md5.Sum([]byte(ua))
	return strings.ToUpper(uuid) + "-" + hex.EncodeToString(sum[:])[:12]
}

// newZaloClient tạo HTTP client với cookie jar chuẩn
func newZaloClient() (*http.Client, *cookiejar.Jar) {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}, jar
}

func get(ctx context.Context, client *http.Client, urlStr string, headers http.Header) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	req.Header = headers
	return client.Do(req)
}

func postForm(ctx context.Context, client *http.Client, urlStr string, headers http.Header, data url.Values) (*http.Response, error) {
	body := strings.NewReader(data.Encode())
	req, _ := http.NewRequestWithContext(ctx, "POST", urlStr, body)
	if headers != nil {
		for k, v := range headers {
			req.Header[k] = v
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.Do(req)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ====================================
// Bước 1: Tạo QR code
// ====================================

const defaultQRVersion = "5.5.7"

func CreateQRLogin(ctx context.Context) (*QRLoginSession, error) {
	client, jar := newZaloClient()
	imei := generateIMEI(defaultUA)

	// Headers giống browser cho GET login page
	bh := http.Header{}
	bh.Set("User-Agent", defaultUA)
	bh.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	bh.Set("Accept-Language", "vi-VN,vi;q=0.9")
	bh.Set("Sec-CH-UA", `"Chromium";v="130"`)
	bh.Set("Sec-CH-UA-Mobile", "?0")
	bh.Set("Sec-CH-UA-Platform", `"Windows"`)

	get(ctx, client, "https://id.zalo.me/account?continue=https%3A%2F%2Fchat.zalo.me%2F", bh)

	// Headers cho AJAX requests (QR generate, poll)
	ah := http.Header{}
	ah.Set("User-Agent", defaultUA)
	ah.Set("Accept", "*/*")
	ah.Set("Accept-Language", "vi-VN,vi;q=0.9")
	ah.Set("Sec-CH-UA", `"Chromium";v="130"`)
	ah.Set("Sec-CH-UA-Mobile", "?0")
	ah.Set("Sec-CH-UA-Platform", `"Windows"`)
	ah.Set("Sec-Fetch-Dest", "empty")
	ah.Set("Sec-Fetch-Mode", "cors")
	ah.Set("Sec-Fetch-Site", "same-origin")
	ah.Set("Referer", "https://id.zalo.me/account?continue=https%3A%2F%2Fzalo.me%2Fpc")

	postForm(ctx, client, "https://id.zalo.me/account/logininfo", ah,
		url.Values{"v": {defaultQRVersion}, "continue": {"https://zalo.me/pc"}})
	postForm(ctx, client, "https://id.zalo.me/account/verify-client", ah,
		url.Values{"v": {defaultQRVersion}, "type": {"device"}, "continue": {"https://zalo.me/pc"}})

	resp4, err := postForm(ctx, client, "https://id.zalo.me/account/authen/qr/generate", ah,
		url.Values{"v": {defaultQRVersion}, "continue": {"https://zalo.me/pc"}})
	if err != nil {
		return nil, fmt.Errorf("generate qr: %w", err)
	}

	body4, _ := readBody(resp4)
	var qrResp struct {
		Data struct {
			Code  string `json:"code"`
			Image string `json:"image"`
		} `json:"data"`
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_message"`
	}
	if err := json.Unmarshal(body4, &qrResp); err != nil {
		return nil, fmt.Errorf("parse qr: %w — body: %s", err, string(body4))
	}
	if qrResp.ErrorCode != 0 {
		return nil, fmt.Errorf("qr error %d: %s", qrResp.ErrorCode, qrResp.ErrorMsg)
	}

	image := strings.TrimPrefix(qrResp.Data.Image, "data:image/png;base64,")

	return &QRLoginSession{
		Code:     qrResp.Data.Code,
		ImageB64: image,
		Version:  defaultQRVersion,
		IMEI:     imei,
		client:   client,
		jar:      jar,
	}, nil
}

// ====================================
// Bước 2: Poll QR + Cookie login (dùng chung client/jar)
// ====================================

func PollQRLogin(ctx context.Context, session *QRLoginSession) (*LoginResult, error) {
	if session == nil {
		return nil, fmt.Errorf("qr session is nil")
	}
	client := session.client
	code := session.Code
	imei := session.IMEI
	version := session.Version
	if version == "" {
		version = defaultQRVersion
	}

	// Headers cho QR poll (giống zcago)
	ph := http.Header{}
	ph.Set("User-Agent", defaultUA)
	ph.Set("Accept", "*/*")
	ph.Set("Accept-Language", "vi-VN,vi;q=0.9,fr-FR;q=0.8,fr;q=0.7,en-US;q=0.6,en;q=0.5")
	ph.Set("Content-Type", "application/x-www-form-urlencoded")
	ph.Set("Sec-CH-UA", `"Chromium";v="130"`)
	ph.Set("Sec-CH-UA-Mobile", "?0")
	ph.Set("Sec-CH-UA-Platform", `"Windows"`)
	ph.Set("Sec-Fetch-Dest", "empty")
	ph.Set("Sec-Fetch-Mode", "cors")
	ph.Set("Sec-Fetch-Site", "same-origin")
	ph.Set("Referer", "https://id.zalo.me/account?continue=https%3A%2F%2Fchat.zalo.me%2F")

	if err := pollQRWaitingScan(ctx, client, ph, version, code); err != nil {
		return nil, err
	}
	if err := pollQRWaitingConfirm(ctx, client, ph, version, code); err != nil {
		return nil, err
	}

	// Check session — dùng browser-like headers
	ch := http.Header{}
	ch.Set("User-Agent", defaultUA)
	ch.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	ch.Set("Accept-Language", "vi-VN,vi;q=0.9")
	ch.Set("Sec-CH-UA", `"Chromium";v="130"`)
	ch.Set("Sec-CH-UA-Mobile", "?0")
	ch.Set("Sec-CH-UA-Platform", `"Windows"`)
	ch.Set("Sec-Fetch-Dest", "document")
	ch.Set("Sec-Fetch-Mode", "navigate")
	ch.Set("Sec-Fetch-Site", "same-origin")

	get(ctx, client, "https://id.zalo.me/account/checksession?continue=https%3A%2F%2Fchat.zalo.me%2Findex.html", ch)

	// User info
	uiHeaders := http.Header{}
	uiHeaders.Set("User-Agent", defaultUA)
	uiHeaders.Set("Accept", "*/*")
	uiHeaders.Set("Referer", "https://chat.zalo.me/")
	get(ctx, client, "https://jr.chat.zalo.me/jr/userinfo", uiHeaders)

	// Cookie login — dùng CHUNG client + jar
	return doCookieLogin(ctx, client, session.jar, imei)
}

func pollQRWaitingScan(ctx context.Context, client *http.Client, ph http.Header, version, code string) error {
	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := postForm(ctx, client, "https://id.zalo.me/account/authen/qr/waiting-scan", ph,
			url.Values{"v": {version}, "code": {code}, "continue": {"https://zalo.me/pc"}})
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		body, _ := readBody(resp)
		var result struct {
			ErrorCode int `json:"error_code"`
		}
		json.Unmarshal(body, &result)

		if result.ErrorCode == 0 {
			return nil
		}
		if i%10 == 0 {
			fmt.Printf("[zcloud] scan poll %d code=%d\n", i+1, result.ErrorCode)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("qr scan timed out")
}

func pollQRWaitingConfirm(ctx context.Context, client *http.Client, ph http.Header, version, code string) error {
	for i := 0; i < 90; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := postForm(ctx, client, "https://id.zalo.me/account/authen/qr/waiting-confirm", ph,
			url.Values{"v": {version}, "code": {code}, "gToken": {""}, "gAction": {"CONFIRM_QR"}, "continue": {"https://zalo.me/pc"}})
		if err != nil {
			return fmt.Errorf("confirm: %w", err)
		}

		body, _ := readBody(resp)
		var result struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_message"`
		}
		json.Unmarshal(body, &result)

		if result.ErrorCode == 0 {
			return nil
		}
		if result.ErrorCode == -13 {
			return fmt.Errorf("từ chối trên điện thoại")
		}
		if i%5 == 0 {
			fmt.Printf("[zcloud] confirm poll %d code=%d msg=%s\n", i+1, result.ErrorCode, result.ErrorMsg)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("qr confirm timed out")
}

// ====================================
// Cookie login — dùng client/jar có sẵn
// ====================================

func doCookieLogin(ctx context.Context, client *http.Client, jar *cookiejar.Jar, imei string) (*LoginResult, error) {
	sess := &Session{
		IMEI:       imei,
		UserAgent:  defaultUA,
		Language:   "vi",
		APIType:    30,
		APIVersion: 688,
	}

	encResult, err := encryptParamsForLogin(sess, true, "getlogininfo")
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}
	if encResult.Enk != nil {
		query.Set("enk", *encResult.Enk)
	}

	loginURL := "https://wpa.chat.zalo.me/api/login/getLoginInfo?" + query.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", loginURL, nil)
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.zalo.me")
	req.Header.Set("Referer", "https://chat.zalo.me/")

	// Dùng client có jar chứa cookies từ QR flow
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getLoginInfo: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var loginResp struct {
		ErrorCode    int              `json:"error_code"`
		ErrorMessage string           `json:"error_message"`
		Data         *json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return nil, fmt.Errorf("parse login: %w", err)
	}
	if loginResp.ErrorCode != 0 {
		return nil, fmt.Errorf("login error %d: %s", loginResp.ErrorCode, loginResp.ErrorMessage)
	}
	if loginResp.Data == nil || encResult.Enk == nil {
		return nil, fmt.Errorf("no data")
	}

	// Parse JSON string -> raw string -> URL unescape -> AES CBC decrypt
	var dataStr string
	if err := json.Unmarshal(*loginResp.Data, &dataStr); err != nil {
		return nil, fmt.Errorf("parse data string: %w", err)
	}
	decodedData, _ := url.PathUnescape(dataStr)
	decrypted, err := DecodeAESCBC([]byte(*encResult.Enk), decodedData)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w — raw: %s", err, decodedData[:min(100, len(decodedData))])
	}

	var innerResp struct {
		ErrorCode int             `json:"error_code"`
		Message   string          `json:"error_message"`
		Data      json.RawMessage `json:"data"`
	}
	json.Unmarshal(decrypted, &innerResp)

	if innerResp.ErrorCode != 0 {
		return nil, fmt.Errorf("login inner error %d: %s", innerResp.ErrorCode, innerResp.Message)
	}
	if innerResp.Data == nil {
		return nil, fmt.Errorf("login: no data in inner response")
	}

	type loginDataRaw struct {
		UID           string          `json:"uid"`
		ZPWEnk        string          `json:"zpw_enk"`
		ZPWS          json.RawMessage `json:"zpw_ws"`
		ZPWServiceMap json.RawMessage `json:"zpw_service_map_v3"`
	}
	var data loginDataRaw
	json.Unmarshal(innerResp.Data, &data)
	// Fallback: thử parse data dạng flat (nếu data là object chứa không phải nested)
	if data.UID == "" {
		var flat loginDataRaw
		json.Unmarshal(decrypted, &flat)
		if flat.UID != "" {
			data = flat
		}
	}

	sess.SecretKey = data.ZPWEnk
	sess.UserID = data.UID
	sess.ExpiresAt = time.Now().Add(24 * time.Hour)

	// Parse zpw_service_map_v3 để lấy URL các service
	if len(data.ZPWServiceMap) > 0 {
		var sm map[string][]string
		if err := json.Unmarshal(data.ZPWServiceMap, &sm); err == nil {
			sess.ServiceMap = sm
		} else {
			var smStr string
			if json.Unmarshal(data.ZPWServiceMap, &smStr) == nil && smStr != "" {
				_ = json.Unmarshal([]byte(smStr), &sm)
				if len(sm) > 0 {
					sess.ServiceMap = sm
				}
			}
		}
	}

	// Parse zpw_ws — WebSocket URLs
	if len(data.ZPWS) > 0 {
		var wsURLs []string
		if err := json.Unmarshal(data.ZPWS, &wsURLs); err == nil {
			sess.WSURLs = wsURLs
		} else {
			var wsStr string
			if json.Unmarshal(data.ZPWS, &wsStr) == nil && wsStr != "" {
				sess.WSURLs = []string{wsStr}
			}
		}
	}
	fmt.Printf("[zcloud] login OK — uid=%s\n", data.UID)

	// Get cookies từ jar
	domains := []string{"https://id.zalo.me/", "https://chat.zalo.me/", "https://wpa.chat.zalo.me/"}
	cookies := make(map[string]string)
	for _, d := range domains {
		u, _ := url.Parse(d)
		for _, c := range jar.Cookies(u) {
			cookies[c.Name] = c.Value
		}
	}
	sess.Cookies = cookies

	// Gọi getServerInfo sau khi có cookies để refresh settings.
	// Thất bại không chặn login; session vẫn dùng dữ liệu từ getLoginInfo.
	if si, err := GetServerInfo(ctx, sess, true); err == nil {
		if len(si.Settings) > 0 {
			sess.Settings = string(si.Settings)
		}
		if len(si.Setttings) > 0 {
			sess.Settings = string(si.Setttings)
		}
		if len(si.ExtraVer) > 0 {
			sess.ExtraVer = string(si.ExtraVer)
		}
	} else {
		fmt.Printf("[zcloud] getServerInfo failed (continue): %v\n", err)
	}

	fmt.Printf("[zcloud] login OK — uid=%s\n", data.UID)
	return &LoginResult{Session: sess, Cookies: cookies}, nil
}

// GetServerInfo lấy cấu hình server sau login. Params được tạo theo chuẩn
// Web: imei/type/client_version/computer_name + signkey MD5("zsecure"+type...).
func GetServerInfo(ctx context.Context, sc SessionContext, encrypt bool) (*ServerInfo, error) {
	encResult, err := encryptParamsForLogin(sc, encrypt, "getserverinfo")
	if err != nil {
		return nil, fmt.Errorf("getServerInfo encrypt: %w", err)
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}

	// Dùng client có sẵn (nếu là *Session) để mang cookies từ login flow.
	var client *http.Client
	if s, ok := sc.(*Session); ok && s != nil {
		jar, _ := cookiejar.New(nil)
		domains := []string{"https://chat.zalo.me/", "https://wpa.chat.zalo.me/", "https://wpa.zaloapp.com/"}
		for _, d := range domains {
			u, _ := url.Parse(d)
			cks := make([]*http.Cookie, 0, len(s.Cookies))
			for k, v := range s.Cookies {
				cks = append(cks, &http.Cookie{Name: k, Value: v, Path: "/"})
			}
			jar.SetCookies(u, cks)
		}
		client = &http.Client{Timeout: 30 * time.Second, Jar: jar}
	} else {
		client = http.DefaultClient
	}

	u := "https://wpa.chat.zalo.me/api/login/getServerInfo?" + query.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.zalo.me")
	req.Header.Set("Referer", "https://chat.zalo.me/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getServerInfo request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var raw struct {
		ErrorCode    int              `json:"error_code"`
		ErrorMessage string           `json:"error_message"`
		Data         *json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("getServerInfo parse: %w", err)
	}
	if raw.ErrorCode != 0 {
		return nil, fmt.Errorf("getServerInfo error %d: %s", raw.ErrorCode, raw.ErrorMessage)
	}
	if raw.Data == nil {
		return nil, fmt.Errorf("getServerInfo: empty data")
	}

	var si ServerInfo
	if err := json.Unmarshal(*raw.Data, &si); err != nil {
		return nil, fmt.Errorf("getServerInfo data: %w", err)
	}
	return &si, nil
}

// ====================================
// CookieLogin — cho trường hợp login bằng cookie có sẵn
// ====================================

func CookieLogin(ctx context.Context, cookies map[string]string, imei, ua string) (*LoginResult, error) {
	if imei == "" {
		imei = generateIMEI(ua)
	}
	if ua == "" {
		ua = defaultUA
	}

	client, jar := newZaloClient()

	// Inject cookies vào jar
	domains := []string{
		"https://id.zalo.me/",
		"https://chat.zalo.me/",
		"https://wpa.chat.zalo.me/",
	}
	for _, d := range domains {
		u, _ := url.Parse(d)
		cks := make([]*http.Cookie, 0, len(cookies))
		for k, v := range cookies {
			cks = append(cks, &http.Cookie{Name: k, Value: v, Path: "/"})
		}
		jar.SetCookies(u, cks)
	}

	return doCookieLogin(ctx, client, jar, imei)
}

// ====================================
// Helpers (giữ lại cho tương thích)
// ====================================

type cookieJar struct{ cookies map[string]string }

func newCookieJar() *cookieJar { return &cookieJar{cookies: make(map[string]string)} }
func (j *cookieJar) Set(c map[string]string) {
	for k, v := range c {
		j.cookies[k] = v
	}
}
func (j *cookieJar) SetFromHeader(h http.Header) {}
func (j *cookieJar) String() string              { return "" }
func (j *cookieJar) Apply(req *http.Request)     {}
func cookiesToString(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}
func extractVersion(body string) string {
	idx := strings.Index(body, "main-")
	if idx < 0 {
		return "688"
	}
	body = body[idx+5:]
	end := strings.IndexAny(body, ".\"")
	if end < 0 {
		return "688"
	}
	return body[:end]
}
