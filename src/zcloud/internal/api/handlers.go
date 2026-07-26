package api

import (
	"context"
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
func ok(w http.ResponseWriter, data interface{}) { writeJSON(w, 200, APIResponse{OK: true, Data: data}) }
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIResponse{OK: false, Error: msg, Code: status})
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"service": "zcloud", "time": time.Now().Unix()})
}

// ========== QR LOGIN ==========

var qrSessions = make(map[string]*core.QRLoginSession)
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
	qrMu.Lock()
	qrSessions[code] = qrSession
	qrMu.Unlock()
	go func() { time.Sleep(5 * time.Minute); qrMu.Lock(); delete(qrSessions, code); qrMu.Unlock() }()
	ok(w, map[string]interface{}{"token": code, "image": qrSession.ImageB64, "expires": time.Now().Add(3 * time.Minute).Unix()})
}

func (s *Server) HandlePollQR(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		var req struct{ Token string `json:"token"` }; json.NewDecoder(r.Body).Decode(&req); token = req.Token
	}
	if token == "" { fail(w, 400, "missing token"); return }
	qrMu.RLock(); qrSession, exists := qrSessions[token]; qrMu.RUnlock()
	if !exists { fail(w, 404, "QR session expired"); return }

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := core.PollQRLogin(ctx, qrSession)
	if err != nil { fail(w, 400, err.Error()); return }

	session := result.Session
	accountID := "acc_" + session.UserID
	s.Store.CreateAccount(accountID, "Zalo User "+session.UserID[:8], 1)
	cookiesJSON, _ := json.Marshal(session.Cookies)
	wsURLsJSON, _ := json.Marshal(session.WSURLs)
	s.Store.SaveSession(&store.Session{
		ID: session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10), AccountID: accountID,
		UserID: session.UserID, Cookies: string(cookiesJSON), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wsURLsJSON), APIType: 30, APIVersion: 665, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})
	qrMu.Lock(); delete(qrSessions, token); qrMu.Unlock()
	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID})
}

// ========== CONVERSATIONS ==========

func (s *Server) HandleConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" { fail(w, 400, "missing accountId"); return }
	convs, _ := s.Store.GetConversations(accountID)
	if convs == nil { convs = []store.Conversation{} }
	ok(w, convs)
}

func (s *Server) HandleSyncConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" { fail(w, 400, "missing accountId"); return }
	sessRec, err := s.Store.GetActiveSession(accountID)
	if err != nil || sessRec == nil { fail(w, 401, "not logged in"); return }

	var cookies map[string]string; json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
	}
	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second); defer cancel()
	convs, err := client.GetConversations(ctx)
	if err != nil { fail(w, 500, err.Error()); return }
	if convs == nil { convs = []core.Conversation{} }

	// Lưu vào DB
	for _, c := range convs {
		s.Store.SaveConversation(&store.Conversation{
			ID: c.ID, AccountID: accountID, Name: c.Name, ConvType: int(c.Type), UpdatedAt: time.Now(),
		})
	}
	ok(w, convs)
}

// ========== MESSAGES ==========

func (s *Server) HandleMessages(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	convID := r.URL.Query().Get("convId")
	if accountID == "" || convID == "" { fail(w, 400, "missing accountId or convId"); return }
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	if cursor == 0 { cursor = time.Now().UnixMilli() + 1 }
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 { limit = 50 }
	msgs, _ := s.Store.GetMessages(accountID, convID, cursor, limit)
	if msgs == nil { msgs = []store.Message{} }
	ok(w, msgs)
}

func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`; To string `json:"to"`; Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { fail(w, 400, "invalid body"); return }
	if req.AccountID == "" || req.Content == "" { fail(w, 400, "missing fields"); return }

	sessRec, err := s.Store.GetActiveSession(req.AccountID)
	if err != nil || sessRec == nil { fail(w, 401, "not logged in"); return }
	var cookies map[string]string; json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
	}
	var wsURLs []string; json.Unmarshal([]byte(sessRec.WSURLs), &wsURLs); session.WSURLs = wsURLs

	client := core.NewClient(session)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second); defer cancel()
	msg, err := client.SendMessage(ctx, req.To, req.Content, core.MsgTypeText)
	if err != nil { fail(w, 500, "Gui tin that bai: "+err.Error()); return }
	ok(w, map[string]interface{}{"sent": true, "msgId": msg.ID, "content": msg.Content, "timestamp": msg.Timestamp})
}

// ========== COOKIE LOGIN ==========

func (s *Server) HandleCookieLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Cookie string `json:"cookie"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { fail(w, 400, "invalid body"); return }
	cookies := parseCookie(req.Cookie)
	if len(cookies) == 0 { fail(w, 400, "no cookies"); return }

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second); defer cancel()
	result, err := core.CookieLogin(ctx, cookies, "", "")
	if err != nil { fail(w, 500, "login failed: "+err.Error()); return }
	session := result.Session
	accountID := "acc_" + session.UserID
	s.Store.CreateAccount(accountID, "Zalo User "+session.UserID[:8], 1)
	cj, _ := json.Marshal(session.Cookies); wj, _ := json.Marshal(session.WSURLs)
	s.Store.SaveSession(&store.Session{
		ID: session.UserID + "_" + strconv.FormatInt(time.Now().Unix(), 10), AccountID: accountID,
		UserID: session.UserID, Cookies: string(cj), SecretKey: session.SecretKey,
		IMEI: session.IMEI, UserAgent: session.UserAgent, Language: "vi",
		WSURLs: string(wj), APIType: 30, APIVersion: 665, IsActive: 1, ExpiresAt: session.ExpiresAt,
	})
	ok(w, map[string]interface{}{"accountId": accountID, "userId": session.UserID})
}

