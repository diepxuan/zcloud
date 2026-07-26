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

// Server gắn kết HTTP server với store + core client
type Server struct {
	Store  *store.Store
	Logger *log.Logger
	mu      sync.RWMutex
	clients map[string]*core.Client
}

func NewServer(s *store.Store, logger *log.Logger) *Server {
	return &Server{
		Store:   s,
		Logger:  logger,
		clients: make(map[string]*core.Client),
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
	ok(w, map[string]interface{}{"service": "zcloud", "time": time.Now().Unix()})
}

// ====================================
// QR Login (2 bước: create + poll)
// ====================================

var qrSessions = make(map[string]*core.QRLoginSession)
var qrMu sync.RWMutex

func (s *Server) HandleCreateQR(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	qrSession, err := core.CreateQRLogin(ctx)
	if err != nil {
		s.Logger.Printf("create QR error: %v", err)
		fail(w, http.StatusInternalServerError, "Tao QR that bai: "+err.Error())
		return
	}

	code := qrSession.Code
	qrMu.Lock()
	qrSessions[code] = qrSession
	qrMu.Unlock()

	go func() {
		time.Sleep(5 * time.Minute)
		qrMu.Lock()
		delete(qrSessions, code)
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
		var req struct{ Token string `json:"token"` }
		json.NewDecoder(r.Body).Decode(&req)
		token = req.Token
	}
	if token == "" {
		fail(w, http.StatusBadRequest, "missing token")
		return
	}

	qrMu.RLock()
	qrSession, exists := qrSessions[token]
	qrMu.RUnlock()

	if !exists {
		fail(w, http.StatusNotFound, "QR session expired")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	result, err := core.PollQRLogin(ctx, qrSession)
	if err != nil {
		s.Logger.Printf("poll QR failed: %v", err)
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

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

	qrMu.Lock()
	delete(qrSessions, token)
	qrMu.Unlock()

	ok(w, map[string]interface{}{
		"accountId": accountID,
		"userId":    session.UserID,
	})
}

// ====================================
// Conversations & Messages
// ====================================

func (s *Server) HandleConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, http.StatusBadRequest, "missing accountId")
		return
	}
	convs, err := s.Store.GetConversations(accountID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convs == nil {
		convs = []store.Conversation{}
	}
	ok(w, convs)
}

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
	if limit <= 0 { limit = 50 }

	msgs, err := s.Store.GetMessages(accountID, convID, cursor, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil { msgs = []store.Message{} }
	ok(w, msgs)
}

// SendMessage gửi tin nhắn qua core API Zalo thật
func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		To        string `json:"to"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.AccountID == "" || req.Content == "" {
		fail(w, http.StatusBadRequest, "missing fields")
		return
	}

	// Lấy Zalo session từ database theo account
	sessRec, err := s.Store.GetActiveSession(req.AccountID)
	if err != nil || sessRec == nil {
		fail(w, http.StatusUnauthorized, "not logged in. Vui lòng đăng nhập lại.")
		return
	}

	// Parse cookies từ JSON
	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)

	// Tạo session + client
	session := &core.Session{
		Cookies:    cookies,
		SecretKey:  sessRec.SecretKey,
		IMEI:       sessRec.IMEI,
		UserAgent:  sessRec.UserAgent,
		APIType:    sessRec.APIType,
		APIVersion: sessRec.APIVersion,
	}
	var wsURLs []string
	json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs)
	session.WSURLs = wsURLs

	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	msg, err := client.SendMessage(ctx, req.To, req.Content, core.MsgTypeText)
	if err != nil {
		s.Logger.Printf("send msg error: %v", err)
		fail(w, http.StatusInternalServerError, "Gửi tin thất bại: "+err.Error())
		return
	}

	// Lưu tin nhắn vào database
	attJSON, _ := json.Marshal(msg.Attachments)
	s.Store.SaveMessage(&store.Message{
		ID:        msg.ID,
		AccountID: req.AccountID,
		ConvID:    req.To,
		FromID:    session.UserID,
		Content:   msg.Content,
		MsgType:   int(msg.Type),
		Timestamp: msg.Timestamp,
		Attachments: string(attJSON),
	})

	ok(w, map[string]interface{}{
		"sent":      true,
		"msgId":     msg.ID,
		"content":   msg.Content,
		"to":        req.To,
		"timestamp": msg.Timestamp,
	})
}

// HandleGetConversationsFromAPI lấy danh sách hội thoại từ Zalo API thật
func (s *Server) HandleGetConversationsFromAPI(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, http.StatusBadRequest, "missing accountId")
		return
	}

	sessRec, err := s.Store.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		fail(w, http.StatusUnauthorized, "not logged in")
		return
	}

	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)

	session := &core.Session{
		Cookies:    cookies,
		SecretKey:  sessRec.SecretKey,
		IMEI:       sessRec.IMEI,
		UserAgent:  sessRec.UserAgent,
		APIType:    sessRec.APIType,
		APIVersion: sessRec.APIVersion,
	}

	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	convs, err := client.GetConversations(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	if convs == nil {
		convs = []core.Conversation{}
	}
	ok(w, convs)
}

// ====================================
// Cookie Login
// ====================================

func (s *Server) HandleCookieLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Cookie string `json:"cookie"` }
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

	result, err := core.CookieLogin(ctx, cookies, "", "")
	if err != nil {
		fail(w, http.StatusInternalServerError, "login failed: "+err.Error())
		return
	}

	session := result.Session
	accountID := "acc_" + session.UserID
	s.Store.CreateAccount(accountID, "Zalo User "+session.UserID[:8], 1)

	cookiesJSON, _ := json.Marshal(session.Cookies)
	wsURLsJSON, _ := json.Marshal(session.WSURLs)
	s.Store.SaveSession(&store.Session{
		ID:        session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10),
		AccountID: accountID, UserID: session.UserID,
		Cookies: string(cookiesJSON), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent,
		Language: "vi", WSURLs: string(wsURLsJSON),
		APIType: 30, APIVersion: 665, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})

	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID})
}

func parseCookieString(s string) map[string]string {
	cookies := make(map[string]string)
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" { continue }
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			cookies[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return cookies
}

// ====================================
// Login Page (HTML)
// ====================================

func (s *Server) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Login</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;display:flex;justify-content:center;align-items:center;min-height:100vh}
.card{background:#fff;border-radius:12px;padding:32px;width:90%;max-width:420px;box-shadow:0 2px 12px rgba(0,0,0,.1);text-align:center}
h1{font-size:22px;margin-bottom:16px;color:#333}
p{color:#666;margin-bottom:16px;font-size:14px}
.qr-box{padding:16px;border:2px dashed #ddd;border-radius:8px;margin:16px auto;text-align:center;min-height:200px;display:flex;flex-direction:column;align-items:center;justify-content:center}
.qr-box img{max-width:220px}.qr-box .loading{color:#999;font-size:14px}
.status{padding:12px;border-radius:6px;margin-top:12px;display:none}
.status.info{background:#e8f4fd;color:#0068ff;display:block}
.status.success{background:#d4edda;color:#155724;display:block}
.status.error{background:#f8d7da;color:#721c24;display:block}
.btn{padding:10px 24px;background:#0068ff;color:#fff;border:none;border-radius:6px;font-size:15px;cursor:pointer;margin-top:12px}
.btn:hover{background:#0052cc}.btn.gray{background:#6c757d}
.hidden{display:none}.mt8{margin-top:8px}
.input-group{margin:12px 0;text-align:left}
.input-group label{font-size:13px;color:#666;display:block;margin-bottom:4px}
.input-group textarea{width:100%;padding:10px;border:1px solid #ddd;border-radius:6px;font-size:13px;min-height:80px;font-family:monospace}
.tabs{display:flex;gap:8px;margin-bottom:16px}
.tabs button{flex:1;padding:8px;background:#eee;color:#333;border-radius:6px;border:none;cursor:pointer;font-size:13px}
.tabs button.on{background:#0068ff;color:#fff}
</style></head><body>
<div class="card">
<h1>ZCloud</h1>
<p>Dang nhap Zalo</p>
<div class="tabs"><button class="on" onclick="st('qr')">QR Code</button><button onclick="st('ck')">Cookie</button></div>
<div id="tqr"><div class="qr-box" id="qrBox"><div class="loading">Dang tao QR code...</div></div><div id="qrSt" class="status"></div></div>
<div id="tck" class="hidden"><div class="input-group"><label>Dan cookie tu Zalo Web (F12 -> Application -> Cookies -> chat.zalo.me):</label>
<textarea id="cki" placeholder="zpsid=...; zpw_sek=..."></textarea></div>
<button class="btn" onclick="lc()">Dang nhap</button><div id="ckSt" class="status"></div></div></div>
<script>
var pt=null;
function st(t){document.querySelectorAll('#tqr,#tck').forEach(function(e){e.classList.add('hidden')});
document.querySelectorAll('.tabs button').forEach(function(b){b.classList.remove('on')});
if(t==='qr'){document.getElementById('tqr').classList.remove('hidden');document.querySelector('.tabs button:first-child').classList.add('on');if(!pt)cq();}
else{document.getElementById('tck').classList.remove('hidden');document.querySelector('.tabs button:last-child').classList.add('on');if(pt){clearInterval(pt);pt=null;}}}
async function cq(){try{var r=await fetch('/api/qr/create');var d=await r.json();
if(d.ok){document.getElementById('qrBox').innerHTML='<img src="data:image/png;base64,'+d.data.image+'" alt="QR">';
document.getElementById('qrSt').className='status info';document.getElementById('qrSt').textContent='Quet QR bang Zalo tren dien thoai';
var ex=d.data.expires*1000;
pt=setInterval(async function(){
if(Date.now()>ex){clearInterval(pt);pt=null;cq();return;}
try{var p=await fetch('/api/qr/poll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:d.data.token})});var p2=await p.json();
if(p2.ok){clearInterval(pt);document.getElementById('qrSt').className='status success';document.getElementById('qrSt').textContent='Dang nhap thanh cong!';setTimeout(function(){window.location.href='/chat'},1000);}
else{document.getElementById('qrSt').className='status error';document.getElementById('qrSt').textContent=p2.error;if(p2.error.indexOf('expired')>=0||p2.error.indexOf('het han')>=0){clearInterval(pt);pt=null;setTimeout(cq,2000);}}
}catch(e){}},2000);}else{document.getElementById('qrBox').innerHTML='<div class="loading">Loi: '+d.error+'</div>';}
}catch(e){document.getElementById('qrBox').innerHTML='<div class="loading">Loi ket noi</div>';}}
async function lc(){var s=document.getElementById('ckSt');var c=document.getElementById('cki').value.trim();
if(!c){s.textContent='Nhap cookie';s.className='status error';return}
s.textContent='Dang dang nhap...';s.className='status info';
try{var r=await fetch('/api/login/cookie',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cookie:c})});var d=await r.json();
if(d.ok){s.textContent='Dang nhap thanh cong!';s.className='status success';setTimeout(function(){window.location.href='/chat'},1000);}
else{s.textContent='Loi: '+d.error;s.className='status error';}
}catch(e){s.textContent='Loi ket noi';s.className='status error';}}
cq();
</script></body></html>`)
}

