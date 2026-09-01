# GEO 生成式引擎优化系统 - Makefile
# 统一管理构建、测试、运行、容器化等任务

BINARY   := geo
MAIN_PKG := ./cmd/geo
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_AT := $(shell date '+%Y-%m-%d %H:%M:%S')
# ldflags 注入版本信息（main.go 的 version/commit/buildAt/buildOS，与 publish.sh/Dockerfile 一致）
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildAt=$(BUILD_AT) -X main.buildOS=$(shell uname -s)
GOFLAGS  := -trimpath
DOCKER_IMAGE ?= geo:latest

.DEFAULT_GOAL := help

.PHONY: help
help: ## 显示帮助信息
	@awk 'BEGIN {FS = ":.*##"; printf "用法:\n  make \033[36m<target>\033[0m\n\n目标:\n"} \
	/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: web-build
web-build: ## 构建前端 SPA 到 internal/server/web/dist（go:embed 读取）
	@cd web-app && npm ci && npm run build
	@echo "✓ 前端构建完成 → internal/server/web/dist"

.PHONY: build
build: ## 编译二进制到 bin/$(BINARY)（不含前端；如需含前端用 build-full）
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(MAIN_PKG)
	@echo "✓ 已构建 bin/$(BINARY)"

.PHONY: build-full
build-full: web-build ## 一键构建前端 SPA + Go 二进制（产物含嵌入前端）
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(MAIN_PKG)
	@echo "✓ 全量构建完成（前端 + 后端）→ bin/$(BINARY)"

.PHONY: build-all
build-all: web-build ## 交叉编译多平台二进制到 bin/（先构建前端，确保 embed 产物存在）
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
serve: build ## 启动本地 Web 服务（端口 7070，纯 Web 入口，无 CLI 子命令）
	bin/$(BINARY) --port 7070

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
clean: ## 清理构建产物（bin/ dist/ build/）
	rm -rf bin/ dist/ build/
	@echo "✓ 已清理 bin/ dist/ build/"

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像
	docker build -t $(DOCKER_IMAGE) --build-arg VERSION=$(VERSION) .
	@echo "✓ 镜像已构建: $(DOCKER_IMAGE)"

.PHONY: docker-run
docker-run: ## 运行 Docker 容器（端口 7070，环境变量从 .env 读取）
	docker run --rm -p 7070:7070 --env-file .env $(DOCKER_IMAGE)

.PHONY: docker-stop
docker-stop: ## 停止 Docker 容器
	-docker rm -f geo-server 2>/dev/null || true

.PHONY: deploy
deploy: ## 一键部署（执行部署脚本）
	@bash scripts/deploy.sh

.PHONY: install
install: ## 安装到 GOBIN
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PKG)

.PHONY: release
release: build-all ## 打包发布产物到 dist/（含前端构建 → 交叉编译 → tar.gz 归档）
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