func parseCookie(s string) map[string]string {
	c := make(map[string]string)
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p); if p == "" { continue }
		parts := strings.SplitN(p, "=", 2); if len(parts) == 2 { c[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1]) }
	}
	return c
}

// ========== LOGIN PAGE ==========

func (s *Server) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Login</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:sans-serif;background:#f5f5f5;display:flex;justify-content:center;align-items:center;min-height:100vh}
.card{background:#fff;border-radius:12px;padding:32px;width:90%;max-width:420px;box-shadow:0 2px 12px rgba(0,0,0,.1);text-align:center}
h1{font-size:22px;margin-bottom:16px;color:#333}p{color:#666;margin-bottom:16px;font-size:14px}
.qr-box{border:2px dashed #ddd;border-radius:8px;margin:16px auto;padding:16px;min-height:200px;display:flex;align-items:center;justify-content:center}
.qr-box img{max-width:220px}.qr-box .loading{color:#999;font-size:14px}
.st{padding:12px;border-radius:6px;margin-top:12px;display:none}
.st.i{background:#e8f4fd;color:#0068ff;display:block}.st.s{background:#d4edda;color:#155724;display:block}.st.e{background:#f8d7da;color:#721c24;display:block}
.btn{padding:10px 24px;background:#0068ff;color:#fff;border:none;border-radius:6px;font-size:15px;cursor:pointer;margin-top:12px}
.btn:hover{background:#0052cc}.btn.g{background:#6c757d}.hd{display:none}
.tabs{display:flex;gap:8px;margin-bottom:16px}
.tabs button{flex:1;padding:8px;background:#eee;color:#333;border-radius:6px;border:none;cursor:pointer;font-size:13px}
.tabs button.on{background:#0068ff;color:#fff}
.tc textarea{width:100%;padding:10px;border:1px solid #ddd;border-radius:6px;font-size:13px;min-height:80px;font-family:monospace;margin-top:8px}
</style></head><body>
<div class="card">
<h1>ZCloud</h1><p>Đăng nhập Zalo</p>
<div class="tabs"><button class="on" onclick="st('qr')">QR Code</button><button onclick="st('ck')">Cookie</button></div>
<div id="tqr"><div class="qr-box" id="qb"><div class="loading">Đang tạo QR...</div></div><div id="qs" class="st"></div></div>
<div id="tck" class="hd"><p style="font-size:13px;color:#666;margin-bottom:8px">Dán cookie từ Zalo Web (F12 > Cookies > chat.zalo.me)</p>
<div class="tc"><textarea id="cki" placeholder="zpsid=...; zpw_sek=..."></textarea></div>
<button class="btn" onclick="lc()">Đăng nhập</button><div id="cs" class="st"></div></div>
</div>
<script>
var pt=null;
function st(t){document.getElementById('tqr').classList.add('hd');document.getElementById('tck').classList.add('hd');
document.querySelectorAll('.tabs button').forEach(function(b){b.classList.remove('on')});
if(t=='qr'){document.getElementById('tqr').classList.remove('hd');document.querySelector('.tabs button:first-child').classList.add('on');if(!pt)cq();}
else{document.getElementById('tck').classList.remove('hd');document.querySelector('.tabs button:last-child').classList.add('on');if(pt){clearInterval(pt);pt=null;}}}
async function cq(){try{var r=await fetch('/api/qr/create');var d=await r.json();
if(d.ok){document.getElementById('qb').innerHTML='<img src="data:image/png;base64,'+d.data.image+'" alt="QR">';
document.getElementById('qs').className='st i';document.getElementById('qs').textContent='Quét QR bằng Zalo trên điện thoại';
var ex=d.data.expires*1000;
pt=setInterval(async function(){
if(Date.now()>ex){clearInterval(pt);pt=null;document.getElementById('qs').className='st e';document.getElementById('qs').textContent='QR hết hạn, tạo mã mới...';setTimeout(cq,2000);return;}
try{var p=await fetch('/api/qr/poll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:d.data.token})});var p2=await p.json();
if(p2.ok){clearInterval(pt);document.getElementById('qs').className='st s';document.getElementById('qs').textContent='Đăng nhập thành công!';setTimeout(function(){window.location.href='/chat'},1000);}
else{document.getElementById('qs').className='st e';document.getElementById('qs').textContent='Lỗi: '+p2.error;if(p2.error.indexOf('hết hạn')>=0||p2.error.indexOf('expired')>=0){clearInterval(pt);pt=null;setTimeout(cq,2000);}}
}catch(e){}},2000);}
else{document.getElementById('qb').innerHTML='<div class="loading">Lỗi: '+d.error+'</div>';}
}catch(e){document.getElementById('qb').innerHTML='<div class="loading">Lỗi kết nối</div>';}}
async function lc(){var s=document.getElementById('cs');var c=document.getElementById('cki').value.trim();
if(!c){s.textContent='Nhập cookie';s.className='st e';return}
s.textContent='Đang đăng nhập...';s.className='st i';
try{var r=await fetch('/api/login/cookie',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cookie:c})});var d=await r.json();
if(d.ok){s.textContent='Đăng nhập thành công!';s.className='st s';setTimeout(function(){window.location.href='/chat'},1000);}
else{s.textContent='Lỗi: '+d.error;s.className='st e';}
}catch(e){s.textContent='Lỗi kết nối';s.className='st e';}}
cq();
</script></body></html>`)
}

// ========== CHAT PAGE ==========

func (s *Server) HandleChatPage(w http.ResponseWriter, r *http.Request) {
	accounts, _ := s.Store.ListAccounts(1)
	accOpts := ""
	for _, a := range accounts {
		sel := ""; if len(accounts) == 1 { sel = "selected" }
		n := a.DisplayName; if n == "" { n = a.ID[:min(20, len(a.ID))] }
		accOpts += fmt.Sprintf(`<option value="%s" %s>%s</option>`, a.ID, sel, n)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>ZCloud Chat</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:sans-serif;background:#f5f5f5;height:100vh;display:flex;flex-direction:column}
.hdr{background:#0068ff;color:#fff;padding:10px 16px;display:flex;align-items:center;gap:12px}
.hdr h1{font-size:18px;flex:1}
.hdr select{background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 10px;border-radius:4px;font-size:13px}
.hdr select option{color:#333}
.hdr button{background:rgba(255,255,255,.2);color:#fff;border:none;padding:6px 12px;border-radius:4px;cursor:pointer}
.ct{display:flex;flex:1;overflow:hidden}
.sb{width:300px;background:#fff;border-right:1px solid #e0e0e0;display:flex;flex-direction:column}
.sbn{display:flex;border-bottom:1px solid #e0e0e0}
.sbn button{flex:1;padding:10px;background:none;border:none;cursor:pointer;font-size:13px;color:#666;border-bottom:2px solid transparent}
.sbn button.on{color:#0068ff;border-bottom-color:#0068ff}
.sbp{flex:1;overflow-y:auto;display:none}
.sbp.on{display:block}
.sr{padding:12px;border-bottom:1px solid #e0e0e0}
.sr input{width:100%%;padding:8px 12px;border:1px solid #ddd;border-radius:6px;font-size:13px}
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
.msg .nm{font-size:11px;color:#0068ff;font-weight:500;margin-bottom:2px}
.ia{display:flex;padding:12px 16px;background:#fff;border-top:1px solid #e0e0e0;gap:8px}
.ia input{flex:1;padding:10px 14px;border:1px solid #ddd;border-radius:20px;font-size:14px;outline:none}
.ia input:focus{border-color:#0068ff}
.ia button{padding:10px 20px;background:#0068ff;color:#fff;border:none;border-radius:20px;font-size:14px;cursor:pointer}
.em{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;color:#999;gap:8px;font-size:48px}
.lm{text-align:center;padding:8px;color:#0068ff;cursor:pointer;font-size:13px}
.hd{display:none}
@media(max-width:768px){.sb{width:100%%}.sb.hide{display:none}}
</style>
</head><body>
<div class="hdr"><h1>ZCloud</h1><select id="ac" onchange="sa()">%s</select><button onclick="window.location.href='/'">Thoát</button></div>
<div class="ct">
<div class="sb">
<div class="sbn"><button class="on" onclick="stp('ch')">Trò chuyện</button><button onclick="stp('co')">Liên hệ</button></div>
<div id="pn-ch" class="sbp on">
<div class="sr"><input type="text" placeholder="Tìm kiếm..." oninput="fc(this.value)"></div>
<div id="cl"></div>
</div>
<div id="pn-co" class="sbp">
<div class="sr"><input type="text" placeholder="Tìm bạn..." oninput="ff(this.value)"></div>
<div id="fl"></div>
</div>
</div>
<div class="ca">
<div class="ch"><h2 id="ctt">Chọn hội thoại</h2></div>
<div class="ms" id="ml"><div class="em">Chọn một hội thoại để bắt đầu</div></div>
<div class="ia hd" id="ia"><input type="text" id="mi" placeholder="Nhập tin nhắn..." onkeydown="if(event.key==='Enter')sm()"><button onclick="sm()">Gửi</button></div>
</div>
</div>
<script>
var cc='',ca=document.getElementById('ac').value,cu=0,ld=false,pt=null;
document.addEventListener('DOMContentLoaded',function(){if(ca)sy()});
function sa(){ca=document.getElementById('ac').value;if(ca)sy();}
function stp(t){document.querySelectorAll('.sbp').forEach(function(e){e.classList.remove('on')});
document.querySelectorAll('.sbn button').forEach(function(b){b.classList.remove('on')});
document.getElementById('pn-'+t).classList.add('on');
document.querySelector('.sbn button'+(t=='ch'?':first-child':':last-child')).classList.add('on');
if(t=='co'&&ca)loadFriends();}
async function sy(){if(!ca)return;
try{var r=await fetch('/api/conversations/sync?accountId='+ca);var d=await r.json();
if(d.ok&&d.data){var el=document.getElementById('cl');el.innerHTML='';
d.data.forEach(function(c){var dv=document.createElement('div');dv.className='cv';dv.dataset.id=c.id;
dv.innerHTML='<div class="cv-n">'+(c.name||c.id)+'</div><div class="cv-p">'+(c.lastMsgId||'')+'</div>';
dv.onclick=function(){sc(c.id)};el.appendChild(dv)});}}catch(e){}}
async function loadFriends(){var el=document.getElementById('fl');
el.innerHTML='<div class="em" style="font-size:14px">Đang tải...</div>';}
function sc(id){cc=id;cu=Date.now()*1000;
document.querySelectorAll('.cv').forEach(function(c){c.classList.toggle('on',c.dataset.id===id)});
document.getElementById('ctt').textContent=id;document.getElementById('ia').classList.remove('hd');
document.getElementById('ml').innerHTML='<div class="lm" onclick="lm()">Tải tin nhắn cũ</div>';lm()}
async function lm(){if(!cc||ld||!ca)return;ld=true;
try{var r=await fetch('/api/messages?accountId='+ca+'&convId='+cc+'&cursor='+cu+'&limit=30');var d=await r.json();
if(d.ok){var el=document.getElementById('ml');if(cu>Date.now()*1000)el.innerHTML='';
d.data.forEach(function(m){var dv=document.createElement('div');dv.className='msg '+(m.fromId===ca?'out':'in');
var t=new Date(m.timestamp);var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
dv.innerHTML=(m.fromName&&m.fromId!==ca?'<div class="nm">'+m.fromName+'</div>':'')+'<div>'+(m.content||'')+'</div><div class="tm">'+ts+'</div>';el.appendChild(dv)});
if(d.data.length>0)cu=d.data[0].timestamp;el.scrollTop=el.scrollHeight;}}catch(e){}finally{ld=false}}
async function sm(){var inp=document.getElementById('mi');var c=inp.value.trim();if(!c||!cc||!ca)return;inp.value='';
try{var r=await fetch('/api/messages/send',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({accountId:ca,to:cc,content:c})});var d=await r.json();
if(d.ok){var el=document.getElementById('ml');var dv=document.createElement('div');dv.className='msg out';
var t=new Date();var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
dv.innerHTML='<div>'+c+'</div><div class="tm">'+ts+'</div>';el.appendChild(dv);el.scrollTop=el.scrollHeight;}}catch(e){}}
function fc(q){document.querySelectorAll('#pn-ch .cv').forEach(function(c){c.style.display=c.textContent.toLowerCase().indexOf(q.toLowerCase())!==-1?'':'none'})}
function ff(q){document.querySelectorAll('#pn-co .cv').forEach(function(c){c.style.display=c.textContent.toLowerCase().indexOf(q.toLowerCase())!==-1?'':'none'})}
</script></body></html>`, accOpts)
}

func min(a, b int) int { if a < b { return a }; return b }
