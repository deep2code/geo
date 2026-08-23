# Dockerfile - GEO 生成式引擎优化系统
# 两阶段构建（依赖与工具链来自 geo-build-base）：前端构建 → Go 后端构建 → Alpine 运行
# 基础镜像自带：Go 1.26 / Node 20 / 已下载的 Go 模块 / 已安装的 npm 包 / 预热的 GOCACHE。
# 本文件只负责「叠加业务源码 + 编译」，因此日常发布很快（依赖不重下、依赖包不重编）。
#
# 统一版本约束：
#   - Go   1.26（与 go.mod / CI ci.yml / release.yml 对齐）
#   - Node.js（含 npm，由 Alpine 仓库提供，当前为 24.x）
#   - Alpine 3.20
#
# 多架构（buildx）：docker buildx build --platform linux/amd64,linux/arm64 --push
#   - web-builder 用 $BUILDPLATFORM（本机架构）跑 npm/vite：产物是纯静态文件，架构无关
#   - builder 用 $TARGETPLATFORM（目标架构）跑 go build：CGO_ENABLED=0 交叉编译
# syntax=docker/dockerfile:1

ARG BASE_IMAGE=geo-build-base:latest

# ===== 阶段 1：前端构建（生产级 SPA；静态产物架构无关，用本机平台构建最快）=====
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE} AS web-builder

WORKDIR /web

# npm 镜像源（默认官方源；国内/受限网络可传 --build-arg NPM_REGISTRY 覆盖）
ARG NPM_REGISTRY=https://registry.npmjs.org
ENV npm_config_registry=$NPM_REGISTRY

# 仅拷贝 package 定义，充分利用 layer 缓存（依赖未变则不重装）
COPY web-app/package.json web-app/package-lock.json ./
# cache mount：npm 缓存持久化，依赖未变时下载走缓存（基础镜像已预热 /root/.npm）
RUN --mount=type=cache,target=/root/.npm \
    npm ci

# 拷贝源码并构建（产物在 dist/ 目录，将被 Go go:embed 读取）
COPY web-app/ ./
RUN npm run build

# ===== 阶段 2：Go 后端构建（目标架构默认即 TARGETPLATFORM；CGO 禁用可安全交叉编译）=====
FROM ${BASE_IMAGE} AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_AT=unknown
# buildx 预定义的目标平台参数
ARG TARGETOS
ARG TARGETARCH

# Go 模块代理与校验库（默认官方源；国内/受限网络可传 build-arg 覆盖）
ARG GOPROXY_URL=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY_URL
ARG GOSUMDB_URL=sum.golang.org
ENV GOSUMDB=$GOSUMDB_URL

WORKDIR /build

# 依赖缓存：基础镜像已把模块下载进 /go/pkg/mod（镜像层），直接复用，不再重新下载。
COPY go.mod go.sum ./
# 拷贝源码
COPY . .

# 从上一阶段拷贝前端构建产物到预期位置（server.go 中 //go:embed web/dist/*，
# 相对 internal/server/ 目录；vite.config.ts 的 outDir 为 ../internal/server/web/dist，
# 在容器 /web 下解析为 /internal/server/web/dist）
COPY --from=web-builder /internal/server/web/dist ./internal/server/web/dist

# 静态编译（纯 Go MySQL 驱动 go-sql-driver/mysql，CGO 可安全禁用；GOOS/GOARCH 来自 buildx）
# 编译缓存策略：
#   - 直接用 buildx 的持久缓存挂载 /gocache（跨构建保留在宿主机，不会被每次构建重置）；
#   - 首次构建冷编全部依赖并写入 /gocache，之后增量命中，仅重编改动的业务代码。
#   - 不再从基础镜像层 cp 预热缓存：overlay 下拷几 GB 海量小文件极慢，且预热的 flag
#     未带 -trimpath、与正式编译对不上，拷过去也用不上（纯属时间黑洞）。
RUN --mount=type=cache,target=/gocache \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOCACHE=/gocache go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildAt=${BUILD_AT}" \
    -o /out/geo \
    ./cmd/geo

# ===== 阶段 3：运行镜像（最小化，含 curl 做健康检查）=====
FROM alpine:3.20

LABEL org.opencontainers.image.title="MyGEO" \
      org.opencontainers.image.description="生成式引擎优化系统 (Generative Engine Optimization)" \
      org.opencontainers.image.source="https://github.com/deep2code/geo" \
      org.opencontainers.image.licenses="MIT"

# 依赖：ca-certificates (HTTPS)、tzdata (时区)、libcap (cap_net_bind_service 低端口)
# 先切换 apk 到阿里云国内镜像（与基础镜像保持一致，避免官方源在国内极慢）
RUN sed -i 's#https\?://dl-cdn.alpinelinux.org/alpine#https://mirrors.aliyun.com/alpine#g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates tzdata libcap && \
    update-ca-certificates && \
    rm -rf /var/cache/apk/*

# 创建非 root 用户（最小权限原则）
RUN addgroup -S geo && adduser -S -G geo geo

WORKDIR /app

# 拷贝二进制并赋予低端口绑定能力（这样 geo -p 80 可在非 root 下启动）
COPY --from=builder /out/geo /app/geo
RUN setcap cap_net_bind_service=+ep /app/geo && \
    chown geo:geo /app/geo && \
    chmod 0755 /app/geo

# 切换非 root 用户
USER geo

# 暴露 HTTP 端口
EXPOSE 8080

ENTRYPOINT ["/app/geo"]
CMD ["--port", "8080"]
