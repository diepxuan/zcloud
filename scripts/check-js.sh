#!/bin/bash
# ============================================
# check-js.sh — Syntax check JS inline trong HTML
# ============================================
# Extract tất cả <script>...</script> từ chat.html / login.html,
# ghi vào /tmp rồi `node --check`. Nếu có SyntaxError → báo file + line.
#
# Dùng khi sửa UI mà không muốn cài Playwright/Chromium.
# Phát hiện: thiếu ngoặc, quote không đóng, khai báo duplicate,
# reserved word sai — gần như mọi SyntaxError.
# KHÔNG phát hiện: runtime error (null deref, fetch fail, async race).
#
# Usage:
#   ./scripts/check-js.sh                # check tất cả file HTML
#   ./scripts/check-js.sh chat.html      # check 1 file cụ thể
# ============================================

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}[check-js]${NC} $1"; }
ok()    { echo -e "${GREEN}[  OK  ]${NC} $1"; }
fail()  { echo -e "${RED}[FAILED]${NC} $1"; }

# Tìm node: PATH thường (systemd có thể thiếu /root/.nvm/...).
find_node() {
	if command -v node &>/dev/null; then
		echo "node"
		return
	fi
	for p in /usr/local/bin/node /usr/bin/node /root/.nvm/versions/node/*/bin/node /opt/node/bin/node; do
		if [ -x "$p" ]; then
			echo "$p"
			return
		fi
	done
	return 1
}

NODE_BIN=$(find_node) || {
	fail "node chưa cài (cần cho \`node --check\`)."
	exit 1
}

cd "$(dirname "$0")/.."
WEB_DIR="src/zcloud/internal/api/web"

if [ -n "$1" ]; then
	FILES="$WEB_DIR/$1"
else
	FILES="$WEB_DIR/chat.html $WEB_DIR/login.html"
fi

FAIL=0
TOTAL=0
for f in $FILES; do
	if [ ! -f "$f" ]; then
		fail "Không tìm thấy $f"
		FAIL=$((FAIL+1))
		continue
	fi
	info "Check $f"
	# Extract từng <script> block ra file .js tạm.
	SCRIPTS=$(python3 -c "
import re, sys
path = sys.argv[1]
with open(path, encoding='utf-8') as f:
	html = f.read()
blocks = re.findall(r'<script[^>]*>(.*?)</script>', html, re.DOTALL)
for i, b in enumerate(blocks):
	if not b.strip():
		continue
	out = '/tmp/zcloud_check_' + path.split('/')[-1].replace('.html','') + '_' + str(i) + '.js'
	with open(out, 'w', encoding='utf-8') as f:
		f.write(b)
	print(out)
" "$f" 2>&1) || {
		fail "  extract failed: $SCRIPTS"
		FAIL=$((FAIL+1))
		continue
	}
	for s in $SCRIPTS; do
		TOTAL=$((TOTAL+1))
		OUT=$("$NODE_BIN" --check "$s" 2>&1) && {
			ok "  $(basename "$s")"
		} || {
			fail "  $(basename "$s"):"
			echo "$OUT" | sed 's/^/      /'
			FAIL=$((FAIL+1))
		}
		rm -f "$s"
	done
done

echo ""
if [ "$FAIL" -eq 0 ]; then
	ok "All $TOTAL script blocks OK."
	exit 0
else
	fail "$FAIL script block(s) have syntax errors."
	exit 1
fi
