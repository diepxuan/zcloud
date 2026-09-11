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
	"strconv"
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
// lastID = "" → listener dùng sentinel lấy batch mới nhất.
func RequestOldMessagesViaListener(accountID, convID string, convType int, lastID string) bool {
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
	if err := client.WS.RequestOldMessages(context.Background(), tt, lastID); err != nil {
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
		go enqueueMessageMediaJobs(st, accountID, msg, logger)
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
		go enqueueMessageMediaJobs(st, accountID, om, logger)
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

	default:
		// Desktop sync events (cmd 590-592 / 630-634) và các event khác
		// chưa được xử lý cụ thể: log + broadcast cho browser nếu có payload.
		if event.DesktopSync != nil {
			ds := event.DesktopSync
			logger.Printf("zalo-ws: desktop-sync cmd=%d sub=%d", ds.Cmd, ds.SubCmd)
			globalWS.Broadcast(accountID, BrowserMessage{
				Type: "desktop_sync",
				Data: map[string]interface{}{
					"cmd":     ds.Cmd,
					"subCmd":  ds.SubCmd,
					"event":   ds.Type.String(),
					"rawData": ds.RawData,
				},
			})
		}
	}
}

type mediaDownloadInfo struct {
	URL      string
	FileName string
	FileExt  string
	MsgID    string
}

const storeMediaDefaultAttempts = 3

// enqueueMessageMediaJobs ghi mỗi media attachment của message vào bảng
// media_jobs. MediaWorker nền sẽ tải về disk + retry + broadcast.
//
// Type đã biết là media (image/sticker/file/voice/video) đều đi qua
// extractAllMedia. Với MsgTypeLink, chỉ enqueue nếu attachment có URL
// ảnh (jpg/png/webp/gif) — Zalo link message có field 'thumb' cho OG
// preview image; nếu không có thì link thuần không cần tải.
func enqueueMessageMediaJobs(st *store.Store, accountID string, msg *core.Message, logger *log.Logger) {
	if st == nil || msg == nil || len(msg.Attachments) == 0 {
		return
	}
	if accountID == "" || msg.ConvID == "" {
		logger.Printf("zalo-ws: skip media enqueue, missing account/conv (msg=%s)", msg.ID)
		return
	}
	if !msg.Type.IsMedia() && !(msg.Type.IsLink() && hasImageAttachment(msg.Attachments)) {
		return
	}
	items := extractAllMedia(msg.Attachments)
	if len(items) == 0 {
		return
	}
	for _, info := range items {
		fileID := info.MsgID
		if fileID == "" {
			fileID = msg.ID
		}
		if fileID == "" {
			continue
		}
		job := &store.MediaJob{
			ID: fileID, AccountID: accountID, ConvID: msg.ConvID,
			MsgID: msg.ID, FileName: info.FileName, FileExt: info.FileExt,
			SourceURL: info.URL, Status: store.MediaJobPending,
			MaxAttempts: storeMediaDefaultAttempts,
		}
		if err := st.SaveMediaJob(job); err != nil {
			logger.Printf("zalo-ws: enqueue media job %s: %v", fileID, err)
			continue
		}
		existing, _ := st.GetMediaJob(fileID, accountID)
		if existing == nil {
			continue
		}
		needReset := existing.Status == store.MediaJobFailed
		if existing.Status == store.MediaJobDone && !fileExists(st, accountID, msg.ConvID, fileID, info.FileExt) {
			needReset = true
		}
		if needReset {
			if err := st.ResetMediaJobPending(fileID, accountID); err != nil {
				logger.Printf("zalo-ws: reset media job %s: %v", fileID, err)
			}
		}
	}
}

func fileExists(st *store.Store, accountID, convID, fileID, ext string) bool {
	if _, err := os.Stat(st.MediaFilePath(accountID, convID, fileID, ext)); err != nil {
		return false
	}
	return true
}

