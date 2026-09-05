package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	mu    sync.RWMutex
	rooms map[string]map[*wsConn]bool // accountID → set of browser conns
}

type wsConn struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

// BrowserMessage message từ/tới browser
type BrowserMessage struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
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

type zaloListenerEntry struct {
	cancel    context.CancelFunc
	client    *core.Client
	sessionID string
	// State tracking cho tab Quan ly: cho biet listener dang chay hay loi.
	connectedAt time.Time
	lastError   string
	// reconnectAttempts dem so lan reconnect that bai lien tiep.
	reconnectAttempts int
}

var (
	zaloListeners  = make(map[string]*zaloListenerEntry)
	zaloListenerMu sync.Mutex
)

const (
	maxReconnectAttempts = 8
	maxReconnectDelay    = 30 * time.Second
)

// StartZaloListener khởi động Zalo WebSocket listener cho account
// RequestOldMessagesViaListener gửi yêu cầu old messages qua WS listener nền
func RequestOldMessagesViaListener(accountID, convID string, convType int) bool {
	zaloListenerMu.Lock()
	entry := zaloListeners[accountID]
	zaloListenerMu.Unlock()
	ok := entry != nil
	var client *core.Client
	if entry != nil {
		client = entry.client
	}
	fmt.Printf("[zcloud] ws-sync: account=%s conv=%s type=%d client_ok=%v ws_nil=%v\n",
		accountID, convID, convType, ok, !ok || client == nil || client.WS == nil)
	if !ok || client == nil || client.WS == nil {
		return false
	}
	tt := core.ThreadUser
	if convType == 1 {
		tt = core.ThreadGroup
	}
	fmt.Printf("[zcloud] ws-sync: sending RequestOldMessages cmd=%d\n", 510+int(tt))
	if err := client.WS.RequestOldMessages(context.Background(), tt, ""); err != nil {
		fmt.Printf("[zcloud] ws-sync: request err=%v\n", err)
		return false
	}
	fmt.Printf("[zcloud] ws-sync: request sent OK\n")
	return true
}

func StartZaloListener(st *store.Store, accountID string, logger *log.Logger) {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()

	sessRec, err := st.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		logger.Printf("zalo-ws: no active session for %s", accountID)
		return
	}

	// Nếu đã chạy với cùng session thì giữ nguyên, tránh tạo duplicate WS.
	if entry, ok := zaloListeners[accountID]; ok {
		if entry.sessionID == sessRec.ID {
			return
		}
		entry.cancel()
		delete(zaloListeners, accountID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	entry := &zaloListenerEntry{cancel: cancel, sessionID: sessRec.ID}
	zaloListeners[accountID] = entry

	go runZaloListener(ctx, entry, st, accountID, logger)

	logger.Printf("zalo-ws: started listener for %s", accountID)
}

// StopZaloListener dừng Zalo listener
func StopZaloListener(accountID string) {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()
	if entry, ok := zaloListeners[accountID]; ok {
		entry.cancel()
		delete(zaloListeners, accountID)
	}
}

func runZaloListener(ctx context.Context, current *zaloListenerEntry, st *store.Store, accountID string, logger *log.Logger) {
	defer func() {
		if current != nil && current.client != nil && current.client.WS != nil {
			_ = current.client.WS.Close()
		}
		zaloListenerMu.Lock()
		if zaloListeners[accountID] == current {
			delete(zaloListeners, accountID)
		}
		zaloListenerMu.Unlock()
	}()

	// Auto-reconnect giới hạn: lỗi mạng retry tối đa, kickout/duplicate dừng
	// ngay để không tạo vòng lặp vô hạn.
	for attempt := 0; attempt < maxReconnectAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sessRec, err := st.GetActiveSession(accountID)
		if err != nil || sessRec == nil {
			logger.Printf("zalo-ws: no active session for %s", accountID)
			return
		}

		client, err := clientFromSession(sessRec)
		if err != nil {
			logger.Printf("zalo-ws: build client error %s: %v", accountID, err)
			return
		}

		zaloListenerMu.Lock()
		if zaloListeners[accountID] != current {
			zaloListenerMu.Unlock()
			return
		}
		current.client = client
		zaloListenerMu.Unlock()

		logger.Printf("zalo-ws: connecting %s (attempt %d)", accountID, attempt+1)

		if err := client.ConnectWS(ctx); err != nil {
			logger.Printf("zalo-ws: connect error %s: %v", accountID, err)
			if current.client != nil && current.client.WS != nil {
				_ = current.client.WS.Close()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff(attempt)):
			}
			continue
		}

		// Nhận events từ Zalo WebSocket
		reason := listenLoop(ctx, st, client, accountID, logger)

		// Mất kết nối → reconnect sau backoff
		if reason.Code == 3000 || reason.Code == 3003 {
			logger.Printf("zalo-ws: %s stopped after kickout code=%d reason=%q", accountID, reason.Code, reason.Reason)
			return
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
	logger.Printf("zalo-ws: %s exceeded reconnect limit, listener stopped", accountID)
}

func clientFromSession(sessRec *store.Session) (*core.Client, error) {
	var cookies map[string]string
	if err := json.Unmarshal([]byte(sessRec.Cookies), &cookies); err != nil {
		return nil, fmt.Errorf("parse cookies: %w", err)
	}
	var wsURLs []string
	json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs)
	var serviceMap map[string][]string
	json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap)

	session := &core.Session{
		Cookies:    cookies,
		SecretKey:  sessRec.SecretKey,
		IMEI:       sessRec.IMEI,
		UserAgent:  sessRec.UserAgent,
		APIType:    sessRec.APIType,
		APIVersion: sessRec.APIVersion,
		WSURLs:     wsURLs,
		ServiceMap: serviceMap,
		UserID:     sessRec.UserID,
	}
	return core.NewClient(session), nil
}

