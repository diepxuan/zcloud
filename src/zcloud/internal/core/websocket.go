package core

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
		msgChan:   make(chan Event, 100),
		errChan:   make(chan error, 10),
		closeChan: make(chan CloseEvent, 4),
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
	fmt.Printf("[zcloud] ws connected: url=%s\n", wsURL)

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

	// Frame format: version(1) + cmd(2 LE) + subCmd(1) + payload
	frame := make([]byte, 4+len(payload))
	frame[0] = 1 // version
	binary.LittleEndian.PutUint16(frame[1:3], cmd)
	frame[3] = subCmd
	copy(frame[4:], payload)

	return w.conn.Write(ctx, websocket.MessageBinary, frame)
}

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

		fmt.Printf("[zcloud] ws frame: len=%d first=%x\n", len(msg), msg[:min(8, len(msg))])
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
		fmt.Printf("[zcloud] ws closed: code=%d reason=%q\n", ce.Code, ce.Reason)
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

	fmt.Printf("[zcloud] ws read error: %v\n", err)
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
			fmt.Printf("[zcloud] ws auth key received: keylen=%d\n", len(keyMsg.Key))
			w.mu.Lock()
			decoded, decErr := base64.StdEncoding.DecodeString(keyMsg.Key)
			if decErr == nil && len(decoded) > 0 {
				w.cipherKey = decoded
			} else {
				w.cipherKey = []byte(keyMsg.Key)
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
			fmt.Printf("[zcloud] ws auth error: code=%d msg=%q\n", keyMsg.ErrorCode, keyMsg.ErrorMsg)
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

	case cmd == 610 && subCmd == 1:
		w.handleReactions(payload, ThreadUser)

	case cmd == 611 && subCmd == 1:
		w.handleReactions(payload, ThreadGroup)

	case cmd == 612 && subCmd == 0:
		w.handleReactions(payload, ThreadUser)
		w.handleReactions(payload, ThreadGroup)

	case cmd == 3000:
		// Duplicate connection - close ngay để listener không tự reconnect.
		fmt.Printf("[zcloud] ws duplicate connection received\n")
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

func (w *WSClient) decryptPayload(payload []byte) []byte {
	w.mu.Lock()
	ck := w.cipherKey
	w.mu.Unlock()
	if len(ck) > 0 {
		if dec, err := DecodeWSEvent(payload, ck); err == nil && len(dec) > 0 {
			return dec
		} else if err != nil {
			fmt.Printf("[zcloud] ws: decrypt err=%v (keylen=%d)\n", err, len(ck))
		}
	} else {
	}
	return payload
}

func (w *WSClient) handleNewMessages(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var rawData struct {
		Msgs      json.RawMessage `json:"msgs"`
		GroupMsgs json.RawMessage `json:"groupMsgs"`
	}
	if err := json.Unmarshal(data, &rawData); err != nil {
		return
	}

	msgsRaw := rawData.Msgs
	if tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	w.parseMessages(msgsRaw, EventNewMessage, tt)
}

// handleOldMessages xử lý cmd 510/511 (old messages response)
func (w *WSClient) handleOldMessages(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var rawData struct {
		Msgs      json.RawMessage `json:"msgs"`
		GroupMsgs json.RawMessage `json:"groupMsgs"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &rawData); err != nil {
		fmt.Printf("[zcloud] ws: 510/1 parse err=%v\n", err)
		return
	}
	msgsRaw := rawData.Msgs
	if len(msgsRaw) == 0 && len(rawData.Data) > 0 {
		// Thử parse từ wrapper {data: {msgs: ...}}
		var inner struct {
			Msgs json.RawMessage `json:"msgs"`
		}
		if json.Unmarshal(rawData.Data, &inner) == nil {
			msgsRaw = inner.Msgs
		}
	}
	if len(msgsRaw) == 0 && tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	if tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	if len(msgsRaw) == 0 {
		return
	}
	w.parseMessages(msgsRaw, EventOldMessages, tt)
}

func (w *WSClient) parseMessages(msgsRaw json.RawMessage, evtType EventType, tt ThreadType) {
	if len(msgsRaw) == 0 {
		return
	}
	var msgs []wsMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil || len(msgs) == 0 {
		return
	}
	for _, m := range msgs {
		if m.MsgID == "" && m.CliMsgID == "" {
			continue
		}
		evt := Event{Type: evtType, Message: m.toMessage(w.session)}
		select {
		case w.msgChan <- evt:
		default:
		}
	}
}

func (w *WSClient) handleMessageStatus(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var raw struct {
		Delivereds json.RawMessage `json:"delivereds"`
		Seens      json.RawMessage `json:"seens"`
		GroupSeens json.RawMessage `json:"groupSeens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	w.emitRawEvent(EventDelivered, raw.Delivereds)
	if tt == ThreadGroup && len(raw.GroupSeens) > 0 {
		w.emitRawEvent(EventSeen, raw.GroupSeens)
	} else {
		w.emitRawEvent(EventSeen, raw.Seens)
	}
}

func (w *WSClient) handleControl(payload []byte) {
	data := w.decryptPayload(payload)
	var raw struct {
		Controls []struct {
			Content struct {
				ActionType string          `json:"act_type"`
				Action     string          `json:"act"`
				Data       json.RawMessage `json:"data"`
				FileID     *int64          `json:"fileId,omitempty"`
			} `json:"content"`
		} `json:"controls"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for _, c := range raw.Controls {
		if c.Content.ActionType != "file_done" {
			continue
		}
		payloadData := c.Content.Data
		if len(payloadData) > 0 && payloadData[0] == '"' {
			var s string
			if json.Unmarshal(payloadData, &s) == nil {
				payloadData = []byte(s)
			}
		}
		var up struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(payloadData, &up) == nil && up.URL != "" {
			evt := Event{Type: EventUploadAttachment, Message: &Message{Content: up.URL}}
			if c.Content.FileID != nil {
				evt.FileID = fmt.Sprintf("%d", *c.Content.FileID)
			}
			select {
			case w.msgChan <- evt:
			default:
			}
		}
	}
}

func (w *WSClient) handleActions(payload []byte) {
	data := w.decryptPayload(payload)
	var raw struct {
		Actions []struct {
			ActionType string          `json:"act_type"`
			Action     string          `json:"act"`
			Data       json.RawMessage `json:"data"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for _, a := range raw.Actions {
		if a.ActionType != "typing" {
			continue
		}
		d := a.Data
		if len(d) > 0 && d[0] == '"' {
			var s string
			if json.Unmarshal(d, &s) == nil {
				d = []byte(s)
			}
		}
		evt := Event{Type: EventTyping}
		if a.Action == "gtyping" {
			var g struct {
				GID string `json:"gid"`
				UID string `json:"uid"`
			}
			json.Unmarshal(d, &g)
			evt.Message = &Message{ConvID: g.GID, FromID: g.UID}
		} else {
			var u struct {
				UID  string `json:"uid"`
				ToID string `json:"toid"`
			}
			json.Unmarshal(d, &u)
			evt.Message = &Message{ConvID: u.ToID, FromID: u.UID}
		}
		select {
		case w.msgChan <- evt:
		default:
		}
	}
}

func (w *WSClient) handleReactions(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var raw struct {
		Reacts      json.RawMessage `json:"reacts"`
		ReactGroups json.RawMessage `json:"reactGroups"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	if tt == ThreadGroup {
		w.emitRawEvent(EventReaction, raw.ReactGroups)
	} else {
		w.emitRawEvent(EventReaction, raw.Reacts)
	}
}

func (w *WSClient) emitRawEvent(typ EventType, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return
	}
	for range arr {
		select {
		case w.msgChan <- Event{Type: typ}:
		default:
		}
	}
}

type wsMessage struct {
	MsgID       string          `json:"msgId"`
	CliMsgID    string          `json:"cliMsgId"`
	Content     json.RawMessage `json:"content"`
	FromUID     string          `json:"uidFrom"`
	DName       string          `json:"dName"`
	ConvID      string          `json:"convId"`
	IDTo        string          `json:"idTo"`
	ToID        string          `json:"toid"`
	Grid        string          `json:"grid"`
	UID         string          `json:"userId"`
	UIN         string          `json:"uin"`
	TS          json.RawMessage `json:"ts"`
	Type        json.RawMessage `json:"msgType"`
	SubType     int             `json:"subType"`
	PropertyExt *struct {
		SubType int    `json:"subType"`
		Ext     string `json:"ext"`
	} `json:"propertyExt"`
	Mentions []wsMention     `json:"mentions"`
	Quote    json.RawMessage `json:"quote"`
}

type wsMention struct {
	UID  string `json:"uid"`
	Pos  int    `json:"pos"`
	Len  int    `json:"len"`
	Name string `json:"name,omitempty"`
}

func (m wsMessage) toMessage(session *Session) *Message {
	msgID := m.MsgID
	if msgID == "" {
		msgID = m.CliMsgID
	}
	convID := firstNonEmpty(m.ConvID, m.Grid, m.IDTo, m.ToID, m.UID)
	fromID := m.FromUID
	if fromID == "" {
		fromID = m.UID
	}
	if session != nil && fromID == "0" {
		fromID = session.UserID
	}
	if session != nil && m.IDTo == "0" {
		convID = session.UserID
	}
	if session != nil && m.IDTo == "0" && m.ToID != "" {
		convID = m.ToID
	}
	msg := &Message{
		ID: msgID, ConvID: convID, FromID: fromID, FromName: m.DName,
		Content:   m.contentText(),
		Timestamp: m.ts(),
		Type:      m.msgType(),
	}
	msg.Attachments = m.attachments()
	for _, mn := range m.Mentions {
		msg.Mentions = append(msg.Mentions, MessageMention{UID: mn.UID, Pos: mn.Pos, Len: mn.Len, Name: mn.Name})
	}
	return msg
}

func (m wsMessage) attachments() []Attachment {
	var out []Attachment
	obj, ok := m.contentObject()
	if !ok {
		return out
	}

	add := func(id string, url string, name string, fileSize int64, width, height int) {
		if id == "" && url == "" {
			return
		}
		if id == "" {
			id = fmt.Sprintf("%s-%d", m.MsgID, len(out))
		}
		out = append(out, Attachment{
			ID: id, URL: url, FileName: name, FileSize: fileSize, Width: width, Height: height,
		})
	}

	url := firstNonEmpty(obj["href"], obj["url"], obj["fileUrl"], obj["normalUrl"], obj["hdUrl"], obj["oriUrl"], obj["thumbUrl"], obj["thumb"])
	name := firstNonEmpty(obj["fileName"], obj["filename"], obj["name"], obj["title"])
	var size int64
	if f, ok := obj["fileSize"].(float64); ok {
		size = int64(f)
	}
	if s, ok := obj["fileSize"].(string); ok {
		fmt.Sscanf(s, "%d", &size)
	}
	var w, h int
	if f, ok := obj["width"].(float64); ok {
		w = int(f)
	}
	if f, ok := obj["height"].(float64); ok {
		h = int(f)
	}
	id := firstNonEmpty(obj["fileId"], obj["photoId"], obj["fileID"], obj["stickerId"], obj["id"])
	add(id, url, name, size, w, h)

	for _, key := range []string{"normalUrl", "hdUrl", "oriUrl", "thumbUrl", "thumb"} {
		if u, ok := obj[key].(string); ok && u != "" && u != url {
			add(id+"-"+key, u, name, size, w, h)
		}
	}
	return out
}

func (m wsMessage) contentObject() (map[string]any, bool) {
	if len(m.Content) == 0 {
		return nil, false
	}
	if m.Content[0] == '"' {
		var s string
		if json.Unmarshal(m.Content, &s) == nil && len(s) > 0 && s[0] == '{' {
			var obj map[string]any
			if json.Unmarshal([]byte(s), &obj) == nil {
				return obj, true
			}
		}
	}
	var obj map[string]any
	if json.Unmarshal(m.Content, &obj) == nil {
		return obj, true
	}
	return nil, false
}

func (m wsMessage) contentText() string {
	if len(m.Content) == 0 {
		return ""
	}
	if m.Content[0] == '"' {
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			return s
		}
	}
	var obj map[string]any
	if json.Unmarshal(m.Content, &obj) == nil {
		if s, ok := obj["text"].(string); ok {
			return s
		}
		if s, ok := obj["title"].(string); ok {
			return s
		}
	}
	return strings.TrimSpace(string(m.Content))
}

func (m wsMessage) ts() int64 {
	if len(m.TS) == 0 {
		return 0
	}
	if m.TS[0] == '"' {
		var s string
		if json.Unmarshal(m.TS, &s) == nil {
			var n int64
			fmt.Sscanf(s, "%d", &n)
			return n
		}
	}
	var n int64
	json.Unmarshal(m.TS, &n)
	return n
}

func (m wsMessage) msgType() MsgType {
	if len(m.Type) == 0 {
		return MsgTypeText
	}
	if m.Type[0] == '"' {
		var s string
		if json.Unmarshal(m.Type, &s) == nil {
			switch {
			case strings.Contains(s, "chat.photo"):
				return MsgTypeImage
			case strings.Contains(s, "chat.sticker"):
				return MsgTypeSticker
			case strings.Contains(s, "chat.file"):
				return MsgTypeFile
			case strings.Contains(s, "chat.voice"):
				return MsgTypeVoice
			case strings.Contains(s, "chat.link"):
				return MsgTypeLink
			case strings.Contains(s, "chat.video"):
				return MsgTypeVideo
			case strings.Contains(s, "chat.card"):
				return MsgTypeCard
			case strings.Contains(s, "chat.location"):
				return MsgTypeLocation
			default:
				return MsgTypeText
			}
		}
	}
	var n int
	json.Unmarshal(m.Type, &n)
	return MsgType(n)
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
