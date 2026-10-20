var cc='',cu=0,ld=false,pt=null,cvCache={};
// me: userId (uid thuần) của account đang chọn, dùng để so sánh m.fromId.
var ccAccountId='';var ccAccountMe='';var ccOtherUid='';
// Browser WS manager (T30): mở 1 WebSocket /ws?accountId=... cho mỗi account
// đang enabled → nhận realtime new_message/old_message/media_downloaded…
// wsStates[accountId] = {sock, timer, attempt, closed}.
var wsStates={};
// unreadConvs['convId|accountId'] = {count, preview} cho conv chưa mở.
var unreadConvs={};
// healthVersionPoll: poll /api/health mỗi 30s. Nếu version khác version lưu
// trong window → location.reload() để pick up code mới (CSS/HTML/JS binary
// mới đã được deploy qua watch mode + atomic rename). Lưu version ban đầu ở
// lần poll đầu; build ldflags inject git short hash + semver vào binary —
// xem internal/version.go + buildBinary trong servcmd/watch.go.
var _serverVersion=null;
function _checkHealthAndMaybeReload(){
	fetch('/api/health').then(function(r){return r.json();}).then(function(d){
		if(!d||!d.ok||!d.data||!d.data.version)return;
		var v=d.data.version;
		if(_serverVersion===null){_serverVersion=v;return;}
		if(v!==_serverVersion){
			console.log('[zcloud] version changed: '+_serverVersion+' → '+v+' → reload');
			location.reload();
		}
	}).catch(function(){});
}
setInterval(_checkHealthAndMaybeReload,30000);
document.addEventListener('DOMContentLoaded',async function(){await la();uh();sy();syncWS();_checkHealthAndMaybeReload();});
// accounts: cache list account cho tab Quan ly. Reload khi can.
var accounts=[];
// qrPollTimer: interval id cua QR poll loop.
var qrPollTimer=null;
// qrToken: token QR dang poll.
var qrToken='';

// loadAccounts lay danh sach account tu server (kem status listening/hasActiveSession).
async function loadAccounts(){try{var r=await fetch('/api/account/list');var d=await r.json();if(d.ok&&d.data){accounts=d.data;renderAccounts();renderStats();renderHeaderAvatars();return true}return false}catch(e){return false}}

// ====================================
// Realtime WebSocket (T30)
// ====================================
// Backend /ws?accountId=... room theo accountId; broadcast new_message,
// old_message, media_downloaded, typing, reaction, seen, delivered.
// Mở 1 socket/account enabled, reconnect backoff khi rớt (server restart...).
function _wsURL(accountId){
	var proto=(location.protocol==='https:')?'wss:':'ws:';
	return proto+'//'+location.host+'/ws?accountId='+encodeURIComponent(accountId);
}
// syncWS: mở socket cho account enabled, đóng socket của account đã tắt/xoá.
function syncWS(){
	if(typeof accounts==='undefined'||!accounts)return;
	var want={};
	accounts.forEach(function(a){if(a&&a.enabled&&a.id)want[a.id]=true;});
	Object.keys(want).forEach(function(id){_wsOpen(id);});
	Object.keys(wsStates).forEach(function(id){if(!want[id])_wsClose(id);});
}
function _wsOpen(accountId){
	if(!accountId)return;
	if(typeof WebSocket==='undefined')return;
	var st=wsStates[accountId];
	if(!st){st=wsStates[accountId]={sock:null,timer:null,attempt:0,closed:false};}
	if(st.sock&&(st.sock.readyState===0||st.sock.readyState===1))return; // CONNECTING/OPEN
	if(st.timer)return;
	st.closed=false;
	var sock;
	try{sock=new WebSocket(_wsURL(accountId));}catch(e){_wsScheduleReconnect(accountId);return;}
	st.sock=sock;
	sock.onopen=function(){st.attempt=0;};
	sock.onmessage=function(ev){
		var m;try{m=JSON.parse(ev.data);}catch(e){return;}
		_wsDispatch(accountId,m);
	};
	sock.onerror=function(){try{sock.close();}catch(e){}};
	sock.onclose=function(){st.sock=null;if(!st.closed)_wsScheduleReconnect(accountId);};
}
function _wsScheduleReconnect(accountId){
	var st=wsStates[accountId];if(!st||st.closed||st.timer)return;
	// Chỉ reconnect khi account vẫn còn và đang enabled — tránh loop vô ích.
	var acc=accounts.find(function(a){return a.id===accountId;});
	if(!acc||!acc.enabled)return;
	if(st.attempt<6)st.attempt++;
	var delay=Math.min(30000,1000*Math.pow(2,st.attempt));
	st.timer=setTimeout(function(){st.timer=null;_wsOpen(accountId);},delay);
}
function _wsClose(accountId){
	var st=wsStates[accountId];if(!st)return;
	st.closed=true;
	if(st.timer){clearTimeout(st.timer);st.timer=null;}
	if(st.sock){try{st.sock.close();}catch(e){}st.sock=null;}
	delete wsStates[accountId];
}
// _wsDispatch route event realtime. Mọi nhánh bọc try/catch để 1 event lỗi
// không làm chết socket.
function _wsDispatch(accountId,m){
	if(!m||!m.type)return;
	var d=m.data||{};
	try{
		switch(m.type){
		case 'new_message':
		case 'old_message':
			_onIncomingMessage(accountId,d);
			break;
		case 'media_downloaded':
			applyMediaDownloaded(d,cc);
			break;
		case 'upload_attachment':
		case 'typing':
		case 'reaction':
		case 'seen':
		case 'delivered':
		case 'desktop_sync':
			// Chưa cần xử lý UI — bỏ qua an toàn.
			break;
		}
	}catch(e){console.log('[zcloud] ws dispatch err',m.type,e);}
}
// _convKey khoá dedupe unread theo (convId, accountId) vì 2 account có thể
// trùng convId (vd self-thread).
function _convKey(convId,accountId){return convId+'|'+(accountId||'');}
function _hasMsgNode(id){
	if(!id)return false;
	var ml=document.getElementById('ml');
	return !!(ml&&ml.querySelector('.msg[data-msg="'+cssEsc(id)+'"]'));
}
// _nearBottom: user đang ở gần cuối thread → auto-scroll tin mới.
function _nearBottom(ml){
	if(!ml)return true;
	return (ml.scrollHeight-ml.scrollTop-ml.clientHeight)<120;
}
// appendMessage chèn 1 bubble realtime vào thread hiện tại. Dedupe theo id,
// giữ vị trí scroll nếu user đang đọc tin cũ, auto-scroll nếu đang ở cuối.
function appendMessage(d,accountId){
	var ml=document.getElementById('ml');if(!ml)return;
	if(_hasMsgNode(d.id))return;
	var near=_nearBottom(ml);
	var em=ml.querySelector('.em');if(em)em.remove();
	ml.appendChild(renderMsg(d));
	regroupMessages();
	if(near)scrollLastMessage(ml,true);
}
// _adoptOutgoing: WS echo của tin mình vừa gửi có thể về TRƯỚC khi response
// /api/messages/send trả msgId. Nếu có bubble optimistic (data-tmp) khớp nội
// dung → gắn data-msg vào luôn thay vì thêm bubble mới (tránh trùng).
function _adoptOutgoing(d){
	var ml=document.getElementById('ml');if(!ml)return false;
	// Chỉ adopt khi tin là do chính mình gửi (tránh nuốt tin của người khác
	// trùng nội dung).
	if(d.fromId&&ccAccountMe&&d.fromId!==ccAccountMe)return false;
	// Tìm đúng bubble optimistic khớp nội dung (có thể có nhiều tin gửi dồn).
	var nodes=ml.querySelectorAll('.msg[data-tmp]'),tmp=null;
	for(var i=0;i<nodes.length;i++){
		if(nodes[i].dataset.tmpContent===d.content){tmp=nodes[i];break;}
	}
	if(!tmp)return false;
	tmp.dataset.msg=d.id;
	tmp.dataset.from=d.fromId||'';
	if(d.timestamp)tmp.dataset.ts=d.timestamp;
	delete tmp.dataset.tmp;
	delete tmp.dataset.tmpContent;
	return true;
}
// _onIncomingMessage: new_message/old_message từ WS.
//  - đúng conversation đang mở → chèn ngay (dedupe), cập nhật sidebar.
//  - conversation khác → đánh dấu unread + debounce refresh sidebar.
function _onIncomingMessage(accountId,d){
	if(!d||!d.id||!d.convId)return;
	var isCur=(cc&&d.convId===cc&&accountId===ccAccountId);
	if(isCur){
		if(_hasMsgNode(d.id))return;
		if(_adoptOutgoing(d)){regroupMessages();_debounceSy();return;}
		appendMessage(d,accountId);
		_debounceSy();
		return;
	}
	_markUnread(d.convId,accountId,d);
	_debounceSy();
}
function _markUnread(convId,accountId,d){
	var k=_convKey(convId,accountId);
	var u=unreadConvs[k]||{count:0,preview:''};
	u.count++;
	if(d&&d.content)u.preview=d.content;
	unreadConvs[k]=u;
}
// clearUnread: xoá badge khi user mở conversation.
function clearUnread(convId,accountId){
	var k=_convKey(convId,accountId);
	if(unreadConvs[k])delete unreadConvs[k];
	var ml=document.getElementById('cl');
	if(ml){var row=ml.querySelector('.cv[data-id="'+cssEsc(convId)+'"][data-account-id="'+cssEsc(accountId)+'"]');if(row){row.classList.remove('unread');var b=row.querySelector('.cv-un');if(b)b.remove();}}
}
// _debounceSy: gộp nhiều event dồn dập thành 1 lần refresh sidebar.
var _syTimer=null;
function _debounceSy(){
	if(_syTimer)return;
	_syTimer=setTimeout(function(){_syTimer=null;sy();},600);
}

