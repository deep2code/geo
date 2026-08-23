#!/usr/bin/env bash
# publish.sh — 一键发布 GEO 到生产环境
#
# 流程：自动提交并推送 git（仅工作区有改动时提交）→ 构建 Docker 镜像 → 推送阿里云 ACR → 远程升级 1Panel 容器
# 无论有无代码变化，都会重新打包并推送远程（可用 --skip-* 跳过对应环节）。
#
#   [本地]  git add -A + commit + push origin <branch>（打包前自动完成；无改动则不提交；--skip-commit 跳过）
#     ↓     自动选择构建方式：本机有 go+npm → 本机编译（GOCACHE 增量秒级，前端有变化才重建）
#     ↓     docker buildx build -f Dockerfile.local（仅 COPY 二进制打包，秒级）；无工具链自动回退容器构建
#     ↓     docker push（自动登录 ACR；本机在 VPC 内[Linux & PUSH_VPC=1]直推内网名，否则推公网变体，同仓库自动可见）
#   [远程]  ssh 到服务器：docker login（可选）→ REMOTE_CMD（默认：内网 VPC 地址拉镜像 +
#           compose up -d；镜像名即内网地址，1Panel 显示为内网；服务器不在内网可覆盖 REMOTE_IMAGE）→ 健康检查
#
# 用法：
#   ./scripts/publish.sh                 # 完整流程（提交推送 + 构建 + 发布）
#   ./scripts/publish.sh --force         # 历史兼容参数：始终重新打包（默认即是此行为）
#   ./scripts/publish.sh --skip-commit   # 跳过自动 git 提交推送
#   ./scripts/publish.sh --skip-remote   # 只构建+推送，不升级远程
#   ./scripts/publish.sh --skip-build    # 跳过构建（复用本地已有镜像）
#   ./scripts/publish.sh --skip-push     # 只构建不推送
#   ./scripts/publish.sh --dry-run       # 演练：只打印将要执行的命令
# 构建方式自动选择：本机有 go+npm → 本机编译 + Dockerfile.local 打包；否则容器内全量构建。
#
# 常用环境变量（均有默认值，按需覆盖）：
#   ACR_IMAGE       完整镜像名（默认内网 VPC：crpi-0xi5k79l9j4opzta-vpc.cn-hangzhou.personal.cr.aliyuncs.com/codeup2026/geo:latest）
#   ACR_LOGIN_USER / ACR_LOGIN_PASSWORD   推送到 ACR 用的账号（可选，未登录时自动 login）
#   PLATFORM        目标架构，默认 linux/amd64（当前服务器架构）；
#                   多架构用逗号分隔：PLATFORM="linux/amd64,linux/arm64"（慢，但 arm 服务器也能拉）
#   NPM_REGISTRY    前端 npm 源（默认 npmmirror）
#   GOPROXY_URL     Go 模块代理（默认 goproxy.cn）
#   GOSUMDB_URL     Go 校验库（默认 off；正式环境建议 goproxy.cn 的 sumdb 代理）
#
# 远程升级配置（1Panel 服务器）：
#   REMOTE_HOST     必填，形如 root@1.2.3.4 或加端口 user@host（用 ssh 密钥免密）
#   REMOTE_PORT     ssh 端口（默认 22）
#   REMOTE_COMPOSE_DIR  服务器上 compose 编排目录（默认 /opt/1panel/docker/compose/geo）
#   REMOTE_CMD      自定义远程升级命令（默认: cd <dir> && docker compose pull && docker compose up -d）
#   REMOTE_HEALTH_URL   远程健康检查地址（默认 http://127.0.0.1:8080/api/v1/health，空则跳过）
#
# 说明：
#   - 远程编排方式由 REMOTE_CMD 定义（默认 docker compose pull + up -d，适合 1Panel/
#     compose 编排；不用 compose 时自定义为 docker pull + docker rm -f + docker run 等）。
#   - 仓库已不提供 docker-compose.yml：远程服务器的编排文件由你在服务器侧自行维护，
#     镜像名须与本脚本 ACR_IMAGE 一致，否则 pull 失败。
#   - 自动提交需要 git 已配置 user.name/user.email；commit message 形如
#     "chore: publish 前自动提交（<时间>，<N> 个文件）"。
#   - 定时自动发布示例（每天 02:00 提交变化并发布）：
#     0 2 * * * cd /path/to/my-geo && ./scripts/publish.sh >> /var/log/geo-publish.log 2>&1
#
set -euo pipefail

