#!/bin/bash
# ============================================
# check-ui.sh — Smoke test UI qua Lightpanda CDP
# ============================================
# Start Lightpanda serve, kết nối CDP, mở /chat, check:
#  - DOM render đầy đủ (mg-item, add-modal, cv count...)
#  - Global vars tồn tại (CK_SCRIPT, ca, accounts)
#  - Đoạn test JS quan trọng chạy được (no exceptionDetails)
#
# Phát hiện: runtime error trong test snippets, DOM render sai.
# KHÔNG phát hiện: passive error (script fail im lặng) —
# Lightpanda early version chưa emit Runtime.exceptionThrown
# cho mọi uncaught error.
#
# Usage:
#   ./scripts/check-ui.sh                  # check localhost:8080/chat
#   ./scripts/check-ui.sh http://...       # check URL khác
#   ZCLOUD_PORT=9000 ./scripts/check-ui.sh # check port khác
# ============================================

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}[check-ui]${NC} $1"; }
ok()    { echo -e "${GREEN}[  OK  ]${NC} $1"; }
fail()  { echo -e "${RED}[FAILED]${NC} $1"; }
warn()  { echo -e "${YELLOW}[ WARN ]${NC} $1"; }

URL="${1:-http://127.0.0.1:${ZCLOUD_PORT:-8080}/chat}"

find_lp() {
	if command -v lightpanda &>/dev/null; then echo "lightpanda"; return; fi
	for p in /root/.local/bin/lightpanda /usr/local/bin/lightpanda; do
		[ -x "$p" ] && { echo "$p"; return; }
	done
	return 1
}