// renderAccounts ve lai danh sach account theo cache + filter search.
function renderAccounts(){
	var q=(document.getElementById('mg-search')||{value:''}).value.toLowerCase().trim();
	var ul=document.getElementById('ul');if(!ul) return;
	ul.innerHTML='';
	var filtered=accounts.filter(function(a){
		if(!q) return true;
		return(a.displayName||'').toLowerCase().indexOf(q)>=0||(a.id||'').toLowerCase().indexOf(q)>=0;
	});
	if(!filtered.length){
		ul.innerHTML='<div class="mg-empty">'
			+(accounts.length?'<div class="mg-empty-i">🔍</div><div>Không có tài khoản khớp "'+esc(q)+'"</div>':'<div class="mg-empty-i">👤</div><div>Chưa có tài khoản nào</div><div class="mg-empty-sub">Bấm <b>+ Thêm</b> để đăng nhập QR hoặc Cookie.</div>')
			+'</div>';
		return;
	}
	filtered.forEach(function(a){
		var dv=document.createElement('div');dv.className='mg-item';
		if(!a.enabled) dv.className+=' disabled';
		// Không còn 'ca' để highlight — bỏ .cur.
		var stClass=a.listening?'on':(a.hasActiveSession?'err':'off');
		var stTitle=a.listening?'WS đang nghe':(a.hasActiveSession?'Session OK, listener chưa chạy':'Không có session active');
		var uId=esc(a.id);
		var showBadge;
		// Không còn 'ca' — bỏ badge 'Đang dùng'.
		showBadge='';
		dv.innerHTML='<div class="mg-enabled-wrap">'
			+'<input type="checkbox" class="mg-enabled" '
			+(a.enabled?'checked':'')+' '
			+'data-id="'+uId+'" '
			+'title="'+(a.enabled?'Ẩn khỏi danh sách chat/danh bạ':'Hiện trong danh sách chat/danh bạ')+'" '
			+'onclick="event.stopPropagation()" '
			+'onchange="toggleAccountEnabled(\''+uId+'\', this.checked)">'
			+'</div>'
			+'<div class="mg-av"><img src="'+(a.avatar||'')+'" onerror="this.style.display=\'none\'" alt=""></div>'
			+'<div class="mg-body">'
			// row 1: ten account + status dot
			+'<div class="mg-row1">'
			+'<span class="mg-st '+stClass+'" title="'+stTitle+'"></span>'
			+'<span class="mg-n">'+(a.displayName||a.id)+'</span>'
			+'</div>'
			// row 2: acc id (mono, nho)
			+'<div class="mg-row2">'
			+'<span class="mg-meta">'+esc(a.id)+'</span>'
			+'</div>'
			// row 3: badge trang thai + transport + nut Restart + nut Xoa (T24)
			+'<div class="mg-row3">'
			+showBadge
			// Transport badge: 'pc' (primary — xanh dương), 'web' (fallback — xám),
			// '' (đã logout — đỏ nhạt).
			+(a.transport==='pc'?'<span class="mg-badge mg-badge-pc" title="Đăng nhập bằng Zalo PC (primary)">PC</span>':
			  a.transport==='web'?'<span class="mg-badge mg-badge-web" title="Đăng nhập bằng Zalo Web (fallback)">Web</span>':
			  '<span class="mg-badge mg-badge-off" title="Đã đăng xuất">Off</span>')
			// Nếu chưa có session (đã logout) → ẩn Restart, hiện nút Login lại.
			// Dùng template literal (backtick) thay vì '...' để nhúng dấu '
			// trong HTML onclick attribute — pattern cũ viết ''+uId+'' gây
			// SyntaxError "Unexpected string" ở parser JS vì string rỗng
			// đứng cạnh ')' không có toán tử (T24 regression).
			+(a.hasActiveSession?
			  `<button class="mg-btn" onclick="restartListener('+uId+')">Restart</button>`:
			  `<button class="mg-btn mg-btn-rel" onclick="showAddModal('+uId+')" title="Đăng nhập lại bằng Cookie mới">🔑 Login</button>`)
			+`<button class="mg-btn" onclick="syncV2Start('+uId+')" title="Sync lịch sử sâu qua SyncV2"${a.transport!=='pc'?' disabled':''}>⤓ SyncV2</button>`
			+(a.disabledReason==='auth_expired'?`<button class="mg-btn mg-btn-rel" onclick="showAddModal('+uId+')" title="Đăng nhập lại bằng Cookie mới">🔑 Re-login</button>`:'')
			+`<button class="mg-del" onclick="xd('+uId+')" title="Xoá toàn bộ user Zalo khỏi zcloud (cascade)">🗑</button>`
			+'</div>'
		ul.appendChild(dv);
	});
}

// renderStats hien thi summary (tong so, dang nghe, offline).
function renderStats(){
	var s=document.getElementById('mg-stats-text');if(!s)return;
	var total=accounts.length;
	var shown=accounts.filter(function(a){return a.enabled}).length;
	var on=accounts.filter(function(a){return a.listening}).length;
	s.textContent=total+' tài khoản • '+shown+' hiển thị • '+on+' đang nghe WS';
}

