# Dockerfile - GEO 生成式引擎优化系统
# 三阶段构建：前端构建 → Go 后端构建 → Alpine 运行
# 统一版本约束：
#   - Go   1.26（与 go.mod / CI ci.yml / release.yml 对齐）
#   - Node 20.x（与 CI / 现代 Vite 要求对齐）
#   - Alpine 3.20

# ===== 阶段 1：前端构建（生产级 SPA）=====
FROM node:20-alpine AS web-builder

WORKDIR /web

# 仅拷贝 package 定义，充分利用 layer 缓存
COPY web-app/package.json web-app/package-lock.json ./
RUN npm ci

# 拷贝源码并构建（产物在 dist/ 目录，将被 Go go:embed 读取）
COPY web-app/ ./
RUN npm run build

# ===== 阶段 2：Go 后端构建 =====
FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_AT=unknown

WORKDIR /build

# 依赖缓存层：先拷贝 go.mod/go.sum 再下载
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码
COPY . .

# 从上一阶段拷贝前端构建产物到预期位置（server.go 中 //go:embed web/dist/*）
COPY --from=web-builder /web/dist ./web/dist

# 静态编译（纯 Go MySQL 驱动 go-sql-driver/mysql，CGO 可安全禁用）
RUN CGO_ENABLED=0 GOOS=linux go build \
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

# 依赖：ca-certificates (HTTPS)、tzdata (时区)、curl (HEALTHCHECK)、libcap (cap_net_bind_service 低端口)
RUN apk add --no-cache ca-certificates tzdata curl libcap && \
    update-ca-certificates && \
    rm -rf /var/cache/apk/*

# 创建非 root 用户（最小权限原则）
RUN addgroup -S geo && adduser -S -G geo geo

WORKDIR /app

# 拷贝二进制并赋予低端口绑定能力（这样 geo serve -p 80 可在非 root 下启动）
COPY --from=builder /out/geo /app/geo
RUN setcap cap_net_bind_service=+ep /app/geo && \
    chown geo:geo /app/geo && \
    chmod 0755 /app/geo

# 切换非 root 用户
USER geo

# 数据持久化：所有模块使用外部 MySQL 8.0+（GEO_*_MYSQL_DSN），本地不存数据文件
# 保留 /data 挂载点以便临时文件/日志
ENV GEO_DATA_DIR=/data/geo
VOLUME /data/geo

# 暴露 HTTP 端口
EXPOSE 8080

# 健康检查：使用 curl（alpine:3.20 默认自带），验证 /api/v1/health 端点返回
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD curl -fsS --max-time 2 http://localhost:8080/api/v1/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/geo"]
CMD ["serve", "-p", "8080"]
