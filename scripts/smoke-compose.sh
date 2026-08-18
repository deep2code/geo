#!/usr/bin/env bash
#
# scripts/smoke-compose.sh — docker compose up 一键启动冒烟测试
#
# 验证「部署开箱即用」：构建镜像 → 起服务 → 探活 → 检查可观测性端点 → 收尾。
# 无需 .env（compose 已对 .env 设 required:false）；管理员接口默认 403（安全）。
#
# 用法：
#   bash scripts/smoke-compose.sh                 # 用默认端口 8080
#   GEO_PORT=9090 bash scripts/smoke-compose.sh   # 自定义端口
#   GEO_ADMIN_KEY=xxx bash scripts/smoke-compose.sh  # 同时校验带 key 的 200
#
# CI 中由 .github/workflows/ci.yml 的 deploy-smoke 作业调用。

set -uo pipefail

cd "$(dirname "$0")/.."

ADMIN_KEY="${GEO_ADMIN_KEY:-smoke-admin-key}"
PORT="${GEO_PORT:-8080}"
MAX_WAIT="${HEALTH_TIMEOUT:-60}"   # 探活最大等待秒数
COMPOSE="docker compose"

# 兼容性：老版本 docker 用 docker-compose
if ! docker compose version >/dev/null 2>&1; then
  COMPOSE="docker-compose"
fi

cleanup() {
  echo "==> 清理 compose（含数据卷）"
  $COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> [1/5] 构建并启动 compose（--build）"
GEO_ADMIN_KEY="$ADMIN_KEY" $COMPOSE up -d --build
echo "    等待容器调度..."

echo "==> [2/5] 等待 geo 健康检查 /api/v1/health"
code=000
for i in $(seq 1 "$MAX_WAIT"); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/api/v1/health" 2>/dev/null || echo 000)
  if [ "$code" = "200" ]; then
    echo "    geo 就绪 (HTTP 200)，耗时 ~${i}s"
    break
  fi
  sleep 1
done
if [ "$code" != "200" ]; then
  echo "!! geo 未在 ${MAX_WAIT}s 内就绪（最后 HTTP $code）"
  $COMPOSE logs geo
  exit 1
fi

echo "==> [3/5] 检查 Prometheus 指标 /metrics"
if curl -fsS "http://localhost:$PORT/metrics" 2>/dev/null | grep -q "geo_llm_cost\|geo_llm_token"; then
  echo "    指标暴露 OK（geo_llm_cost_* / geo_llm_token_*）"
else
  echo "!! /metrics 未包含预期 GEO 指标"
  curl -fsS "http://localhost:$PORT/metrics" 2>/dev/null | grep -i "geo_" | head
  exit 1
fi

echo "==> [4/5] 管理员成本端点鉴权（默认无 key → 403）"
no_key=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/api/v1/admin/cost" 2>/dev/null || echo 000)
if [ "$no_key" = "403" ]; then
  echo "    无 X-Admin-Key → 403 OK（默认安全）"
else
  echo "!! 期望 403，实得 $no_key"
  exit 1
fi

echo "==> [5/5] 管理员成本端点（带 key → 200）"
with_key=$(curl -s -o /dev/null -w '%{http_code}' -H "X-Admin-Key: $ADMIN_KEY" "http://localhost:$PORT/api/v1/admin/cost" 2>/dev/null || echo 000)
if [ "$with_key" = "200" ]; then
  echo "    带 X-Admin-Key → 200 OK"
else
  echo "!! 期望 200，实得 $with_key"
  exit 1
fi

echo ""
echo "✅ 部署冒烟全部通过：docker compose up 可一键启动并对外提供健康/指标/鉴权端点。"
