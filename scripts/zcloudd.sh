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
    info "Code thay đổi → check JS + build + restart"
    # Check JS syntax truoc. Neu fail → giu binary cu chay, canh bao.
    if ! check_js; then
        info "JS syntax error — KHÔNG restart, giữ binary cũ. Xem loi ben tren."
        continue
    fi
    stop_binary
    build
    start_binary
done
