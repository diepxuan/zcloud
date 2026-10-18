package core

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// CloseEvent mô tả lý do WebSocket bị đóng.
type CloseEvent struct {
	Code   int
	Reason string
}

// ====================================
// WebSocket client cho Zalo real-time
// ====================================

// WSClient quản lý kết nối WebSocket tới Zalo
type WSClient struct {
	conn    *websocket.Conn
	connCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	msgChan   chan Event
	errChan   chan error
	closeChan chan CloseEvent
	authErr   error

	cipherKey   []byte // Key cho AES-GCM decrypt event data
	encryptMode byte    // WSEncryptAESGCM (2) | WSEncryptAESGCMRaw (3) | 0 = plain
	session     *Session
	url         string
	requestIDs  uint64
	pingStarted bool

	mu sync.Mutex
}

// NewWSClient tạo WebSocket client mới
func NewWSClient(session *Session) *WSClient {
	url := "wss://ws1-msg.chat.zalo.me"
	if len(session.WSURLs) > 0 && session.WSURLs[0] != "" {
		url = session.WSURLs[0]
	}

	return &WSClient{
		msgChan:    make(chan Event, 100),
		errChan:    make(chan error, 10),
		closeChan:  make(chan CloseEvent, 4),
		encryptMode: 0, // 0 = plain; AES-GCM set sau khi nhận cipherKey từ server.
		session:    session,
		url:        url,
	}
}

// Messages trả về channel nhận event
func (w *WSClient) Messages() <-chan Event {
	return w.msgChan
}

// Errors trả về channel nhận lỗi
func (w *WSClient) Errors() <-chan error {
	return w.errChan
}

// CloseEvents trả về channel nhận lý do đóng kết nối từ Zalo.
func (w *WSClient) CloseEvents() <-chan CloseEvent {
	return w.closeChan
}

// Connect kết nối WebSocket tới Zalo
func (w *WSClient) Connect(ctx context.Context) error {
	w.mu.Lock()
	session := w.session
	baseURL := w.url
	w.mu.Unlock()

	// Tạo header với cookies
	header := make(map[string]string)
	ua := session.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	header["User-Agent"] = ua
	header["Origin"] = "https://chat.zalo.me"
	header["Accept"] = "*/*"
	header["Accept-Language"] = "vi-VN,vi;q=0.9,en-US;q=0.8,en;q=0.7"
	header["Cookie"] = cookiesToString(session.Cookies)

	// Build query params
	query := fmt.Sprintf("zpw_ver=%d&zpw_type=%d&t=%d",
		session.APIVersion, session.APIType, time.Now().UnixMilli())

	wsURL := baseURL + "?" + query

	// Dial với header
	dialOpts := &websocket.DialOptions{
		HTTPHeader: stringMapToHTTP(header),
	}

	conn, _, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	slog.Info("ws_connected", "url", wsURL)

	w.mu.Lock()
	w.conn = conn
	w.connCtx, w.cancel = context.WithCancel(ctx)
	w.cipherKey = nil
	w.pingStarted = false
	w.authErr = nil
	w.wg.Add(1)
	w.mu.Unlock()

	// Start read loop. Vòng chờ key exchange bên dưới không được giữ w.mu,
	// nếu không readLoop sẽ không lock được để ghi cipherKey → deadlock.
	go w.readLoop()

	// Key exchange thường đến ngay sau handshake. Chờ tối đa 15s để listener
	// có cipher key trước khi nhận event được mã hoá.
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		ready := w.cipherKey != nil
		authErr := w.authErr
		w.mu.Unlock()
		if authErr != nil {
			return authErr
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ws key exchange: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("ws key exchange timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}

	return nil
}

// Close đóng kết nối WebSocket
func (w *WSClient) Close() error {
	w.mu.Lock()
	conn := w.conn
	cancel := w.cancel
	w.conn = nil
	w.cancel = nil
	w.cipherKey = nil
	w.pingStarted = false
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.CloseNow()
	}
	w.wg.Wait()
	return nil
}

// SendWS gửi binary frame theo protocol Zalo
func (w *WSClient) SendWS(ctx context.Context, cmd uint16, subCmd uint8, data map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("ws: not connected")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("ws marshal: %w", err)
	}

	// Nếu có cipherKey + encryptMode (PC), wrap payload thành envelope JSON
	// {data: base64(iv|aad|ct), encrypt: N} thay vì raw JSON — match PC protocol.
	if w.encryptMode >= WSEncryptAESGCM && len(w.cipherKey) > 0 {
		aad := make([]byte, 16)
		copy(aad, payload[:min(16, len(payload))])
		ct, encErr := EncodeAESGCM(w.cipherKey, aad, payload, w.encryptMode)
		if encErr != nil {
			return fmt.Errorf("ws encrypt: %w", encErr)
		}
		envelope, _ := json.Marshal(map[string]any{
			"data":    base64.StdEncoding.EncodeToString(ct),
			"encrypt": int(w.encryptMode),
		})
		payload = envelope
	}

	// Frame format: version(1) + cmd(2 LE) + subCmd(1) + payload
	frame := make([]byte, 4+len(payload))
	frame[0] = 1 // version
	binary.LittleEndian.PutUint16(frame[1:3], cmd)
	frame[3] = subCmd
	copy(frame[4:], payload)

	return w.conn.Write(ctx, websocket.MessageBinary, frame)
}

