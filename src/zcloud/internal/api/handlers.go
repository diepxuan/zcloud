package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal"
	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/logging"
	"github.com/diepxuan/zcloud/internal/store"
)

type Server struct {
	Store     *store.Store
	Logger    *logging.Logger
	mu        sync.RWMutex
	qrFlows   map[string]*qrFlow
	WSManager *WSManager
	clients   map[string]*core.Client

	zaloListenerMu sync.Mutex
	zaloListeners  map[string]*zaloListenerEntry
	syncV2Mu       sync.Mutex
	syncV2Registry map[string]*syncV2Entry

	// Tránh nhiều request /api/conversations/sync của cùng account chạy chồng
	// nhau. Sync có thể mất 30-45s; request cũ hoàn tất muộn có thể ghi đè dữ
	// liệu mới hơn. Khóa theo account để các account vẫn sync song song.
	conversationSyncMu    sync.Mutex
	conversationSyncLocks map[string]*sync.Mutex
}

func NewServer(s *store.Store, logger *logging.Logger) *Server {
	if logger == nil {
		logger = logging.New(0)
	}
	svr := &Server{
		Store:          s,
		Logger:         logger,
		clients:        make(map[string]*core.Client),
		qrFlows:        make(map[string]*qrFlow),
		WSManager:      &WSManager{rooms: make(map[string]map[*wsConn]bool)},
		zaloListeners:  make(map[string]*zaloListenerEntry),
		syncV2Registry: make(map[string]*syncV2Entry),
	}
	return svr
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
	// Trả về version (semver + git hash) để frontend poll detect khi binary
	// được rebuild → auto-reload UI. Xem internal/version.go + ldflags trong
	// buildBinary (servcmd/watch.go).
	ok(w, map[string]interface{}{
		"service": "zcloud",
		"time":    time.Now().Unix(),
		"version": internal.Full(),
	})
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
		s.Logger.Warn("account_fetch_failed", "account_id", accountID, "error", err)
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
	// ?enabledOnly=true: chỉ trả account có enabled=true (UI header dùng để
	// merge convs/friends từ subset). Mặc định false — management panel
	// vẫn thấy tất cả kèm flag enabled.
	enabledOnly := r.URL.Query().Get("enabledOnly") == "true"
	accounts, _ := s.Store.ListAccounts(1, enabledOnly)
	if accounts == nil {
		accounts = []store.Account{}
	}
	// Build session status map (1 query thay vi N).
	sessStatus, _ := s.Store.GetActiveSessionsForAccounts()
	// Build listener status map (in-memory).
	listenerSet := make(map[string]bool)
	for _, snap := range s.ListListeners() {
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

// HandleAccountSetEnabled bật/tắt flag enabled của account.
// WS listener KHÔNG bị ảnh hưởng — account disabled vẫn listen + sync.
// Body: {accountId: string, enabled: bool}
func (s *Server) HandleAccountSetEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.AccountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	if err := s.Store.SetAccountEnabled(req.AccountID, req.Enabled); err != nil {
		fail(w, 500, "update enabled: "+err.Error())
		return
	}
	ok(w, map[string]interface{}{"accountId": req.AccountID, "enabled": req.Enabled})
}

// HandlePCLogin tạo session cho Zalo PC login.
// Body: {zpsid, zpw_sek, cipherKey, transport: "pc"} (cipherKey optional — server sẽ lấy từ WS auth nếu rỗng).
// Transport flag 'pc' → set transport trong session để WS layer AES-GCM encrypt.
func (s *Server) HandlePCLogin(w http.ResponseWriter, r *http.Request) {
	// Body: {zpsid, zpw_sek, cipherKey} (cipherKey optional — server sẽ lấy từ
	// WS auth nếu rỗng). Transport flag "pc" → WS layer AES-GCM encrypt.
	//
	// Flow 4-step (T24.4) giống HandleCookieLogin — chỉ khác transport="pc".
	var req struct {
		ZPSID     string `json:"zpsid"`
		ZPWSEK    string `json:"zpw_sek"`
		CipherKey string `json:"cipherKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.ZPSID == "" || req.ZPWSEK == "" {
		fail(w, 400, "thiếu zpsid hoặc zpw_sek")
		return
	}
	cookies := map[string]string{
		"zpsid":   req.ZPSID,
		"zpw_sek": req.ZPWSEK,
	}
	// === Step 1: CookieLogin → session ===
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := core.CookieLogin(ctx, cookies, "", "")
	if err != nil {
		fail(w, 500, "login failed: "+err.Error())
		return
	}
	session := result.Session

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

	// === Step 2: UpsertZaloAccount ===
	zaID, err := s.Store.UpsertZaloAccount(session.UserID, displayName, avatar, "")
	if err != nil {
		fail(w, 500, "upsert zalo_account: "+err.Error())
		return
	}

	// === Step 3: FindOrCreateAccountByZaloAccountID ===
	accountID, err := s.Store.FindOrCreateAccountByZaloAccountID(zaID, session.UserID, displayName, avatar)
	if err != nil {
		fail(w, 500, "find/create account: "+err.Error())
		return
	}
	s.Logger.Info("pc_login_committed", "zalo_account_id", zaID, "account_id", accountID, "transport", "pc")

	// === Step 4: SaveSession + StartZaloListener ===
	cj, _ := json.Marshal(session.Cookies)
	wsList := session.WSURLs
	if wsList == nil {
		wsList = []string{}
	}
	wj, _ := json.Marshal(wsList)
	smj, _ := json.Marshal(session.ServiceMap)
	if session.ServiceMap == nil {
		smj = []byte("{}")
	}

	s.StopZaloListener(accountID)
	sessionID := session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10)
	prevTransport := s.getAccountTransport(accountID)
	if err := s.Store.SaveSession(&store.Session{
		ID: sessionID, AccountID: accountID,
		UserID: session.UserID, Cookies: string(cj), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wj), ServiceMap: string(smj), APIType: session.APIType, APIVersion: session.APIVersion, IsActive: 1, ExpiresAt: session.ExpiresAt,
		Transport: "pc", CipherKey: req.CipherKey,
	}); err != nil {
		fail(w, 500, "save session: "+err.Error())
		return
	}
	if err := s.Store.SetAccountTransport(accountID, "pc"); err != nil {
		s.Logger.Error("account_transport_set_failed", "account_id", accountID, "error", err)
	}
	if prevTransport != "" && prevTransport != "pc" {
		// Transport đã đổi (vd web → pc). Reset syncv2_state để request-sync lại.
		s.Logger.Info("account_transport_changed", "account_id", accountID, "from", prevTransport, "to", "pc", "syncv2_reset", true)
		_ = s.Store.SetAccountSyncV2State(accountID, "{}")
	}
	go s.StartZaloListener(accountID)
	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID, "transport": "pc", "zaloAccountId": zaID})
}

// HandleAccountRestart stop + start zalo listener cho account (fix WS loi,
// session con song). Tra ve status moi.
func (s *Server) HandleAccountRestart(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	s.StopZaloListener(accountID)
	// Nếu session is_active=0 (do bị deactivate bởi DeleteSession hoặc setAccountEnabled),
	// thử refresh bằng cookie login trước khi StartZaloListener — nếu không StartZaloListener
	// sẽ log "no active session" và listener không start.
	sessRec, _ := s.Store.GetActiveSession(accountID)
	if sessRec == nil {
		var err error
		sessRec, err = s.Store.LoadSessionByAccountID(accountID)
		if err == nil && sessRec != nil {
			var ck map[string]string
			if json.Unmarshal([]byte(sessRec.Cookies), &ck) == nil && len(ck) > 0 {
				sessRec = s.autoRefresh(sessRec)
			}
		}
	}
	if sessRec == nil {
		fail(w, 404, "no session + refresh failed, please re-login")
		return
	}
	go s.StartZaloListener(accountID)
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

	s.mu.Lock()
	s.qrFlows[code] = flow
	s.mu.Unlock()

	go func() {
		time.Sleep(5 * time.Minute)
		s.mu.Lock()
		if current, ok := s.qrFlows[code]; ok && current == flow {
			delete(s.qrFlows, code)
		}
		s.mu.Unlock()
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

	s.mu.RLock()
	flow := s.qrFlows[token]
	s.mu.RUnlock()
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

		s.mu.Lock()
		delete(s.qrFlows, token)
		s.mu.Unlock()

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
		s.commitLoginSession(w, result.Session, "")
		return
	case <-time.After(1500 * time.Millisecond):
		ok(w, map[string]interface{}{"pending": true})
		return
	case <-r.Context().Done():
		ok(w, map[string]interface{}{"pending": true})
		return
	}
}

// accountIDHint: accountID user mong đợi (từ re-login flow). "" nếu là đăng
// nhập mới / QR flow. Nếu khác rỗng và khác accountID suy ra từ session.UserID
// → fail vì cookie thuộc user khác.
func (s *Server) commitLoginSession(w http.ResponseWriter, session *core.Session, accountIDHint string) {
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

	// Nếu account đã tồn tại (re-login), KHÔNG đặt enabled=false để tránh
	// xoá trạng thái cũ. CreateAccount dùng upsert (ON CONFLICT DO NOTHING) nên an toàn.
	if accountIDHint != "" && accountIDHint != accountID {
		fail(w, 400, "accountId không khớp userID của cookie mới")
		return
	}
	s.Store.CreateAccount(accountID, displayName, 1)
	if avatar != "" {
		s.Store.UpdateAccount(accountID, displayName, avatar)
	}
	// Re-activate: xoá cờ disabled + disabled_reason nếu có (re-login).
	if err := s.Store.SetAccountDisabledReason(accountID, ""); err != nil {
		s.Logger.Error("account_disabled_reason_clear_failed", "account_id", accountID, "error", err)
	}
	if err := s.Store.SetAccountEnabled(accountID, true); err != nil {
		s.Logger.Error("account_enable_failed", "account_id", accountID, "error", err)
	}
	if err := s.Store.SetAccountUserID(accountID, session.UserID); err != nil {
		s.Logger.Error("account_user_id_set_failed", "account_id", accountID, "error", err)
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
	s.StopZaloListener(accountID)
	s.Store.SaveSession(&store.Session{
		ID: session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10), AccountID: accountID,
		UserID: session.UserID, Cookies: string(cookiesJSON), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wsURLsJSON), ServiceMap: string(serviceMapJSON), APIType: session.APIType, APIVersion: session.APIVersion, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})
	go s.StartZaloListener(accountID)

	// Auto-detect transport dựa trên session.CipherKey:
	//   - CipherKey != "" → PC (AES-GCM, Zalo PC client + PC QR scan tương lai)
	//   - CipherKey == "" → Web (AES-CBC, chat.zalo.me QR scan + cookie)
	// Reset syncv2_state khi transport đổi để request-sync lại
	// (câu hỏi Sếp #6 — pattern giống HandleCookieLogin).
	transport := "web"
	if session.CipherKey != "" {
		transport = "pc"
	}
	prevTransport := s.getAccountTransport(accountID)
	if err := s.Store.SetAccountTransport(accountID, transport); err != nil {
		s.Logger.Error("account_transport_set_failed", "account_id", accountID, "error", err)
	}
	if prevTransport != "" && prevTransport != transport {
		s.Logger.Info("account_transport_changed", "account_id", accountID, "from", prevTransport, "to", transport, "syncv2_reset", true)
		_ = s.Store.SetAccountSyncV2State(accountID, "{}")
	}

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
	unlock, locked := s.tryLockConversationSync(accountID)
	if !locked {
		dbConvs, _ := s.Store.GetConversations(accountID)
		if dbConvs == nil {
			dbConvs = []store.Conversation{}
		}
		ok(w, dbConvs)
		return
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()

	// Start Zalo WS listener nền (Go server tự làm Zalo client)
	go s.StartZaloListener(accountID)

	// Tự động refresh session nếu sắp hết hạn hoặc Zalo báo lỗi
	sessRec = s.autoRefresh(sessRec)
	// Start listener lại với session mới sau autoRefresh.
	go s.StartZaloListener(accountID)

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

	existingConvs, _ := s.Store.GetConversations(accountID)
	existingByID := make(map[string]*store.Conversation, len(existingConvs))
	for i := range existingConvs {
		existingByID[existingConvs[i].ID] = &existingConvs[i]
	}

	fetchConversations := func() ([]core.Conversation, []core.Message, error) {
		// GetConversations thường mất 15-40s. Dùng timeout riêng để group/profile
		// resolution phía sau vẫn có context còn hiệu lực.
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		return client.GetConversations(ctx)
	}

	convs, lastMsgs, err := fetchConversations()
	if err != nil {
		if sessRec2 := s.autoRefresh(sessRec); sessRec2.ID != sessRec.ID {
			// Refresh thành công, thử lại
			sessRec = sessRec2
			json.Unmarshal([]byte(sessRec.Cookies), &cookies)
			session.Cookies = cookies
			session.SecretKey = sessRec.SecretKey
			client = core.NewClient(session)
			convs, lastMsgs, err = fetchConversations()
		}
	}
	if err != nil {
		// Giữ UI hiển thị dữ liệu đã có trong DB nếu Zalo API lỗi.
		s.Logger.Warn("conversation_sync_zalo_failed", "account_id", accountID, "error", err)
		dbConvs, _ := s.Store.GetConversations(accountID)
		if dbConvs == nil {
			dbConvs = []store.Conversation{}
		}
		unlock()
		unlock = nil
		ok(w, dbConvs)
		return
	}
	if convs == nil {
		convs = []core.Conversation{}
	}

	// Lưu vào DB. last_msg_id ưu tiên c.LastMsgID (từ clearUnreads API — có cho
	// TẤT CẢ 156 conv) rồi mới fall back về c.LastMsg.ID (chỉ có khi tin nằm
	// trong data.msgs/data.groupMsgs — thường chỉ 2-3 conv có realtime push mới).
	// Bulk save lastMsgs (tất cả ~15-18 tin từ data.msgs/data.groupMsgs) vào
	// DB để backfill history cho thread 1-1 mà WS sync 510/511 trả rỗng.
	// SaveMessage có merge logic: insert nếu chưa có, update field phụ nếu row
	// cũ rỗng, KHÔNG ghi đè content/timestamp/from_id. An toàn khi chạy nhiều lần.
	for _, m := range lastMsgs {
		if m.ID == "" || m.ConvID == "" {
			continue
		}
		attJSON, _ := json.Marshal(m.Attachments)
		if err := s.Store.SaveMessage(&store.Message{
			ID: m.ID, AccountID: accountID, ConvID: m.ConvID,
			FromID: m.FromID, FromName: m.FromName,
			Content: m.Content, MsgType: int(m.Type),
			Timestamp: m.Timestamp, Attachments: string(attJSON),
		}); err != nil {
			s.Logger.Error("conversation_message_save_failed", "account_id", accountID, "message_id", m.ID, "error", err)
		}
	}

	for _, c := range convs {
		conv := store.Conversation{
			ID: c.ID, AccountID: accountID, Name: c.Name, Avatar: c.Avatar,
			ConvType: int(c.Type), UpdatedAt: time.Now(),
			LastMsgID: c.LastMsgID,
		}
		if c.LastMsg != nil {
			if conv.LastMsgAt.Time.IsZero() {
				conv.LastMsgAt = sql.NullTime{Time: time.UnixMilli(c.LastMsg.Timestamp), Valid: true}
			}
		}
		mergeConversationForSync(&conv, existingByID[c.ID])
		if err := s.Store.SaveConversation(&conv); err != nil {
			s.Logger.Error("conversation_save_failed", "account_id", accountID, "conversation_id", conv.ID, "error", err)
		}
	}

	// Cập nhật tên + avatar cho account (đồng bộ)
	// Dùng context nền cho bước cập nhật DB: client có thể đóng tab/timeout
	// sau khi conversations đã tải xong, nhưng tên Zalo vẫn nên được lưu.
	profileCtx, profileCancel := context.WithTimeout(context.Background(), 10*time.Second)
	profileName, profileAvatar, profileErr := client.GetMyProfile(profileCtx)
	profileCancel()
	if profileErr != nil {
		s.Logger.Warn("conversation_profile_failed", "account_id", accountID, "error", profileErr)
	} else if profileName != "" {
		if profileAvatar == "" {
			if account, err := s.Store.GetAccount(accountID); err == nil && account != nil {
				profileAvatar = account.Avatar
			}
		}
		if err := s.Store.UpdateAccount(accountID, profileName, profileAvatar); err != nil {
			s.Logger.Error("account_profile_update_failed", "account_id", accountID, "error", err)
		}
		if _, err := s.Store.UpsertZaloAccount(sessRec.UserID, profileName, profileAvatar, ""); err != nil {
			s.Logger.Error("zalo_account_profile_update_failed", "account_id", accountID, "error", err)
		}
	}

	// Resolve group names — cập nhật tên + avatar cho group vào cả convs và DB
	var groupIDs []string
	groupSeen := make(map[string]bool)
	for _, c := range convs {
		if c.Type == core.ConvGroup {
			groupIDs = append(groupIDs, c.ID)
			groupSeen[c.ID] = true
		}
	}
	// Giữ các group cũ chỉ còn trong DB (stale conversation) — vẫn resolve
	// lại để tên không bị kẹt ở ID số khi Zalo không trả group đó trong
	// clearUnreads hiện tại.
	for _, c := range existingConvs {
		if c.ConvType == int(core.ConvGroup) && !groupSeen[c.ID] {
			groupIDs = append(groupIDs, c.ID)
			groupSeen[c.ID] = true
		}
	}
	if len(groupIDs) > 0 {
		groupCtx, groupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		gm, err := client.GetGroupInfo(groupCtx, groupIDs)
		groupCancel()
		if err != nil {
			s.Logger.Warn("conversation_group_info_failed", "account_id", accountID, "groups", len(groupIDs), "error", err)
		} else {
			if gm == nil {
				gm = make(map[string]struct{ Name, Avatar string })
			}
			// API đôi lúc bỏ sót 1 group trong batch lớn. Retry riêng các ID
			// còn thiếu để tránh lưu tên group thành ID số.
			var missingIDs []string
			for _, id := range groupIDs {
				if _, ok := gm[id]; !ok {
					missingIDs = append(missingIDs, id)
				}
			}
			for _, id := range missingIDs {
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 10*time.Second)
				one, retryErr := client.GetGroupInfo(retryCtx, []string{id})
				retryCancel()
				if retryErr != nil {
					s.Logger.Warn("conversation_group_info_retry_failed",
						"account_id", accountID, "conversation_id", id, "error", retryErr)
					continue
				}
				for gid, info := range one {
					gm[gid] = info
				}
			}
			var unresolved []string
			for _, id := range groupIDs {
				if _, ok := gm[id]; !ok {
					unresolved = append(unresolved, id)
				}
			}
			if len(unresolved) > 0 {
				s.Logger.Warn("conversation_group_info_incomplete",
					"account_id", accountID, "groups", len(groupIDs), "resolved", len(gm),
					"missing", len(unresolved), "missing_ids", strings.Join(unresolved, ","))
			}
			for _, id := range groupIDs {
				g, ok := gm[id]
				if !ok {
					continue
				}
				name := strings.TrimSpace(g.Name)
				if name == "" {
					continue
				}
				avatar := g.Avatar
				if avatar == "" && existingByID[id] != nil {
					avatar = existingByID[id].Avatar
				}
				for i := range convs {
					if convs[i].ID == id {
						convs[i].Name = name
						convs[i].Avatar = avatar
						break
					}
				}
				if err := s.Store.UpdateConversationIdentity(accountID, id, name, avatar); err != nil {
					s.Logger.Error("conversation_identity_update_failed",
						"account_id", accountID, "conversation_id", id, "error", err)
				}
			}
		}
	}

	// Trả về từ DB để có đầy đủ tên + avatar
	dbConvs, _ := s.Store.GetConversations(accountID)
	if dbConvs == nil {
		dbConvs = []store.Conversation{}
	}
	unlock()
	unlock = nil
	ok(w, dbConvs)
}

func (s *Server) tryLockConversationSync(accountID string) (func(), bool) {
	s.conversationSyncMu.Lock()
	if s.conversationSyncLocks == nil {
		s.conversationSyncLocks = make(map[string]*sync.Mutex)
	}
	mu := s.conversationSyncLocks[accountID]
	if mu == nil {
		mu = &sync.Mutex{}
		s.conversationSyncLocks[accountID] = mu
	}
	s.conversationSyncMu.Unlock()

	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

func conversationNameMissing(name, id string) bool {
	name = strings.TrimSpace(name)
	return name == "" || name == id
}

// mergeConversationForSync giữ dữ liệu DB cũ khi payload Zalo thiếu field.
// Đặc biệt không để tên group đã resolve bị ghi đè bằng ID chỉ vì
// clearUnreads trả userName rỗng.
func mergeConversationForSync(incoming, existing *store.Conversation) {
	if existing == nil {
		return
	}
	if conversationNameMissing(incoming.Name, incoming.ID) &&
		!conversationNameMissing(existing.Name, existing.ID) {
		incoming.Name = existing.Name
	}
	if incoming.Avatar == "" {
		incoming.Avatar = existing.Avatar
	}
	if incoming.LastMsgID == "" {
		incoming.LastMsgID = existing.LastMsgID
	}
	if !incoming.LastMsgAt.Valid {
		incoming.LastMsgAt = existing.LastMsgAt
	}
	incoming.Unread = existing.Unread
}

// ========== MESSAGES ==========

func (s *Server) HandleMessages(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("convId")
	if convID == "" {
		fail(w, 400, "missing convId")
		return
	}
	// accountId optional — nếu trống, tự tìm theo convId (ưu tiên enabled + lastMsgAt).
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		var err error
		accountID, err = s.Store.FindAccountByConvID(convID)
		if err != nil {
			fail(w, 500, "find account: "+err.Error())
			return
		}
		if accountID == "" {
			fail(w, 404, "conv not found")
			return
		}
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
	if req.To == "" || req.Content == "" {
		fail(w, 400, "missing to or content")
		return
	}
	// accountId optional — tự tìm theo conv (ưu tiên account enabled).
	if req.AccountID == "" {
		var err error
		req.AccountID, err = s.Store.FindAccountByConvID(req.To)
		if err != nil {
			fail(w, 500, "find account: "+err.Error())
			return
		}
		if req.AccountID == "" {
			fail(w, 404, "conv not found")
			return
		}
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
	// Broadcast tới browser WS (cùng shape với handleZaloEvent) để tab chat
	// đang mở thấy tin ngay, không cần reload/refetch.
	s.WSManager.Broadcast(req.AccountID, BrowserMessage{
		Type: "new_message",
		Data: map[string]interface{}{
			"id":          msg.ID,
			"convId":      msg.ConvID,
			"fromId":      sessRec.UserID,
			"fromName":    "",
			"content":     msg.Content,
			"timestamp":   msg.Timestamp,
			"type":        msg.Type,
			"attachments": msg.Attachments,
			"isAck":       msg.IsDeliveryAck,
			"ackStatus":   msg.AckStatus,
		},
	})
	ok(w, map[string]interface{}{"sent": true, "msgId": msg.ID, "content": msg.Content, "timestamp": msg.Timestamp})
}

// ========== SYNC MESSAGES ==========

func (s *Server) HandleSyncMessages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		ConvID    string `json:"convId"`
		LastID    string `json:"lastId"`
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
	lastID := req.LastID
	if conv != nil {
		convType = conv.ConvType
		if lastID == "" {
			lastID = conv.LastMsgID
		}
	}
	beforeCount, _ := s.Store.CountMessages(req.AccountID, req.ConvID)
	wsSent := s.RequestOldMessagesViaListener(req.AccountID, req.ConvID, convType, lastID)

	// 510/511 chỉ xác nhận đã gửi request, không đảm bảo Zalo trả dữ liệu.
	// Chờ DB tăng trong thời gian ngắn; nếu WS im lặng thì dùng REST history.
	afterCount := beforeCount
	if wsSent {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			afterCount, _ = s.Store.CountMessages(req.AccountID, req.ConvID)
			if afterCount > beforeCount {
				break
			}
		}
	}
	if afterCount > beforeCount {
		synced += afterCount - beforeCount
		if err := s.Store.RefreshConversationLastMessage(req.AccountID, req.ConvID); err != nil {
			s.Logger.Error("conversation_last_message_refresh_failed",
				"account_id", req.AccountID, "conversation_id", req.ConvID, "error", err)
		}
	}

	// REST fallback cho group khi WS không chạy hoặc không trả thêm message.
	if needsGroupHistoryFallback(convType, wsSent, beforeCount, afterCount) {
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
			if err != nil || len(msgs) == 0 {
				if legacyMsgs, legacyErr := client.GetGroupHistory(ctx, req.ConvID, 50); legacyErr == nil && len(legacyMsgs) > 0 {
					msgs, err = legacyMsgs, nil
				} else if legacyErr != nil {
					s.Logger.Warn("group_history_legacy_fallback_failed",
						"account_id", req.AccountID, "conversation_id", req.ConvID, "error", legacyErr)
				}
			}
			if err == nil {
				for _, m := range msgs {
					if m.ID == "" {
						continue
					}
					attJSON, _ := json.Marshal(m.Attachments)
					if err := s.Store.SaveGroupHistoryMessage(&store.Message{
						ID: m.ID, AccountID: req.AccountID, ConvID: req.ConvID,
						FromID: m.FromID, FromName: m.FromName, Content: m.Content,
						MsgType: int(m.Type), Timestamp: m.Timestamp, Attachments: string(attJSON),
					}); err != nil {
						s.Logger.Error("group_history_message_save_failed",
							"account_id", req.AccountID, "conversation_id", req.ConvID,
							"message_id", m.ID, "error", err)
					}
				}
				restCount, _ := s.Store.CountMessages(req.AccountID, req.ConvID)
				added := restCount - afterCount
				if added > 0 {
					synced += added
				}
				if len(msgs) > 0 {
					if err := s.Store.RefreshConversationLastMessage(req.AccountID, req.ConvID); err != nil {
						s.Logger.Error("conversation_last_message_refresh_failed",
							"account_id", req.AccountID, "conversation_id", req.ConvID, "error", err)
					}
				}
				s.Logger.Info("group_history_fallback_completed",
					"account_id", req.AccountID, "conversation_id", req.ConvID,
					"messages", len(msgs), "added", added)
				if len(msgs) > 0 && added == 0 {
					s.Logger.Warn("group_history_messages_not_inserted",
						"account_id", req.AccountID, "conversation_id", req.ConvID,
						"first_message_id", msgs[0].ID, "first_timestamp", msgs[0].Timestamp,
						"first_type", msgs[0].Type)
				}
			} else {
				s.Logger.Warn("group_history_fallback_failed",
					"account_id", req.AccountID, "conversation_id", req.ConvID, "error", err)
			}
		}
	}
	ok(w, map[string]interface{}{"syncing": wsSent || synced > 0, "synced": synced})
}

func needsGroupHistoryFallback(convType int, wsSent bool, beforeCount, afterCount int) bool {
	return convType == int(core.ConvGroup) && (!wsSent || afterCount <= beforeCount)
}

// HandleFriends trả về danh sách friend cache trong DB (bảng contacts) cho
// các account enabled. KHÔNG gọi Zalo live — FriendsWorker định kỳ upsert
// vào DB để tránh rate-limit (429).
//
// Query params:
//
//	accountId (optional) — nếu có, lọc 1 account. Nếu không, merge từ TẤT
//	CẢ enabled accounts (mặc định — UI dùng cái này).
func (s *Server) HandleFriends(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")

	var accountIDs []string
	if accountID != "" {
		accountIDs = []string{accountID}
	} else {
		// Lấy tất cả enabled account IDs.
		enabledAccs, err := s.Store.ListAccounts(0, true)
		if err != nil {
			fail(w, 500, "list accounts: "+err.Error())
			return
		}
		for _, a := range enabledAccs {
			accountIDs = append(accountIDs, a.ID)
		}
	}
	if len(accountIDs) == 0 {
		ok(w, []store.Contact{})
		return
	}

	contacts, err := s.Store.ListContactsByAccounts(accountIDs)
	if err != nil {
		fail(w, 500, "list contacts: "+err.Error())
		return
	}

	// Trả về Contact[] (có accountId) để frontend merge đúng theo account.
	ok(w, contacts)
}

// ========== LOGOUT ==========

// HandleLogout xoá session + reset cipher_key. GIỮ identity (zalo_accounts)
// + data zcloud đã thu thập (messages/conversations/media). Sếp logout rồi
// login lại cùng UID → vẫn thấy data cũ.
//
// Tách rõ 2 hành vi (T24.3):
//   - HandleLogout (đây) → LogoutAccount: xoá sessions, giữ data + identity.
//   - HandleDeleteAccount → DeleteAccountByZaloAccountID: cascade sạch toàn bộ.
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
	s.StopZaloListener(req.AccountID)
	if err := s.Store.LogoutAccount(req.AccountID); err != nil {
		fail(w, 500, "logout: "+err.Error())
		return
	}
	ok(w, map[string]interface{}{"loggedOut": true, "accountId": req.AccountID})
}

// HandleDeleteAccount xoá sạch toàn bộ footprint của account Zalo khỏi zcloud
// (T24.3 — UI nút "Xoá"). Cascade:
//
//	zalo_accounts → accounts → sessions, messages, conversations, media, contacts
//
// Sau khi xoá, account không hiển thị trong UI nữa (cả panel Quản lý + convs).
// Khác HandleLogout: HandleLogout giữ identity + data, HandleDeleteAccount
// xoá tất cả. Caller (UI) nên confirm trước khi gọi.
//
// Body: {accountId: string}
func (s *Server) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
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
	// Lấy zalo_account_id để cascade.
	acc, err := s.Store.GetAccount(req.AccountID)
	if err != nil || acc == nil {
		fail(w, 404, "account not found")
		return
	}
	if acc.ZaloAccountID == "" {
		fail(w, 500, "account không có zalo_account_id (chưa qua T24 migration?)")
		return
	}
	s.StopZaloListener(req.AccountID)
	if err := s.Store.DeleteAccountByZaloAccountID(acc.ZaloAccountID); err != nil {
		fail(w, 500, "delete: "+err.Error())
		return
	}
	s.Logger.Info("account_deleted", "zalo_account_id", acc.ZaloAccountID, "account_id", req.AccountID)
	ok(w, map[string]interface{}{"deleted": true, "accountId": req.AccountID, "zaloAccountId": acc.ZaloAccountID})
}

// ========== COOKIE LOGIN ==========

func (s *Server) HandleCookieLogin(w http.ResponseWriter, r *http.Request) {
	// Body: {zpsid: string, zpw_sek: string} — copy thủ công từ DevTools →
	// Application → Cookies → chat.zalo.me. Hai field là tối thiểu để
	// core.CookieLogin / getLoginInfo thành công.
	//
	// Transport mặc định = "web" (câu hỏi Sếp #2: pc là primary nhưng web
	// vẫn là fallback khi pc fail). PC login dùng HandlePCLogin riêng.
	var req struct {
		ZPSID     string `json:"zpsid"`
		ZPWSEK    string `json:"zpw_sek"`
		AccountID string `json:"accountId,omitempty"` // optional: re-login vào account cũ (vd auth_expired)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid body")
		return
	}
	if req.ZPSID == "" || req.ZPWSEK == "" {
		fail(w, 400, "thiếu zpsid hoặc zpw_sek")
		return
	}
	cookies := map[string]string{
		"zpsid":   req.ZPSID,
		"zpw_sek": req.ZPWSEK,
	}

	// === Step 1: CookieLogin → session ===
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := core.CookieLogin(ctx, cookies, "", "")
	if err != nil {
		fail(w, 500, "login failed: "+err.Error())
		return
	}
	session := result.Session

	// Lấy tên thật từ Zalo API (không block login nếu GetMyProfile fail).
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

	// === Step 2: UpsertZaloAccount (T24 — 3-tier identity) ===
	// Tạo/cập nhật identity Zalo. Idempotent — login lại chỉ refresh name/avatar.
	zaID, err := s.Store.UpsertZaloAccount(session.UserID, displayName, avatar, "")
	if err != nil {
		fail(w, 500, "upsert zalo_account: "+err.Error())
		return
	}

	// === Step 3: FindOrCreateAccountByZaloAccountID ===
	// Tìm accounts row đã có theo FK, hoặc tạo mới nếu chưa có (lần đầu login).
	// Quyết định Sếp #3: 1 zalo_account = 1 account (reuse row cũ nếu có).
	accountID, err := s.Store.FindOrCreateAccountByZaloAccountID(zaID, session.UserID, displayName, avatar)
	if err != nil {
		fail(w, 500, "find/create account: "+err.Error())
		return
	}
	s.Logger.Info("web_login_committed", "zalo_account_id", zaID, "account_id", accountID, "transport", "web")

	// === Step 4: SaveSession + StartZaloListener ===
	// Transport "web" (câu hỏi Sếp #2: fallback khi pc fail).
	if req.AccountID != "" && req.AccountID != accountID {
		s.Logger.Warn("login_account_mismatch", "requested_account_id", req.AccountID, "resolved_account_id", accountID)
	}
	cj, _ := json.Marshal(session.Cookies)
	wj, _ := json.Marshal(session.WSURLs)
	smj, _ := json.Marshal(session.ServiceMap)
	if session.ServiceMap == nil {
		smj = []byte("{}")
	}
	sessionID := session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10)
	prevTransport := s.getAccountTransport(accountID) // để reset syncv2_state khi transport đổi (câu hỏi Sếp #6)
	if err := s.Store.SaveSession(&store.Session{
		ID: sessionID, AccountID: accountID,
		UserID: session.UserID, Cookies: string(cj), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wj), ServiceMap: string(smj), APIType: session.APIType, APIVersion: session.APIVersion, IsActive: 1, ExpiresAt: session.ExpiresAt,
		Transport: "web",
	}); err != nil {
		fail(w, 500, "save session: "+err.Error())
		return
	}
	// Set transport trên account row + reset syncv2_state nếu transport đổi.
	if err := s.Store.SetAccountTransport(accountID, "web"); err != nil {
		s.Logger.Error("account_transport_set_failed", "account_id", accountID, "error", err)
	}
	if prevTransport != "" && prevTransport != "web" {
		// Transport đã đổi (vd pc → web). Reset syncv2_state để request-sync lại.
		s.Logger.Info("account_transport_changed", "account_id", accountID, "from", prevTransport, "to", "web", "syncv2_reset", true)
		_ = s.Store.SetAccountSyncV2State(accountID, "{}")
	}
	go s.StartZaloListener(accountID)
	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID, "transport": "web", "zaloAccountId": zaID})
}

// getAccountTransport đọc transport hiện tại của account (helper nội bộ).
func (s *Server) getAccountTransport(accountID string) string {
	a, err := s.Store.GetAccount(accountID)
	if err != nil || a == nil {
		return ""
	}
	return a.Transport
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
	// Đã có account enabled trong DB → chuyển thẳng sang /chat thay vì
	// hiện QR login. Trước đây root / luôn trả login page bất kể session,
	// Sếp tưởng bị logout khi mở tab mới. Lỗi DB thì fallback hiện login.
	if accounts, err := s.Store.ListAccounts(0, true); err == nil && len(accounts) > 0 {
		http.Redirect(w, r, "/chat", http.StatusFound)
		return
	}
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
	accounts, _ := s.Store.ListAccounts(1, false)
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
		s.Logger.Error("session_refresh_cookie_login_failed", "account_id", sessRec.AccountID, "error", err)
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
	s.StopZaloListener(accountID)
	saveErr := s.Store.SaveSession(newSession)
	if saveErr != nil {
		s.Logger.Error("session_save_failed", "account_id", accountID, "session_id", newSession.ID, "error", saveErr)
	}
	// SaveSession tự vô hiệu hoá session active cũ của account.
	go s.StartZaloListener(accountID)
	s.Logger.Info("session_refreshed", "account_id", accountID, "session_id", newSession.ID, "expires_at", session.ExpiresAt.Format(time.RFC3339))
	return newSession
}

// RefreshAllSessions refresh tat ca session sap het han (goi tu background goroutine)
func (s *Server) RefreshAllSessions() {
	accounts, err := s.Store.ListAccounts(1, false)
	if err != nil {
		return
	}
	for _, a := range accounts {
		sessRec, err := s.Store.GetActiveSession(a.ID)
		if err != nil || sessRec == nil {
			continue
		}
		if time.Now().After(sessRec.ExpiresAt.Add(-30 * time.Minute)) {
			s.Logger.Debug("session_refresh_background_account", "account_id", a.ID)
			s.autoRefresh(sessRec)
		}
	}
}