// ====================================
// Chat Page (HTML)
// ====================================

func (s *Server) HandleChatPage(w http.ResponseWriter, r *http.Request) {
	accounts, _ := s.Store.ListAccounts(1)
	accountOptions := ""
	for _, a := range accounts {
		sel := ""
		if len(accounts) == 1 { sel = "selected" }
		name := a.DisplayName
		if name == "" { name = a.ID }
		accountOptions += fmt.Sprintf(`<option value="%s" %s>%s</option>`, a.ID, sel, name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Chat</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;height:100vh;display:flex;flex-direction:column}
.hdr{background:#0068ff;color:#fff;padding:12px 16px;display:flex;align-items:center;gap:12px}
.hdr h1{font-size:18px;flex:1}
.hdr select{background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 10px;border-radius:4px;font-size:13px}
.hdr select option{color:#333}.hdr button{background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 12px;border-radius:4px;cursor:pointer}
.ct{display:flex;flex:1;overflow:hidden}
.sb{width:300px;background:#fff;border-right:1px solid #e0e0e0;overflow-y:auto}
.sb .sr{padding:12px;border-bottom:1px solid #e0e0e0}
.sb .sr input{width:100%%;padding:8px 12px;border:1px solid #ddd;border-radius:6px;font-size:13px}
.cv{padding:12px 16px;cursor:pointer;border-bottom:1px solid #f0f0f0}
.cv:hover{background:#f5f8ff}.cv.on{background:#e8f0ff}
.cv-n{font-size:14px;font-weight:500;color:#333}
.cv-p{font-size:12px;color:#999;margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ca{flex:1;display:flex;flex-direction:column}
.ch{padding:12px 16px;background:#fff;border-bottom:1px solid #e0e0e0}
.ch h2{font-size:16px;color:#333}
.ms{flex:1;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:8px}
.msg{max-width:70%%;padding:10px 14px;border-radius:12px;font-size:14px;line-height:1.4}
.msg.in{background:#fff;align-self:flex-start;border-bottom-left-radius:4px}
.msg.out{background:#0068ff;color:#fff;align-self:flex-end;border-bottom-right-radius:4px}
.msg .tm{font-size:10px;opacity:.6;margin-top:4px;text-align:right}
.msg.out .tm{color:rgba(255,255,255,.7)}
.ia{display:flex;padding:12px 16px;background:#fff;border-top:1px solid #e0e0e0;gap:8px}
.ia input{flex:1;padding:10px 14px;border:1px solid #ddd;border-radius:20px;font-size:14px;outline:none}
.ia input:focus{border-color:#0068ff}
.ia button{padding:10px 20px;background:#0068ff;color:#fff;border:none;border-radius:20px;font-size:14px;cursor:pointer}
.em{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;color:#999;gap:8px;font-size:48px}
.lm{text-align:center;padding:8px;color:#0068ff;cursor:pointer;font-size:13px}
.hd{display:none}
@media(max-width:768px){.sb{width:100%%}.sb.hide{display:none}}
</style></head><body>
<div class="hdr"><h1>ZCloud</h1><select id="ac" onchange="sa()">%s</select><button onclick="window.location.href='/'">Thoat</button></div>
<div class="ct"><div class="sb"><div class="sr"><input type="text" placeholder="Tim kiem..." oninput="fc(this.value)"></div><div id="cl"></div></div>
<div class="ca"><div class="ch"><h2 id="ctt">Chon hoi thoai</h2></div>
<div class="ms" id="ml"><div class="em">Chon mot hoi thoai de bat dau</div></div>
<div class="ia hd" id="ia"><input type="text" id="mi" placeholder="Nhap tin nhan..." onkeydown="if(event.key==='Enter')sm()"><button onclick="sm()">Gui</button></div></div></div>
<script>
var cc='',ca=document.getElementById('ac').value,cu=0,ld=false;
document.addEventListener('DOMContentLoaded',function(){if(ca)lc()});
function sa(){ca=document.getElementById('ac').value;if(ca)lc()}
async function lc(){if(!ca)return;
try{var r=await fetch('/api/conversations/sync?accountId='+ca);var d=await r.json();
if(d.ok&&d.data){var el=document.getElementById('cl');el.innerHTML='';
d.data.forEach(function(c){var dv=document.createElement('div');dv.className='cv';dv.dataset.id=c.id;
dv.innerHTML='<div class="cv-n">'+(c.name||c.id)+'</div><div class="cv-p">'+(c.lastMsg&&c.lastMsg.content?'Tin cuoi: '+c.lastMsg.content:'')+'</div>';
dv.onclick=function(){sc(c.id)};el.appendChild(dv)});}}catch(e){}}

function sc(id){cc=id;cu=Date.now()*1000;
document.querySelectorAll('.cv').forEach(function(c){c.classList.toggle('on',c.dataset.id===id)});
document.getElementById('ctt').textContent=id;document.getElementById('ia').classList.remove('hd');
document.getElementById('ml').innerHTML='<div class="lm" onclick="lm()">Tai tin nhan cu</div>';lm()}
async function lm(){if(!cc||ld||!ca)return;ld=true;
try{var r=await fetch('/api/messages?accountId='+ca+'&convId='+cc+'&cursor='+cu+'&limit=30');var d=await r.json();
if(d.ok){var el=document.getElementById('ml');if(cu>Date.now()*1000)el.innerHTML='';
d.data.forEach(function(m){var dv=document.createElement('div');dv.className='msg '+(m.fromId===ca?'out':'in');
var t=new Date(m.timestamp);var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
dv.innerHTML='<div>'+m.content+'</div><div class="tm">'+ts+'</div>';el.appendChild(dv)});
if(d.data.length>0)cu=d.data[0].timestamp;el.scrollTop=el.scrollHeight;}}catch(e){}finally{ld=false}}
async function sm(){var inp=document.getElementById('mi');var c=inp.value.trim();if(!c||!cc||!ca)return;inp.value='';
try{var r=await fetch('/api/messages/send',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({accountId:ca,to:cc,content:c})});var d=await r.json();
if(d.ok){var el=document.getElementById('ml');var dv=document.createElement('div');dv.className='msg out';
var t=new Date();var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
dv.innerHTML='<div>'+c+'</div><div class="tm">'+ts+'</div>';el.appendChild(dv);el.scrollTop=el.scrollHeight;}}catch(e){}}
function fc(q){document.querySelectorAll('.cv').forEach(function(c){c.style.display=c.textContent.toLowerCase().indexOf(q.toLowerCase())!==-1?'':'none'})}
</script></body></html>`, accountOptions)
}