// encryptPayloadIfNeeded wrap payload thành envelope khi cần AES-GCM.
// Trả về payload đã wrap (hoặc payload gốc nếu không encrypt).

func (w *WSClient) SendWSWithID(ctx context.Context, cmd uint16, subCmd uint8, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["req_id"] = fmt.Sprintf("req_%d", w.nextRequestID())
	return w.SendWS(ctx, cmd, subCmd, data)
}

func (w *WSClient) nextRequestID() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requestIDs++
	return w.requestIDs
}

// handleDesktopSync ghi nhận các frame cross-device/backup. Payload thật phụ
// thuộc trạng thái PC bundle, chưa đủ thông tin để parse thành message model.
func (w *WSClient) handleDesktopSync(payload []byte, cmd uint16, subCmd uint8) {
	dec := w.decryptPayload(payload)
	// Dispatch structured event để caller (api/ws.go) có thể hook vào backup
	// flow. Data để dạng RawMessage (JSON parse) để schema chi tiết có thể
	// được reverse thêm mà không phá Event struct.
	evType := CmdToEventType(cmd, subCmd)
	var raw json.RawMessage
	if err := json.Unmarshal(dec, &raw); err != nil {
		raw = dec
	}
	select {
	case w.msgChan <- Event{Type: evType, Message: nil, DesktopSync: &DesktopSyncEvent{
		Cmd: cmd, SubCmd: subCmd, Type: evType, RawData: raw,
	}}:
	default:
		// channel đầy — bỏ qua, không block WS read loop.
	}
	slog.Debug("ws_desktop_sync", "cmd", cmd, "sub_cmd", subCmd, "len", len(dec), "payload", truncateForLog(dec, 300))
}

// ====================================
// Read loop
// ====================================

func (w *WSClient) readLoop() {
	defer w.wg.Done()
	defer close(w.msgChan)
	defer close(w.errChan)
	defer close(w.closeChan)

	// Zalo dùng WebSocket close frame với các mã đóng tuỳ biến; không nên gọi
	// CloseRead vì nó chuyển các close frame thành lỗi không kèm CloseStatus.

	for {
		select {
		case <-w.connCtx.Done():
			return
		default:
		}

		_, msg, err := w.conn.Read(w.connCtx)
		if err != nil {
			w.handleReadError(err)
			return
		}

		slog.Debug("ws_frame", "len", len(msg), "first", fmt.Sprintf("%x", msg[:min(8, len(msg))]))
		w.handleFrame(msg)
	}
}

