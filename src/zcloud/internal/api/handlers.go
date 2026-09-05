package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

type Server struct {
	Store   *store.Store
	Logger  *log.Logger
	mu      sync.RWMutex
	clients map[string]*core.Client
}

func NewServer(s *store.Store, logger *log.Logger) *Server {
	return &Server{Store: s, Logger: logger, clients: make(map[string]*core.Client)}
}

type APIResponse struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
	Code  int         `json:"code,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
func ok(w http.ResponseWriter, data interface{}) {
	writeJSON(w, 200, APIResponse{OK: true, Data: data})
}
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIResponse{OK: false, Error: msg, Code: status})
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"service": "zcloud", "time": time.Now().Unix()})
}

func (s *Server) HandleAccount(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	a, err := s.Store.GetAccount(accountID)
	if err != nil {
		// Giữ UI hiển thị dữ liệu đã có trong DB nếu Zalo API lỗi.
		s.Logger.Printf("conversations/sync: zalo error=%v", err)
		dbConvs, _ := s.Store.GetConversations(accountID)
		if dbConvs == nil {
			dbConvs = []store.Conversation{}
		}
		ok(w, dbConvs)
		return
	}
	if a == nil {
		fail(w, 404, "not found")
		return
	}
	ok(w, a)
}

func (s *Server) HandleAccountList(w http.ResponseWriter, r *http.Request) {
	accounts, _ := s.Store.ListAccounts(1)
	if accounts == nil {
		accounts = []store.Account{}
	}
	// Build session status map (1 query thay vi N).
	sessStatus, _ := s.Store.GetActiveSessionsForAccounts()
	// Build listener status map (in-memory).
	listenerSet := make(map[string]bool)
	for _, snap := range ListListeners() {
		listenerSet[snap.AccountID] = snap.Listening
	}
	// Enriched list: them listening + hasActiveSession.
	type accWithStatus struct {
		store.Account
		Listening        bool `json:"listening"`
		HasActiveSession bool `json:"hasActiveSession"`
	}
	out := make([]accWithStatus, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, accWithStatus{
			Account:          a,
			Listening:        listenerSet[a.ID],
			HasActiveSession: sessStatus[a.ID],
		})
	}
	ok(w, out)
}

// HandleAccountRestart stop + start zalo listener cho account (fix WS loi,
// session con song). Tra ve status moi.
func (s *Server) HandleAccountRestart(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	StopZaloListener(accountID)
	go StartZaloListener(s.Store, accountID, s.Logger)
	ok(w, map[string]interface{}{"restarted": true, "accountId": accountID})
}
// ========== QR LOGIN ==========

// qrFlow là một lần tạo QR trên server. Poll chỉ khởi động một goroutine
// duy nhất, tránh nhiều HTTP poll cùng lúc gọi login hoặc tạo nhiều session.
type qrFlow struct {
	qr       *core.QRLoginSession
	mu       sync.Mutex
	started  bool
	consumed bool
	done     chan struct{}
	result   *core.LoginResult
	err      error
}

var qrFlows = make(map[string]*qrFlow)
var qrMu sync.RWMutex

func (s *Server) HandleCreateQR(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	qrSession, err := core.CreateQRLogin(ctx)
	if err != nil {
		fail(w, 500, "Tao QR that bai: "+err.Error())
		return
	}
	code := qrSession.Code
	flow := &qrFlow{qr: qrSession, done: make(chan struct{})}

	qrMu.Lock()
	qrFlows[code] = flow
	qrMu.Unlock()

	go func() {
		time.Sleep(5 * time.Minute)
		qrMu.Lock()
		if current, ok := qrFlows[code]; ok && current == flow {
			delete(qrFlows, code)
		}
		qrMu.Unlock()
	}()

	ok(w, map[string]interface{}{
		"token":   code,
		"image":   qrSession.ImageB64,
		"expires": time.Now().Add(3 * time.Minute).Unix(),
	})
}

func (s *Server) HandlePollQR(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		var req struct {
			Token string `json:"token"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		token = req.Token
	}
	if token == "" {
		fail(w, 400, "missing token")
		return
	}

	qrMu.RLock()
	flow := qrFlows[token]
	qrMu.RUnlock()
	if flow == nil {
		fail(w, 404, "QR session expired")
		return
	}

	// Chỉ poll QR login một lần. Các request sau chỉ đọc trạng thái.
	flow.mu.Lock()
	if !flow.started {
		flow.started = true
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		go func() {
			defer cancel()
			result, err := core.PollQRLogin(ctx, flow.qr)
			flow.mu.Lock()
			flow.result = result
			flow.err = err
			close(flow.done)
			flow.mu.Unlock()
		}()
	}
	flow.mu.Unlock()

	select {
	case <-flow.done:
		flow.mu.Lock()
		result := flow.result
		err := flow.err
		consumed := flow.consumed
		flow.consumed = true
		flow.mu.Unlock()

		qrMu.Lock()
		delete(qrFlows, token)
		qrMu.Unlock()

		if consumed {
			fail(w, 404, "QR session expired")
			return
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		if result == nil || result.Session == nil {
			fail(w, 500, "login failed: empty session")
			return
		}
		s.commitLoginSession(w, result.Session)
		return
	case <-time.After(1500 * time.Millisecond):
		ok(w, map[string]interface{}{"pending": true})
		return
	case <-r.Context().Done():
		ok(w, map[string]interface{}{"pending": true})
		return
	}
}

