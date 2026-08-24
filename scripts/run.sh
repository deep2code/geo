#!/usr/bin/env bash
# run.sh - GEO 系统本地运行脚本
#
# 功能：
#   1. 自动杀死旧版本进程（释放端口 + 进程名匹配）
#   2. 编译最新代码
#   3. 后台启动服务并打印访问地址
#
# 用法：
#   bash scripts/run.sh              # 默认端口 7070
#   bash scripts/run.sh -p 9090      # 指定端口
#   PORT=9090 bash scripts/run.sh    # 通过环境变量指定端口
#   bash scripts/run.sh --no-build   # 跳过编译，直接运行已有二进制

set -euo pipefail

# ===== 配置 =====
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY_NAME="geo"
BIN_PATH="$PROJECT_DIR/bin/$BINARY_NAME"
PID_FILE="/tmp/geo-server.pid"
LOG_FILE="/tmp/geo-server.log"
PORT="${PORT:-7070}"
SKIP_BUILD=false

# 解析参数
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--port) PORT="$2"; shift 2 ;;
    --no-build) SKIP_BUILD=true; shift ;;
    -h|--help)
      echo "用法: bash scripts/run.sh [-p PORT] [--no-build]"
      echo "  -p, --port    指定端口（默认 7070，也可用 PORT 环境变量）"
      echo "  --no-build    跳过编译，直接运行已有二进制"
      echo "  -h, --help    显示帮助"
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

# 颜色输出
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${CYAN}==>${NC} $*"; }

# ===== 1. 杀死旧进程 =====
# 优雅退出：先发 SIGTERM，等待最多 10s（graceful shutdown 处理在途请求），
# 仍未退出再 SIGKILL 强杀。避免 kill -9 直接中断审计导致数据丢失。
kill_old_process() {
  step "检查并停止旧版本进程..."

  local killed=0

  # graceful_stop <pid>：发 SIGTERM，轮询等待最多 10s，超时强杀
  graceful_stop() {
    local pid=$1
    kill "$pid" 2>/dev/null || true
    for i in $(seq 1 10); do
      if ! kill -0 "$pid" 2>/dev/null; then
        return 0
      fi
      sleep 1
    done
    warn "进程 $pid 10s 内未退出，强制杀死"
    kill -9 "$pid" 2>/dev/null || true
  }

  # 方式1：通过 PID 文件
  if [[ -f "$PID_FILE" ]]; then
    local old_pid
    old_pid=$(cat "$PID_FILE" 2>/dev/null || echo "")
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
      info "通过 PID 文件发现旧进程 (PID: $old_pid)，正在优雅停止..."
      graceful_stop "$old_pid"
      info "已停止旧进程 (PID: $old_pid)"
      killed=1
    fi
    rm -f "$PID_FILE"
  fi

  # 方式2：通过端口占用查找进程
  local port_pids
  port_pids=$(lsof -ti ":$PORT" 2>/dev/null || true)
  if [[ -n "$port_pids" ]]; then
    for pid in $port_pids; do
      if kill -0 "$pid" 2>/dev/null; then
        info "端口 $PORT 被占用 (PID: $pid)，正在优雅停止..."
        graceful_stop "$pid"
        info "已停止占用端口的进程 (PID: $pid)"
        killed=1
      fi
    done
  fi

  # 方式3：通过进程名匹配（兜底，捕获所有 geo 进程；二进制不再有子命令）
  local name_pids
  name_pids=$(pgrep -f "$BINARY_NAME" 2>/dev/null || true)
  if [[ -n "$name_pids" ]]; then
    for pid in $name_pids; do
      # 排除当前脚本自身
      if [[ "$pid" == "$$" ]]; then continue; fi
      if kill -0 "$pid" 2>/dev/null; then
        info "发现 geo 进程 (PID: $pid)，正在优雅停止..."
        graceful_stop "$pid"
        info "已停止 geo 进程 (PID: $pid)"
        killed=1
      fi
    done
  fi

  if [[ $killed -eq 0 ]]; then
    info "未发现旧版本进程"
  fi

  # 等待端口释放
  sleep 1
}

# ===== 2. 编译 =====
build_binary() {
  if [[ "$SKIP_BUILD" == "true" ]]; then
    step "跳过编译（--no-build）"
    if [[ ! -x "$BIN_PATH" ]]; then
      error "二进制文件不存在: $BIN_PATH，请去掉 --no-build 重新运行"
      exit 1
    fi
    return
  fi

  step "编译最新代码..."
  cd "$PROJECT_DIR"
  mkdir -p bin
  go build -o "$BIN_PATH" ./cmd/geo
  info "编译完成: $BIN_PATH"
}

# ===== 3. 启动服务 =====
start_service() {
  step "启动服务 (端口 $PORT)..."

  # 后台启动，记录 PID（setsid 完全脱离终端，确保进程持久运行）
  if command -v setsid > /dev/null 2>&1; then
    setsid "$BIN_PATH" --port "$PORT" > "$LOG_FILE" 2>&1 < /dev/null &
  else
    nohup "$BIN_PATH" --port "$PORT" > "$LOG_FILE" 2>&1 < /dev/null &
  fi
  local pid=$!
  disown 2>/dev/null || true
  echo "$pid" > "$PID_FILE"

  # 等待服务就绪（最多 10 秒）
  local ready=false
  for i in $(seq 1 20); do
    if ! kill -0 "$pid" 2>/dev/null; then
      error "进程启动后立即退出，查看日志: $LOG_FILE"
      tail -20 "$LOG_FILE" 2>/dev/null || true
      exit 1
    fi
    if curl -s "http://localhost:$PORT/api/v1/health" > /dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 0.5
  done

  if [[ "$ready" != "true" ]]; then
    warn "服务启动中，健康检查未就绪，可能需要稍等"
    warn "查看日志: tail -f $LOG_FILE"
  fi

  echo ""
  echo -e "${GREEN}========================================${NC}"
  echo -e "${GREEN} GEO 服务已启动${NC}"
  echo -e "${GREEN}========================================${NC}"
  echo -e " PID:     ${CYAN}$pid${NC}"
  echo -e " 端口:    ${CYAN}$PORT${NC}"
  echo -e " Web UI:  ${CYAN}http://localhost:$PORT${NC}"
  echo -e " API:     ${CYAN}http://localhost:$PORT/api/v1${NC}"
  echo -e " 日志:    ${CYAN}$LOG_FILE${NC}"
  echo -e " PID文件: ${CYAN}$PID_FILE${NC}"
  echo ""
  echo -e " 停止服务: ${YELLOW}kill \$(cat $PID_FILE)${NC}"
  echo -e " 查看日志: ${YELLOW}tail -f $LOG_FILE${NC}"
  echo -e " 重新运行: ${YELLOW}bash scripts/run.sh${NC}（会自动杀死旧版本）"
  echo ""
}

# ===== 主流程 =====
main() {
  echo -e "${CYAN}════════════════════════════════════════${NC}"
  echo -e "${CYAN} GEO 系统本地运行脚本${NC}"
  echo -e "${CYAN}════════════════════════════════════════${NC}"
  echo ""

  kill_old_process
  build_binary
  start_service
}

main