func listenLoop(ctx context.Context, st *store.Store, client *core.Client, accountID string, logger *log.Logger) core.CloseEvent {
	for {
		select {
		case <-ctx.Done():
			return core.CloseEvent{Code: -1}
		case event, ok := <-client.WS.Messages():
			if !ok {
				return core.CloseEvent{Code: -1}
			}
			handleZaloEvent(ctx, st, event, accountID, logger)

		case err, ok := <-client.WS.Errors():
			if !ok {
				return core.CloseEvent{Code: -1}
			}
			logger.Printf("zalo-ws: event error %s: %v", accountID, err)
			return core.CloseEvent{Code: -1}

		case ce, ok := <-client.WS.CloseEvents():
			if !ok {
				return core.CloseEvent{Code: -1}
			}
			return ce
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
		go maybeAutoDownloadMedia(context.Background(), st, accountID, msg, logger)
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
				"id":          msg.ID,
				"convId":      msg.ConvID,
				"fromId":      msg.FromID,
				"fromName":    msg.FromName,
				"content":     msg.Content,
				"timestamp":   msg.Timestamp,
				"type":        msg.Type,
				"attachments": msg.Attachments,
				"isAck":       msg.IsDeliveryAck,
				"ackStatus":   msg.AckStatus,
			},
		})

		logger.Printf("zalo-ws: new msg from %s in %s", msg.FromID, msg.ConvID)

	case core.EventTyping:
		if event.Message == nil {
			return
		}
		globalWS.Broadcast(accountID, BrowserMessage{
			Type: "typing",
			Data: map[string]interface{}{
				"convId": event.Message.ConvID,
				"fromId": event.Message.FromID,
			},
		})

	case core.EventReaction:
		globalWS.Broadcast(accountID, BrowserMessage{Type: "reaction"})

	case core.EventSeen:
		globalWS.Broadcast(accountID, BrowserMessage{Type: "seen"})

	case core.EventDelivered:
		globalWS.Broadcast(accountID, BrowserMessage{Type: "delivered"})

	case core.EventUploadAttachment:
		globalWS.Broadcast(accountID, BrowserMessage{
			Type: "upload_attachment",
			Data: map[string]interface{}{
				"fileId": event.FileID,
				"url":    uploadAttachmentURL(event),
			},
		})

	case core.EventOldMessages:
		if event.Message == nil {
			logger.Printf("zalo-ws: old msg nil")
			return
		}
		om := event.Message
		logger.Printf("zalo-ws: old msg from %s in %s", om.FromID, om.ConvID)
		oaJSON, _ := json.Marshal(om.Attachments)
		go maybeAutoDownloadMedia(context.Background(), st, accountID, om, logger)
		st.SaveMessage(&store.Message{
			ID: om.ID, AccountID: accountID, ConvID: om.ConvID,
			FromID: om.FromID, FromName: om.FromName,
			Content: om.Content, MsgType: int(om.Type),
			Timestamp: om.Timestamp, Attachments: string(oaJSON),
		})
		globalWS.Broadcast(accountID, BrowserMessage{
			Type: "old_message",
			Data: map[string]interface{}{
				"id": om.ID, "convId": om.ConvID, "fromId": om.FromID,
				"fromName": om.FromName, "content": om.Content,
				"timestamp": om.Timestamp, "type": om.Type,
				"attachments": om.Attachments,
				"isAck":     om.IsDeliveryAck,
				"ackStatus": om.AckStatus,
			},
		})

	case core.EventReconnect:
		logger.Printf("zalo-ws: reconnected %s", accountID)
	}
}