func (s *Server) commitLoginSession(w http.ResponseWriter, session *core.Session) {
	if session == nil || session.UserID == "" {
		fail(w, 500, "login failed: missing user id")
		return
	}

	accountID := "acc_" + session.UserID

	// Lấy tên thật từ Zalo API; nếu API tạm lỗi vẫn đăng nhập được.
	friendClient := core.NewClient(session)
	innerCtx, innerCancel := context.WithTimeout(context.Background(), 10*time.Second)
	displayName, avatar := "", ""
	if n, a, err := friendClient.GetMyProfile(innerCtx); err == nil && n != "" {
		displayName, avatar = n, a
	}
	innerCancel()
	if displayName == "" {
		displayName = safeDisplayName(session.UserID)
	}

	s.Store.CreateAccount(accountID, displayName, 1)
	if avatar != "" {
		s.Store.UpdateAccount(accountID, displayName, avatar)
	}

	cookiesJSON, _ := json.Marshal(session.Cookies)
	wsList := session.WSURLs
	if wsList == nil {
		wsList = []string{}
	}
	wsURLsJSON, _ := json.Marshal(wsList)
	serviceMapJSON, _ := json.Marshal(session.ServiceMap)
	if session.ServiceMap == nil {
		serviceMapJSON = []byte("{}")
	}

	// Chỉ giữ 1 session active cho account. Stop listener cũ trước khi lưu
	// session mới để không còn WebSocket cũ bị Zalo coi là duplicate/kickout.
	StopZaloListener(accountID)
	s.Store.SaveSession(&store.Session{
		ID: session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10), AccountID: accountID,
		UserID: session.UserID, Cookies: string(cookiesJSON), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wsURLsJSON), ServiceMap: string(serviceMapJSON), APIType: session.APIType, APIVersion: session.APIVersion, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})
	go StartZaloListener(s.Store, accountID, s.Logger)

	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID})
}

// ========== CONVERSATIONS ==========

func (s *Server) HandleConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	convs, _ := s.Store.GetConversations(accountID)
	if convs == nil {
		convs = []store.Conversation{}
	}
	ok(w, convs)
}

