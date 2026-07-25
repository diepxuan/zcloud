#!/bin/bash
# ============================================
# zcloud — Quản lý service zcloud daemon
# ============================================
# Cách dùng: ./scripts/zcloud.sh {start|stop|restart|logs|status|watch}
#
# Liên kết:
# - Task 00: thiết lập môi trường Go
# - Task 05: xây dựng server daemon
# ============================================

set -e

cd "$(dirname "$0")/.."
PROJECT_ROOT=$(pwd)
BINARY="$PROJECT_ROOT/zcloudd"
SERVICE_NAME="zcloud"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
WATCH_PID_FILE="/tmp/zcloud-watch.pid"

# Màu cho log
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()  { echo -e "${CYAN}[zcloud]${NC} $1"; }
ok()    { echo -e "${GREEN}[  OK  ]${NC} $1"; }
warn()  { echo -e "${YELLOW}[ WARN ]${NC} $1"; }
fail()  { echo -e "${RED}[FAILED]${NC} $1"; }

# ============================================
# Kiểm tra toolchain
# ============================================
check_go() {
    if ! command -v go &>/dev/null; then
        export PATH=$PATH:/usr/local/go/bin
        if ! command -v go &>/dev/null; then
            fail "Go chưa được cài. Chạy: apt install golang-go hoặc tải từ https://go.dev/dl/"
            exit 1
        fi
    fi
    info "Go: $(go version)"
}

# ============================================
# Build binary
# ============================================
build() {
    info "Đang build zcloudd..."
    check_go
    cd "$PROJECT_ROOT/src/zcloud"
    go build -o "$BINARY" ./cmd/zcloudd/
    chmod +x "$BINARY"
    ok "Build xong: $BINARY ($(du -h "$BINARY" | cut -f1))"
}

# ============================================
# Cài systemd service
# ============================================
install_service() {
    if [ ! -f "$BINARY" ]; then
        build
    fi

    info "Cài đặt systemd service..."
    cat > "${SERVICE_FILE}.tmp" <<EOF
[Unit]
Description=ZCloud Daemon — Zalo Cloud Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$PROJECT_ROOT
ExecStart=$PROJECT_ROOT/zcloudd --port 8080 --dev
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
EOF

    if [ -f "$SERVICE_FILE" ]; then
        if ! diff -q "${SERVICE_FILE}.tmp" "$SERVICE_FILE" &>/dev/null; then
            cp "${SERVICE_FILE}.tmp" "$SERVICE_FILE"
            systemctl daemon-reload
            ok "Cập nhật $SERVICE_FILE"
        else
            ok "Service file không thay đổi"
        fi
    else
        mv "${SERVICE_FILE}.tmp" "$SERVICE_FILE"
        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME"
        ok "Cài đặt $SERVICE_FILE và enable"
    fi
    rm -f "${SERVICE_FILE}.tmp"
}

# ============================================
# Start
# ============================================
do_start() {
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        warn "$SERVICE_NAME đang chạy. Dùng restart để khởi động lại."
        return 0
    fi

    install_service

    if [ ! -f "$BINARY" ]; then
        build
    fi

    systemctl start "$SERVICE_NAME"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        ok "$SERVICE_NAME đã khởi động"
        systemctl status "$SERVICE_NAME" --no-pager -l | grep -E "Active:|zcloudd" | head -3
    else
        fail "Khởi động thất bại"
        systemctl status "$SERVICE_NAME" --no-pager -l | tail -10
        exit 1
    fi
}

# ============================================
# Stop
# ============================================
do_stop() {
    # Tắt watch nếu đang chạy
    if [ -f "$WATCH_PID_FILE" ]; then
        pid=$(cat "$WATCH_PID_FILE")
        kill "$pid" 2>/dev/null && ok "Đã tắt watch (PID $pid)" || warn "Watch không chạy"
        rm -f "$WATCH_PID_FILE"
    fi

    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        systemctl stop "$SERVICE_NAME"
        ok "$SERVICE_NAME đã tắt"
    else
        warn "$SERVICE_NAME không chạy"
    fi
}

