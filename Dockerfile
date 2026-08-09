# Dockerfile - GEO 生成式引擎优化系统
# 多阶段构建，最小化最终镜像体积

# ===== 构建阶段 =====
FROM golang:1.21-alpine AS builder

ARG VERSION=dev

WORKDIR /build

# 利用 Docker 缓存：先拷贝依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码
COPY . .

# 静态编译（CGO 禁用，便于 alpine 运行）
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/geo \
    ./cmd/geo

# ===== 运行阶段 =====
FROM alpine:3.19

LABEL org.opencontainers.image.title="GEO" \
      org.opencontainers.image.description="生成式引擎优化系统" \
      org.opencontainers.image.source="https://github.com/my-geo"

# 安装 ca-certificates（HTTPS 调用必需）+ tzdata（时区）
RUN apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates

# 创建非 root 用户
RUN addgroup -S geo && adduser -S -G geo geo

WORKDIR /app

# 拷贝二进制
COPY --from=builder /out/geo /app/geo

# 切换非 root 用户
USER geo

# 暴露 API 端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["/app/geo"]
CMD ["serve", "-p", "8080"]