func (s *Server) HandleSyncConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	sessRec, err := s.Store.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		fail(w, 401, "not logged in")
		return
	}

	// Start Zalo WS listener nền (Go server tự làm Zalo client)
	go StartZaloListener(s.Store, accountID, s.Logger)

	// Tự động refresh session nếu sắp hết hạn hoặc Zalo báo lỗi
	sessRec = s.autoRefresh(sessRec)
	// Start listener lại với session mới sau autoRefresh.
	go StartZaloListener(s.Store, accountID, s.Logger)

	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
		UserID: sessRec.UserID,
	}
	var serviceMap map[string][]string
	if json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap) == nil {
		session.ServiceMap = serviceMap
	}
	var wsURLs []string
	if json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs) == nil {
		session.WSURLs = wsURLs
	}
	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	convs, err := client.GetConversations(ctx)
	if err != nil {
		if sessRec2 := s.autoRefresh(sessRec); sessRec2.ID != sessRec.ID {
			// Refresh thành công, thử lại
			sessRec = sessRec2
			json.Unmarshal([]byte(sessRec.Cookies), &cookies)
			session.Cookies = cookies
			session.SecretKey = sessRec.SecretKey
			client = core.NewClient(session)
			convs, err = client.GetConversations(ctx)
		}
	}
	if err != nil {
		// Giữ UI hiển thị dữ liệu đã có trong DB nếu Zalo API lỗi.
		s.Logger.Printf("conversations/sync: zalo error=%v", err)
		dbConvs, _ := s.Store.GetConversations(accountID)
		if dbConvs == nil {
			dbConvs = []store.Conversation{}
		}
		ok(w, dbConvs)
		return
	}
	if convs == nil {
		convs = []core.Conversation{}
	}

	// Lưu vào DB
	for _, c := range convs {
		conv := store.Conversation{
			ID: c.ID, AccountID: accountID, Name: c.Name, Avatar: c.Avatar,
			ConvType: int(c.Type), UpdatedAt: time.Now(),
		}
		if c.LastMsg != nil {
			conv.LastMsgID = c.LastMsg.ID
			conv.LastMsgAt = sql.NullTime{Time: time.UnixMilli(c.LastMsg.Timestamp), Valid: true}
			// Lưu message cuối vào bảng messages để có lịch sử ngay khi mở thread.
			attJSON, _ := json.Marshal(c.LastMsg.Attachments)
			s.Store.SaveMessage(&store.Message{
				ID: c.LastMsg.ID, AccountID: accountID, ConvID: c.ID,
				FromID: c.LastMsg.FromID, FromName: c.LastMsg.FromName,
				Content: c.LastMsg.Content, MsgType: int(c.LastMsg.Type),
				Timestamp: c.LastMsg.Timestamp, Attachments: string(attJSON),
			})
		}
		s.Store.SaveConversation(&store.Conversation{
			ID: c.ID, AccountID: accountID, Name: conv.Name, Avatar: conv.Avatar,
			ConvType: int(c.Type), LastMsgID: conv.LastMsgID,
			LastMsgAt: conv.LastMsgAt, UpdatedAt: time.Now(),
		})
	}

	// Cập nhật tên + avatar cho account (đồng bộ)
	if n, a, err := client.GetMyProfile(context.Background()); err == nil && n != "" {
		s.Store.UpdateAccount(accountID, n, a)
	}

	// Resolve group names — cập nhật tên + avatar cho group vào cả convs và DB
	var groupIDs []string
	for _, c := range convs {
		if c.Type == core.ConvGroup {
			groupIDs = append(groupIDs, c.ID)
		}
	}
	if len(groupIDs) > 0 {
		if gm, err := client.GetGroupInfo(ctx, groupIDs); err == nil {
			for i := range convs {
				if g, ok := gm[convs[i].ID]; ok {
					convs[i].Name = g.Name
					convs[i].Avatar = g.Avatar
					s.Store.SaveConversation(&store.Conversation{
						ID: convs[i].ID, AccountID: accountID, Name: g.Name, Avatar: g.Avatar, ConvType: int(convs[i].Type), UpdatedAt: time.Now(),
					})
				}
			}
		}
	}

	// Trả về từ DB để có đầy đủ tên + avatar
	dbConvs, _ := s.Store.GetConversations(accountID)
	if dbConvs == nil {
		dbConvs = []store.Conversation{}
	}
	ok(w, dbConvs)
}

