#!/usr/bin/env bash
# deploy.sh - GEO 生成式引擎优化系统部署脚本
#
# 用法：
#   bash scripts/deploy.sh              # Docker 部署（默认）
#   bash scripts/deploy.sh --binary     # 二进制部署
#   bash scripts/deploy.sh --stop       # 停止服务
#   bash scripts/deploy.sh --status     # 查看状态
#   bash scripts/deploy.sh --logs       # 查看日志
#   bash scripts/deploy.sh --build-only # 仅构建不启动

set -euo pipefail

# ===== 配置 =====
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE_NAME="geo"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="geo-server"
SERVICE_PORT="${GEO_PORT:-8080}"
INSTALL_DIR="${INSTALL_DIR:-/opt/geo}"
BINARY_NAME="geo"

# 颜色输出
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${CYAN}==>${NC} $*"; }

# ===== 帮助 =====
usage() {
    cat <<EOF
GEO 部署脚本

用法: bash scripts/deploy.sh [选项]

选项:
  (无)          Docker Compose 部署（默认）
  --binary      二进制部署（编译 + systemd）
  --build-only  仅构建镜像/二进制，不启动
  --stop        停止服务
  --restart     重启服务
  --status      查看服务状态
  --logs        查看日志
  --clean       清理镜像和产物
  -h, --help    显示帮助

环境变量:
  GEO_PORT      服务端口（默认 8080）
  IMAGE_TAG     镜像标签（默认 latest）
  INSTALL_DIR   二进制安装路径（默认 /opt/geo）

EOF
}

# ===== 前置检查 =====
check_env_file() {
    if [[ ! -f "$PROJECT_DIR/.env" ]]; then
        warn "未找到 .env 文件，从模板创建..."
        cp "$PROJECT_DIR/.env.example" "$PROJECT_DIR/.env"
        info "已创建 .env，请按需编辑: $PROJECT_DIR/.env"
        warn "⚠ 模板包含默认弱密码（geoPass），生产环境务必修改后重启！"
    fi
    # 检测默认弱密码：拒绝带默认口令上线
    if grep -qE 'geoPass' "$PROJECT_DIR/.env" 2>/dev/null; then
        warn "⚠ .env 中仍存在默认弱密码（geoPass），生产部署前请修改: GEO_*_MYSQL_DSN / GEO_MYSQL_PASSWORD"
    fi
}

# ===== Docker 部署 =====
deploy_docker() {
    step "检查 Docker 环境"
    if ! command -v docker &>/dev/null; then
        error "未安装 Docker，请先安装: https://docs.docker.com/get-docker/"
        exit 1
    fi

    check_env_file

    step "构建 Docker 镜像 ${IMAGE_NAME}:${IMAGE_TAG}"
    cd "$PROJECT_DIR"
    docker build -t "${IMAGE_NAME}:${IMAGE_TAG}" .

    if [[ "${1:-}" == "--build-only" ]]; then
        info "仅构建模式，镜像已就绪: ${IMAGE_NAME}:${IMAGE_TAG}"
        return
    fi

    step "启动服务（docker compose）"
    if command -v docker-compose &>/dev/null; then
        docker-compose up -d
    else
        docker compose up -d
    fi

    sleep 2
    step "健康检查"
    if curl -sf "http://localhost:${SERVICE_PORT}/api/v1/health" >/dev/null 2>&1; then
        info "服务运行正常: http://localhost:${SERVICE_PORT}"
        echo ""
        echo "  健康检查:  curl http://localhost:${SERVICE_PORT}/api/v1/health"
        echo "  查看策略:  curl http://localhost:${SERVICE_PORT}/api/v1/strategies"
        echo "  优化内容:  curl -X POST http://localhost:${SERVICE_PORT}/api/v1/optimize -H 'Content-Type: application/json' -d '{\"content\":\"...\"}'"
        echo ""
    else
        warn "服务可能还在启动中，请稍后重试健康检查"
        warn "查看日志: bash scripts/deploy.sh --logs"
    fi
}