func (w *WSClient) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.connCtx.Done():
			return
		case <-ticker.C:
			w.SendWSWithID(w.connCtx, 2, 1, map[string]any{
				"eventId": time.Now().UnixMilli(),
			})
		}
	}
}

func (w *WSClient) handleReadError(err error) {
	closeErr := &websocket.CloseError{}
	if errors.As(err, &closeErr) {
		ce := CloseEvent{Code: int(closeErr.Code), Reason: closeErr.Reason}
		slog.Info("ws_closed", "code", ce.Code, "reason", ce.Reason)
		select {
		case w.closeChan <- ce:
		default:
		}
		select {
		case w.errChan <- fmt.Errorf("ws closed: code=%d reason=%q", ce.Code, ce.Reason):
		default:
		}
		return
	}

	slog.Error("ws_read_error", "error", err)
	select {
	case w.errChan <- fmt.Errorf("ws read: %w", err):
	default:
	}
}

// handleFrame xử lý binary frame từ Zalo
func (w *WSClient) handleFrame(data []byte) {
	if len(data) < 4 {
		return
	}

	cmd := binary.LittleEndian.Uint16(data[1:3])
	subCmd := data[3]
	payload := data[4:]

	switch {
	case cmd == 1 && subCmd == 1:
		// Key exchange — lưu cipherKey
		var keyMsg struct {
			Key       string `json:"key"`
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_message"`
		}
		if err := json.Unmarshal(payload, &keyMsg); err == nil && keyMsg.Key != "" {
			slog.Debug("ws_auth_key_received", "key_len", len(keyMsg.Key), "transport", w.session.Transport)
			w.mu.Lock()
			decoded, decErr := base64.StdEncoding.DecodeString(keyMsg.Key)
			if decErr == nil && len(decoded) > 0 {
				w.cipherKey = decoded
			} else {
				w.cipherKey = []byte(keyMsg.Key)
			}
			// PC transport: bật AES-GCM encrypt cho cả send/receive.
			if w.session.Transport == "pc" {
				w.encryptMode = WSEncryptAESGCMRaw
			}
			w.mu.Unlock()
			w.mu.Lock()
			if !w.pingStarted {
				w.pingStarted = true
				w.mu.Unlock()
				go w.pingLoop()
			} else {
				w.mu.Unlock()
			}
			return
		}
		if keyMsg.ErrorCode != 0 || keyMsg.ErrorMsg != "" {
			w.mu.Lock()
			w.authErr = fmt.Errorf("ws auth error %d: %s", keyMsg.ErrorCode, keyMsg.ErrorMsg)
			w.mu.Unlock()
			slog.Error("ws_auth_error", "error_code", keyMsg.ErrorCode, "error_message", keyMsg.ErrorMsg)
			_ = w.conn.CloseNow()
			return
		}

	case cmd == 501 && subCmd == 0:
		// New user messages
		w.handleNewMessages(payload, ThreadUser)

	case cmd == 521 && subCmd == 0:
		// New group messages
		w.handleNewMessages(payload, ThreadGroup)

	case cmd == 502 && subCmd == 0:
		w.handleMessageStatus(payload, ThreadUser)

	case cmd == 522 && subCmd == 0:
		w.handleMessageStatus(payload, ThreadGroup)

	case cmd == 601 && subCmd == 0:
		w.handleControl(payload)

	case cmd == 602 && subCmd == 0:
		w.handleActions(payload)

	case cmd == 510 && subCmd == 1:
		w.handleOldMessages(payload, ThreadUser)

	case cmd == 511 && subCmd == 1:
		w.handleOldMessages(payload, ThreadGroup)

	case (cmd == 510 || cmd == 511) && subCmd == 0:
		w.handleOldMessages(payload, ThreadUser)
		if cmd == 511 {
			w.handleOldMessages(payload, ThreadGroup)
		}

	case cmd == 2 && subCmd == 1:
		// Ping — send pong
		w.SendWSWithID(w.connCtx, 2, 2, map[string]any{
			"eventId": time.Now().UnixMilli(),
		})

	case cmd == 2 && subCmd == 2:
		// Pong nhận từ server, giữ kết nối.

	case cmd >= 590 && cmd <= 592:
		w.handleDesktopSync(data, cmd, subCmd)

	case cmd >= 630 && cmd <= 634:
		w.handleDesktopSync(data, cmd, subCmd)

	case cmd == 610 && subCmd == 1:
		w.handleReactions(payload, ThreadUser)

	case cmd == 611 && subCmd == 1:
		w.handleReactions(payload, ThreadGroup)

	case cmd == 612 && subCmd == 0:
		w.handleReactions(payload, ThreadUser)
		w.handleReactions(payload, ThreadGroup)

	case cmd == 3000:
		// Duplicate connection - close ngay để listener không tự reconnect.
		slog.Warn("ws_duplicate_connection")
		select {
		case w.errChan <- fmt.Errorf("ws: duplicate connection detected"):
		default:
		}
		if w.conn != nil {
			_ = w.conn.CloseNow()
		}
	}
}

