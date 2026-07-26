#!/bin/bash
# ============================================
# zcloud — Quản lý service zcloud daemon
# ============================================
# Cách dùng: ./scripts/zcloud.sh {start|stop|restart|logs|status}
# ============================================

set -e

cd "$(dirname "$0")/.."
PROJECT_ROOT=$(pwd)
BINARY="$PROJECT_ROOT/zcloudd"
SERVICE_NAME="zcloud"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}[zcloud]${NC} $1"; }
ok()    { echo -e "${GREEN}[  OK  ]${NC} $1"; }
warn()  { echo -e "${YELLOW}[ WARN ]${NC} $1"; }
fail()  { echo -e "${RED}[FAILED]${NC} $1"; }

check_go() {
    if ! command -v go &>/dev/null; then
        export PATH=$PATH:/usr/local/go/bin
        if ! command -v go &>/dev/null; then
            fail "Go chưa cài. Tải từ https://go.dev/dl/"
            exit 1
        fi
    fi
}

build() {
    info "Build zcloudd..."
    check_go
    cd "$PROJECT_ROOT/src/zcloud"
    go build -o "$BINARY" ./cmd/zcloudd/
    chmod +x "$BINARY"
    ok "Build xong: $BINARY ($(du -h "$BINARY" | cut -f1))"
}

install_service() {
    if [ ! -f "$BINARY" ]; then build; fi
    info "Cài systemd service..."
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
        fi
    else
        mv "${SERVICE_FILE}.tmp" "$SERVICE_FILE"
        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME"
        ok "Cài $SERVICE_FILE và enable"
    fi
    rm -f "${SERVICE_FILE}.tmp"
}

do_start() {
    systemctl daemon-reload 2>/dev/null
    systemctl enable "$SERVICE_NAME" 2>/dev/null
    systemctl restart "$SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        ok "zcloud đang chạy — http://zcloud.diepxuan.corp:8080"
        info "Watch active — code thay đổi → tự build + restart"
    else
        fail "Khởi động thất bại"
        systemctl status "$SERVICE_NAME" --no-pager -l | tail -5
        exit 1
    fi
}

do_stop() {
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        systemctl stop "$SERVICE_NAME"
        ok "zcloud đã tắt"
    else
        warn "zcloud không chạy"
    fi
}

do_restart() {
    build
    systemctl restart "$SERVICE_NAME"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        ok "zcloud đã restart"
    else
        fail "Restart thất bại"
        systemctl status "$SERVICE_NAME" --no-pager -l | tail -10
        exit 1
    fi
}

do_status() {
    echo ""
    echo "=== zcloud service ==="
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        echo -e "  Trạng thái: ${GREEN}ACTIVE${NC}"
    else
        echo -e "  Trạng thái: ${RED}INACTIVE${NC}"
    fi
    systemctl status "$SERVICE_NAME" --no-pager -l 2>/dev/null | head -12
    echo ""
    echo "=== Port 8080 ==="
    ss -tlnp | grep 8080 || echo "  Không có process nào"
}

do_logs() {
    if [ "$1" == "-f" ] || [ "$1" == "--follow" ]; then
        journalctl -u "$SERVICE_NAME" -f --no-pager -o cat
    else
        journalctl -u "$SERVICE_NAME" --no-pager -n 50 -o cat
    fi
}

case "${1:-help}" in
    start)   do_start ;;
    stop)    do_stop ;;
    restart) do_restart ;;
    status)  do_status ;;
    logs)    do_logs "$2" ;;
    help|--help|-h)
        echo ""
        echo "zcloud — Quản lý service zcloud daemon"
        echo ""
        echo "  start   — Build + cài service + khởi động"
        echo "  stop    — Tắt service"
        echo "  restart — Build lại + restart"
        echo "  status  — Xem trạng thái"
        echo "  logs    — Xem logs (thêm -f để follow)"
        echo ""
        ;;
    *) fail "Lệnh sai: $1"; echo "Dùng: $0 {start|stop|restart|status|logs}"; exit 1 ;;
esac