// ========== MESSAGES ==========

func (s *Server) HandleMessages(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	convID := r.URL.Query().Get("convId")
	if accountID == "" || convID == "" {
		fail(w, 400, "missing accountId or convId")
		return
	}
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	if cursor == 0 {
		cursor = time.Now().UnixMilli() + 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	msgs, _ := s.Store.GetMessages(accountID, convID, cursor, limit)
	if msgs == nil {
		msgs = []store.Message{}
	}
	// Map sang core.Message để đính kèm cờ delivery ack + status.
	out := make([]core.Message, len(msgs))
	for i, m := range msgs {
		out[i] = core.Message{
			ID: m.ID, ConvID: m.ConvID, FromID: m.FromID, FromName: m.FromName,
			Content: m.Content, Type: core.MsgType(m.MsgType), Timestamp: m.Timestamp,
		}
		var atts []core.Attachment
		if len(m.Attachments) > 0 && m.Attachments != "null" {
			_ = json.Unmarshal([]byte(m.Attachments), &atts)
		}
		out[i].Attachments = atts
		// MarkDeliveryAck điền cả IsDeliveryAck + AckStatus nếu content là ack JSON.
		core.MarkDeliveryAck(&out[i])
	}
	ok(w, out)
}

func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		To        string `json:"to"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.AccountID == "" || req.Content == "" {
		fail(w, 400, "missing fields")
		return
	}

	sessRec, err := s.Store.GetActiveSession(req.AccountID)
	if err != nil || sessRec == nil {
		fail(w, 401, "not logged in")
		return
	}
	// Refresh session nếu hết hạn — tránh gửi tin với secret_key cũ (zpw_sek bị thiếu/không đúng)
	sessRec = s.autoRefresh(sessRec)
	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
	}
	var wsURLs []string
	json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs)
	session.WSURLs = wsURLs
	var serviceMap map[string][]string
	if json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap) == nil {
		session.ServiceMap = serviceMap
	}

	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	msg, err := client.SendMessage(ctx, req.To, req.Content, core.MsgTypeText)
	if err != nil {
		fail(w, 500, "Gui tin that bai: "+err.Error())
		return
	}
	// Lưu tin đã gửi vào DB để UI/history hiển thị ngay.
	s.Store.SaveMessage(&store.Message{
		ID: msg.ID, AccountID: req.AccountID, ConvID: req.To,
		FromID: sessRec.UserID, FromName: "", Content: msg.Content,
		MsgType: int(msg.Type), Timestamp: msg.Timestamp,
	})
	ok(w, map[string]interface{}{"sent": true, "msgId": msg.ID, "content": msg.Content, "timestamp": msg.Timestamp})
}

// ========== SYNC MESSAGES ==========

func (s *Server) HandleSyncMessages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		ConvID    string `json:"convId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.AccountID == "" || req.ConvID == "" {
		fail(w, 400, "missing fields")
		return
	}
	conv, _ := s.Store.GetConversation(req.AccountID, req.ConvID)
	synced := 0
	// Gửi request old messages qua WS listener nền
	convType := 0
	if conv != nil {
		convType = conv.ConvType
	}
	if RequestOldMessagesViaListener(req.AccountID, req.ConvID, convType) {
		synced++
	}

	// REST fallback: nếu WS không chạy hoặc không có response, thử lấy history
	// qua REST để không để UI trống.
	if convType == 1 && synced == 0 {
		sessRec, err := s.Store.GetActiveSession(req.AccountID)
		if err == nil && sessRec != nil {
			var cookies map[string]string
			json.Unmarshal([]byte(sessRec.Cookies), &cookies)
			session := &core.Session{
				Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
				UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
				UserID: sessRec.UserID,
			}
			var serviceMap map[string][]string
			if json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap) == nil {
				session.ServiceMap = serviceMap
			}
			client := core.NewClient(session)
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			msgs, err := client.GetGroupHistoryV2(ctx, req.ConvID, 50)
			if err == nil {
				for _, m := range msgs {
					attJSON, _ := json.Marshal(m.Attachments)
					s.Store.SaveMessage(&store.Message{
						ID: m.ID, AccountID: req.AccountID, ConvID: req.ConvID,
						FromID: m.FromID, FromName: m.FromName, Content: m.Content,
						MsgType: int(m.Type), Timestamp: m.Timestamp, Attachments: string(attJSON),
					})
				}
				if len(msgs) > 0 {
					synced++
				}
			}
		}
	}
	ok(w, map[string]interface{}{"syncing": synced > 0, "synced": synced})
}