# ===== 配置（环境变量可覆盖） =====
# 阿里云 ACR 个人版：内网 VPC 与公网指向【同一仓库】（互相可见，推一个另一个立即可见）。
# 规范镜像名（容器运行 / compose / 1Panel 引用）= 内网 VPC 地址，让面板显示为内网地址。
# 推送端点自动选择：本机在 VPC 内（Linux 且 PUSH_VPC=1）→ 直接推内网名；
#   否则（Mac / 非 VPC 机器）推公网变体（同仓库自动可见，不占内网带宽也无妨）。
ACR_REGISTRY_VPC="${ACR_REGISTRY_VPC:-crpi-0xi5k79l9j4opzta-vpc.cn-hangzhou.personal.cr.aliyuncs.com}"
ACR_REGISTRY_PUBLIC="${ACR_REGISTRY_PUBLIC:-crpi-0xi5k79l9j4opzta.cn-hangzhou.personal.cr.aliyuncs.com}"
ACR_IMAGE="${ACR_IMAGE:-${ACR_REGISTRY_VPC}/codeup2026/geo:latest}"   # tag 固定 latest

# 构建镜像源（国内网络默认走镜像，CI 可用官方源覆盖）
NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmmirror.com}"
GOPROXY_URL="${GOPROXY_URL:-https://goproxy.cn,direct}"
GOSUMDB_URL="${GOSUMDB_URL:-off}"

# 目标平台（buildx 多架构）。默认 linux/amd64（当前服务器架构）；
# 多架构用逗号分隔：PLATFORM="linux/amd64,linux/arm64"（构建慢一倍，但 arm 服务器也能拉）
PLATFORM="${PLATFORM:-linux/amd64}"

# 远程 1Panel 服务器（REMOTE_HOST 为空则只做本地构建+推送）
REMOTE_HOST="${REMOTE_HOST:-}"
REMOTE_PORT="${REMOTE_PORT:-22}"
REMOTE_COMPOSE_DIR="${REMOTE_COMPOSE_DIR:-/opt/1panel/docker/compose/geo}"
REMOTE_HEALTH_URL="${REMOTE_HEALTH_URL:-http://127.0.0.1:8080/api/v1/health}"
# 远程服务器拉取镜像地址：默认 = ACR_IMAGE（已是内网 VPC 地址，服务器与 ACR 同 VPC 时走内网）；
# 若服务器不在内网，覆盖 REMOTE_IMAGE 为公网地址即可。
REMOTE_IMAGE="${REMOTE_IMAGE:-${ACR_IMAGE}}"
# 远程默认升级命令（可整体自定义）：内网拉取 → compose up -d（镜像名即内网地址，1Panel 显示内网）
REMOTE_CMD="${REMOTE_CMD:-cd '${REMOTE_COMPOSE_DIR}' && docker pull '${REMOTE_IMAGE}' && docker compose up -d}"

# ACR 登录（可选）
ACR_LOGIN_USER="${ACR_LOGIN_USER:-}"
ACR_LOGIN_PASSWORD="${ACR_LOGIN_PASSWORD:-}"

# ===== 颜色输出 =====
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${CYAN}==>${NC} $*"; }

# ===== 参数 =====
FORCE=0; SKIP_BUILD=0; SKIP_PUSH=0; SKIP_REMOTE=0; DRY_RUN=0; SKIP_COMMIT=0
usage() {
    sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h|--help) usage ;;
        --force) FORCE=1 ;;
        --skip-build) SKIP_BUILD=1 ;;
        --skip-push) SKIP_PUSH=1 ;;
        --skip-commit) SKIP_COMMIT=1 ;;
        --skip-remote) SKIP_REMOTE=1 ;;
        --dry-run) DRY_RUN=1 ;;
        *) error "未知参数: $1"; usage ;;
    esac
    shift
done

# ===== 工具函数 =====
# 注意：CDPATH 含 "." 时相对 cd 会向 stdout 打印目录，必须 CDPATH= 前缀避免污染
SCRIPT_DIR="$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(CDPATH= cd "$SCRIPT_DIR/.." && pwd)"