// hasImageAttachment trả về true nếu bất kỳ attachment nào có URL trông giống ảnh
// (jpg/jpeg/png/gif/webp). Dùng để quyết định MsgTypeLink có OG preview cần tải.
func hasImageAttachment(atts []core.Attachment) bool {
	imageExts := map[string]bool{"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true}
	for _, a := range atts {
		if a.URL == "" {
			continue
		}
		ext := strings.ToLower(extFromFileOrURL(a.FileName, a.URL))
		if imageExts[ext] {
			return true
		}
	}
	return false
}

// extractAllMedia duyệt toàn bộ attachments, trả về thông tin download cho
// từng attachment có URL. Zalo thường gửi cùng 1 file với nhiều variant
// (-0 = original, -1 = thumb, -2 = preview, …) — dedupe theo base ID và
// giữ variant có URL dài nhất (thường là original HD, không phải thumb).
func extractAllMedia(atts []core.Attachment) []mediaDownloadInfo {
	// bestByID[baseID] = index in out; ta sẽ ghi đè nếu variant sau có URL dài hơn.
	bestByID := make(map[string]int)
	var out []mediaDownloadInfo
	for _, a := range atts {
		if a.URL == "" {
			continue
		}
		base := baseAttachmentID(a.ID)
		if idx, ok := bestByID[base]; ok {
			// Giữ variant URL dài hơn (thường là original, không phải thumb).
			if len(a.URL) > len(out[idx].URL) {
				out[idx].URL = a.URL
				out[idx].FileName = a.FileName
				out[idx].MsgID = a.ID
			}
			continue
		}
		bestByID[base] = len(out)
		out = append(out, mediaDownloadInfo{
			URL:      a.URL,
			FileName: a.FileName,
			FileExt:  extFromFileOrURL(a.FileName, a.URL),
			MsgID:    a.ID,
		})
	}
	return out
}

// baseAttachmentID strip variant suffix "-N" nếu phần sau là số.
// VD: "8235996590219-0" -> "8235996590219", "abc-def-2" -> "abc-def",
// "raw-id" -> "raw-id". Dùng để dedupe các variant của cùng 1 attachment.
func baseAttachmentID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		if _, err := strconv.Atoi(id[i+1:]); err == nil {
			return id[:i]
		}
	}
	return id
}

// extFromFileOrURL suy ra extension từ filename (ưu tiên) hoặc URL path.
func extFromFileOrURL(fileName, rawURL string) string {
	if i := strings.LastIndex(fileName, "."); i >= 0 {
		return fileName[i+1:]
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		return strings.TrimPrefix(path.Ext(u.Path), ".")
	}
	return "bin"
}

const mediaDownloadRetries = 3

// downloadOneMedia tải 1 file với retry. Skip nếu file đã tồn tại trên disk.
// Còn được dùng bởi test (TestDownloadOneMedia_Success và test khác trong media_test.go).
func downloadOneMedia(ctx context.Context, st *store.Store, accountID string, msg *core.Message, info mediaDownloadInfo, logger *log.Logger) {
	mediaDir := st.MediaDir(accountID, msg.ConvID)
	fileID := info.MsgID
	if fileID == "" {
		fileID = msg.ID
	}
	if fileID == "" {
		fileID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	filePath := filepath.Join(mediaDir, fileID+"."+info.FileExt)
	// Dedupe: nếu file đã tồn tại trên disk, không tải lại.
	if _, err := os.Stat(filePath); err == nil {
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}
	var data []byte
	for attempt := 1; attempt <= mediaDownloadRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", info.URL, nil)
		if err != nil {
			logger.Printf("zalo-ws: media request err=%v", err)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			logger.Printf("zalo-ws: media download err=%v (attempt %d/%d)", err, attempt, mediaDownloadRetries)
			if attempt < mediaDownloadRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			logger.Printf("zalo-ws: media http %d (attempt %d/%d)", resp.StatusCode, attempt, mediaDownloadRetries)
			if attempt < mediaDownloadRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return
		}
		buf, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr != nil {
			logger.Printf("zalo-ws: media read err=%v (attempt %d/%d)", rerr, attempt, mediaDownloadRetries)
			if attempt < mediaDownloadRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return
		}
		if len(buf) == 0 {
			return
		}
		data = buf
		break
	}
	if len(data) == 0 {
		return
	}
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		logger.Printf("zalo-ws: media mkdir err=%v", err)
		return
	}
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
