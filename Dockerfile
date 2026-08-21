# Dockerfile - GEO 生成式引擎优化系统
# 三阶段构建：前端构建 → Go 后端构建 → Alpine 运行
# 统一版本约束：
#   - Go   1.26（与 go.mod / CI ci.yml / release.yml 对齐）
#   - Node 20.x（与 CI / 现代 Vite 要求对齐）
#   - Alpine 3.20
#
# 多架构（buildx）：docker buildx build --platform linux/amd64,linux/arm64 --push
#   - web-builder 用 $BUILDPLATFORM（本机架构）跑 npm/vite：产物是纯静态文件，
#     架构无关，本机原生构建最快
#   - builder 用 $TARGETPLATFORM（目标架构）跑 go build：CGO_ENABLED=0 交叉编译
# syntax=docker/dockerfile:1

# ===== 阶段 1：前端构建（生产级 SPA；静态产物架构无关，用本机平台构建最快）=====
FROM --platform=$BUILDPLATFORM node:20-alpine AS web-builder

WORKDIR /web

# npm 镜像源（默认官方源；国内/受限网络可传 --build-arg NPM_REGISTRY 覆盖）
ARG NPM_REGISTRY=https://registry.npmjs.org
ENV npm_config_registry=$NPM_REGISTRY

# 仅拷贝 package 定义，充分利用 layer 缓存
COPY web-app/package.json web-app/package-lock.json ./
# cache mount：npm 缓存持久化，依赖未变时下载走缓存
RUN --mount=type=cache,target=/root/.npm \
    npm ci

# 拷贝源码并构建（产物在 dist/ 目录，将被 Go go:embed 读取）
COPY web-app/ ./
RUN npm run build

# ===== 阶段 2：Go 后端构建（目标架构默认即 TARGETPLATFORM；CGO 禁用可安全交叉编译）=====
FROM golang:1.26-alpine AS builder

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

# 依赖缓存层：先拷贝 go.mod/go.sum 再下载（cache mount 保留模块缓存，依赖未变不重下）
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 拷贝源码
COPY . .

# 从上一阶段拷贝前端构建产物到预期位置（server.go 中 //go:embed web/dist/*，
# 相对 internal/server/ 目录；vite.config.ts 的 outDir 为 ../internal/server/web/dist，
# 在容器 /web 下解析为 /internal/server/web/dist）
COPY --from=web-builder /internal/server/web/dist ./internal/server/web/dist

# 静态编译（纯 Go MySQL 驱动 go-sql-driver/mysql，CGO 可安全禁用；GOOS/GOARCH 来自 buildx）
# cache mount：Go 编译缓存（GOCACHE）与模块缓存持久化——未变化的包直接命中缓存，
# 避免每次构建全量重编（首次 ~2.5min，后续小改动仅重编变化包，通常数秒~数十秒）
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
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
RUN apk add --no-cache ca-certificates tzdata libcap && \
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

# 数据持久化：所有模块使用外部 MySQL 8.0+（GEO_*_MYSQL_DSN），本地不存数据文件。
# 日志/临时文件写入容器可写层（不挂 VOLUME，重启即清；需要持久化日志请用
# docker 日志驱动/外部采集）。
ENV GEO_DATA_DIR=/data/geo

# 暴露 HTTP 端口
EXPOSE 8080

ENTRYPOINT ["/app/geo"]
CMD ["--port", "8080"]
