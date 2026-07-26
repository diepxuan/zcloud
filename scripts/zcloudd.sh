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
    info "Code thay đổi → build + restart"
    stop_binary
    build
    start_binary
done
