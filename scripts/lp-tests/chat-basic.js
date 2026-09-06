// ============================================
// chat-basic.js — PandaScript smoke test cho /chat
// ============================================
// Chay: lightpanda run scripts/lp-tests/chat-basic.js
// Can zcloudd dang chay o http://127.0.0.1:8080
// Override URL: LP_CHAT_URL env, vd:
//   LP_CHAT_URL=http://localhost:9000/chat lightpanda run scripts/lp-tests/chat-basic.js
//
// Test 3 case:
//   1. Load /chat → check DOM render + global vars.
//   2. Click + Thêm → modal hien + QR load (neu server OK).
//   3. Switch tab Cookie → modal pane doi + 4 buoc huong dan.

const URL = (typeof LP_CHAT_URL !== 'undefined' && LP_CHAT_URL) || 'http://127.0.0.1:8080/chat';

const page = new Page();
console.log('=== Test 1: load /chat ===');
await page.goto(URL);
await page.waitForSelector('#mg-add', { timeout: 5000 });
// Doi DOMContentLoaded xong + conversations sync xong (max 5s).
await page.evaluate('new Promise(r => { if (document.readyState === "complete") r(); else window.addEventListener("load", r); })');
await page.evaluate(`new Promise(r => { let tries = 0; const t = setInterval(() => { if (document.querySelectorAll('.cv').length > 0 || tries++ > 50) { clearInterval(t); r(); } }, 100); })`);

const title = await page.evaluate('document.title');
console.log('title:', title);
if (title !== 'ZCloud Chat') throw new Error('title mismatch: ' + title);

const checksJson = await page.evaluate(`
JSON.stringify({
  hasAddBtn: !!document.getElementById('mg-add'),
  addBtnText: document.getElementById('mg-add') && document.getElementById('mg-add').textContent,
  hasModal: !!document.getElementById('add-modal'),
  cvCount: document.querySelectorAll('.cv').length,
  mgItemCount: document.querySelectorAll('.mg-item').length,
  hasCK: typeof CK_SCRIPT === 'string',
  hasCa: typeof ca === 'string'
})
`);
const c = JSON.parse(checksJson);
console.log('checks:', JSON.stringify(c));
if (!c.hasAddBtn) throw new Error('missing #mg-add');
if (!c.addBtnText || c.addBtnText.trim() !== '+ Thêm') throw new Error('add button text: ' + c.addBtnText);
if (!c.hasModal) throw new Error('missing #add-modal');
if (c.cvCount === 0) throw new Error('no conversations rendered');
if (!c.hasCK) throw new Error('CK_SCRIPT not defined (bug?)');
if (!c.hasCa) throw new Error('ca not defined (no active account?)');
console.log('[OK] Test 1 passed');

console.log('\n=== Test 2: click + Thêm (QR modal) ===');
await page.evaluate('document.getElementById("mg-add").click()');
await page.waitForSelector('#add-modal[style*="flex"]', { timeout: 3000 });
// Doi QR image load (server /api/qr/create co the cham).
await page.evaluate(`new Promise(r => { let tries = 0; const t = setInterval(() => { if (document.getElementById('qr-img').src && document.getElementById('qr-img').style.display !== 'none') { clearInterval(t); r(); } if (tries++ > 60) { clearInterval(t); r(); } }, 100); })`);
const modalJson = await page.evaluate(`
JSON.stringify({
  modalDisplay: getComputedStyle(document.getElementById('add-modal')).display,
  qrLoading: document.getElementById('qr-loading').style.display !== 'none',
  qrImgSrc: (document.getElementById('qr-img').src || '').slice(0, 50),
  activeTab: document.querySelector('.modal-tab.on') && document.querySelector('.modal-tab.on').dataset.tab
})
`);
console.log('modal state:', modalJson);
const m = JSON.parse(modalJson);
if (m.modalDisplay !== 'flex') throw new Error('modal not shown: ' + m.modalDisplay);
if (m.activeTab !== 'qr') throw new Error('QR tab not active: ' + m.activeTab);
if (!m.qrImgSrc.startsWith('data:image/')) throw new Error('QR image not loaded: ' + m.qrImgSrc);
console.log('[OK] Test 2 passed - QR loaded (' + m.qrImgSrc.slice(0, 30) + '...)');

console.log('\n=== Test 3: switch to Cookie tab ===');
await page.evaluate('document.querySelector(".modal-tab[data-tab=\\"cookie\\"]").click()');
await page.evaluate('new Promise(r => setTimeout(r, 500))');
const cookieJson = await page.evaluate(`
JSON.stringify({
  qrPaneHidden: document.getElementById('qr-pane').style.display === 'none',
  cookiePaneShown: document.getElementById('cookie-pane').style.display !== 'none',
  hasGuide: !!document.querySelector('.ck-guide'),
  hasCopyBtn: !!document.querySelector('.ck-copy'),
  stepCount: document.querySelectorAll('.ck-step').length
})
`);
console.log('cookie state:', cookieJson);
const ck = JSON.parse(cookieJson);
if (!ck.qrPaneHidden) throw new Error('QR pane should be hidden');
if (!ck.cookiePaneShown) throw new Error('Cookie pane should be shown');
if (!ck.hasGuide) throw new Error('Cookie guide not rendered');
if (!ck.hasCopyBtn) throw new Error('Copy script button missing');
if (ck.stepCount !== 4) throw new Error('expected 4 steps, got ' + ck.stepCount);
console.log('[OK] Test 3 passed');

console.log('\n=== All tests passed ===');