type mediaDownloadInfo struct {
	URL      string
	FileName string
	FileExt  string
	MsgID    string
}

func extractMediaFromAttachments(atts []core.Attachment) mediaDownloadInfo {
	if len(atts) == 0 {
		return mediaDownloadInfo{}
	}
	a := atts[0]
	if a.URL == "" {
		return mediaDownloadInfo{}
	}
	ext := ""
	if i := strings.LastIndex(a.FileName, "."); i >= 0 {
		ext = a.FileName[i+1:]
	}
	if ext == "" {
		u, err := url.Parse(a.URL)
		if err == nil {
			ext = strings.TrimPrefix(path.Ext(u.Path), ".")
		}
	}
	if ext == "" {
		ext = "bin"
	}
	return mediaDownloadInfo{
		URL: a.URL, FileName: a.FileName, FileExt: ext, MsgID: a.ID,
	}
}

func maybeAutoDownloadMedia(ctx context.Context, st *store.Store, accountID string, msg *core.Message, logger *log.Logger) {
	if st == nil || msg == nil || len(msg.Attachments) == 0 {
		return
	}
	info := extractMediaFromAttachments(msg.Attachments)
	if info.URL == "" {
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", info.URL, nil)
	if err != nil {
		logger.Printf("zalo-ws: media request err=%v", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Printf("zalo-ws: media download err=%v", err)
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Printf("zalo-ws: media read err=%v", err)
		return
	}
	if len(data) == 0 {
		return
	}
	mediaDir := st.MediaDir(accountID, msg.ConvID)
	fileID := info.MsgID
	if fileID == "" {
		fileID = msg.ID
	}
	if fileID == "" {
		fileID = fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	filePath := filepath.Join(mediaDir, fileID+"."+info.FileExt)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		logger.Printf("zalo-ws: media save err=%v", err)
		return
	}
	rel, _ := filepath.Rel(st.MediaPath(), filePath)
	fileInfo, _ := os.Stat(filePath)
	savedPath, err := st.SaveMedia(&store.MediaFile{
		ID: fileID, AccountID: accountID, ConvID: msg.ConvID, MsgID: msg.ID,
		FileName: info.FileName, FilePath: rel, FileExt: info.FileExt,
		FileSize: fileInfo.Size(), SourceURL: info.URL, IsDownloaded: 1,
	})
	if err != nil {
		logger.Printf("zalo-ws: media save meta err=%v", err)
	}
	_ = savedPath
	globalWS.Broadcast(accountID, BrowserMessage{
		Type: "media_downloaded",
		Data: map[string]interface{}{
			"msgId": msg.ID, "convId": msg.ConvID,
			"url": "/media/" + accountID + "/" + msg.ConvID + "/" + fileID + "." + info.FileExt,
		},
	})
}

func uploadAttachmentURL(event core.Event) string {
	if event.Message != nil {
		return event.Message.Content
	}
	return ""
}

// IsListening tra ve true neu zalo listener dang chay cho accountID.
func IsListening(accountID string) bool {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()
	_, ok := zaloListeners[accountID]
	return ok
}

// backoff tính thời gian chờ reconnect (1s → 2s → 4s → ... → max 30s)
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > maxReconnectDelay {
		d = maxReconnectDelay
	}
	return d
}

// ListenerSnapshot la trang thai listener cho 1 account (dung cho UI Quan ly).
type ListenerSnapshot struct {
	AccountID string `json:"accountId"`
	Listening bool   `json:"listening"`
}

// ListListeners tra ve snapshot cho moi account dang co listener.
func ListListeners() []ListenerSnapshot {
	zaloListenerMu.Lock()
	defer zaloListenerMu.Unlock()
	out := make([]ListenerSnapshot, 0, len(zaloListeners))
	for id := range zaloListeners {
		out = append(out, ListenerSnapshot{AccountID: id, Listening: true})
	}
	return out
}
