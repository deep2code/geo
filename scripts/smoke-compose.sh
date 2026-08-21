#!/usr/bin/env bash
#
# scripts/smoke-compose.sh — docker compose up 一键启动冒烟测试
#
# 验证「部署开箱即用」：构建镜像 → 起服务 → 探活 → 检查可观测性端点 → 账号登录鉴权 → 收尾。
# 无需 .env（compose 已对 .env 设 required:false）。
#
# 用法：
#   bash scripts/smoke-compose.sh                 # 用默认端口 8080
#   GEO_PORT=9090 bash scripts/smoke-compose.sh   # 自定义端口
#
# CI 中由 .github/workflows/ci.yml 的 deploy-smoke 作业调用。

set -uo pipefail

cd "$(dirname "$0")/.."

PORT="${GEO_PORT:-8080}"
MAX_WAIT="${HEALTH_TIMEOUT:-60}"   # 探活最大等待秒数
COMPOSE="docker compose"

# 冒烟用账号体系凭据（启用 AUTH + 预置管理员，验证 JWT 登录后管理接口 200）
AUTH_EMAIL="smoke-admin@example.com"
AUTH_PASSWORD="SmokePass123"

# 兼容性：老版本 docker 用 docker-compose
if ! docker compose version >/dev/null 2>&1; then
  COMPOSE="docker-compose"
fi

cleanup() {
  echo "==> 清理 compose（含数据卷）"
  $COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> [1/6] 构建并启动 compose（--build，启用账号体系）"
GEO_AUTH_ENABLED=true \
GEO_JWT_SECRET="smoke-jwt-secret-0123456789abcdef" \
GEO_ADMIN_EMAIL="$AUTH_EMAIL" \
GEO_ADMIN_PASSWORD="$AUTH_PASSWORD" \
  $COMPOSE up -d --build
echo "    等待容器调度..."

echo "==> [2/6] 等待 geo 健康检查 /api/v1/health"
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

echo "==> [3/6] 检查 Prometheus 指标 /metrics"
if curl -fsS "http://localhost:$PORT/metrics" 2>/dev/null | grep -q "geo_llm_cost\|geo_llm_token"; then
  echo "    指标暴露 OK（geo_llm_cost_* / geo_llm_token_*）"
else
  echo "!! /metrics 未包含预期 GEO 指标"
  curl -fsS "http://localhost:$PORT/metrics" 2>/dev/null | grep -i "geo_" | head
  exit 1
fi

echo "==> [4/6] 管理端点未登录 → 401（账号体系强制登录）"
no_key=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/api/v1/admin/cost" 2>/dev/null || echo 000)
if [ "$no_key" = "401" ]; then
  echo "    未登录 → 401 OK（账号体系生效）"
else
  echo "!! 期望 401，实得 $no_key"
  exit 1
fi

echo "==> [5/6] 管理员登录（POST /api/v1/auth/login）获取 access_token"
login_body=$(curl -fsS -X POST -H 'Content-Type: application/json' \
  -d "{\"email\":\"$AUTH_EMAIL\",\"password\":\"$AUTH_PASSWORD\"}" \
  "http://localhost:$PORT/api/v1/auth/login" 2>/dev/null) || {
  echo "!! 登录失败（检查 BootstrapAdmin 是否成功创建管理员）"
  $COMPOSE logs geo | tail -30
  exit 1
}
TOKEN=$(printf '%s' "$login_body" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
if [ -z "$TOKEN" ]; then
  echo "!! 登录响应未包含 access_token：$login_body"
  exit 1
fi
echo "    登录成功（access_token 已获取）"

echo "==> [6/6] 管理端点携带 Bearer token → 200（Owner/Admin 角色放行）"
with_key=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "http://localhost:$PORT/api/v1/admin/cost" 2>/dev/null || echo 000)
if [ "$with_key" = "200" ]; then
  echo "    带 Bearer token → 200 OK"
else
  echo "!! 期望 200，实得 $with_key"
  exit 1
fi

echo ""
echo "✅ 部署冒烟全部通过：docker compose up 可一键启动并对外提供健康/指标/账号鉴权端点。"
