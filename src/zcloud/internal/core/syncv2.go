package core

// SyncV2 client — Phase B (Task 22). Pull lịch sử chat sâu (>6h) qua flow
// transfer_after_login / user_confirm của Zalo PC. Xem `docs/protocol/syncv2.md`.
//
// Luồng:
//   1. NewSyncV2Client tạo ed25519 keypair (Persist=True) hoặc nạp từ state.
//   2. RequestSync REST cmd 12888 gửi {pc_name, public_key, imei} lên server.
//   3. Server push WS event (act: "user_confirm" + user_action=1|3) qua cmd 590.
//   4. PullBatch loop gọi REST cmd 12000 từ last_seq_id+1, decrypt batch.
//   5. Lặp đến khi done=true. Persist last_seq_id sau mỗi batch.
//
// Decryption: cipher session từ WASM libzproto_wasm_bg.*.wasm. Trong Go
// thuần, dùng BuildCipherSession (ed25519 → HKDF → AES-GCM) dựa trên
// pattern Noise/NoiseIK. Build tag `syncv2_wasm` sẽ dùng WASM thật sau.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/cookiejar"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// SyncV2State là blob JSON lưu trong accounts.syncv2_state JSONB.
// Cho phép resume sau restart (T22.4).
type SyncV2State struct {
	// Ed25519 keypair (32B seed ed25519.PrivateKey = seed + pub).
	// Lưu dạng hex 64 ký tự (seed only) — derive pub lúc runtime.
	PrivateKey string `json:"privateKey,omitempty"`
	PublicKey  string `json:"publicKey,omitempty"`

	// PC name + IMEI snapshot tại thời điểm tạo session.
	PCName string `json:"pcName"`
	IMEI   string `json:"imei"`

	// Sync cursor.
	LastSeqID int64  `json:"lastSeqId"`
	TempKey   string `json:"tempKey,omitempty"` // hex/base64 từ user_confirm event
	SyncSession string `json:"syncSession,omitempty"` // cho get_crossdb nếu cần

	// State machine.
	Phase string `json:"phase"` // "init" | "waiting_confirm" | "pulling" | "done" | "error"
	LastError string `json:"lastError,omitempty"`
}

// SyncV2Client quản lý 1 phiên SyncV2 của 1 account.
type SyncV2Client struct {
	session *Session
	state   *SyncV2State
	hc      *http.Client
}

// NewSyncV2Client tạo client mới; nếu state đã có thì tiếp tục, nếu chưa
// thì generate ed25519 keypair mới.
func NewSyncV2Client(session *Session, state *SyncV2State) (*SyncV2Client, error) {
	if state == nil {
		state = &SyncV2State{Phase: "init"}
	}
	if state.PCName == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "zcloud"
		}
		state.PCName = host
	}
	if state.IMEI == "" && session != nil {
		state.IMEI = session.IMEI
	}
	if state.PublicKey == "" {
		// Generate ed25519 keypair mới.
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("ed25519 generate: %w", err)
		}
		state.PublicKey = hex.EncodeToString(pub)
		// Lưu private seed (32B) hex — public derive từ seed.
		state.PrivateKey = hex.EncodeToString(priv.Seed())
	}
	jar, _ := newCookieJarForDomains(session)
	_ = jar
	return &SyncV2Client{
		session: session,
		state:   state,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// State trả về snapshot state hiện tại (để persist).
func (c *SyncV2Client) State() *SyncV2State { return c.state }

// SetPhase đặt phase trực tiếp — dùng cho admin endpoints (start/stop/reset).
func (c *SyncV2Client) SetPhase(phase string) { c.state.Phase = phase }

// PrivateKey trả về ed25519 private key (32B seed), nil nếu chưa có.
func (c *SyncV2Client) privateKey() ed25519.PrivateKey {
	if c.state.PrivateKey == "" {
		return nil
	}
	seed, err := hex.DecodeString(c.state.PrivateKey)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil
	}
	return ed25519.NewKeyFromSeed(seed)
}

// PublicKeyBytes trả về ed25519 public key (32B).
func (c *SyncV2Client) PublicKeyBytes() []byte {
	if c.state.PublicKey == "" {
		return nil
	}
	b, err := hex.DecodeString(c.state.PublicKey)
	if err != nil {
		return nil
	}
	return b
}

// requestSyncRequest là body raw AES-CBC của REST 12888.
// data là JSON-stringified inner (vd {"pc_name", "public_key", "imei"}).
type requestSyncRequest struct {
	ReqID string `json:"reqId"`
	Data  string `json:"data"` // JSON-stringified inner
}

