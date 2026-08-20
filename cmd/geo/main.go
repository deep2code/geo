// Package main 是 GEO 系统的入口。
//
// 本程序不再提供任何子命令——所有能力（内容优化、评分、分析、品牌审计、
// 评测、规则集管理、工商库导入、MCP 集成等）均通过内置的 Web 前端界面操作。
// 直接运行即启动 Web 服务（REST API + 前端 SPA），默认监听 :8080。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/mcpserver"
	"my-geo/internal/config"
	"my-geo/internal/server"
	"my-geo/pkg/geo"
)

// 版本信息，通过 ldflags 注入：
//
//	go build -ldflags "-X main.version=v1.2.3 -X main.commit=$(git rev-parse --short HEAD) -X 'main.buildAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
var (
	version = "dev"   // 语义化版本号（release 时由 CI 注入）
	commit  = "none"  // git commit 短哈希
	buildAt = "unknown" // 构建时间（UTC ISO8601）
)

func main() {
	// 先加载 .env（可选，文件不存在时静默跳过），使其中配置（含日志级别）先于后续初始化生效。
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 加载 .env 失败:", err)
	}
	// 解析命令行标志（仅保留 --port / --version 两个运维常用项）。
	portFlag := flag.String("port", "", "Web 服务端口（默认 GEO_PORT 环境变量，缺省 8080）")
	versionFlag := flag.Bool("version", false, "打印版本信息并退出")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("%s (commit %s, built %s)\n", version, commit, buildAt)
		return
	}

	initLogger()
	// 同步构建信息到 server 包（/metrics 的 geo_build_info 与 --version 保持一致）。
	server.SetBuildInfo(version, commit, buildAt)
	// 启动关键配置 fail-fast 校验（弱密钥/缺 DSN 拒绝启动）。
	if err := config.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	// DB 配置层：从 MySQL app_settings 加载管理后台可改的配置覆盖表（DB > 环境变量 > 默认值）。
	// 设置库与账号体系同库（GEO_AUTH_MYSQL_DSN）。失败仅告警不退出——配置回退环境变量/默认值，
	// 系统仍可运行，仅管理后台「系统设置」不可用。
	{
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := config.InitSettings(ctx, os.Getenv("GEO_AUTH_MYSQL_DSN"), true); err != nil {
			slog.Warn("DB 配置层未加载（管理后台系统设置不可用，配置使用环境变量+默认值）", slog.Any("error", err))
		}
		cancel()
	}
	defer config.Close()

	engine := buildEngineFromEnv()

	// MCP Server 与 Web 服务同进程启动（不再有独立子命令）。
	// 配置 GEO_MCP_API_KEY 后远程客户端需携带鉴权；未配置则仅允许本机回环访问。
	if be := server.BuildBrandEngineFromEnv(); be != nil {
		startMCPServer(be, engine)
	} else {
		slog.Warn("品牌引擎未初始化（无可用适配器），MCP Server 不启动；geo_brand_audit 等工具将不可用。请配置各引擎 API Key 环境变量。")
	}

	// Web 服务：所有功能均通过前端界面操作，替代原 CLI 子命令。
	addr := resolveAddr(*portFlag)
	srv := server.New(engine, addr)
	slog.Info("GEO Web 服务已启动", slog.String("addr", "http://localhost"+addr))
	slog.Info("所有操作请通过浏览器前端界面完成（原 CLI 子命令已移除）")
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

// startMCPServer 在独立 goroutine 中启动 MCP Server（默认端口 9090，可用 GEO_MCP_PORT 覆盖）。
func startMCPServer(brandEngine *brand.Engine, geoEngine *geo.Engine) {
	mcpPort := config.Env("GEO_MCP_PORT", "9090")
	mcpAddr := ":" + mcpPort
	apiKey := config.Env("GEO_MCP_API_KEY", "")
	mcpSrv := mcpserver.New(brandEngine, geoEngine, mcpAddr, apiKey)
	go func() {
		slog.Info("MCP Server 已启动（与 Web 服务同进程）",
			slog.String("addr", "http://localhost:"+mcpPort+"/mcp"),
			slog.Bool("auth", apiKey != ""))
		if err := mcpSrv.Start(); err != nil {
			slog.Error("MCP Server 退出", slog.Any("error", err))
		}
	}()
}

// resolveAddr 计算监听地址：优先级 --port > GEO_PORT 环境变量 > 8080。
func resolveAddr(portFlag string) string {
	port := portFlag
	if port == "" {
		port = config.Env("GEO_PORT", "8080")
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	return port
}

// initLogger 初始化 slog 日志处理器。
// 支持环境变量：
//   - GEO_LOG_LEVEL: debug | info | warn | error（默认 info）
//   - GEO_LOG_FORMAT: text | json（默认 text；K8s/Docker 建议用 json）
//   - 自动检测 K8s 环境（KUBERNETES_SERVICE_HOST）时默认 JSON
func initLogger() {
	var level slog.Level = slog.LevelInfo
	if v := strings.TrimSpace(config.Env("GEO_LOG_LEVEL", "")); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			slog.Warn("GEO_LOG_LEVEL 无效，使用默认级别 info", slog.String("value", v), slog.Any("error", err))
		}
	}
	format := strings.TrimSpace(config.Env("GEO_LOG_FORMAT", ""))
	if format == "" {
		if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
			format = "json"
		} else {
			format = "text"
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

// buildEngineFromEnv 从环境变量构建内容优化引擎（与 Web 服务共享的默认引擎）。
func buildEngineFromEnv() *geo.Engine {
	key := config.Env("GEO_LLM_KEY", "")
	base := config.Env("GEO_LLM_BASE", "")
	model := config.Env("GEO_LLM_MODEL", "")
	budget := 0.0
	if v := config.Env("GEO_LLM_BUDGET_USD", ""); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			budget = f
		}
	}
	e := geo.New(geo.WithOpenAI(key, base, model), geo.WithBudgetUSD(budget))
	if rsPath := config.Env("GEO_RULES", ""); rsPath != "" {
		if rs, err := config.LoadRuleSet(rsPath); err == nil {
			e.ApplyRuleSet(rs)
		}
	}
	return e
}
