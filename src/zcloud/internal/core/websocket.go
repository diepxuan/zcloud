package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ====================================
// WebSocket client cho Zalo real-time
// ====================================

// WSClient quản lý kết nối WebSocket tới Zalo
type WSClient struct {
	conn      *websocket.Conn
	connCtx   context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	msgChan   chan Event
	errChan   chan error

	cipherKey []byte // Key cho AES-GCM decrypt event data
	session   *Session
	url       string

	mu        sync.Mutex
}

// NewWSClient tạo WebSocket client mới
func NewWSClient(session *Session) *WSClient {
	url := "wss://wpa.chat.zalo.me"
	if len(session.WSURLs) > 0 {
		url = session.WSURLs[0]
	}

	return &WSClient{
		msgChan:   make(chan Event, 100),
		errChan:   make(chan error, 10),
		session:   session,
		url:       url,
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

// Connect kết nối WebSocket tới Zalo
func (w *WSClient) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Tạo header với cookies
	header := make(map[string]string)
	header["User-Agent"] = w.session.UserAgent
	if w.session.UserAgent == "" {
		header["User-Agent"] = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36"
	}
	header["Cookie"] = cookiesToString(w.session.Cookies)

	// Build query params
	query := fmt.Sprintf("zpw_ver=%d&zpw_type=%d&t=%d",
		w.session.APIVersion, w.session.APIType, time.Now().UnixMilli())

	wsURL := w.url + "?" + query

	// Dial với header
	dialOpts := &websocket.DialOptions{
		HTTPHeader: stringMapToHTTP(header),
	}

	conn, _, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	w.conn = conn
	w.connCtx, w.cancel = context.WithCancel(ctx)

	// Start read loop
	w.wg.Add(1)
	go w.readLoop()

	return nil
}

// Close đóng kết nối WebSocket
func (w *WSClient) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}
	if w.conn != nil {
		w.conn.Close(websocket.StatusNormalClosure, "client closing")
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

	// Frame format: version(1) + cmd(2 LE) + subCmd(1) + payload
	frame := make([]byte, 4+len(payload))
	frame[0] = 1 // version
	binary.LittleEndian.PutUint16(frame[1:3], cmd)
	frame[3] = subCmd
	copy(frame[4:], payload)

	return w.conn.Write(ctx, websocket.MessageBinary, frame)
}

// ====================================
// Read loop
// ====================================

func (w *WSClient) readLoop() {
	defer w.wg.Done()
	defer close(w.msgChan)
	defer close(w.errChan)

	for {
		select {
		case <-w.connCtx.Done():
			return
		default:
		}

		_, msg, err := w.conn.Read(w.connCtx)
		if err != nil {
			select {
			case w.errChan <- fmt.Errorf("ws read: %w", err):
			default:
			}
			return
		}

		w.handleFrame(msg)
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
			Key string `json:"key"`
		}
		if err := json.Unmarshal(payload, &keyMsg); err == nil && keyMsg.Key != "" {
			w.mu.Lock()
			w.cipherKey = []byte(keyMsg.Key)
			w.mu.Unlock()
		}

	case cmd == 501 && subCmd == 0:
		// New user messages
		w.handleNewMessages(payload, ThreadUser)

	case cmd == 521 && subCmd == 0:
		// New group messages
		w.handleNewMessages(payload, ThreadGroup)

	case cmd == 510 && subCmd == 1:
		// Old user messages
		w.handleOldMessages(payload, ThreadUser)

	case cmd == 511 && subCmd == 1:
		// Old group messages
		w.handleOldMessages(payload, ThreadGroup)

	case cmd == 2 && subCmd == 1:
		// Ping — send pong
		w.SendWS(w.connCtx, 2, 2, map[string]any{
			"eventId": time.Now().UnixMilli(),
		})

	case cmd == 3000:
		// Duplicate connection
		select {
		case w.errChan <- fmt.Errorf("ws: duplicate connection detected"):
		default:
		}
	}
}

// handleNewMessages parse và emit new message events
// RequestOldMessages gửi yêu cầu lấy tin nhắn cũ qua WebSocket
func (w *WSClient) RequestOldMessages(ctx context.Context, tt ThreadType, lastMsgID string) error {
	cmd := uint16(510)
	if tt == ThreadGroup { cmd = 511 }
	data := map[string]any{
		"first":  true,
		"lastId": lastMsgID,
		"preIds": []string{},
	}
	return w.SendWS(ctx, cmd, 1, data)
}

func (w *WSClient) handleNewMessages(payload []byte, tt ThreadType) {
	// Zalo WebSocket messages có thể là plain JSON hoặc encrypted
	// Try parse như JSON trước
	var rawData struct {
		Msgs json.RawMessage `json:"msgs"`
	}
	if err := json.Unmarshal(payload, &rawData); err != nil {
		return
	}

	if len(rawData.Msgs) > 0 {
		msg := Event{
			Type: EventNewMessage,
		}
		var msgs []struct {
			MsgID     string          `json:"msgId"`
			Content   string          `json:"content"`
			FromUID   string          `json:"fromUid"`
			ConvID    string          `json:"convId"`
			Timestamp int64           `json:"timestamp"`
			Type      int             `json:"type"`
			DName     string          `json:"dName"`
		}
			if err := json.Unmarshal(rawData.Msgs, &msgs); err == nil && len(msgs) > 0 {
				for _, m := range msgs {
					msg.Message = &Message{
						ID:        m.MsgID,
						FromID:    m.FromUID,
						FromName:  m.DName,
						Content:   m.Content,
						Timestamp: m.Timestamp,
						Type:      MsgType(m.Type),
					}
					// Emit message
					select {
					case w.msgChan <- msg:
					default:
					}
				}
			}
	}
}

// handleOldMessages xử lý cmd 510/511 (old messages response)
func (w *WSClient) handleOldMessages(payload []byte, tt ThreadType) {
	var rawData struct {
		Msgs      json.RawMessage `json:"msgs"`
		GroupMsgs json.RawMessage `json:"groupMsgs"`
	}
	if err := json.Unmarshal(payload, &rawData); err != nil { return }
	msgsRaw := rawData.Msgs
	if tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	if len(msgsRaw) == 0 { return }
	var msgs []struct {
		MsgID     string `json:"msgId"`
		Content   string `json:"content"`
		FromUID   string `json:"fromUid"`
		ConvID    string `json:"convId"`
		Timestamp int64  `json:"timestamp"`
		Type      int    `json:"type"`
		DName     string `json:"dName"`
	}
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil || len(msgs) == 0 { return }
	evt := Event{Type: EventOldMessages}
	for _, m := range msgs {
		evt.Message = &Message{
			ID: m.MsgID, FromID: m.FromUID, FromName: m.DName,
			Content: m.Content, Timestamp: m.Timestamp, Type: MsgType(m.Type),
		}
		select { case w.msgChan <- evt: default: }
	}
}

// ====================================
// Helpers
// ====================================

func stringMapToHTTP(h map[string]string) http.Header {
	hh := make(http.Header)
	for k, v := range h {
		hh.Set(k, v)
	}
	return hh
}