// renderHeaderAvatars render danh sách avatar account vào header.
// Dùng createElement + addEventListener (không innerHTML) để tránh hoàn toàn
// bug escape JS template string trong onclick/onerror attributes.
// Avatar header mirror checkbox "Hiển thị" trong panel Quản lý (a.enabled).
// Click avatar → toggle enabled qua /api/account/enabled, giống checkbox.
// Trạng thái:
//   - inactive (!listening)     → ẩn hoàn toàn.
//   - enabled=false (đang nghe) → border xám + opacity 0.55 (mờ).
//   - enabled=true  (đang nghe) → border xanh + opacity 1 (rõ).
//   - ccAccountId (composer) là state RIÊNG, không liên quan avatar.
// - Avatar có URL → hiển thị <img>; không có → fallback 1 chữ cái đầu.
function renderHeaderAvatars(){
	var box=document.getElementById('hdr-avatars');
	if(!box)return;
	box.innerHTML='';
	// Chỉ hiện account đang nghe WS. enabled=true/false đều hiện (chỉ đổi
	// màu border) để Sếp có thể toggle qua click avatar.
	var items=accounts.filter(function(a){return a.listening;});
	if(items.length===0){
		var empty=document.createElement('div');
		empty.className='hdr-empty';
		empty.textContent='ZCloud';
		box.appendChild(empty);
		return;
	}
	for(var i=0;i<items.length;i++){
		var a=items[i];
		var div=document.createElement('div');
		div.className='hdr-av'+(a.enabled?' sel':'');
		div.dataset.id=a.id;
		div.title=(a.displayName||a.id)+(a.listening?' (đang nghe WS)':' (offline)');
		div.addEventListener('click',(function(id){return function(){selectHeaderAccount(id);};})(a.id));
		if(a.avatar){
			var img=document.createElement('img');
			img.src=a.avatar;
			img.alt='';
			img.addEventListener('error',function(){this.style.display='none';});
			div.appendChild(img);
		}
		var fb=document.createElement('div');
		fb.className='hdr-av-fb';
		fb.textContent=(a.displayName||a.id||'?').slice(0,1).toUpperCase();
		div.appendChild(fb);
		box.appendChild(div);
	}
}

// selectHeaderAccount mirror checkbox "Hiển thị" trong panel: click avatar
// → toggle enabled qua API. Cùng state với checkbox nên 2 control luôn sync.
async function selectHeaderAccount(id){
	var acc=accounts.find(function(a){return a.id===id;});
	if(!acc)return;
	await toggleAccountEnabled(id,!acc.enabled);
	renderHeaderAvatars();
}

// switchAcc: deprecated — composer không cần biết account nào đang chat.
// Vẫn giữ để không break code khác; chỉ làm noop.
function switchAcc(id){sy();st('ch');}

// toggleAccountEnabled bật/tắt flag enabled qua API.
// Nếu account bị disabledReason='auth_expired' (zpw_sek invalid) và user tick bật
// lại → từ chối + thông báo cần đăng nhập lại (cookie/QR mới).
async function toggleAccountEnabled(id,enabled){
	var acc=accounts.find(function(a){return a.id===id});
	if(enabled && acc && acc.disabledReason==='auth_expired'){
		alert('Tài khoản này đã bị Zalo vô hiệu hóa (zpw_sek hết hạn).\nVui lòng đăng nhập lại bằng QR hoặc Cookie mới.');
		await loadAccounts(); // reload để checkbox reset
		return;
	}
	try{
		var r=await fetch('/api/account/enabled',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({accountId:id,enabled:enabled})});
		var d=await r.json();
		if(!d.ok){console.log('toggle enabled err',d.error);return;}
	}catch(e){console.log('toggle enabled network err',e);return;}
	if(acc) acc.enabled=enabled;
	// Nếu account bị tắt và đang được select qua header → bỏ chọn composer
	// để tránh composer gửi nhầm từ account đã disabled.
	if(!enabled && ccAccountId===id){cc='';ccAccountId='';ccAccountMe='';}
	await loadAccounts();
	renderAccounts();renderStats();
	sy();loadFriends();
	syncWS();
}

// toggleAllEnabled bật/tắt tất cả cùng lúc.
// Bỏ qua account có disabledReason='auth_expired' (cần re-login mới bật lại được).
async function toggleAllEnabled(){
	// allEnabled chỉ tính những account KHÔNG bị auth_expired.
	var toggleable=accounts.filter(function(a){return a.disabledReason!=='auth_expired';});
	var allEnabled=toggleable.length>0 && toggleable.every(function(a){return a.enabled});
	var wantEnabled=!allEnabled;
	for(var i=0;i<accounts.length;i++){
		var a=accounts[i];
		if(a.enabled===wantEnabled) continue;
		if(a.disabledReason==='auth_expired' && wantEnabled) continue;
		try{
			await fetch('/api/account/enabled',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({accountId:a.id,enabled:wantEnabled})});
			a.enabled=wantEnabled;
		}catch(e){console.log('toggle all err',a.id,e);}
	}
	await loadAccounts();
	renderAccounts();renderStats();
	sy();loadFriends();
	syncWS();
}

// restartListener stop+start listener (fix khi WS bi loi, session con song).
async function restartListener(id){
	try{
		await fetch('/api/account/restart?accountId='+encodeURIComponent(id),{method:'POST'});
		setTimeout(loadAccounts,500);
	}catch(e){console.log('restart err',e)}
}

// syncV2Start trigger SyncV2 (Phase B) cho 1 account: gọi
// POST /api/sync/v2/start — server tạo ed25519 keypair + gửi
// /api/transfer-sync-v2/request-sync. Khi server push WS event
// user_confirm, PullBatch loop tự chạy lưu messages 2022-2024 vào DB.
async function syncV2Start(id){
	try{
		var r=await fetch('/api/sync/v2/start?accountId='+encodeURIComponent(id),{method:'POST'});
		var d=await r.json();
		renderAck(d.ok?{ok:true,data:'SyncV2 started — chờ WS user_confirm rồi tự kéo tin'}:{ok:false,data:(d.data||'fail')+''});
		if(d.ok) setTimeout(function(){syncV2ShowStatus(id);},500);
	}catch(e){renderAck({ok:false,data:'syncV2 err: '+(e||'')});}
}

// syncV2ShowStatus poll status endpoint và hiện phase + lastSeqId.
// Nếu phase=pulling → tiếp tục poll mỗi 3s cho đến done/error.
async function syncV2ShowStatus(id){
	try{
		var r=await fetch('/api/sync/v2/status?accountId='+encodeURIComponent(id));
		var d=await r.json();
		if(!d.ok) return;
		var s=d.data||{};
		var msg='phase='+(s.phase||'?')+' lastSeqId='+(s.lastSeqId||0)+
			' pc='+(s.pcName||'-')+' hasTempKey='+(!!s.hasTempKey)+
			(s.lastError?' err='+s.lastError:'');
		renderAck({ok:true,data:msg});
		if(s.phase==='pulling') setTimeout(function(){syncV2ShowStatus(id);},3000);
	}catch(e){console.log('syncV2 status err',e);}
}

// syncV2Stop dừng PullBatch loop cho account.
async function syncV2Stop(id){
	try{
		var r=await fetch('/api/sync/v2/stop?accountId='+encodeURIComponent(id),{method:'POST'});
		var d=await r.json();
		renderAck(d.ok?{ok:true,data:'SyncV2 stopped'}:{ok:false,data:(d.data||'fail')+''});
	}catch(e){renderAck({ok:false,data:'syncV2 stop err: '+(e||'')});}
}

// syncV2Reset xóa hoàn toàn state, generate keypair mới lần start kế tiếp.
async function syncV2Reset(id){
	if(!confirm('Reset SyncV2 state cho '+id+'? Keypair + lastSeqId sẽ bị xoá.')) return;
	try{
		var r=await fetch('/api/sync/v2/reset?accountId='+encodeURIComponent(id),{method:'POST'});
		var d=await r.json();
		renderAck(d.ok?{ok:true,data:'SyncV2 reset'}:{ok:false,data:(d.data||'fail')+''});
	}catch(e){renderAck({ok:false,data:'syncV2 reset err: '+(e||'')});}
}

