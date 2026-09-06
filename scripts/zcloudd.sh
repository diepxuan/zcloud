#!/bin/bash
# ============================================
# zcloudd.sh — Chạy zcloud daemon với watch mode
# ============================================
# Script này được systemd gọi.
# Nó build binary, start server, và inotifywait
# để tự động build + restart khi code thay đổi.
# ============================================

set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.."; pwd)"
BINARY="$PROJECT_ROOT/zcloudd"
SOURCE="$PROJECT_ROOT/src/zcloud"

info()  { echo "[zcloud] $1"; }
ok()    { echo "[  OK  ] $1"; }
warn()  { echo "[ WARN ] $1"; }

build() {
    cd "$SOURCE"
    go build -o "$BINARY" ./cmd/zcloudd/
    chmod +x "$BINARY"
    cd "$PROJECT_ROOT"
}

# check_js: chay node --check tren cac <script> trong HTML. Neu fail → return 1.
check_js() {
    local SCRIPT_DIR="$(cd "$(dirname "$0")"; pwd)"
    if [ -x "$SCRIPT_DIR/check-js.sh" ]; then
        "$SCRIPT_DIR/check-js.sh" || return 1
    fi
    return 0
}

# check_ui: smoke test UI qua Lightpanda CDP. Can zcloudd dang chay.
# Tra ve 0 neu pass, 1 neu fail. Khong can binary moi — verify binary HIEN TAI.
check_ui() {
    local SCRIPT_DIR="$(cd "$(dirname "$0")"; pwd)"
    if [ ! -x "$SCRIPT_DIR/check-ui.sh" ]; then
        info "check-ui.sh khong ton tai, skip."
        return 0
    fi
    if ! command -v lightpanda >/dev/null 2>&1 && [ ! -x /root/.local/bin/lightpanda ]; then
        info "lightpanda chua cai, skip UI check."
        return 0
    fi
    "$SCRIPT_DIR/check-ui.sh" || return 1
    return 0
}

stop_binary() {
    if [ -n "$BINARY_PID" ] && kill -0 "$BINARY_PID" 2>/dev/null; then
        kill "$BINARY_PID" 2>/dev/null
        wait "$BINARY_PID" 2>/dev/null
    fi
}

start_binary() {
    $BINARY --port 8080 --dev &
    BINARY_PID=$!
    ok "zcloudd chạy (PID $BINARY_PID)"
}

# Build lần đầu
build
start_binary

# Watch source code
info "Watch source code: $SOURCE"
while true; do
    inotifywait -r -e modify,create,delete,move \
        "$SOURCE" \
        --exclude '(.git|.db|_test.go|.sum)' \
        2>/dev/null || sleep 2
    info "Code thay đổi → check JS + build + restart + check UI"
    # Step 1: Check JS syntax truoc. Neu fail → giu binary cu chay, canh bao.
    if ! check_js; then
        info "JS syntax error — KHÔNG restart, giữ binary cũ."
        continue
    fi
    # Step 2: Build + restart binary.
    stop_binary
    build
    start_binary
    # Step 3: Wait cho server ready (port bind + DB migration).
    sleep 2
    # Step 4: UI smoke test qua Lightpanda CDP.
    # Neu fail → log warning nhung KHONG rollback (JS pass rồi, có thể là
    # data-driven issue, vd thieu account). Operator check log de debug.
    if ! check_ui; then
        warn "UI smoke test FAILED — binary da restart. Check log."
    fi
done