func (s *Server) HandleFriends(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	sessRec, err := s.Store.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		fail(w, 401, "not logged in")
		return
	}
	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
	}
	var serviceMap map[string][]string
	if json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap) == nil {
		session.ServiceMap = serviceMap
	}
	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	users, err := client.GetFriends(ctx)
	if err != nil {
		// Không làm mất UI Contact khi Zalo tạm lỗi.
		s.Logger.Printf("friends: zalo error=%v", err)
		ok(w, []core.User{})
		return
	}
	ok(w, users)
}

// ========== LOGOUT ==========

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.AccountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	StopZaloListener(req.AccountID)
	s.Store.DeleteAccount(req.AccountID)
	ok(w, map[string]interface{}{"loggedOut": true})
}

// ========== COOKIE LOGIN ==========

func (s *Server) HandleCookieLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	cookies := parseCookie(req.Cookie)
	if len(cookies) == 0 {
		fail(w, 400, "no cookies")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := core.CookieLogin(ctx, cookies, "", "")
	if err != nil {
		fail(w, 500, "login failed: "+err.Error())
		return
	}
	session := result.Session
	accountID := "acc_" + session.UserID

	// Lấy tên thật từ Zalo API
	friendClient := core.NewClient(session)
	innerCtx, innerCancel := context.WithTimeout(context.Background(), 10*time.Second)
	displayName, avatar := "", ""
	if n, a, err := friendClient.GetMyProfile(innerCtx); err == nil && n != "" {
		displayName, avatar = n, a
	}
	innerCancel()
	if displayName == "" {
		displayName = safeDisplayName(session.UserID)
	}

	s.Store.CreateAccount(accountID, displayName, 1)
	if avatar != "" {
		s.Store.UpdateAccount(accountID, displayName, avatar)
	}
	cj, _ := json.Marshal(session.Cookies)
	wj, _ := json.Marshal(session.WSURLs)
	smj, _ := json.Marshal(session.ServiceMap)
	if session.ServiceMap == nil {
		smj = []byte("{}")
	}
	s.Store.SaveSession(&store.Session{
		ID: session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10), AccountID: accountID,
		UserID: session.UserID, Cookies: string(cj), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wj), ServiceMap: string(smj), APIType: session.APIType, APIVersion: session.APIVersion, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})
	go StartZaloListener(s.Store, accountID, s.Logger)
	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID})
}

func parseCookie(s string) map[string]string {
	c := make(map[string]string)
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts := strings.SplitN(p, "=", 2)
		if len(parts) == 2 {
			c[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return c
}

// ========== LOGIN PAGE ==========

func (s *Server) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html, err := webFS.ReadFile("web/login.html")
	if err != nil {
		http.Error(w, "template error", 500)
		return
	}
	w.Write(html)
}

// ========== CHAT PAGE ==========
// ========== CHAT PAGE ==========

func (s *Server) HandleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	fav, _ := webFS.ReadFile("web/favicon.svg")
	if fav != nil {
		w.Write(fav)
	}
}

