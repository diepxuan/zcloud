package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"sync"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// Client đại diện cho một Zalo client (core stub)
type Client struct {
	AccountID string
	Session   map[string]string
}

// Server gắn kết HTTP server với store + core client
type Server struct {
	Store  *store.Store
	Logger *log.Logger

	// Multi-user: map accountID → ZaloClient
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewServer tạo server instance
func NewServer(s *store.Store, logger *log.Logger) *Server {
	return &Server{
		Store:   s,
		Logger:  logger,
		clients: make(map[string]*Client),
	}
}

// ====================================
// Response helpers
// ====================================

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
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: data})
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIResponse{OK: false, Error: msg, Code: status})
}

// ====================================
// Health
// ====================================

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"service": "zcloud",
		"time":    time.Now().Unix(),
	})
}

// ====================================
// QR Login
// ====================================

// HandleLoginQR tạo QR code và trả về base64 image + token
func (s *Server) HandleLoginQR(w http.ResponseWriter, r *http.Request) {
	// Tạo context với timeout
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// QR login real
	result, err := core.QRLogin(ctx)
	if err != nil {
		s.Logger.Printf("QR login error: %v", err)
		fail(w, http.StatusInternalServerError, "QR login failed: "+err.Error())
		return
	}

	// Lưu session vào database
	session := result.Session
	cookiesJSON, _ := json.Marshal(session.Cookies)

	// Tạo account nếu chưa có
	accountID := "acc_" + session.UserID
	if err := s.Store.CreateAccount(accountID, "Zalo User "+session.UserID[:8], 1); err != nil {
		s.Logger.Printf("create account error: %v", err)
	}

	// Lưu session
	wsURLsJSON, _ := json.Marshal(session.WSURLs)
	sessRecord := &store.Session{
		ID:         session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10),
		AccountID:  accountID,
		UserID:     session.UserID,
		Cookies:    string(cookiesJSON),
		SecretKey:  session.SecretKey,
		IMEI:       session.IMEI,
		UserAgent:  session.UserAgent,
		Language:   "vi",
		WSURLs:     string(wsURLsJSON),
		APIType:    30,
		APIVersion: 665,
		IsActive:   1,
		ExpiresAt:  session.ExpiresAt,
	}
	if err := s.Store.SaveSession(sessRecord); err != nil {
		s.Logger.Printf("save session error: %v", err)
	}

	ok(w, map[string]interface{}{
		"accountId": accountID,
		"userId":    session.UserID,
		"expiresAt": session.ExpiresAt,
	})
}

// ====================================
// Conversations
// ====================================

func (s *Server) HandleConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, http.StatusBadRequest, "missing accountId")
		return
	}

	convs, err := s.Store.GetConversations(accountID)
	if err != nil {
		s.Logger.Printf("get convs error: %v", err)
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convs == nil {
		convs = []store.Conversation{}
	}
	ok(w, convs)
}

// ====================================
// Messages
// ====================================