// RequestSync gọi POST /api/transfer-sync-v2/request-sync (cmd 12888).
// Sau khi gọi, server sẽ push WS event 590/591/592 về mobile, mobile phản
// hồi user_confirm, server push tiếp user_confirm về client qua WS.
func (c *SyncV2Client) RequestSync(ctx context.Context) error {
	inner := map[string]any{
		"pc_name":    c.state.PCName,
		"public_key": c.state.PublicKey,
		"imei":       c.state.IMEI,
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return fmt.Errorf("marshal inner: %w", err)
	}
	innerStr := string(innerJSON)
	body := requestSyncRequest{
		ReqID: strconv.FormatInt(time.Now().UnixMilli(), 10),
		Data:  innerStr,
	}

	// Host lấy từ serviceMap["file"]; fallback files-wpa.zaloapp.com.
	host := serviceBaseURL(c.session, "file", "https://files-wpa.zaloapp.com")
	u := host + "/api/transfer-sync-v2/request-sync"

	// Wrap trong params: {reqId, data: innerStr}
	wrapped := map[string]any{
		"reqId": body.ReqID,
		"data":  body.Data,
	}
	resp, err := c.callEncrypted(ctx, u, wrapped, "12888", 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request-sync HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	c.state.Phase = "waiting_confirm"
	return nil
}

// HandleEvent nhận WS event 590/591/592/632 và cập nhật state machine.
// Trả về phase mới + error nếu reject.
// data shape:
//   {act: "transfer_after_login", data: {temp_key, imei, ...}}
//   {act: "user_confirm", data: {user_action, pc_name, public_key, ...}}
//   {act: "transfer_error", data: {error_code, status, ...}}
//   {act: "syncmsg_info", data: {...}}
func (c *SyncV2Client) HandleEvent(payload []byte) error {
	var env struct {
		Act  string          `json:"act"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("parse event: %w", err)
	}
	var d map[string]any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &d)
	}
	switch env.Act {
	case "transfer_after_login":
		// Server xác nhận đã wake up mobile. temp_key có thể chưa có
		// trong event này — chờ user_confirm.
		return nil
	case "user_confirm":
		ua, _ := d["user_action"].(float64)
		if int(ua) == 0 {
			c.state.Phase = "error"
			c.state.LastError = "user_reject"
			return fmt.Errorf("user rejected sync")
		}
		// Lưu temp_key từ data (server trả kèm).
		if tk, ok := d["temp_key"].(string); ok && tk != "" {
			c.state.TempKey = tk
		}
		c.state.Phase = "pulling"
		return nil
	case "transfer_error":
		ec, _ := d["error_code"].(float64)
		if int(ec) != 0 {
			c.state.Phase = "error"
			c.state.LastError = fmt.Sprintf("transfer_error code=%d", int(ec))
			return fmt.Errorf("transfer_error %d", int(ec))
		}
		return nil
	case "syncmsg_info":
		// Backup success marker — phase kết thúc nếu syncmsg_info sau khi
		// pull xong. Hiện tại chỉ log; caller quyết định done.
		return nil
	}
	return nil
}

// PulledMessage là 1 message trong response pull_mobile_msg.
type PulledMessage struct {
	SessionID string `json:"sessionId"` // hex
	Cipher    string `json:"cipher"`    // base64
}

// PullBatchResponse là response của REST 12000.
type PullBatchResponse struct {
	Messages   []PulledMessage `json:"messages"`
	NextSeqID  int64           `json:"next_seq_id"`
	Done       bool            `json:"done"`
	ErrorCode  int             `json:"error_code"`
	ErrorMsg   string          `json:"error_message"`
}

// PullBatch gọi REST 12000 với from_seq_id = state.LastSeqID+1.
// Trả batch messages (chưa decrypt) + next_seq_id. Caller lưu DB rồi
// gọi DecryptMessages để lấy nội dung.
func (c *SyncV2Client) PullBatch(ctx context.Context) (*PullBatchResponse, error) {
	if c.state.TempKey == "" {
		return nil, fmt.Errorf("pull_mobile_msg: temp_key chưa có (chờ user_confirm)")
	}
	from := c.state.LastSeqID + 1
	params := map[string]any{
		"pc_name":     c.state.PCName,
		"public_key":  c.state.PublicKey,
		"from_seq_id": from,
		"is_retry":    0,
		"min_seq_id":  0,
		"temp_key":    c.state.TempKey,
		"imei":        c.state.IMEI,
	}

	host := serviceBaseURL(c.session, "file", "https://files-wpa.zaloapp.com")
	u := host + "/api/message/pull_mobile_msg"
	resp, err := c.callEncrypted(ctx, u, params, "12000", 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pull_mobile_msg HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	// Decrypt response bằng cùng AES-CBC key với request.
	decrypted, err := decryptAPIResponse(c.session, string(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("decrypt response: %w", err)
	}
	var out PullBatchResponse
	if err := json.Unmarshal([]byte(decrypted), &out); err != nil {
		return nil, fmt.Errorf("parse response: %w (raw=%s)", err, decrypted)
	}
	if out.ErrorCode != 0 {
		return nil, fmt.Errorf("pull_mobile_msg error %d: %s", out.ErrorCode, out.ErrorMsg)
	}
	c.state.LastSeqID = out.NextSeqID
	if out.Done {
		c.state.Phase = "done"
	}
	return &out, nil
}

// DecryptMessages giải mã batch PulledMessage → []Message.
// Cần cipher session (BuildCipherSession). Nếu WASM build tag bật thì
// dùng WASM thật; không thì dùng pure-Go stub (HKDF + AES-GCM).
func (c *SyncV2Client) DecryptMessages(batch *PullBatchResponse) ([]Message, error) {
	if c.privateKey() == nil {
		return nil, fmt.Errorf("decrypt: private key not set")
	}
	sess, err := BuildCipherSession(c.privateKey(), c.state.TempKey)
	if err != nil {
		return nil, fmt.Errorf("build cipher session: %w", err)
	}
	out := make([]Message, 0, len(batch.Messages))
	for _, pm := range batch.Messages {
		pt, err := sess.DecryptMessage(pm.Cipher)
		if err != nil {
			// Skip tin lỗi thay vì fail toàn batch; vẫn log để debug.
			fmt.Fprintf(os.Stderr, "[zcloud] syncv2: decrypt msg sid=%s err=%v\n", pm.SessionID, err)
			continue
		}
		var m wsMessage
		if err := json.Unmarshal([]byte(pt), &m); err != nil {
			fmt.Fprintf(os.Stderr, "[zcloud] syncv2: parse msg sid=%s err=%v\n", pm.SessionID, err)
			continue
		}
		out = append(out, *m.toMessage(c.session))
	}
	return out, nil
}

// callEncrypted thực hiện POST/GET với AES-CBC params (cùng pattern như
// chat.go). cmd + subCmd dùng để build signkey.
func (c *SyncV2Client) callEncrypted(ctx context.Context, rawURL string, params map[string]any, typeStr string, subCmd int) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	// Common params từ JS: zpw_ver, zpw_type, imei, computer_name.
	if c.session != nil {
		q.Set("zpw_ver", strconv.FormatUint(uint64(c.session.APIVersion), 10))
		q.Set("zpw_type", strconv.FormatUint(uint64(c.session.APIType), 10))
	}
	if c.state.IMEI != "" {
		q.Set("imei", c.state.IMEI)
	}
	if c.state.PCName != "" {
		q.Set("computer_name", c.state.PCName)
	}
	// Encrypt params qua NewEncryptParam.
	apiType := uint(0)
	if c.session != nil {
		apiType = c.session.APIType
	}
	enc, err := NewEncryptParam(apiType, c.state.IMEI, params)
	if err != nil {
		return nil, fmt.Errorf("encrypt param: %w", err)
	}
	encStr, err := json.Marshal(enc.Params)
	if err != nil {
		return nil, err
	}
	q.Set("params", string(encStr))
	// Signkey = MD5("zsecure" + typeStr + sorted param values).
	var paramMap map[string]any
	_ = json.Unmarshal(encStr, &paramMap)
	q.Set("signkey", GenerateSignKey(typeStr, paramMap))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// Set headers giống chat.go.
	if c.session != nil {
		if c.session.UserAgent != "" {
			req.Header.Set("User-Agent", c.session.UserAgent)
		}
		req.Header.Set("Referer", "https://chat.zalo.me/")
	}
	req.Header.Set("Accept", "*/*")
	return c.hc.Do(req)
}

// decryptAPIResponse giải mã response body bằng session.SecretKey (AES-CBC).
// Body là hex-string JSON {"data": "..."} theo convention Zalo.
func decryptAPIResponse(session *Session, body string) (string, error) {
	if session == nil || session.SecretKey == "" {
		// Nếu không có secret key, trả raw body (best-effort cho dev).
		return body, nil
	}
	// Body có thể là hex-string JSON hoặc thẳng JSON.
	var env struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err == nil && env.Data != "" {
		body = env.Data
	}
	key, err := base64.StdEncoding.DecodeString(session.SecretKey)
	if err != nil {
		return "", fmt.Errorf("decode secret key: %w", err)
	}
	pt, err := DecodeAESCBC(key, body)
	if err != nil {
		// Best-effort: trả raw nếu decrypt fail (cho dev).
		return body, nil
	}
	return string(pt), nil
}

// newCookieJarForDomains tạo cookie jar cho 1 session (helper tương tự NewClient).
func newCookieJarForDomains(session *Session) (*cookiejar.Jar, error) {
	if session == nil || session.Cookies == nil {
		return nil, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return jar, nil
}