func (s *Server) HandleChatPage(w http.ResponseWriter, r *http.Request) {
	accounts, _ := s.Store.ListAccounts(1)
	if len(accounts) == 0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	accOpts := ""
	for _, a := range accounts {
		sel := ""
		if len(accounts) == 1 {
			sel = "selected"
		}
		n := a.DisplayName
		if n == "" {
			n = a.ID[:min(20, len(a.ID))]
		}
		accOpts += fmt.Sprintf(`<option value="%s" %s>%s</option>`, a.ID, sel, n)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html, err := webFS.ReadFile("web/chat.html")
	if err != nil {
		http.Error(w, "template error", 500)
		return
	}
	// Dùng strings.Replace thay vì fmt.Fprintf — tránh lỗi %! trong CSS
	w.Write([]byte(strings.Replace(string(html), "%s", accOpts, 1)))
}

func safeDisplayName(userID string) string {
	if userID == "" {
		return "Zalo User"
	}
	short := userID
	if len(short) > 8 {
		short = short[:8]
	}
	return "Zalo User " + short
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// autoRefresh kiểm tra và refresh session nếu sắp hết hạn hoặc token hết hạn
func (s *Server) autoRefresh(sessRec *store.Session) *store.Session {
	if sessRec == nil {
		return nil
	}
	now := time.Now()
	// Refresh nếu còn dưới 1 tiếng hoặc đã hết hạn
	if now.Before(sessRec.ExpiresAt.Add(-1*time.Hour)) && now.Before(sessRec.ExpiresAt) {
		return sessRec // Còn hạn, không cần refresh
	}
	// Thử refresh bằng cookie login
	var cookies map[string]string
	if err := json.Unmarshal([]byte(sessRec.Cookies), &cookies); err != nil || len(cookies) == 0 {
		return sessRec
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := core.CookieLogin(ctx, cookies, sessRec.IMEI, sessRec.UserAgent)
	if err != nil {
		s.Logger.Printf("autoRefresh: cookie login failed: %v", err)
		return sessRec
	}
	// Lưu session mới
	session := result.Session
	accountID := sessRec.AccountID
	cj, _ := json.Marshal(session.Cookies)
	wj, _ := json.Marshal(session.WSURLs)
	smj, _ := json.Marshal(session.ServiceMap)
	if session.ServiceMap == nil {
		smj = []byte("{}")
	}
	newSession := &store.Session{
		ID:        session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10),
		AccountID: accountID, UserID: session.UserID,
		Cookies: string(cj), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wj), ServiceMap: string(smj), APIType: session.APIType, APIVersion: session.APIVersion,
		IsActive: 1, ExpiresAt: session.ExpiresAt,
	}
	// Ngừng listener cũ trước khi đổi session; nếu không, Zalo vẫn giữ
	// WebSocket cũ và có thể kick connection mới.
	StopZaloListener(accountID)
	saveErr := s.Store.SaveSession(newSession)
	s.Logger.Printf("autoRefresh: SaveSession err=%v id=%s", saveErr, newSession.ID)
	// SaveSession tự vô hiệu hoá session active cũ của account.
	go StartZaloListener(s.Store, accountID, s.Logger)
	s.Logger.Printf("autoRefresh: session refreshed for %s — new expires %s", accountID, session.ExpiresAt.Format(time.RFC3339))
	return newSession
}

// RefreshAllSessions refresh tat ca session sap het han (goi tu background goroutine)
func (s *Server) RefreshAllSessions() {
	accounts, err := s.Store.ListAccounts(1)
	if err != nil {
		return
	}
	for _, a := range accounts {
		sessRec, err := s.Store.GetActiveSession(a.ID)
		if err != nil || sessRec == nil {
			continue
		}
		if time.Now().After(sessRec.ExpiresAt.Add(-30 * time.Minute)) {
			s.Logger.Printf("autoRefresh: background refreshing %s", a.ID)
			s.autoRefresh(sessRec)
		}
	}
}