// handleNewMessages parse và emit new message events
// RequestOldMessages gửi yêu cầu lấy tin nhắn cũ qua WebSocket
func (w *WSClient) RequestOldMessages(ctx context.Context, tt ThreadType, lastMsgID string) error {
	cmd := uint16(510)
	if tt == ThreadGroup {
		cmd = 511
	}
	lastId := lastMsgID
	if lastId == "" {
		lastId = "10000000000000000"
	} // Giá trị lớn để lấy tin mới nhất
	data := map[string]any{
		"first":  true,
		"lastId": lastId,
		"preIds": []string{},
	}
	return w.SendWSWithID(ctx, cmd, 1, data)
}

// encryptPayloadIfNeeded wrap payload thành envelope khi cần AES-GCM.
func (w *WSClient) encryptPayloadIfNeeded(payload []byte) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.encryptMode < WSEncryptAESGCM || len(w.cipherKey) == 0 {
		return payload, nil
	}
	aad := make([]byte, 16)
	copy(aad, payload[:min(16, len(payload))])
	ct, err := EncodeAESGCM(w.cipherKey, aad, payload, w.encryptMode)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"data":    base64.StdEncoding.EncodeToString(ct),
		"encrypt": int(w.encryptMode),
	})
}
func (w *WSClient) decryptPayload(payload []byte) []byte {
	w.mu.Lock()
	ck := w.cipherKey
	w.mu.Unlock()
	if len(ck) > 0 {
		if dec, err := DecodeWSEvent(payload, ck); err == nil && len(dec) > 0 {
			return dec
		} else if err != nil {
			slog.Error("ws_decrypt_failed", "error", err, "key_len", len(ck))
		}
	} else {
	}
	return payload
}

// Helpers
// ====================================

func stringMapToHTTP(h map[string]string) http.Header {
	hh := make(http.Header)
	for k, v := range h {
		hh.Set(k, v)
	}
	return hh
}

func firstNonEmpty(values ...interface{}) string {
	for _, v := range values {
		switch x := v.(type) {
		case string:
			if x != "" {
				return x
			}
		case fmt.Stringer:
			if s := x.String(); s != "" {
				return s
			}
		default:
			if x != nil {
				if s := fmt.Sprintf("%v", x); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// skipZero coi sentinel "0" của Zalo như giá trị rỗng.
func skipZero(v string) string {
	if v == "0" {
		return ""
	}
	return v
}

// firstNonEmptyID trả về id đầu tiên khác rỗng và khác sentinel "0".
func firstNonEmptyID(values ...string) string {
	for _, v := range values {
		if s := skipZero(v); s != "" {
			return s
		}
	}
	return ""
}

// truncateForLog cắt payload log cho gọn (dùng bởi handleDesktopSync).
func truncateForLog(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