func (s *Server) HandleMessages(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	convID := r.URL.Query().Get("convId")
	cursorStr := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")

	if accountID == "" || convID == "" {
		fail(w, http.StatusBadRequest, "missing accountId or convId")
		return
	}

	cursor, _ := strconv.ParseInt(cursorStr, 10, 64)
	if cursor == 0 {
		cursor = time.Now().UnixMilli() + 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	msgs, err := s.Store.GetMessages(accountID, convID, cursor, limit)
	if err != nil {
		s.Logger.Printf("get msgs error: %v", err)
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	ok(w, msgs)
}

// ====================================
// Send Message (qua core)
// ====================================

func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		To        string `json:"to"`   // conversation ID
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	if req.AccountID == "" || req.Content == "" {
		fail(w, http.StatusBadRequest, "missing accountId or content")
		return
	}

	// TODO: qua core gửi tin nhắn thật
	// Hiện tại return success giả

	ok(w, map[string]interface{}{
		"sent":    true,
		"content": req.Content,
		"to":      req.To,
	})
}

// ====================================
// QR Code page (trả về HTML với QR image)
// ====================================

func (s *Server) HandleQRPage(w http.ResponseWriter, r *http.Request) {
	// Lấy QR image từ login
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := core.QRLogin(ctx)
	if err != nil {
		// Nếu lỗi, hiển thị form cho phép nhập cookie
		s.serveQRForm(w, "QR login error: "+err.Error())
		return
	}

	// Trích xuất QR image từ login flow (lấy từ step 4)
	qrImage := ""
	if result.LoginInfo != nil {
		if img, ok := result.LoginInfo["image"].(string); ok {
			qrImage = img
		}
	}

	if qrImage == "" {
		// Không có QR từ flow (do flow tự động login), cho phép login cookie
		s.serveQRForm(w, "")
		return
	}

	// Hiển thị QR + tự động check login status
	s.serveQRWithCheck(w, qrImage, result)
}

func (s *Server) serveQRForm(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	errBlock := ""
	if errMsg != "" {
		errBlock = fmt.Sprintf(`<div style="color:red;margin-bottom:12px">%s</div>`, errMsg)
	}
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Login</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;display:flex;justify-content:center;align-items:center;min-height:100vh}
.card{background:#fff;border-radius:12px;padding:32px;width:90%;max-width:420px;box-shadow:0 2px 12px rgba(0,0,0,.1)}
h1{font-size:22px;margin-bottom:16px;color:#333}
input,textarea{width:100%;padding:10px;border:1px solid #ddd;border-radius:6px;font-size:14px;margin-bottom:12px}
button{width:100%;padding:12px;background:#0068ff;color:#fff;border:none;border-radius:6px;font-size:16px;cursor:pointer}
button:hover{background:#0052cc}
.qr{padding:24px;background:#fff;border-radius:8px;margin-bottom:16px;text-align:center}
.qr img{max-width:240px}
.hidden{display:none}
.tab{display:flex;margin-bottom:16px;gap:8px}
.tab button{flex:1;padding:8px;background:#eee;color:#333;border-radius:6px}
.tab button.active{background:#0068ff;color:#fff}
</style>
</head><body>
<div class="card">
<h1>ZCloud Login</h1>
`+errBlock+`
<div class="tab">
<button class="active" onclick="showTab('qr')">QR Code</button>
<button onclick="showTab('cookie')">Cookie</button>
</div>

<div id="tab-qr">
<p style="margin-bottom:16px;color:#666">Đang tạo QR code...</p>
</div>

<div id="tab-cookie" class="hidden">
<p style="margin-bottom:12px;color:#666">Dán cookie từ Zalo Web (F12 → Application → Cookies → chat.zalo.me)</p>
<textarea id="cookieInput" rows="6" placeholder="zpsid=...; zpw_sek=..."></textarea>
<button onclick="loginCookie()">Đăng nhập</button>
</div>

<div id="status" style="margin-top:16px;padding:12px;border-radius:6px;display:none"></div>
</div>

<script>
function showTab(tab){
document.querySelectorAll('.tab button').forEach(b=>b.classList.remove('active'));
document.getElementById('tab-qr').classList.add('hidden');
document.getElementById('tab-cookie').classList.add('hidden');
if(tab==='qr'){
document.querySelector('.tab button:first-child').classList.add('active');
document.getElementById('tab-qr').classList.remove('hidden');
}else{
document.querySelector('.tab button:last-child').classList.add('active');
document.getElementById('tab-cookie').classList.remove('hidden');
}
}
async function loginCookie(){
const s=document.getElementById('status');
const cookie=document.getElementById('cookieInput').value.trim();
if(!cookie){s.innerHTML='<span style="color:red">Nhập cookie</span>';s.style.display='block';return}
s.innerHTML='Đang đăng nhập...';s.style.display='block';s.style.background='#e8f4fd';
try{
const r=await fetch('/api/login/cookie',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cookie})});
const d=await r.json();
if(d.ok){s.innerHTML='Đăng nhập thành công! Đang chuyển...';s.style.background='#d4edda';setTimeout(()=>window.location.href='/chat',1000)}
else{s.innerHTML='<span style="color:red">Lỗi: '+d.error+'</span>';s.style.background='#f8d7da'}
}catch(e){s.innerHTML='<span style="color:red">Lỗi kết nối</span>';s.style.background='#f8d7da'}
}
</script>
</body></html>`)
}

func (s *Server) serveQRWithCheck(w http.ResponseWriter, qrImage string, result *core.LoginResult) {
	qrBlock := ""
	if qrImage != "" {
		if strings.HasPrefix(qrImage, "data:image") {
			qrBlock = fmt.Sprintf(`<img src="%s" alt="QR Code">`, qrImage)
		} else {
			qrBlock = fmt.Sprintf(`<img src="data:image/png;base64,%s" alt="QR Code">`, qrImage)
		}
	}

	userID := ""
	if result != nil && result.Session != nil {
		userID = result.Session.UserID
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud — Chat</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;display:flex;justify-content:center;align-items:center;min-height:100vh}
.card{background:#fff;border-radius:12px;padding:32px;width:90%%;max-width:420px;box-shadow:0 2px 12px rgba(0,0,0,.1);text-align:center}
h1{font-size:20px;margin-bottom:8px;color:#333}
p{color:#666;margin-bottom:8px;font-size:14px}
.qr-box{padding:16px;border:2px dashed #ddd;border-radius:8px;margin:16px 0}
.qr-box img{max-width:200px}
.badge{display:inline-block;padding:4px 12px;background:#e8f4fd;color:#0068ff;border-radius:12px;font-size:12px;margin-top:8px}
</style>
</head><body>
<div class="card">
<h1>ZCloud</h1>
<p>Đăng nhập thành công</p>
<div class="badge">UID: %s</div>
<div class="qr-box">%s</div>
<p style="font-size:13px;color:#999">Đã đăng nhập. Đang chuyển đến chat...</p>
</div>
<script>setTimeout(()=>window.location.href='/chat',2000)</script>
</body></html>`, userID, qrBlock)
}

// ====================================
// Cookie login endpoint
// ====================================

func (s *Server) HandleCookieLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid body")
		return
	}

	cookies := parseCookieString(req.Cookie)
	if len(cookies) == 0 {
		fail(w, http.StatusBadRequest, "no cookies found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := core.LoginWithCookies(ctx, cookies, "", "")
	if err != nil {
		fail(w, http.StatusInternalServerError, "login failed: "+err.Error())
		return
	}

	// Lưu session
	session := result.Session
	accountID := "acc_" + session.UserID
	s.Store.CreateAccount(accountID, "Zalo User "+session.UserID[:8], 1)

	cookiesJSON, _ := json.Marshal(session.Cookies)
	wsURLsJSON, _ := json.Marshal(session.WSURLs)
	s.Store.SaveSession(&store.Session{
		ID:         session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10),
		AccountID:  accountID,
		UserID:     session.UserID,
		Cookies:    string(cookiesJSON),
		SecretKey:  session.SecretKey,
		IMEI:       session.IMEI,
		UserAgent:  session.UserAgent,
		Language:   "vi",
		WSURLs:     string(wsURLsJSON),
		APIType:    30,
		APIVersion: 665,
		IsActive:   1,
		ExpiresAt:  session.ExpiresAt,
	})

	ok(w, map[string]interface{}{
		"accountId": accountID,
		"userId":    session.UserID,
	})
}

func parseCookieString(s string) map[string]string {
	cookies := make(map[string]string)
	pairs := strings.Split(s, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			cookies[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return cookies
}

// ====================================
// Chat page (Web UI chính)
// ====================================

func (s *Server) HandleChatPage(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.Store.ListAccounts(1) // Zalo User
	if err != nil {
		accounts = nil
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	accountOptions := ""
	if len(accounts) > 0 {
		for _, a := range accounts {
			sel := ""
			if len(accounts) == 1 {
				sel = "selected"
			}
			displayName := a.DisplayName
			if displayName == "" {
				displayName = a.ID
			}
			accountOptions += fmt.Sprintf(`<option value="%s" %s>%s</option>`, a.ID, sel, displayName)
		}
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Chat</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;height:100vh;display:flex;flex-direction:column}
.header{background:#0068ff;color:#fff;padding:12px 16px;display:flex;align-items:center;gap:12px}
.header h1{font-size:18px;flex:1}
.header select{background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 10px;border-radius:4px;font-size:13px}
.header select option{color:#333}
.container{display:flex;flex:1;overflow:hidden}
.sidebar{width:300px;background:#fff;border-right:1px solid #e0e0e0;overflow-y:auto}
.sidebar .search{padding:12px;border-bottom:1px solid #e0e0e0}
.sidebar .search input{width:100%%;padding:8px 12px;border:1px solid #ddd;border-radius:6px;font-size:13px}
.conv{padding:12px 16px;cursor:pointer;border-bottom:1px solid #f0f0f0}
.conv:hover{background:#f5f8ff}
.conv.active{background:#e8f0ff}
.conv-name{font-size:14px;font-weight:500;color:#333}
.conv-preview{font-size:12px;color:#999;margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.chat-area{flex:1;display:flex;flex-direction:column}
.chat-header{padding:12px 16px;background:#fff;border-bottom:1px solid #e0e0e0}
.chat-header h2{font-size:16px;color:#333}
.messages{flex:1;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:8px}
.msg{max-width:70%%;padding:10px 14px;border-radius:12px;font-size:14px;line-height:1.4}
.msg.in{background:#fff;align-self:flex-start;border-bottom-left-radius:4px}
.msg.out{background:#0068ff;color:#fff;align-self:flex-end;border-bottom-right-radius:4px}
.msg .time{font-size:10px;color:inherit;opacity:.6;margin-top:4px;text-align:right}
.msg.out .time{color:rgba(255,255,255,.7)}
.input-area{display:flex;padding:12px 16px;background:#fff;border-top:1px solid #e0e0e0;gap:8px}
.input-area input{flex:1;padding:10px 14px;border:1px solid #ddd;border-radius:20px;font-size:14px;outline:none}
.input-area input:focus{border-color:#0068ff}
.input-area button{padding:10px 20px;background:#0068ff;color:#fff;border:none;border-radius:20px;font-size:14px;cursor:pointer}
.input-area button:hover{background:#0052cc}
.empty-state{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;color:#999;gap:8px}
.empty-state .icon{font-size:48px}
.load-more{text-align:center;padding:8px;color:#0068ff;cursor:pointer;font-size:13px}
.hidden{display:none}
@media(max-width:768px){.sidebar{width:100%%}.sidebar.hide{display:none}}
</style>
</head><body>
<div class="header">
<h1>ZCloud</h1>
<select id="accountSelect" onchange="switchAccount()">%s</select>
<button onclick="window.location.href='/'" style="background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 12px;border-radius:4px;cursor:pointer">Logout</button>
</div>
<div class="container">
<div class="sidebar" id="sidebar">
<div class="search"><input type="text" placeholder="Tìm kiếm..." oninput="filterConvs(this.value)"></div>
<div id="convList"></div>
</div>
<div class="chat-area">
<div class="chat-header"><h2 id="chatTitle">Chọn hội thoại</h2></div>
<div class="messages" id="msgList">
<div class="empty-state"><div class="icon">💬</div><div>Chọn một hội thoại để bắt đầu</div></div>
</div>
<div class="input-area hidden" id="inputArea">
<input type="text" id="msgInput" placeholder="Nhập tin nhắn..." onkeydown="if(event.key==='Enter')sendMsg()">
<button onclick="sendMsg()">Gửi</button>
</div>
</div>
</div>

<script>
let currentConv = '';
let currentAccount = document.getElementById('accountSelect').value;
let cursor = 0;
let loading = false;
let ws = null;

document.addEventListener('DOMContentLoaded',()=>{if(currentAccount)loadConvs()});

function switchAccount(){
currentAccount=document.getElementById('accountSelect').value;
if(currentAccount){loadConvs()}
}

async function loadConvs(){
if(!currentAccount)return;
try{
const r=await fetch('/api/conversations?accountId='+currentAccount);
const d=await r.json();
if(d.ok){
const list=document.getElementById('convList');
list.innerHTML='';
d.data.forEach(c=>{
const div=document.createElement('div');
div.className='conv';
div.dataset.id=c.id;
div.innerHTML='<div class="conv-name">'+escapeHtml(c.name||c.id)+'</div>'+
'<div class="conv-preview">'+escapeHtml(c.lastMsgId||'')+'</div>';
div.onclick=()=>selectConv(c.id);
list.appendChild(div);
});
}
}catch(e){console.error(e)}
}

function selectConv(id){
currentConv=id;
cursor=Date.now()*1000;
document.querySelectorAll('.conv').forEach(c=>c.classList.toggle('active',c.dataset.id===id));
document.getElementById('chatTitle').textContent=id;
document.getElementById('inputArea').classList.remove('hidden');
document.getElementById('msgList').innerHTML='<div class="load-more" onclick="loadMsgs()">Tải tin nhắn cũ</div>';
loadMsgs();
}

async function loadMsgs(){
if(!currentConv||loading||!currentAccount)return;
loading=true;
try{
const r=await fetch('/api/messages?accountId='+currentAccount+'&convId='+currentConv+'&cursor='+cursor+'&limit=30');
const d=await r.json();
if(d.ok){
const list=document.getElementById('msgList');
if(cursor>Date.now()*1000)list.innerHTML='';
d.data.forEach(m=>{
const div=document.createElement('div');
div.className='msg '+(m.fromId===currentAccount?'out':'in');
const t=new Date(m.timestamp);
const timeStr=t.getHours().toString().padStart(2,'0')+':'+t.getMinutes().toString().padStart(2,'0');
div.innerHTML='<div>'+escapeHtml(m.content)+'</div><div class="time">'+timeStr+'</div>';
list.appendChild(div);
});
if(d.data.length>0){cursor=d.data[0].timestamp}
list.scrollTop=list.scrollHeight;
}
}catch(e){console.error(e)}
finally{loading=false}
}

async function sendMsg(){
const input=document.getElementById('msgInput');
const content=input.value.trim();
if(!content||!currentConv||!currentAccount)return;
input.value='';
try{
const r=await fetch('/api/messages/send',{method:'POST',headers:{'Content-Type':'application/json'},
body:JSON.stringify({accountId:currentAccount,to:currentConv,content})});
const d=await r.json();
if(d.ok){
const list=document.getElementById('msgList');
const div=document.createElement('div');
div.className='msg out';
const t=new Date();
div.innerHTML='<div>'+escapeHtml(content)+'</div><div class="time">'+t.getHours().toString().padStart(2,'0')+':'+t.getMinutes().toString().padStart(2,'0')+'</div>';
list.appendChild(div);
list.scrollTop=list.scrollHeight;
}
}catch(e){console.error(e)}
}

function filterConvs(q){
document.querySelectorAll('.conv').forEach(c=>{
c.style.display=c.textContent.toLowerCase().includes(q.toLowerCase())?'':'none';
});
}

function escapeHtml(s){
const d=document.createElement('div');
d.textContent=s;
return d.innerHTML;
}
</script>
</body></html>`, accountOptions)
}

// HandleLoginPage is an alias for HandleQRPage for compatibility
func (s *Server) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	// Sử dụng QR form page
	s.serveQRForm(w, "")
}