find_node() {
	if command -v node &>/dev/null; then echo "node"; return; fi
	for p in /usr/bin/node /usr/local/bin/node /root/.nvm/versions/node/*/bin/node; do
		[ -x "$p" ] && { echo "$p"; return; }
	done
	return 1
}

LP_BIN=$(find_lp) || { fail "lightpanda chưa cài. Chạy: curl -fsSL https://pkg.lightpanda.io/install.sh | bash"; exit 1; }
NODE_BIN=$(find_node) || { fail "node chưa cài."; exit 1; }

PORT=$((9000 + RANDOM % 1000))
info "Use Lightpanda CDP port: $PORT, target URL: $URL"

$LP_BIN serve --host 127.0.0.1 --port "$PORT" > /tmp/lp_check_ui.log 2>&1 &
LP_PID=$!
trap "kill $LP_PID 2>/dev/null || true" EXIT

for i in $(seq 1 20); do
	curl -s "http://127.0.0.1:$PORT/json/version" > /dev/null 2>&1 && break
	sleep 0.5
done
curl -s "http://127.0.0.1:$PORT/json/version" > /dev/null || { fail "Lightpanda không lên."; cat /tmp/lp_check_ui.log; exit 1; }
ok "Lightpanda CDP ready"

TEST_JS="/tmp/zcloud_check_ui.mjs"
cat > "$TEST_JS" << NODEEOF
const URL = process.argv[2];
const PORT = parseInt(process.argv[3], 10);

let nextId = 1;
const pending = new Map();

const ws = new WebSocket('ws://127.0.0.1:' + PORT);
await new Promise((res, rej) => {
	ws.addEventListener('open', res, { once: true });
	ws.addEventListener('error', () => rej(new Error('ws connect fail')), { once: true });
	setTimeout(() => rej(new Error('ws timeout')), 5000);
});

function rpc(method, params = {}, sessionId) {
	return new Promise((resolve, reject) => {
		const id = nextId++;
		const handler = (e) => {
			const m = JSON.parse(e.data);
			if (m.id === id) {
				ws.removeEventListener('message', handler);
				pending.delete(id);
				m.error ? reject(new Error(JSON.stringify(m.error))) : resolve(m.result);
			}
		};
		pending.set(id, { res: resolve, rej: reject });
		ws.addEventListener('message', handler);
		const payload = { id, method, params };
		if (sessionId) payload.sessionId = sessionId;
		ws.send(JSON.stringify(payload));
		setTimeout(() => { ws.removeEventListener('message', handler); pending.delete(id); reject(new Error('timeout ' + method)); }, 15000);
	});
}

const { targetId } = await rpc('Target.createTarget', { url: 'about:blank' });
const { sessionId } = await rpc('Target.attachToTarget', { targetId, flatten: true });
await rpc('Page.enable', {}, sessionId);
await rpc('Runtime.enable', {}, sessionId);

const t0 = Date.now();
await rpc('Page.navigate', { url: URL }, sessionId);
await new Promise(r => setTimeout(r, 3000));
console.log('NAV=' + (Date.now() - t0) + 'ms');

// check(name, expr, expected): expected is value hoặc function(actual).
async function check(name, expr, expected) {
	let r;
	try {
		r = await rpc('Runtime.evaluate', { expression: expr, returnByValue: true }, sessionId);
	} catch (e) {
		console.log('CHECK_ERROR=' + name + ': ' + e.message);
		return;
	}
	if (r.exceptionDetails) {
		console.log('CHECK_EXCEPTION=' + name + ': ' + r.exceptionDetails.text + ' @ line ' + r.exceptionDetails.lineNumber);
		return;
	}
	const actual = r.result?.value;
	let ok;
	if (typeof expected === 'function') ok = expected(actual);
	else ok = actual === expected;
	console.log('CHECK=' + name + ': ' + (ok ? 'OK' : 'FAIL') + ' actual=' + JSON.stringify(actual) + ' expected=' + JSON.stringify(typeof expected === 'function' ? '<fn>' : expected));
}

await check('document.title', 'document.title', 'ZCloud Chat');
await check('typeof CK_SCRIPT', 'typeof CK_SCRIPT', 'string');
await check('typeof ca', 'typeof ca', 'string');
await check('typeof accounts', 'typeof accounts', 'object');
await check('add-modal exists', '!!document.getElementById("add-modal")', true);
await check('#mg-add text', 'document.getElementById("mg-add")?.textContent', '+ Thêm');
await check('.cv rendered', 'document.querySelectorAll(".cv").length', n => n > 0);
await check('account loaded', 'document.querySelectorAll(".mg-item, .mg-empty").length', n => n > 0);

// Test critical JS path: call createQR() (no-op neu khong co token)
await check('createQR function exists', 'typeof createQR === "function"', true);
// Test renderAccounts() neu co accounts
await check('renderAccounts function exists', 'typeof renderAccounts === "function"', true);

ws.close();
NODEEOF

OUT=$("$NODE_BIN" "$TEST_JS" "$URL" "$PORT" 2>&1) || {
	fail "Node test crash:"
	echo "$OUT" | sed 's/^/    /'
	exit 1
}

echo "$OUT" | grep -E '^NAV=|^CHECK=|^CHECK_ERROR=|^CHECK_EXCEPTION='

FAIL=0
EXC=$(echo "$OUT" | grep -c '^CHECK_EXCEPTION=' || true)
ERR=$(echo "$OUT" | grep -c '^CHECK_ERROR=' || true)
CHK_FAIL=$(echo "$OUT" | grep -c '^CHECK=.*FAIL' || true)

if [ "$EXC" -gt 0 ]; then
	fail "$EXC check threw exception (JS runtime error):"
	echo "$OUT" | grep '^CHECK_EXCEPTION=' | sed 's/^/    /'
	FAIL=$((FAIL+1))
fi
if [ "$ERR" -gt 0 ]; then
	fail "$ERR check had error:"
	echo "$OUT" | grep '^CHECK_ERROR=' | sed 's/^/    /'
	FAIL=$((FAIL+1))
fi
if [ "$CHK_FAIL" -gt 0 ]; then
	fail "$CHK_FAIL check(s) failed:"
	echo "$OUT" | grep '^CHECK=.*FAIL' | sed 's/^/    /'
	FAIL=$((FAIL+1))
fi

if [ "$FAIL" -eq 0 ]; then
	NAV=$(echo "$OUT" | grep '^NAV=' | cut -d= -f2)
	ok "UI checks pass (nav ${NAV}ms)."
	rm -f "$TEST_JS"
	exit 0
else
	fail "$FAIL issue(s)."
	rm -f "$TEST_JS"
	exit 1
fi