# ===== 二进制部署 =====
deploy_binary() {
    step "编译二进制（Linux amd64）"
    cd "$PROJECT_DIR"
    mkdir -p bin
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "bin/${BINARY_NAME}-linux-amd64" ./cmd/geo

    if [[ "${1:-}" == "--build-only" ]]; then
        info "二进制已就绪: bin/${BINARY_NAME}-linux-amd64"
        return
    fi

    step "安装到 ${INSTALL_DIR}"
    sudo mkdir -p "${INSTALL_DIR}"
    sudo cp "bin/${BINARY_NAME}-linux-amd64" "${INSTALL_DIR}/${BINARY_NAME}"
    sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    # 低端口绑定能力（仅当端口 <1024 时非 root 需要）
    if (( SERVICE_PORT < 1024 )); then
        sudo setcap cap_net_bind_service=+ep "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    if [[ -f "$PROJECT_DIR/.env" ]]; then
        sudo cp "$PROJECT_DIR/.env" "${INSTALL_DIR}/.env"
    fi

    step "创建 systemd 服务（非 root 运行）"
    # 创建专用系统用户（幂等），最小权限原则
    sudo id geo &>/dev/null || sudo useradd --system --no-create-home --home-dir "${INSTALL_DIR}" --shell /usr/sbin/nologin geo
    sudo chown -R geo:geo "${INSTALL_DIR}"
    cat <<EOF | sudo tee /etc/systemd/system/geo.service >/dev/null
[Unit]
Description=GEO Generative Engine Optimization Service
After=network.target

[Service]
Type=simple
User=geo
Group=geo
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${INSTALL_DIR}/${BINARY_NAME} serve -p ${SERVICE_PORT}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable geo
    sudo systemctl restart geo

    sleep 2
    step "检查服务状态"
    if sudo systemctl is-active --quiet geo; then
        info "GEO 服务已启动: http://localhost:${SERVICE_PORT}"
        echo ""
        echo "  查看状态:  sudo systemctl status geo"
        echo "  查看日志:  sudo journalctl -u geo -f"
        echo "  停止服务:  sudo systemctl stop geo"
        echo ""
    else
        error "服务启动失败，查看日志: sudo journalctl -u geo -e"
        exit 1
    fi
}

# ===== 停止服务 =====
stop_service() {
    if [[ -f /etc/systemd/system/geo.service ]] && sudo systemctl is-active --quiet geo 2>/dev/null; then
        step "停止 systemd 服务"
        sudo systemctl stop geo
        info "已停止 geo 服务"
    fi
    step "停止 Docker 服务"
    cd "$PROJECT_DIR"
    if command -v docker-compose &>/dev/null; then
        docker-compose down 2>/dev/null || true
    else
        docker compose down 2>/dev/null || true
    fi
    info "已停止 Docker 容器"
}

# ===== 重启服务 =====
restart_service() {
    step "重启服务"
    if [[ -f /etc/systemd/system/geo.service ]] && sudo systemctl is-active --quiet geo 2>/dev/null; then
        sudo systemctl restart geo
        info "已重启 systemd 服务"
    else
        cd "$PROJECT_DIR"
        if command -v docker-compose &>/dev/null; then
            docker-compose restart
        else
            docker compose restart
        fi
        info "已重启 Docker 容器"
    fi
}

# ===== 查看状态 =====
show_status() {
    echo "===== GEO 服务状态 ====="
    echo ""
    # Docker
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "${CONTAINER_NAME}"; then
        info "Docker 容器运行中:"
        docker ps --filter "name=${CONTAINER_NAME}" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    else
        warn "Docker 容器未运行"
    fi
    echo ""
    # systemd
    if [[ -f /etc/systemd/system/geo.service ]]; then
        if sudo systemctl is-active --quiet geo 2>/dev/null; then
            info "systemd 服务运行中"
        else
            warn "systemd 服务未运行"
        fi
    fi
    echo ""
    # 健康检查
    if curl -sf "http://localhost:${SERVICE_PORT}/api/v1/health" >/dev/null 2>&1; then
        info "健康检查: 通过 (端口 ${SERVICE_PORT})"
    else
        warn "健康检查: 端口 ${SERVICE_PORT} 无响应"
    fi
}

# ===== 查看日志 =====
show_logs() {
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "${CONTAINER_NAME}"; then
        step "Docker 日志"
        docker logs -f --tail 100 "${CONTAINER_NAME}"
    elif [[ -f /etc/systemd/system/geo.service ]]; then
        step "systemd 日志"
        sudo journalctl -u geo -f --no-pager -n 100
    else
        warn "未发现运行中的服务"
    fi
}

# ===== 清理 =====
clean_all() {
    step "清理构建产物"
    cd "$PROJECT_DIR"
    rm -rf bin/ dist/
    info "已清理 bin/ dist/"
    step "清理 Docker 镜像"
    docker rmi "${IMAGE_NAME}:${IMAGE_TAG}" 2>/dev/null && info "已删除镜像 ${IMAGE_NAME}:${IMAGE_TAG}" || warn "无镜像可清理"
}

# ===== 主入口 =====
main() {
    case "${1:-}" in
        -h|--help) usage; exit 0 ;;
        --binary) deploy_binary "${2:-}" ;;
        --build-only) deploy_docker --build-only ;;
        --stop) stop_service ;;
        --restart) restart_service ;;
        --status) show_status ;;
        --logs) show_logs ;;
        --clean) clean_all ;;
        "") deploy_docker ;;
        *) error "未知选项: $1"; usage; exit 1 ;;
    esac
}

main "$@"