// Hien thi modal them tai khoan.
async function showAddModal(reloginID){
	var m=document.getElementById('add-modal');if(!m)return;
	// Mode re-login — preselect accountID, lock vào tab cookie, đổi title.
	if(reloginID){
		m.dataset.reloginId=reloginID;
		var acc=accounts.find(function(a){return a.id===reloginID});
		var accName=acc?acc.displayName:reloginID;
		document.getElementById('add-modal-title').textContent='Đăng nhập lại — '+accName;
		switchAddTab('cookie');
	} else {
		delete m.dataset.reloginId;
		document.getElementById('add-modal-title').textContent='Thêm tài khoản Zalo';
	}
	m.style.display='flex';
	// Chi goi switchAddTab de reset state va trigger createQR ben trong no.
	// Khong goi createQR truc tiep de tranh 2 request song song.
	switchAddTab('qr');
}
// An modal.
function hideAddModal(){
	var m=document.getElementById('add-modal');if(!m)return;
	m.style.display='none';
	stopQRPoll();
}
// Chuyen tab QR/Cookie trong modal.
function switchAddTab(t){
	document.querySelectorAll('.modal-tab').forEach(function(b){b.classList.toggle('on',b.dataset.tab===t)});
	document.getElementById('qr-pane').style.display=t==='qr'?'':'none';
	document.getElementById('cookie-pane').style.display=t==='cookie'?'':'none';
	if(t==='qr') createQR();
	else stopQRPoll();
}
// Tao QR moi + bat dau polling.
async function createQR(){
	document.getElementById('qr-loading').style.display='';
	document.getElementById('qr-img').style.display='none';
	document.getElementById('qr-expire').style.display='none';
	document.getElementById('qr-err').style.display='none';
	try{
		var r=await fetch('/api/qr/create');var d=await r.json();
		if(!d.ok||!d.data){throw new Error(d.error||'tao QR that bai')}
		qrToken=d.data.token;
		// Phai co prefix data:image/png;base64, — neu khong browser khong hien thi.
		document.getElementById('qr-img').src='data:image/png;base64,'+d.data.image;
		document.getElementById('qr-img').style.display='';
		document.getElementById('qr-loading').style.display='none';
		// Poll sau 1.5s
		setTimeout(pollQR,1500);
	}catch(e){
		document.getElementById('qr-loading').style.display='none';
		var er=document.getElementById('qr-err');
		er.innerHTML='<div>'+esc(e.message||String(e))+'</div>'
			+'<button class="modal-primary" style="margin-top:var(--s-2)" onclick="createQR()">↻ Tạo lại QR</button>';
		er.style.display='';
	}
}
// Poll QR. Khi thanh cong -> reload page.
async function pollQR(){
	if(!qrToken)return;
	try{
		var r=await fetch('/api/qr/poll?token='+encodeURIComponent(qrToken));var d=await r.json();
		if(d.ok&&d.data&&d.data.pending){
			setTimeout(pollQR,1500);return;
		}
		if(d.ok&&d.data&&d.data.accountId){
			// Login thanh cong!
			stopQRPoll();hideAddModal();window.location.reload();return;
		}
		// Loi: hien thi + cho nut tao lai
		if(!d.ok){
			var ex=document.getElementById('qr-expire');
			ex.innerHTML='<div>'+esc(d.error||'QR hết hạn hoặc bị lỗi')+'</div>'
				+'<button class="modal-primary" style="margin-top:var(--s-2)" onclick="createQR()">↻ Tạo lại QR</button>';
			ex.style.display='';
			stopQRPoll();
			return;
		}
	}catch(e){console.log('poll err',e)}
	setTimeout(pollQR,1500);
}
function stopQRPoll(){if(qrPollTimer){clearTimeout(qrPollTimer);qrPollTimer=null}qrToken=''}

// Cookie login: nhập zpsid + zpw_sek thủ công, submit qua /api/login/cookie.
async function submitCookie(){
	var zpsid=document.getElementById('ck-zpsid').value.trim();
	var zpwsek=document.getElementById('ck-zpwsek').value.trim();
	var m=document.getElementById('add-modal');
	var reloginId=m&&m.dataset.reloginId?m.dataset.reloginId:'';
	var er=document.getElementById('cookie-err');
	er.style.display='none';
	if(!zpsid||!zpwsek){er.textContent='Nhập cả zpsid và zpw_sek';er.style.display='';return}
	try{
		var r=await fetch('/api/login/cookie',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(reloginId?{accountId:reloginId,zpsid:zpsid,zpw_sek:zpwsek}:{zpsid:zpsid,zpw_sek:zpwsek})});
		var d=await r.json();
		if(d.ok){
			hideAddModal();window.location.reload();
		}else{
			er.textContent=d.error||'Login thất bại';er.style.display='';
		}
	}catch(e){er.textContent=String(e);er.style.display=''}
}

// Backward compat: la() goi loadAccounts de giu signature cu.
async function la(){return loadAccounts()}
// uh() deprecated — header không còn hiển thị account đang chat (composer
// tự chọn account theo conv qua backend).
function uh(){}
function st(t){document.querySelectorAll('.sp').forEach(function(e){e.classList.remove('on')});document.querySelectorAll('.sb-btn').forEach(function(b){b.classList.remove('on')});document.getElementById('pn-'+t).classList.add('on');document.querySelector('.sb-btn[onclick*="'+t+'"]').classList.add('on');if(t=='co')loadFriends();if(t=='mg'){la()}}
function el(i,h){var e=document.getElementById(i);if(h!==undefined)e.innerHTML=h;return e}
function esc(s){return String(s==null?'':s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]})}
// renderAck trả về badge cho delivery ack (tin server chỉ xác nhận gửi/nhận/xem).
// status: "sent" (mặc định — server đã nhận), "delivered", "seen", "unsent".
// ackId: globalDelMsgId từ Zalo (nếu có) để debug.
function renderAck(m){
  var st=m.ackStatus||'sent';
  var labels={sent:'Đã gửi',delivered:'Đã nhận',seen:'Đã xem',unsent:'Thu hồi'};
  var ic={sent:'✓',delivered:'✓✓',seen:'✓✓',unsent:'⟲'};
  var lb=labels[st]||('Đã gửi'+(st?(' ('+st+')'):' (chưa rõ)'));
  var icn=ic[st]||'✓';
  var dv=document.createElement('div');
  dv.className='msg ack '+st;
  dv.dataset.msg=m.id;
  var t=new Date(m.timestamp);
  var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
  var hint='';
  if(st==='sent'&&(!m.ackStatus||!m.ackId)) hint=' — chưa rõ trạng thái';
  var idPart=m.ackId?' <span class="ack-id">#'+esc(m.ackId)+'</span>':'';
  dv.innerHTML='<span class="ack-ic">'+icn+'</span><span class="ack-tx">'+esc(lb)+esc(hint)+'</span>'+idPart+'<div class="tm">'+ts+'</div>';
  return dv;
}

// GROUP_WINDOW_MS: 2 tin cùng fromId + cách nhau ≤ window → group lại.
// Zalo mobile mặc định 60s. Có thể chỉnh nếu Sếp thấy chật/chậm.
var GROUP_WINDOW_MS=60000;

