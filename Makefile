# GEO 生成式引擎优化系统 - Makefile
# 统一管理构建、测试、运行、容器化等任务

BINARY   := geo
MAIN_PKG := ./cmd/geo
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_AT := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS  := -s -w
GOFLAGS  := -trimpath
DOCKER_IMAGE ?= geo:latest

.DEFAULT_GOAL := help

.PHONY: help
help: ## 显示帮助信息
	@awk 'BEGIN {FS = ":.*##"; printf "用法:\n  make \033[36m<target>\033[0m\n\n目标:\n"} \
	/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: build
build: ## 编译二进制到 bin/$(BINARY)
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(MAIN_PKG)
	@echo "✓ 已构建 bin/$(BINARY)"

.PHONY: build-all
build-all: ## 交叉编译多平台二进制到 bin/
	@mkdir -p bin
	@for osarch in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${osarch%/*}; arch=$${osarch#*/}; \
		echo "  构建 $$os/$$arch ..."; \
		GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o bin/$(BINARY)-$$os-$$arch $(MAIN_PKG); \
	done
	@echo "✓ 多平台构建完成 bin/"

.PHONY: run
run: ## 直接运行（go run）
	go run $(MAIN_PKG)

.PHONY: serve
serve: build ## 启动本地 Web 服务（端口 8080，纯 Web 入口，无 CLI 子命令）
	bin/$(BINARY) --port 8080

.PHONY: test
test: ## 运行测试
	go test -v -race -cover ./...

.PHONY: vet
vet: ## 静态检查
	go vet ./...

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -s -w .
	goimports -w -local my-geo . 2>/dev/null || true

.PHONY: tidy
tidy: ## 整理依赖
	go mod tidy

.PHONY: clean
clean: ## 清理构建产物
	rm -rf bin/ dist/
	@echo "✓ 已清理"

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像
	docker build -t $(DOCKER_IMAGE) --build-arg VERSION=$(VERSION) .
	@echo "✓ 镜像已构建: $(DOCKER_IMAGE)"

.PHONY: docker-run
docker-run: ## 运行 Docker 容器（端口 8080）
	docker run --rm -p 8080:8080 --env-file .env $(DOCKER_IMAGE)

.PHONY: docker-up
docker-up: ## 通过 docker-compose 启动
	docker compose up -d
	@echo "✓ 服务已启动: http://localhost:8080"

.PHONY: docker-down
docker-down: ## 停止 docker-compose
	docker compose down

.PHONY: deploy
deploy: ## 一键部署（执行部署脚本）
	@bash scripts/deploy.sh

.PHONY: install
install: ## 安装到 GOBIN
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PKG)

.PHONY: release
release: build-all ## 打包发布产物到 dist/
	@mkdir -p dist
	@for f in bin/$(BINARY)-*; do \
		osarch=$$(basename $$f | sed 's/$(BINARY)-//'); \
		os=$${osarch%-*}; arch=$${osarch#*-}; \
		tar -czf dist/$(BINARY)-$$os-$$arch-$(VERSION).tar.gz -C bin $$(basename $$f); \
	done
	@echo "✓ 发布包已生成于 dist/"

# ===== 安全扫描 =====

.PHONY: security
security: security-go security-web ## 安全扫描：Go 漏洞扫描 + 前端依赖审计
	@echo "✓ 安全扫描完成"

.PHONY: security-go
security-go: ## Go 依赖漏洞扫描（govulncheck）
	@echo "→ Go 依赖漏洞扫描..."
	@which govulncheck > /dev/null 2>&1 || { \
		echo "  govulncheck 未安装，正在安装..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	}
	govulncheck ./...
	@echo "✓ Go 依赖漏洞扫描通过"

.PHONY: security-web
security-web: ## 前端依赖漏洞审计（npm audit）
	@echo "→ 前端依赖漏洞审计..."
	cd web-app && npm audit --omit=dev --audit-level=high || { \
		echo "⚠ 发现高危漏洞，请运行 'cd web-app && npm audit fix' 修复"; \
		exit 1; \
	}
	@echo "✓ 前端依赖漏洞审计通过"

.PHONY: security-fix
security-fix: ## 自动修复前端高危依赖
	cd web-app && npm audit fix --force
	@echo "✓ 前端依赖已尝试自动修复，请重新运行 make security 验证"
