package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// ====================================
// WebSocket Manager — quản lý các kết nối
// ====================================

// WSManager quản lý browser WebSocket connections + Zalo listeners
type WSManager struct {
	mu      sync.RWMutex
	rooms   map[string]map[*wsConn]bool // accountID → set of browser conns
}

type wsConn struct {
	conn  *websocket.Conn
	ctx   context.Context
	cancel context.CancelFunc
}

// BrowserMessage message từ/tới browser
type BrowserMessage struct {
	Type    string      `json:"type"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

var globalWS *WSManager

func init() {
	globalWS = &WSManager{
		rooms: make(map[string]map[*wsConn]bool),
	}
}

// AddConn thêm browser connection vào room
func (m *WSManager) AddConn(accountID string, conn *wsConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rooms[accountID] == nil {
		m.rooms[accountID] = make(map[*wsConn]bool)
	}
	m.rooms[accountID][conn] = true
}

// RemoveConn xoá browser connection
func (m *WSManager) RemoveConn(accountID string, conn *wsConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rooms[accountID] != nil {
		delete(m.rooms[accountID], conn)
		if len(m.rooms[accountID]) == 0 {
			delete(m.rooms, accountID)
		}
	}
}

// Broadcast gửi message đến tất cả browser connections của account
func (m *WSManager) Broadcast(accountID string, msg BrowserMessage) {
	m.mu.RLock()
	conns := m.rooms[accountID]
	m.mu.RUnlock()

	data, _ := json.Marshal(msg)
	for conn := range conns {
		conn.conn.Write(conn.ctx, websocket.MessageText, data)
	}
}

// ====================================
// HTTP → WebSocket handler cho browser
// ====================================

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		http.Error(w, "missing accountId", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.Logger.Printf("ws accept: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	wc := &wsConn{conn: conn, ctx: ctx, cancel: cancel}

	globalWS.AddConn(accountID, wc)
	s.Logger.Printf("ws: browser connected — account=%s", accountID)

	// Lắng nghe message từ browser (ping)
	go func() {
		defer func() {
			globalWS.RemoveConn(accountID, wc)
			cancel()
			conn.Close(websocket.StatusNormalClosure, "bye")
			s.Logger.Printf("ws: browser disconnected — account=%s", accountID)
		}()

		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}()

	// Start Zalo listener nếu chưa có
	StartZaloListener(s.Store, accountID, s.Logger)

	<-ctx.Done()
}

// ====================================
// Zalo Listener Manager (chạy nền)
// ====================================

var (
	zaloListeners   = make(map[string]context.CancelFunc)
	zaloListenerMu  sync.Mutex
)

// StartZaloListener khởi động Zalo WebSocket listener cho account
func StartZaloListener(st *store.Store, accountID string, logger *log.Logger) {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()

	// Nếu đã chạy rồi thì skip
	if _, ok := zaloListeners[accountID]; ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	zaloListeners[accountID] = cancel

	go runZaloListener(ctx, st, accountID, logger)

	logger.Printf("zalo-ws: started listener for %s", accountID)
}

// StopZaloListener dừng Zalo listener
func StopZaloListener(accountID string) {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()
	if cancel, ok := zaloListeners[accountID]; ok {
		cancel()
		delete(zaloListeners, accountID)
	}
}

func runZaloListener(ctx context.Context, st *store.Store, accountID string, logger *log.Logger) {
	defer func() {
		zaloListenerMu.Lock()
		delete(zaloListeners, accountID)
		zaloListenerMu.Unlock()
	}()

	// Lấy session từ DB
	sessRec, err := st.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		logger.Printf("zalo-ws: no session for %s", accountID)
		return
	}

	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)

	var wsURLs []string
	json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs)

	session := &core.Session{
		Cookies:    cookies,
		SecretKey:  sessRec.SecretKey,
		IMEI:       sessRec.IMEI,
		UserAgent:  sessRec.UserAgent,
		APIType:    sessRec.APIType,
		APIVersion: sessRec.APIVersion,
		WSURLs:     wsURLs,
		UserID:     sessRec.UserID,
	}

	client := core.NewClient(session)

	// Auto-reconnect loop
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		logger.Printf("zalo-ws: connecting %s (attempt %d)", accountID, attempt+1)

		if err := client.ConnectWS(ctx); err != nil {
			logger.Printf("zalo-ws: connect error %s: %v", accountID, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff(attempt)):
			}
			continue
		}

		// Nhận events từ Zalo WebSocket
		listenLoop(ctx, st, client, accountID, logger)

		// Mất kết nối → reconnect sau backoff
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func listenLoop(ctx context.Context, st *store.Store, client *core.Client, accountID string, logger *log.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.WS.Messages():
			if !ok {
				return
			}
			handleZaloEvent(ctx, st, event, accountID, logger)

		case err, ok := <-client.WS.Errors():
			if !ok {
				return
			}
			logger.Printf("zalo-ws: event error %s: %v", accountID, err)
			return
		}
	}
}

func handleZaloEvent(ctx context.Context, st *store.Store, event core.Event, accountID string, logger *log.Logger) {
	switch event.Type {
	case core.EventNewMessage:
		if event.Message == nil {
			return
		}
		msg := event.Message

		// Lưu vào database
		attJSON, _ := json.Marshal(msg.Attachments)
		st.SaveMessage(&store.Message{
			ID:          msg.ID,
			AccountID:   accountID,
			ConvID:      msg.ConvID,
			FromID:      msg.FromID,
			FromName:    msg.FromName,
			Content:     msg.Content,
			MsgType:     int(msg.Type),
			Timestamp:   msg.Timestamp,
			Attachments: string(attJSON),
		})

		// Broadcast đến browser
		globalWS.Broadcast(accountID, BrowserMessage{
			Type: "new_message",
			Data: map[string]interface{}{
				"id":        msg.ID,
				"convId":    msg.ConvID,
				"fromId":    msg.FromID,
				"content":   msg.Content,
				"timestamp": msg.Timestamp,
				"type":      msg.Type,
			},
		})

		logger.Printf("zalo-ws: new msg from %s in %s", msg.FromID, msg.ConvID)

	case core.EventReconnect:
		logger.Printf("zalo-ws: reconnected %s", accountID)
	}
}

// backoff tính thời gian chờ reconnect (1s → 2s → 4s → ... → max 60s)
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}