// renderMsg trả về DOM node cho 1 message bubble.
// Layout mới (Zalo style): mỗi bubble có:
//   - avatar (chỉ .msg.in, 28x28 pill)
//   - .bubble wrapper chứa tên + body + timestamp
// Group consecutive (cùng fromId trong GROUP_WINDOW_MS):
//   - middle ẩn avatar + tên + timestamp (sẽ được toggle bởi regroupMessages)
//   - tin đầu group giữ tên (.group-head)
//   - tin cuối group giữ avatar + timestamp (.group-tail)
// data-from + data-ts được gán để regroupMessages quét nhanh.
function renderMsg(m){
  // Nếu server đánh dấu đây là delivery ack → render badge thay vì text thô.
  if(m.isAck||(m.content&&m.content.charAt(0)==='['&&m.content.indexOf('actionType')>=0)){
    return renderAck(m);
  }
  var isMe=m.fromId===ccAccountMe;
  var dv=document.createElement('div');
  dv.className='msg '+(isMe?'out':'in');
  dv.dataset.msg=m.id;
  dv.dataset.from=m.fromId||'';
  dv.dataset.ts=m.timestamp||0;

  // Avatar: chỉ .msg.in có avatar (tin mình gửi dính lề phải, không avatar).
  // Out bubble không cần avatar — CSS .msg.out .av{display:none} đã lo.
  // Nhưng ta vẫn cần placeholder div để flex layout ổn định khi sender đổi từ in↔out.
  var avUrl=(m.fromId && cvCache[m.fromId] && cvCache[m.fromId].avatar) || '';
  var avCls='av'+(avUrl?'':' empty');
  var avStyle=avUrl?('background-image:url('+esc(avUrl)+')'):'';
  var avHtml='<div class="'+avCls+'" style="'+avStyle+'"></div>';

  var attHtml=renderAttachments(m.attachments||[],m.id,m.convId||cc,m.content);
  if(attHtml.indexOf('data-only-imgs="1"')>=0) dv.classList.add('img-only');
  var t=new Date(m.timestamp);
  var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');

  // Tên: hiện cho MỌI bubble (kể cả tin mình gửi — dùng "Bạn" nếu là mình).
  // Tên thật ưu tiên: m.fromName > cvCache[m.fromId].name > ''.
  var senderName='';
  if(isMe){
    senderName='Bạn';
  } else {
    senderName=(m.fromName && m.fromId!==ccAccountMe)?m.fromName
              :((cvCache[m.fromId]&&cvCache[m.fromId].name)||'');
  }
  var body=attHtml+esc(m.content||'');
  var bubbleInner=(senderName?'<div class="nm">'+esc(senderName)+'</div>':'')
                +body
                +'<div class="tm">'+ts+'</div>';
  dv.innerHTML=avHtml+'<div class="bubble">'+bubbleInner+'</div>';
  return dv;
}

// regroupMessages quét toàn bộ .msg trong thread và gán class group-head/tail/grouped.
// Quy tắc: cùng fromId + timestamp diff ≤ GROUP_WINDOW_MS → group lại.
// Middle: .grouped (ẩn avatar/tên/tm qua CSS).
// Head: .grouped.group-head (giữ tên).
// Tail: .grouped.group-tail (giữ avatar + tm).
// Ack (.msg.ack) là đường viền riêng, KHÔNG group.
function regroupMessages(){
  var ml=document.getElementById('ml');
  if(!ml) return;
  var nodes=ml.querySelectorAll('.msg');
  var prevFrom='',prevTs=0,groupStart=-1,groupCount=0;
  function endGroup(tailIdx){
    if(groupStart<0) return;
    var len=tailIdx-groupStart+1;
    if(len>1){
      // group có > 1 tin → đánh head/tail
      nodes[groupStart].classList.add('grouped','group-head');
      for(var k=groupStart+1;k<tailIdx;k++){
        nodes[k].classList.add('grouped');
        nodes[k].classList.remove('group-head','group-tail');
      }
      nodes[tailIdx].classList.add('grouped','group-tail');
      nodes[tailIdx].classList.remove('group-head');
    } else {
      // group 1 tin → reset hết
      nodes[groupStart].classList.remove('grouped','group-head','group-tail');
    }
  }
  for(var i=0;i<nodes.length;i++){
    var n=nodes[i];
    var fid=n.dataset.from||'';
    var ts=parseInt(n.dataset.ts||'0',10);
    // Ack không group
    if(n.classList.contains('ack')){
      endGroup(i-1);
      groupStart=-1;prevFrom='';prevTs=0;
      n.classList.remove('grouped','group-head','group-tail');
      continue;
    }
    var samePerson=fid===prevFrom && fid!=='';
    var inWindow=Math.abs(ts-prevTs)<=GROUP_WINDOW_MS;
    if(samePerson && inWindow && groupStart>=0){
      // tiếp tục group hiện tại
      groupCount++;
    } else {
      // kết thúc group trước, mở group mới
      endGroup(i-1);
      groupStart=i;groupCount=1;
      n.classList.remove('grouped','group-head','group-tail');
    }
    prevFrom=fid;prevTs=ts;
  }
  // kết thúc group cuối
  endGroup(nodes.length-1);
}