# 执行（dry-run 时只打印）
run() {
    if [[ "$DRY_RUN" == 1 ]]; then
        echo -e "${YELLOW}[DRY-RUN]${NC} $*"
    else
        "$@"
    fi
}
run_ssh() { # run_ssh <命令串>
    if [[ "$DRY_RUN" == 1 ]]; then
        echo -e "${YELLOW}[DRY-RUN]${NC} ssh -p ${REMOTE_PORT} ${REMOTE_HOST} \"$1\""
    else
        ssh -p "$REMOTE_PORT" "$REMOTE_HOST" "$1"
    fi
}

check_deps() {
    step "检查依赖"
    for c in docker git; do
        command -v "$c" &>/dev/null || { error "缺少命令: $c"; exit 1; }
    done
    if [[ "$SKIP_REMOTE" == 0 && -n "$REMOTE_HOST" ]]; then
        command -v ssh &>/dev/null || { error "缺少命令: ssh"; exit 1; }
    fi
    info "依赖 OK"
}

# 自动提交并推送 git（仅当工作区有改动时提交，避免空提交；无改动跳过 git 操作）。
# 注意：无论有无代码变化，后续都会重新打包并推送远程（见 main）。
auto_commit_and_push() {
    step "自动提交并推送 git（打包前；仅工作区有改动时提交）"
    cd "$PROJECT_DIR"
    if ! git remote | grep -q .; then
        error "未配置 git remote，无法自动推送（可本地手动 git push）"
        exit 1
    fi
    local branch
    branch="$(git branch --show-current 2>/dev/null || true)"
    if [[ -z "$branch" ]]; then
        error "无法确定当前 git 分支"
        exit 1
    fi
    if [[ -z "$(git status --porcelain)" ]]; then
        info "工作区无改动，无需提交 git（仍会重新打包并推送远程）"
        return 0
    fi
    local changed
    changed="$(git status --porcelain | wc -l | tr -d ' ')"
    run git add -A
    run git commit -m "chore: publish 前自动提交（$(date '+%Y-%m-%d %H:%M:%S')，${changed} 个文件）"
    info "已提交 ${changed} 个文件"
    run git push origin "$branch"
    info "已推送到 origin/${branch}"
}

# needs_web 判断前端是否需要重建：dist 缺失/为空，或 web 源码/配置比 dist 新 → 需要。
needs_web() {
    local dist="$PROJECT_DIR/internal/server/web/dist/index.html"
    if [[ ! -f "$dist" ]]; then
        return 0
    fi
    find "$PROJECT_DIR/web-app/src" "$PROJECT_DIR/web-app/package.json" \
         "$PROJECT_DIR/web-app/vite.config.ts" -newer "$dist" -print -quit 2>/dev/null | grep -q . && return 0
    return 1
}