# ============================================
# Restart
# ============================================
do_restart() {
    build
    install_service
    systemctl restart "$SERVICE_NAME"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        ok "$SERVICE_NAME đã khởi động lại"
    else
        fail "Khởi động lại thất bại"
        systemctl status "$SERVICE_NAME" --no-pager -l | tail -10
        exit 1
    fi
}

# ============================================
# Status
# ============================================
do_status() {
    echo ""
    echo "========== Service Status =========="
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        echo -e "  Trạng thái: ${GREEN}ACTIVE${NC}"
    else
        echo -e "  Trạng thái: ${RED}INACTIVE${NC}"
    fi
    systemctl status "$SERVICE_NAME" --no-pager -l 2>/dev/null | head -15
    echo ""
    echo "========== Binary =========="
    if [ -f "$BINARY" ]; then
        echo "  Binary: $BINARY ($(du -h "$BINARY" | cut -f1))"
    else
        echo "  Binary: chưa build"
    fi
    echo ""
    echo "========== Cổng 8080 =========="
    ss -tlnp | grep 8080 || echo "  Không có process nào listen port 8080"
}

# ============================================
# Logs
# ============================================
do_logs() {
    if [ "$1" == "--follow" ] || [ "$1" == "-f" ]; then
        journalctl -u "$SERVICE_NAME" -f --no-pager -o cat
    else
        journalctl -u "$SERVICE_NAME" --no-pager -n 50 -o cat
    fi
}

# ============================================
# Watch — auto-restart khi code thay đổi
# ============================================
do_watch() {
    info "Chạy watch mode — tự động restart khi code thay đổi..."
    info "Dùng: ${YELLOW}$0 stop${NC} để tắt watch"
    echo ""

    # Build lần đầu
    build
    install_service
    systemctl restart "$SERVICE_NAME"

    # Lưu PID để stop sau này
    echo $$ > "$WATCH_PID_FILE"

    # Cần inotifywait
    if ! command -v inotifywait &>/dev/null; then
        warn "inotifywait chưa cài. Chạy: apt install inotify-tools"
        info "Fallback: poll mỗi 3 giây..."
        while true; do
            inotify_hack
            sleep 3
        done
    else
        info "Theo dõi thay đổi trong src/zcloud/..."
        while true; do
            inotifywait -r -e modify,create,delete,move \
                "$PROJECT_ROOT/src/zcloud" \
                --exclude '(.git|.db|_test.go|node_modules|.sum)' \
                2>/dev/null
            build
            systemctl restart "$SERVICE_NAME"
            ok "Code thay đổi → restart server"
        done
    fi
}

# Fallback khi không có inotify
inotify_hack() {
    local dir="$PROJECT_ROOT/src/zcloud"
    local last=$(find "$dir" -name '*.go' -newer "$BINARY" 2>/dev/null | head -1)
    if [ -n "$last" ]; then
        build
        systemctl restart "$SERVICE_NAME"
        ok "Code thay đổi → restart server"
    fi
}

# ============================================
# Main
# ============================================
case "${1:-help}" in
    start)
        do_start
        ;;
    stop)
        do_stop
        ;;
    restart)
        do_restart
        ;;
    status)
        do_status
        ;;
    logs)
        do_logs "$2"
        ;;
    watch)
        do_watch
        ;;
    help|--help|-h)
        echo ""
        echo "zcloud — Zalo Cloud Service Manager"
        echo ""
        echo "  Cách dùng: $0 {start|stop|restart|status|logs|watch}"
        echo ""
        echo "  start   — Build + cài service + khởi động"
        echo "  stop    — Tắt service + watch"
        echo "  restart — Build lại + restart service"
        echo "  status  — Xem trạng thái service"
        echo "  logs    — Xem logs (thêm -f để follow)"
        echo "  watch   — Dev mode, auto-restart khi code đổi"
        echo ""
        ;;
    *)
        fail "Lệnh không hợp lệ: $1"
        echo "Dùng: $0 {start|stop|restart|status|logs|watch}"
        exit 1
        ;;
esac