// imageExt: đuôi ảnh browser hiểu được. Trả về ext nếu là ảnh, '' nếu không.
function imageExt(nameOrUrl){
  var ext=(nameOrUrl||'').split('.').pop().toLowerCase().split('?')[0].split('#')[0];
  if(['jpg','jpeg','png','gif','webp','bmp'].indexOf(ext)>=0) return ext;
  return '';
}
function videoExt(nameOrUrl){
  var ext=(nameOrUrl||'').split('.').pop().toLowerCase().split('?')[0].split('#')[0];
  if(['mp4','webm','mov','m4v'].indexOf(ext)>=0) return ext;
  return '';
}
// localMediaUrl: nếu url đã trỏ về /media/ thì dùng luôn, không tải lại.
function localMediaUrl(url){
  if(!url) return '';
  if(url.charAt(0)==='/'&&url.indexOf('/media/')===0) return url;
  return '';
}
// renderAttachments: trả HTML cho mọi attachment của 1 message.
// Ảnh: <img> grid (1 ảnh max 320px, 2-3 ảnh 2 cột vuông, 4+ ảnh 3 cột).
//       URL Zalo CDN → auto POST /api/media/download để proxy về /media/.
// Video: <video controls>. File khác: <a> tải về.
function renderAttachments(atts,msgId,convId,content){
  if(!atts||!atts.length) return '';
  var imgs=[],vids=[],files=[];
  for(var i=0;i<atts.length;i++){
    var a=atts[i];if(!a||!a.url) continue;
    var id=a.id||'';
    // Bỏ variant kích thước: -hdUrl/-oriUrl/-thumbUrl/-thumb/-normalUrl.
    if(/-(hdUrl|oriUrl|thumbUrl|thumb|normalUrl)$/.test(id)) continue;
    var ie=imageExt(a.fileName||a.url);
    var ve=videoExt(a.fileName||a.url);
    if(ie){imgs.push({url:a.url,fileName:a.fileName,id:imgBaseId(id),ie:ie})}
    else if(ve){vids.push({url:a.url,fileName:a.fileName,id:vidBaseId(id),ve:ve})}
    else{files.push({url:a.url,fileName:a.fileName,fileSize:a.fileSize,id:id})}
  }
  var html='';
  if(imgs.length){
    var onlyImgs=!(content||'').trim()&&!vids.length&&!files.length;
    html+=renderImageGrid(imgs,msgId,convId,onlyImgs);
  }
  for(var j=0;j<vids.length;j++){
    var v=vids[j];
    var vSrc=localMediaUrl(v.url)||v.url;
    html+='<video class="media" controls preload="metadata" src="'+esc(vSrc)+'"></video>';
  }
  for(var k=0;k<files.length;k++){
    var f=files[k];
    var fSrc=localMediaUrl(f.url)||f.url;
    var label=f.fileName||'Tải file';
    if(f.fileSize){label+=' ('+humanSize(f.fileSize)+')'}
    html+='<a class="file" href="'+esc(fSrc)+'" target="_blank" rel="noopener">'+esc(label)+'</a>';
  }
  return html;
}
function imgBaseId(id){var i=id.lastIndexOf('-');return i>0?id.substring(0,i):id;}
function vidBaseId(id){var i=id.lastIndexOf('-');return i>0?id.substring(0,i):id;}
function humanSize(n){
  if(!n||n<1024) return n+'B';
  if(n<1024*1024) return Math.round(n/1024)+'KB';
  if(n<1024*1024*1024) return Math.round(n/1024/1024)+'MB';
  return (n/1024/1024/1024).toFixed(1)+'GB';
}
// renderImageGrid: render 1-n ảnh theo layout responsive.
// onlyImgs=true → grid đánh dấu data-only-imgs để bubble cha gắn class 'img-only'
// (full-bleed, không padding/border-radius).
function renderImageGrid(imgs,msgId,convId,onlyImgs){
  var cls='imgs imgs-'+(imgs.length<=1?'1':imgs.length<=3?'2-3':'4p');
  var flag=onlyImgs?' data-only-imgs="1"':'';
  var html='<div class="'+cls+'"'+flag+'>';
  for(var i=0;i<imgs.length;i++){
    var a=imgs[i];
    var local=localMediaUrl(a.url);
    var src=local||a.url;
    var dn=local?'':' data-zalo="1" data-fname="'+esc(a.fileName||'')+'" data-ext="'+esc(a.ie)+'"';
    html+='<img class="img" loading="lazy" src="'+esc(src)+'" alt="'+esc(a.fileName||'')+'"'+dn;
    html+=' onclick="openLightbox(this)" onerror="imgErr(this)" data-mid="'+esc(msgId)+'" data-cid="'+esc(convId)+'">';
  }
  html+='</div>';
  return html;
}
// imgErr: ảnh Zalo CDN load fail → tự download về server rồi swap src sang /media/.
function imgErr(img){
  if(img.dataset.zalo!=='1'){img.style.display='none';return}
  img.dataset.zalo='2';
  var mid=img.dataset.mid,cid=img.dataset.cid,fname=img.dataset.fname||'',ext=img.dataset.ext||'jpg';
  var url=img.getAttribute('src');
  var ph=document.createElement('div');ph.className='img-spinner';ph.textContent='Đang tải...';
  img.parentNode.insertBefore(ph,img);img.style.display='none';
  fetch('/api/media/download',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({convId:cid,url:url,fileName:fname,fileExt:ext,msgId:mid})
  }).then(function(r){return r.json()}).then(function(d){
    if(d.ok&&d.data&&d.data.url){
      var local=d.data.url;
      var sels='.img[data-mid="'+cssEsc(mid)+'"][data-cid="'+cssEsc(cid)+'"][src="'+cssEsc(url)+'"]';
      document.querySelectorAll(sels).forEach(function(el){
        el.src=local;el.dataset.zalo='';el.style.display='';
      });
      if(ph&&ph.parentNode) ph.parentNode.removeChild(ph);
    }else{
      ph.textContent='Lỗi tải ảnh';ph.className='img-err';
    }
  }).catch(function(){
    ph.textContent='Lỗi tải ảnh';ph.className='img-err';
  });
}
function cssEsc(s){return String(s||'').replace(/["\\]/g,'\\$&')}
// openLightbox: phóng to ảnh khi click. Esc/click backdrop đóng.
var lbEl=null;
function openLightbox(img){
  if(!lbEl){
    lbEl=document.createElement('div');lbEl.className='lb';
    lbEl.innerHTML='<div class="lb-bg" onclick="closeLightbox()"></div><img class="lb-img" alt=""><div class="lb-cap"></div>';
    document.body.appendChild(lbEl);
    document.addEventListener('keydown',function(e){if(e.key==='Escape')closeLightbox()});
  }
  lbEl.querySelector('.lb-img').src=img.src;
  lbEl.querySelector('.lb-cap').textContent=img.alt||'';
  lbEl.classList.add('on');
}
function closeLightbox(){if(lbEl) lbEl.classList.remove('on')}
// applyMediaDownloaded: server báo đã tải xong media của 1 message. Swap src.
function applyMediaDownloaded(data,curConv){
  if(!data||!data.msgId) return;
  if(curConv&&data.convId&&data.convId!==curConv) return;
  var local=data.url;
  document.querySelectorAll('#ml .msg[data-msg="'+cssEsc(data.msgId)+'"] .img[data-zalo]').forEach(function(el){
    if(local){el.src=local;el.dataset.zalo=''}
  });
}

async function loadFriends(){
	// Load friends cho TẤT CẢ enabled accounts, merge theo (id) + đếm số account
	// có contact này. Sắp xếp: contact chung (xuất hiện ở nhiều account) trước,
	// sau đó alphabet displayName. Mỗi contact hiển thị 1 entry với badge
	// tất cả account đang có contact (vd 'Sep + Chăn Ga').
	//
	// Backend (/api/friends) đọc từ DB contacts — FriendsWorker upsert mỗi phút,
	// tránh rate-limit 429 khi gọi Zalo live. Response shape: Contact[].
	//  Mỗi contact có {accountId, userId, name, avatar}.
	var enabled=accounts.filter(function(a){return a.enabled});
	if(!enabled.length){el('fl','<div style="padding:20px;text-align:center;color:#999">Chưa có tài khoản hiển thị</div>');return}
	el('fl','<div style="padding:20px;text-align:center;color:#999">Đang tải...</div>');
	// 1 call duy nhất — backend merge từ tất cả enabled accounts.
	var r=await fetch('/api/friends');
	var d=await r.json();
	if(!d.ok){el('fl','<div style="padding:20px;text-align:center;color:#999">Lỗi tải danh bạ</div>');return}
	// Map userId → {name, avatar, accounts: [{id, label}]}.
	var merged={};
	var rows=(d.data||[]);
	for(var i=0;i<rows.length;i++){
		var u=rows[i];
		if(!merged[u.userId]){merged[u.userId]={id:u.userId,name:u.name||u.userId,avatar:u.avatar||'',accounts:[]};}
		var acc=enabled.find(function(a){return a.id===u.accountId;});
		merged[u.userId].accounts.push({id:u.accountId,label:acc?acc.displayName:u.accountId});
		// Ưu tiên tên/avatar từ row đầu tiên (đã được backend sắp xếp).
		if(!merged[u.userId]._picked && u.name){merged[u.userId].name=u.name;merged[u.userId]._picked=true;}
		if(!merged[u.userId].avatar && u.avatar){merged[u.userId].avatar=u.avatar;}
	}
	var all=Object.values(merged);
	if(!all.length){el('fl','<div style="padding:20px;text-align:center;color:#999">Không có bạn bè</div>');return;}
	// Sort: ưu tiên chung (accountCount DESC), sau đó alphabet.
	var collator=new Intl.Collator('vi',{sensitivity:'base'});
	all.sort(function(a,b){
		if(b.accounts.length!==a.accounts.length){return b.accounts.length-a.accounts.length;}
		return collator.compare(a.name||'',b.name||'');
	});
	el('fl','');
	all.forEach(function(u){
		// primary account = đầu tiên trong list (để click → mở conv).
		var primary=u.accounts[0];
		var dv=document.createElement('div');dv.className='cv';dv.dataset.id=u.id;dv.dataset.accountId=primary.id;
		var labels=u.accounts.map(function(a){return esc(a.label);}).join(' + ');
		var shared=u.accounts.length>1?' <span class="cv-shared" title="Xuất hiện ở '+u.accounts.length+' tài khoản">×'+u.accounts.length+'</span>':'';
		var accLabel=labels?'<div class="cv-acc">'+labels+shared+'</div>':'';
		dv.innerHTML='<div class="cv-a"><img src="'+(u.avatar||'')+'" onerror="this.style.display=\'none\'"></div>'
			+'<div class="cv-b">'
			+'<div class="cv-n">'+esc(u.name)+'</div>'
			+accLabel
			+'</div>';
		dv.onclick=function(){
			var acc=accounts.find(function(a){return a.id===primary.id;});
			sc(u.id,primary.id,(acc&&acc.userId)||'',u.name,u.avatar);
		};
		el('fl').appendChild(dv);
	});
}
async function sy(){
	// Không còn state 'ca' — backend tự tìm account theo convId khi gửi/nhận.
	if(!accounts.some(function(a){return a.enabled;})){
		el('cl','<div style="padding:20px;text-align:center;color:#999">Chưa có tài khoản hiển thị</div>');return;
	}
	// Sync conversations cho từng enabled account (background).
	accounts.filter(function(a){return a.enabled;}).forEach(function(a){
		try{fetch('/api/conversations/sync?accountId='+a.id);}catch(e){}
	});
	// Load convs DB cho TẤT CẢ enabled accounts, merge + sort theo lastMsgAt DESC.
	var enabled=accounts.filter(function(a){return a.enabled});
	var allConvs=[];
	for(var i=0;i<enabled.length;i++){
		var acc=enabled[i];
		try{
			var r=await fetch('/api/conversations?accountId='+acc.id);
			var d=await r.json();
			if(d.ok&&d.data){
				d.data.forEach(function(c){c._accountId=acc.id;c._accountLabel=acc.displayName||acc.id;});
				allConvs=allConvs.concat(d.data);
			}
		}catch(e){console.log('conv load err',acc.id,e);}
	}
	// Dedupe theo (id, accountId) — vì 2 account khác nhau có thể có cùng id (vd self-thread).
	var seen={};
	allConvs=allConvs.filter(function(c){var k=c.id+'|'+c._accountId;if(seen[k])return false;seen[k]=true;return true;});
	// Sort theo lastMsgAt DESC (rỗng xuống cuối).
	allConvs.sort(function(a,b){var ta=a.lastMsgAt?new Date(a.lastMsgAt).getTime():0;var tb=b.lastMsgAt?new Date(b.lastMsgAt).getTime():0;return tb-ta;});
	el('cl','');
	if(!allConvs.length){el('cl','<div style="padding:20px;text-align:center;color:#999">Chưa có hội thoại</div>');return;}
	allConvs.forEach(function(c){
		cvCache[c.id]={name:c.name,avatar:c.avatar};
		var isCur=(cc&&c.id===cc&&c._accountId===ccAccountId);
		var un=unreadConvs[_convKey(c.id,c._accountId)];
		var dv=document.createElement('div');
		dv.className='cv'+(isCur?' on':'')+(un?' unread':'');
		dv.dataset.id=c.id;dv.dataset.accountId=c._accountId;
		var accLabel=enabled.length>1?'<div class="cv-acc">'+esc(c._accountLabel)+'</div>':'';
		var unBadge=un?'<span class="cv-un" title="'+un.count+' tin chưa đọc">'+(un.count>9?'9+':un.count)+'</span>':'';
		dv.innerHTML='<div class="cv-a"><img src="'+(c.avatar||'')+'" onerror="this.style.display=\'none\'"></div>'
			+'<div class="cv-b">'
			+'<div class="cv-n">'+(c.name||c.id)+'</div>'
			+'<div class="cv-p">'+(esc(c.lastMsgContent||c.lastMsgId||''))+'</div>'
			+accLabel
			+'</div>'
			+unBadge;
		dv.onclick=function(){clearUnread(c.id,c._accountId);var acc=accounts.find(function(a){return a.id===c._accountId;});sc(c.id,c._accountId,(acc&&acc.userId)||'',c.name,c.avatar);};
		el('cl').appendChild(dv);
	});
}
// scrollLastMessage: scroll container đến element tin nhắn cuối cùng.
// Dùng scrollIntoView({block:'end',behavior:'smooth'|'auto'}) thay vì
// scrollTop=scrollHeight — chính xác hơn với flex layout có gap,
// và tự chờ render ảnh/video hoàn tất (browser tự tính layout).
// Thử 2 lần (immediate + rAF) để cover ảnh load chậm.
function scrollLastMessage(ml, smooth){
  if(!ml) return;
  var last=ml.lastElementChild;
  if(!last||last.classList.contains('lm-end')||last.id==='lm-btn'||last.id==='lm-load'){
    // Nếu element cuối là button/spinner/end marker, tìm .msg cuối.
    var msgs=ml.querySelectorAll('.msg');
    if(msgs.length===0) return;
    last=msgs[msgs.length-1];
  }
  var b=smooth?'smooth':'auto';
  try{
    last.scrollIntoView({block:'end', inline:'nearest', behavior:b});
  }catch(e){
    // Fallback cho browser cũ.
    ml.scrollTop=ml.scrollHeight;
  }
  requestAnimationFrame(function(){last.scrollIntoView({block:'end',behavior:b});});
}


function sc(id,accId,accMe,nm,av){cc=id;ccAccountId=accId;ccAccountMe=accMe;ccOtherUid='';cvCache[id]={name:nm,avatar:av};cu=0;cuEnd=false;
clearUnread(id,accId);
document.querySelectorAll('#cl .cv').forEach(function(c){c.classList.toggle('on',c.dataset.id===id&&(c.dataset.accountId||'')===accId)});
document.getElementById('ctt').textContent=nm||id;document.getElementById('ctt-id').textContent=id;document.getElementById('ia').classList.remove('hd');
el('ml','<div class="lm" id="lm-btn" onclick="lm(true)">Tải tin nhắn cũ</div>');
fetch('/api/messages/sync',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({convId:id})}).then(function(r){if(!r.ok)console.log('sync unavailable')}).catch(function(){});lm()}
// lm(manual) — load thêm tin cũ. manual=true khi user click button.
// Khi cuộn lên đầu thread (scrollTop <= 80px) sẽ tự gọi.
// Giữ vị trí scroll: trước khi append lưu scrollHeight, sau khi append
// bù lại phần chênh lệch để tin đang đọc không bị giật.
async function lm(manual){
  if(!cc||ld||cuEnd)return;
  ld=true;
  var ml=document.getElementById('ml');
  var btn=document.getElementById('lm-btn');
  var load=document.getElementById('lm-load');
  if(manual){
    // click thủ công: không cần preserve scroll (user đang ở đầu rồi).
    if(btn){btn.className='lm-load';btn.textContent='Đang tải...';btn.onclick=null;}
  }else{
    // auto-scroll trigger: chèn spinner tạm ở đầu để tránh nhảy layout.
    if(!load){load=document.createElement('div');load.id='lm-load';load.className='lm-load';load.textContent='Đang tải...';ml.insertBefore(load,ml.firstChild);}
  }
  try{
    var r=await fetch('/api/messages?convId='+cc+'&cursor='+cu+'&limit=30');
    var d=await r.json();
    if(d.ok&&d.data&&d.data.length>0){
      var oldH=ml.scrollHeight,oldT=ml.scrollTop;
      if(cu===0){el('ml','');ml=document.getElementById('ml');}
      // Chèn tin cũ vào đầu, giữ nguyên các node hiện tại.
      // Dedupe theo msg id: tránh thêm node trùng với node đã có trong
      // thread (vd khi batch có nhiều tin cùng timestamp ở ranh giới cursor,
      // hoặc khi user đã có sẵn tin realtime từ WS trước khi load cũ).
      var existing=new Set();
      ml.querySelectorAll('.msg').forEach(function(n){if(n.dataset.msg)existing.add(n.dataset.msg)});
      var frag=document.createDocumentFragment();
      d.data.forEach(function(m){if(existing.has(m.id))return;existing.add(m.id);frag.appendChild(renderMsg(m))});
      // Sau khi xóa spinner (nếu có), chèn fragment lên đầu.
      var sp=document.getElementById('lm-load');if(sp)sp.remove();
      ml.insertBefore(frag,ml.firstChild);
      // Group consecutive messages (cùng fromId + GROUP_WINDOW_MS).
      regroupMessages();
      // Bù scroll để giữ vị trí.
      var newH=ml.scrollHeight;
      if(cu===0){
        // First load: cuộn xuống cuối. requestAnimationFrame chờ browser
        // layout xong (ảnh cần load height); không thì scrollTop set trước
        // khi content thực sự render → chỉ cuộn được 1 phần.
        scrollLastMessage(ml,null,false);
        // Thread 1-1: dò tìm UID người kia từ batch đầu để header hiển thị
        // UID người chat thay vì conv_id (vd Phan Xuân 6036488311923736737
        // hiển thị UID người đó thay vì thread id khác nhau theo từng acc).
        if(ccAccountMe){
          for(var mi=0;mi<d.data.length;mi++){
            var fm=d.data[mi];
            if(fm.fromId && fm.fromId!==ccAccountMe && !fm.isAck){
              ccOtherUid=fm.fromId;
              document.getElementById('ctt-id').textContent=ccOtherUid;
              break;
            }
          }
        }
      }else{
        ml.scrollTop=oldT+(newH-oldH);
      }
      // Lấy timestamp tin CŨ NHẤT trong batch (d.data[0]) để page sau lấy
      // được tin cũ hơn. Trước đây lấy d.data[len-1] (tin mới nhất) khiến
      // query strict "<" trùng 29/30 tin với page trước → user thấy "load
      // đi lặp lại tin mới nhất".
      cu=d.data[0].timestamp;
      // Nếu batch đầy → vẫn còn tin cũ hơn → giữ button.
      // Nếu batch < limit → hết → đổi sang thông báo end.
      if(d.data.length<30){
        cuEnd=true;
        if(btn){btn.remove();}
        var end=document.createElement('div');end.className='lm-end';end.textContent='Đã tải hết lịch sử';
        ml.insertBefore(end,ml.firstChild);
      }else if(btn){
        btn.className='lm';btn.textContent='Tải tin nhắn cũ';btn.onclick=function(){lm(true);};
      }
    }else{
      cuEnd=true;
      var sp=document.getElementById('lm-load');if(sp)sp.remove();
      if(btn){btn.remove();}
      var end=document.createElement('div');end.className='lm-end';end.textContent='Đã tải hết lịch sử';
      ml.insertBefore(end,ml.firstChild);
    }
  }catch(e){
    var sp=document.getElementById('lm-load');if(sp)sp.remove();
    if(btn){btn.className='lm';btn.textContent='Tải tin nhắn cũ (thử lại)';btn.onclick=function(){lm(true);};}
  }finally{ld=false;}
}
// Auto-load khi cuộn lên đầu thread.
document.addEventListener('DOMContentLoaded',function(){
  var ml=document.getElementById('ml');
  ml.addEventListener('scroll',function(){
    if(ml.scrollTop<=80&&!ld&&!cuEnd&&cc){lm(false);}
  });
});
async function sm(){
	var inp=document.getElementById('mi');if(!inp)return;
	var c=inp.value.trim();if(!c||!cc)return;inp.value='';
	var sendConv=cc,sendAcc=ccAccountId;
	var ml=el('ml');
	var em=ml.querySelector('.em');if(em)em.remove();
	var dv=document.createElement('div');dv.className='msg out';
	// data-tmp: đánh dấu bubble optimistic đang chờ msgId. WS echo có thể về
	// trước response /api/messages/send → _adoptOutgoing() gắn data-msg luôn
	// thay vì thêm bubble thứ hai.
	dv.dataset.tmp='t'+Date.now()+Math.random().toString(36).slice(2,7);
	dv.dataset.tmpContent=c;
	dv.dataset.from=ccAccountMe||'';
	dv.dataset.ts=Date.now();
	var t=new Date();var ts=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
	dv.innerHTML='<div class="av empty"></div><div class="bubble"><div class="nm">Bạn</div><div>'+esc(c)+'</div><div class="tm">'+ts+'</div></div>';
	ml.appendChild(dv);
	regroupMessages();
	scrollLastMessage(ml,true);
	try{
		var r=await fetch('/api/messages/send',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({to:sendConv,content:c,accountId:sendAcc||undefined})});
		var d=await r.json();
		if(d.ok&&d.data&&d.data.msgId){
			// WS echo chưa adopt → gắn data-msg từ response.
			if(dv.dataset.tmp){delete dv.dataset.tmp;delete dv.dataset.tmpContent;dv.dataset.msg=d.data.msgId;if(d.data.timestamp)dv.dataset.ts=d.data.timestamp;regroupMessages();}
		}else{
			if(dv.parentNode)dv.remove();
			alert('Gửi thất bại: '+((d&&d.error)||r.statusText));
		}
	}catch(e){
		if(dv.parentNode)dv.remove();
		alert('Gửi thất bại: '+e);
	}
}
// xd xoá toàn bộ user Zalo khỏi zcloud (T24.3 — cascade: accounts +
// sessions + messages + conversations + media + contacts + zalo_accounts).
// Confirm 2 lần vì hành động không thể undo. Khác HandleLogout (chỉ xoá
// session, giữ data) — UI có 2 nút riêng: "Đăng xuất" (gọi /api/logout)
// và nút "🗑" (gọi hàm này).
async function xd(id){if(!confirm('Xoá user '+id+' khỏi zcloud? Hành động này KHÔNG THỂ undo — toàn bộ tin nhắn, media, danh bạ sẽ bị xoá.'))return;if(!confirm('Xác nhận lần 2: xoá vĩnh viễn '+id+'?'))return;try{var r=await fetch('/api/account/delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({accountId:id})});var d=await r.json();if(!r.ok||!d.ok){alert('Xoá thất bại: '+(d&&d.error||r.statusText));return}}catch(e){alert('Xoá thất bại: '+e);return}window.location.reload()}
function rd(s){return s.normalize('NFD').replace(/[̀-ͯ]/g,'').replace(/đ/g,'d').replace(/Đ/g,'d').toLowerCase()}
function fc(q){
  // Chỉ filter theo tên conv (.cv-n), KHÔNG match tên account (.cv-acc)
  // — tránh search 'điệp xuân' trả về tất cả conv thuộc account 'Chăn Ga Gối Nệm Điệp Xuân'.
  var qr=rd(q);
  document.querySelectorAll('#pn-ch .cv').forEach(function(c){
    var n=c.querySelector('.cv-n');
    c.style.display=(n && rd(n.textContent).indexOf(qr)!==-1)?'':'none';
  });
}
function ff(q){
  // Chỉ filter theo tên friend (.cv-n), KHÔNG match tên account (.cv-acc).
  var qr=rd(q);
  document.querySelectorAll('#pn-co .cv').forEach(function(c){
    var n=c.querySelector('.cv-n');
    c.style.display=(n && rd(n.textContent).indexOf(qr)!==-1)?'':'none';
  });
}