do_build() {
    step "构建镜像 ${ACR_IMAGE}（platform: ${PLATFORM}）"
    if [[ "$SKIP_BUILD" == 1 ]]; then
        warn "--skip-build：复用本地已有镜像"
        docker image inspect "$ACR_IMAGE" &>/dev/null || { error "本地不存在镜像 ${ACR_IMAGE}"; exit 1; }
        return
    fi
    cd "$PROJECT_DIR"
    if ! docker buildx version &>/dev/null; then
        error "需要 docker buildx（Docker Desktop / buildx 插件）"
        exit 1
    fi
    # 本机编译模式（自动选择）：本机有 go+npm → 内置前端构建 + Go 交叉编译
    # （GOCACHE 常驻，增量秒级）→ Dockerfile.local 轻量打包；否则回退容器内全量构建。
    if command -v go &>/dev/null && command -v npm &>/dev/null; then
        local archs version commit build_at arch out
        archs="$(printf '%s' "$PLATFORM" | tr ',' '\n' | sed 's|^linux/||' | tr '\n' ',' | sed 's/,$//')"
        version="$(git describe --tags --always 2>/dev/null || echo dev)"
        commit="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
        build_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        if [[ "$DRY_RUN" == 1 ]]; then
            echo -e "${YELLOW}[DRY-RUN]${NC} 本机编译：npm ci + npm run build（web-app/，前端有变化时）→ internal/server/web/dist"
            echo -e "${YELLOW}[DRY-RUN]${NC} 交叉编译：GOOS=linux GOARCH=${archs//,/,} -ldflags(v=${version} c=${commit}) → build/geo-linux-<arch>"
            echo -e "${YELLOW}[DRY-RUN]${NC} docker buildx build --platform ${PLATFORM} -f Dockerfile.local -t ${ACR_IMAGE}"
            return
        fi
        mkdir -p "$PROJECT_DIR/build"
        # 前端构建（Vite → internal/server/web/dist，go:embed 读取）：仅源码/配置有变化时重建
        if needs_web; then
            info "前端有变化或 dist 缺失，重新构建前端"
            (cd "$PROJECT_DIR/web-app" && npm ci && npm run build)
        else
            info "前端无变化，复用已有 internal/server/web/dist"
        fi
        # 交叉编译（CGO_ENABLED=0 可跨平台；ldflags 注入版本信息）
        for arch in ${archs//,/ }; do
            out="$PROJECT_DIR/build/geo-linux-${arch}"
            info "交叉编译 linux/${arch} → ${out}"
            (cd "$PROJECT_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
                -ldflags "-s -w -X main.version=${version} -X main.commit=${commit} -X main.buildAt=${build_at}" \
                -o "$out" ./cmd/geo)
        done
        # 轻量打包（仅 COPY 二进制；buildx 多平台时按 TARGETARCH 匹配 build/geo-linux-<arch>）
        if [[ "$PLATFORM" == *","* ]]; then
            run docker buildx build --platform "$PLATFORM" --push -f Dockerfile.local -t "$ACR_IMAGE" .
        else
            run docker buildx build --platform "$PLATFORM" --load -f Dockerfile.local -t "$ACR_IMAGE" .
        fi
        info "镜像打包完成: ${ACR_IMAGE}"
        return
    fi
    warn "本机缺少 go/npm，使用容器内构建（自动先构建 geo-build-base 基础镜像：工具链+依赖+编译缓存已固化，仅叠加源码，日常发布快）"
    local version commit build_at base_tag
    version="$(git describe --tags --always 2>/dev/null || echo dev)"
    commit="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
    build_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

    # 1) 基础镜像（geo-build-base）：工具链 + 依赖下载 + 依赖编译缓存。
    #    依赖(go.mod/go.sum/web-app/package*.json)未变时 layer 命中，几乎瞬时。
    if [[ "$PLATFORM" == *","* ]]; then
        base_tag="${ACR_REGISTRY_PUBLIC}/geo-build-base:latest"
        run docker buildx build --platform "$PLATFORM" --push -f Dockerfile.base -t "$base_tag" .
    else
        base_tag="geo-build-base:latest"
        run docker buildx build --platform "$PLATFORM" --load -f Dockerfile.base -t "$base_tag" .
    fi
    info "基础镜像就绪: ${base_tag}"

    # 2) app 镜像（FROM 基础镜像，仅叠加业务源码并编译业务代码）。
    #    单平台：--load 到本地再 push；多平台（含逗号）：--push 直接推 manifest 列表。
    if [[ "$PLATFORM" == *","* ]]; then
        run docker buildx build \
            --platform "$PLATFORM" \
            --push \
            --build-arg "BASE_IMAGE=${base_tag}" \
            --build-arg "VERSION=${version}" \
            --build-arg "COMMIT=${commit}" \
            --build-arg "BUILD_AT=${build_at}" \
            --build-arg "NPM_REGISTRY=${NPM_REGISTRY}" \
            --build-arg "GOPROXY_URL=${GOPROXY_URL}" \
            --build-arg "GOSUMDB_URL=${GOSUMDB_URL}" \
            -t "$ACR_IMAGE" -f Dockerfile .
    else
        run docker buildx build \
            --platform "$PLATFORM" \
            --load \
            --build-arg "BASE_IMAGE=${base_tag}" \
            --build-arg "VERSION=${version}" \
            --build-arg "COMMIT=${commit}" \
            --build-arg "BUILD_AT=${build_at}" \
            --build-arg "NPM_REGISTRY=${NPM_REGISTRY}" \
            --build-arg "GOPROXY_URL=${GOPROXY_URL}" \
            --build-arg "GOSUMDB_URL=${GOSUMDB_URL}" \
            -t "$ACR_IMAGE" -f Dockerfile .
    fi
    info "镜像构建完成: ${ACR_IMAGE}"
}

do_login() { # do_login <registry>（docker login 到指定 registry）
    local reg="$1"
    if [[ -n "$ACR_LOGIN_USER" && -n "$ACR_LOGIN_PASSWORD" ]]; then
        step "登录 ACR ${reg}"
        echo "$ACR_LOGIN_PASSWORD" | run docker login "$reg" -u "$ACR_LOGIN_USER" --password-stdin
    fi
}

do_push() {
    step "推送镜像到 ACR"
    if [[ "$SKIP_PUSH" == 1 ]]; then
        warn "--skip-push：跳过推送"
        return
    fi
    # 推送端点选择：
    #  - 本机在 VPC 内（Linux 且 PUSH_VPC=1）→ 直接推内网名 ACR_IMAGE（crpi-xxx-vpc）；
    #  - 否则（Mac / 非 VPC 机器）→ 推公网变体（crpi-xxx，无 -vpc），ACR 个人版公网/内网同仓库自动可见。
    # Mac 无法直连 VPC，只能走公网，但这不影响最终结果：镜像在仓库里只有一个，面板按 ACR_IMAGE（内网名）运行。
    local push_image push_registry
    if [[ "$(uname -s)" == "Linux" && "${PUSH_VPC:-1}" == "1" ]]; then
        push_image="$ACR_IMAGE"
    else
        push_image="$(printf '%s' "$ACR_IMAGE" | sed -E 's|^crpi-([a-z0-9]+)-vpc\.(cn-[a-z0-9-]+)\.|crpi-\1.\2.|')"
    fi
    push_registry="$(printf '%s' "$push_image" | sed -E 's|/.*||')"
    do_login "$push_registry"
    if [[ "$push_image" != "$ACR_IMAGE" ]]; then
        run docker tag "$ACR_IMAGE" "$push_image"
    fi
    info "推送镜像到 ACR（本机 $(uname -s)，端点 ${push_registry}）: ${push_image}"
    run docker push "$push_image"
    info "推送完成: ${push_image}"
}

remote_upgrade() {
    if [[ -z "$REMOTE_HOST" ]]; then
        warn "未配置 REMOTE_HOST，跳过远程升级"
        warn "提示：只做了本地构建+推送。配置 REMOTE_HOST 后重跑本脚本即可升级远程。"
        return
    fi
    step "远程升级 1Panel 容器（${REMOTE_HOST}）"
    # 远程 ACR 登录（可选）
    if [[ -n "$ACR_LOGIN_USER" && -n "$ACR_LOGIN_PASSWORD" ]]; then
        local remote_registry="${REMOTE_IMAGE%%/*}"
        info "远程登录 ACR（${remote_registry}）..."
        run_ssh "echo '${ACR_LOGIN_PASSWORD}' | docker login '${remote_registry}' -u '${ACR_LOGIN_USER}' --password-stdin"
    fi
    info "执行远程命令: ${REMOTE_CMD}"
    run_ssh "$REMOTE_CMD"
    info "远程容器已升级"
}

remote_health() {
    if [[ -z "$REMOTE_HOST" || -z "$REMOTE_HEALTH_URL" ]]; then
        return
    fi
    step "远程健康检查: ${REMOTE_HEALTH_URL}"
    if run_ssh "curl -fsS --max-time 15 '${REMOTE_HEALTH_URL}' >/dev/null 2>&1 && echo OK || echo FAIL"; then
        info "远程服务健康"
    else
        warn "远程健康检查未通过，请登录服务器排查: journalctl/面板容器日志"
    fi
}

# ===== 主流程 =====
main() {
    info "GEO 发布脚本 | 镜像: ${ACR_IMAGE}"
    check_deps

    # 1) 打包前自动提交并推送 git（--skip-commit 可跳过；仅工作区有改动时提交）
    if [[ "$SKIP_COMMIT" == 1 ]]; then
        warn "--skip-commit：跳过自动提交推送"
    else
        auto_commit_and_push
    fi
    if [[ "$FORCE" == 1 ]]; then
        info "--force：始终重新打包并推送远程（本脚本默认即此行为）"
    fi

    # 2) 无论有无代码变化，一律重新打包并推送远程
    do_build
    do_push
    if [[ "$SKIP_REMOTE" == 1 ]]; then
        warn "--skip-remote：跳过远程升级"
    else
        remote_upgrade
        remote_health
    fi
    echo ""
    info "✅ 发布流程结束"
    if [[ "$SKIP_REMOTE" == 0 && -n "$REMOTE_HOST" && -z "$DRY_RUN" ]]; then
        echo "  远程容器: docker ps（${REMOTE_HOST}）"
        echo "  面板查看: 1Panel → 容器 → geo"
    fi
}

main
