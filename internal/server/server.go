// Package server 提供 GEO 系统的 REST API 服务与 Web 前端界面。
//
// 基于 net/http 标准库，路由设计参考 GEORank 的 REST API：
//
//	GET  /                     Web 前端工作台界面
//	GET  /api/v1/health        健康检查
//	GET  /api/v1/strategies    列出可用策略
//	POST /api/v1/analyze       分析内容信号
//	POST /api/v1/score         评分
//	POST /api/v1/optimize      优化内容
package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/brand"
	"my-geo/internal/brand/chinacheck"
	"my-geo/internal/brand/crawlability"
	"my-geo/internal/brand/discover"
	"my-geo/internal/brand/drift"
	"my-geo/internal/brand/externalsignals"
	"my-geo/internal/brand/history"
	"my-geo/internal/brand/knowledge"
	"my-geo/internal/brand/kol"
	"my-geo/internal/brand/localseo"
	"my-geo/internal/brand/market"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/readiness"
	"my-geo/internal/brand/report"
	"my-geo/internal/brand/scheduler"
	"my-geo/internal/brand/social"
	"my-geo/internal/brand/topsource"
	"my-geo/internal/brand/vertical"
	"my-geo/internal/config"
	"my-geo/internal/llm"
	"my-geo/internal/mail"
	"my-geo/internal/models"
	"my-geo/internal/optimizer/autorewriter"
	"my-geo/internal/util"
	"my-geo/pkg/geo"
)

//go:embed web/dist/*
var webFS embed.FS

// Whitelabel 白标定制配置。
type Whitelabel struct {
	BrandName    string `json:"brand_name"`
	LogoURL      string `json:"logo_url"`
	PrimaryColor string `json:"primary_color"`
	FaviconURL   string `json:"favicon_url"`
	Domain       string `json:"domain"`
}

// Server GEO REST API 服务。
type Server struct {
	engine      *geo.Engine
	brandEngine *brand.Engine
	scheduler   *scheduler.Scheduler
	mailSender  *mail.Sender // SMTP 邮件发送器（未配置时为 nil）
	whitelabel  Whitelabel
	addr        string
	mux         *http.ServeMux
	httpServer  *http.Server // 用于 graceful shutdown
}

// loadWhitelabelFromEnv 从环境变量读取白标定制配置，提供默认值与空值容错。
func loadWhitelabelFromEnv() Whitelabel {
	return Whitelabel{
		BrandName:    config.Env("GEO_WL_BRAND_NAME", "GEO"),
		LogoURL:      config.Env("GEO_WL_LOGO_URL", ""),
		PrimaryColor: config.Env("GEO_WL_PRIMARY_COLOR", "#3B82F6"),
		FaviconURL:   config.Env("GEO_WL_FAVICON_URL", ""),
		Domain:       config.Env("GEO_WL_DOMAIN", ""),
	}
}

// New 创建 API 服务。
//
// brandEngine 为 nil 时仍可正常启动（内容优化功能不受影响），仅品牌审计接口
// 返回 503。此处打印启动警告以便运维快速发现。
func New(engine *geo.Engine, addr string) *Server {
	be := newBrandEngineFromEnv()
	if be == nil {
		slog.Warn("品牌审计引擎未初始化（无可用适配器）；POST /api/v1/brand/audit 将返回 503。请配置各引擎 API Key 环境变量。")
	} else if os.Getenv("GEO_LLM_KEY") == "" {
		slog.Warn("未配置 GEO_LLM_KEY，品牌智能补全（autocomplete）将不可用。")
	}
	// 管理员安全：未配置 GEO_ADMIN_KEY 时所有 /api/admin/* 将默认拒绝，此处统一打一次告警
	if strings.TrimSpace(os.Getenv("GEO_ADMIN_KEY")) == "" {
		slog.Warn("未配置 GEO_ADMIN_KEY，管理员接口（/api/admin/*）默认全部拒绝访问。" +
			"如需启用请：export GEO_ADMIN_KEY=$(openssl rand -hex 16)")
	}
	s := &Server{
		engine:      engine,
		brandEngine: be,
		whitelabel:  loadWhitelabelFromEnv(),
		addr:        addr,
		mux:         http.NewServeMux(),
	}
	// 初始化邮件发送器（未配置 SMTP 时为 nil，邮件接口返回未启用提示）
	if ms, err := mail.NewSender(); err != nil {
		slog.Warn("邮件发送器初始化失败", slog.Any("error", err))
	} else if ms != nil {
		s.mailSender = ms
		slog.Info("SMTP 邮件发送器已启用（告警/周报/PDF 报告邮件可用）。")
	}
	// 初始化定时审计调度器（默认不启动，需通过 API 或配置文件启用）
	if be != nil {
		s.scheduler = newSchedulerFromEnv(be)
		if s.scheduler != nil {
			s.scheduler.Start()
		}
	}
	s.registerRoutes()
	return s
}

// newBrandEngineFromEnv 从环境变量构建品牌可见度引擎。
//
// 各引擎 API Key 通过独立环境变量配置（GEO_GLM_KEY 等），
// 未配置的引擎返回模拟响应，不影响流程运行。
// 若适配器创建全部失败或为 0 个，返回 nil 并打印启动警告。
// China-Check MCP（工商核验）默认启用（免鉴权、免费），可通过
// GEO_CHINACHECK_ENABLED=false 显式关闭，或 GEO_CHINACHECK_URL 指定自定义端点。
// 离线工商 SQLite 库默认启用（~/.local/share/geo/geo_offline_companies.db），即便空库也会打开以便后续写入。
func newBrandEngineFromEnv() *brand.Engine {
	adapters, errs := config.BrandAdaptersFromEnv()
	for eng, e := range errs {
		slog.Warn("LLM 适配器创建失败", slog.String("engine", string(eng)), slog.Any("error", e))
	}
	if len(adapters) == 0 {
		return nil
	}
	// 为每个适配器包装降级缓存（外部 LLM 不可用时返回缓存结果）
	adapters = adapter.WrapWithFallback(adapters)
	slog.Info("LLM 适配器降级缓存已启用", slog.String("ttl", "1h"), slog.Int("max_per_engine", 1000))
	llmMgr := newLLMManagerFromEnv()
	opts := []brand.Option{
		brand.WithAdapters(adapters),
		brand.WithLLM(llmMgr),
	}
	// 注入 China-Check 工商核验客户端（默认启用，可通过环境变量关闭）
	if cc := newChinaCheckFromEnv(); cc != nil {
		opts = append(opts, brand.WithChinaCheck(cc))
		slog.Info("China-Check MCP 工商核验已启用（GSXT/SAMR 官方数据，免鉴权免费）。")
	}
	// 注入离线工商 SQLite 库（默认启用，空库也打开）
	if odb := newOfflineDBFromEnv(); odb != nil {
		opts = append(opts, brand.WithOfflineDB(odb))
		st, err := odb.Stats(context.Background())
		if err != nil {
			slog.Warn("离线工商库打开成功但统计失败", slog.Any("error", err))
		} else {
			slog.Info("离线工商 SQLite 库已启用",
				slog.String("path", st.Path),
				slog.Int64("count", st.Count),
				slog.Int64("size_bytes", st.FileSize),
				slog.String("seed_source", "guichong/- 仓库 json 分支"))
		}
	}
	// 注入审计历史 SQLite 库（默认启用）
	if hdb := newHistoryDBFromEnv(); hdb != nil {
		opts = append(opts, brand.WithHistoryDB(hdb))
		slog.Info("审计历史 SQLite 库已启用", slog.String("path", hdb.Path()))
	}
	return brand.New(opts...)
}

// BuildBrandEngineFromEnv 从环境变量构建品牌可见度引擎（导出版本，供 MCP Server 等复用）。
//
// 内部逻辑与 newBrandEngineFromEnv 完全一致，仅做导出封装，避免在 MCP Server
// 命令中重复实现 ChinaCheck / OfflineDB / LLM / HistoryDB 的环境变量解析逻辑。
func BuildBrandEngineFromEnv() *brand.Engine {
	return newBrandEngineFromEnv()
}

// newHistoryDBFromEnv 打开/创建审计历史 SQLite 库。
//
// 环境变量：
//
//	GEO_HISTORY_DB_ENABLED=true/false   总开关（默认 true）
//	GEO_HISTORY_DB_PATH=/path/to.db     自定义库文件路径
func newHistoryDBFromEnv() history.DB {
	enabled := config.Env("GEO_HISTORY_DB_ENABLED", "true")
	if strings.EqualFold(enabled, "false") || strings.EqualFold(enabled, "0") || strings.EqualFold(enabled, "off") {
		slog.Info("审计历史 SQLite 库已通过 GEO_HISTORY_DB_ENABLED=false 禁用。")
		return nil
	}
	path := config.Env("GEO_HISTORY_DB_PATH", "")
	db, err := history.Open(path)
	if err != nil {
		slog.Warn("审计历史库打开失败（将无历史记录）", slog.Any("error", err))
		return nil
	}
	return db
}

// newSchedulerFromEnv 从环境变量构建定时审计调度器。
//
// 环境变量：
//
//	GEO_SCHEDULER_ENABLED=true/false     总开关（默认 false，需显式开启）
//	GEO_SCHEDULER_WEBHOOK=https://...    全局告警 webhook
//	GEO_SCHEDULER_CONFIG=/path/to.json   定时审计配置文件（JSON 数组）
func newSchedulerFromEnv(be *brand.Engine) *scheduler.Scheduler {
	enabled := config.Env("GEO_SCHEDULER_ENABLED", "false")
	if !(strings.EqualFold(enabled, "true") || strings.EqualFold(enabled, "1") || strings.EqualFold(enabled, "on")) {
		return nil
	}
	webhook := config.Env("GEO_SCHEDULER_WEBHOOK", "")
	configPath := config.Env("GEO_SCHEDULER_CONFIG", "")
	if configPath == "" {
		slog.Warn("定时审计已启用但未配置 GEO_SCHEDULER_CONFIG，调度器为空。")
		return scheduler.New(be, be.HistoryDB(), nil, webhook)
	}
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		slog.Warn("读取调度配置文件失败", slog.String("path", configPath), slog.Any("error", err))
		return nil
	}
	var configs []scheduler.ScheduleConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		slog.Warn("解析调度配置文件失败", slog.String("path", configPath), slog.Any("error", err))
		return nil
	}
	slog.Info("定时审计调度器已加载品牌配置", slog.Int("count", len(configs)))
	return scheduler.New(be, be.HistoryDB(), configs, webhook)
}

// newChinaCheckFromEnv 从环境变量构建 China-Check MCP 客户端（默认启用 + 默认启用本地持久化缓存）。
// 未显式关闭时默认创建并连接官方公共端点，缓存文件位于 ~/.cache/geo/geo_chinacheck_cache.jsonl。
//
// 环境变量：
//
//	GEO_CHINACHECK_ENABLED=true/false        总开关（默认 true）
//	GEO_CHINACHECK_URL=https://...           自定义 MCP endpoint
//	GEO_CHINACHECK_LANG=zh/en/ja/...         enum 字段翻译语言（默认 zh）
//	GEO_CHINACHECK_CACHE_ENABLED=true/false  缓存开关（默认 true）
//	GEO_CHINACHECK_CACHE_PATH=/var/xx.jsonl  自定义缓存文件路径
//	GEO_CHINACHECK_CACHE_MAX_ITEMS=20000     最大缓存条目（默认 10000）
//	GEO_CHINACHECK_CACHE_TTL_HOURS=720       单条目 TTL 小时（默认 720=30 天）
func newChinaCheckFromEnv() *chinacheck.Client {
	enabled := config.Env("GEO_CHINACHECK_ENABLED", "true")
	if strings.EqualFold(enabled, "false") || strings.EqualFold(enabled, "0") || strings.EqualFold(enabled, "off") {
		slog.Info("China-Check MCP 已通过 GEO_CHINACHECK_ENABLED=false 禁用。")
		return nil
	}
	opts := []chinacheck.Option{}
	if url := config.Env("GEO_CHINACHECK_URL", ""); url != "" {
		opts = append(opts, chinacheck.WithURL(url))
	}
	if lang := config.Env("GEO_CHINACHECK_LANG", ""); lang != "" {
		opts = append(opts, chinacheck.WithLanguage(lang))
	}

	// ---------- 缓存层（默认启用）----------
	cacheEnabled := config.Env("GEO_CHINACHECK_CACHE_ENABLED", "true")
	if !(strings.EqualFold(cacheEnabled, "false") || strings.EqualFold(cacheEnabled, "0") || strings.EqualFold(cacheEnabled, "off")) {
		cacheOpts := []chinacheck.CacheOption{}
		if maxStr := config.Env("GEO_CHINACHECK_CACHE_MAX_ITEMS", ""); maxStr != "" {
			if n, err := strconv.Atoi(maxStr); err == nil && n > 0 {
				cacheOpts = append(cacheOpts, chinacheck.WithMaxItems(n))
			}
		}
		if ttlStr := config.Env("GEO_CHINACHECK_CACHE_TTL_HOURS", ""); ttlStr != "" {
			if h, err := strconv.Atoi(ttlStr); err == nil && h > 0 {
				cacheOpts = append(cacheOpts, chinacheck.WithTTL(time.Duration(h)*time.Hour))
			}
		}
		cachePath := config.Env("GEO_CHINACHECK_CACHE_PATH", "")
		ca, err := chinacheck.NewCache(cachePath, cacheOpts...)
		if err != nil {
			slog.Warn("China-Check 缓存初始化失败（将无缓存运行）", slog.Any("error", err))
		} else {
			st := ca.Stats()
			slog.Info("China-Check MCP 本地缓存已启用",
				slog.String("file", st.File),
				slog.Int("count", st.Count),
				slog.Int("max", st.MaxItems),
				slog.Int("ttl_h", int(st.TTLSeconds/3600)),
				slog.Int64("size_bytes", st.FileSizeByte))
			opts = append(opts, chinacheck.WithCache(ca))
		}
	}

	return chinacheck.New(opts...)
}

// newOfflineDBFromEnv 打开/创建离线工商库（多后端，默认 SQLite）。
//
// 环境变量：
//
//	GEO_OFFLINE_DB_ENABLED=true/false   总开关（默认 true）
//	GEO_OFFLINE_DB_PATH=/path/to.db     自定义库文件路径
//	GEO_OFFLINE_DB_TYPE=sqlite/duckdb   后端类型（默认 sqlite）
func newOfflineDBFromEnv() offlinedb.DB {
	enabled := config.Env("GEO_OFFLINE_DB_ENABLED", "true")
	if strings.EqualFold(enabled, "false") || strings.EqualFold(enabled, "0") || strings.EqualFold(enabled, "off") {
		slog.Info("离线工商库已通过 GEO_OFFLINE_DB_ENABLED=false 禁用。")
		return nil
	}
	path := config.Env("GEO_OFFLINE_DB_PATH", "")
	db, err := offlinedb.Open(path)
	if err != nil {
		slog.Warn("离线工商库打开失败（将无离线库运行）", slog.Any("error", err))
		return nil
	}
	return db
}

// humanBytes 兼容别名（已迁移至 util.HumanBytes，保留对外引用以防外部调用）。
// 精度：util.HumanBytes 使用 2 位小数；此前内部未被外部包使用，不构成 API 变更。
func humanBytes(n int64) string {
	return util.HumanBytes(n)
}

// newLLMManagerFromEnv 从环境变量构建 LLM 管理器（用于品牌智能补全）。
//
// 环境变量：GEO_LLM_KEY / GEO_LLM_BASE / GEO_LLM_MODEL
// 未配置 key 时返回仅含 Stub 的管理器（Autocomplete 会返回错误提示）。
func newLLMManagerFromEnv() *llm.Manager {
	key := config.Env("GEO_LLM_KEY", "")
	if key == "" {
		return llm.NewManager(llm.NewStub())
	}
	opts := []llm.OpenAIOption{}
	if base := config.Env("GEO_LLM_BASE", ""); base != "" {
		opts = append(opts, llm.WithBaseURL(base))
	}
	if model := config.Env("GEO_LLM_MODEL", ""); model != "" {
		opts = append(opts, llm.WithModel(model))
	}
	return llm.NewManager(llm.NewOpenAI(key, opts...))
}

// ListenAndServe 启动服务（阻塞，收到 SIGINT/SIGTERM 时优雅退出）。
//
// graceful shutdown：给在途请求最多 30 秒收尾，期间不再接受新连接。
// 调用方只需直接 return 其返回值（无需自行处理信号）。
func (s *Server) ListenAndServe() error {
	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           s.withMiddleware(s.mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second, // 审计可能耗时较长
		IdleTimeout:       120 * time.Second,
	}
	// 监听退出信号
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// goroutine 启动服务
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	// 等待信号或服务异常退出
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭", slog.String("timeout", "30s"))
	}
	// 优雅关闭：给在途请求 30s
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("优雅关闭失败", slog.Any("error", err))
	}
	s.Close()
	return nil
}

// Close 释放服务持有的资源（调度器、数据库连接等）。
// 在 ListenAndServe 退出后由 graceful shutdown 自动调用，也可独立调用。
func (s *Server) Close() {
	if s.scheduler != nil {
		s.scheduler.Stop()
	}
	if s.brandEngine != nil {
		s.brandEngine.Close()
	}
}

// Handler 返回 HTTP Handler（便于测试）。
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

func (s *Server) registerRoutes() {
	// REST API
	s.mux.HandleFunc("/api/v1/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/ready", s.handleReady)
	s.mux.HandleFunc("/api/v1/meta/whitelabel", s.handleWhitelabel)
	s.mux.HandleFunc("/api/v1/strategies", s.handleStrategies)
	s.mux.HandleFunc("/api/v1/analyze", s.handleAnalyze)
	s.mux.HandleFunc("/api/v1/score", s.handleScore)
	s.mux.HandleFunc("/api/v1/optimize", s.handleOptimize)
	s.mux.HandleFunc("/api/v1/brand/audit", s.handleBrandAudit)
	s.mux.HandleFunc("/api/v1/brand/autocomplete", s.handleBrandAutocomplete)
	s.mux.HandleFunc("/api/v1/brand/profile/autocomplete", s.handleBrandProfileAutocomplete)
	s.mux.HandleFunc("/api/v1/brand/knowledge/search", s.handleBrandKnowledgeSearch)
	// 多语言/多市场审计（#8）：返回支持的市场列表
	s.mux.HandleFunc("/api/v1/brand/markets", s.handleBrandMarkets)
	// 品牌可见度报告导出（HTML，可打印为 PDF）
	s.mux.HandleFunc("/api/v1/brand/report/html", s.handleBrandReport)
	s.mux.HandleFunc("/api/v1/brand/report/download", s.handleBrandReport)
	s.mux.HandleFunc("/api/v1/brand/report/pdf", s.handleBrandReportPDF)
	s.mux.HandleFunc("/api/v1/brand/report/email", s.handleBrandReportEmail)
	// 邮件通用接口（测试发送 / 自定义发送）
	s.mux.HandleFunc("/api/v1/mail/send", s.handleMailSend)
	s.mux.HandleFunc("/api/v1/mail/status", s.handleMailStatus)
	// China-Check MCP 工商核验调试接口
	s.mux.HandleFunc("/api/v1/brand/chinacheck/search", s.handleChinaCheckSearch)
	s.mux.HandleFunc("/api/v1/brand/chinacheck/snapshot", s.handleChinaCheckSnapshot)
	s.mux.HandleFunc("/api/v1/brand/chinacheck/cache", s.handleChinaCheckCache)
	// 离线工商 SQLite 库调试接口
	s.mux.HandleFunc("/api/v1/brand/offlinedb/stats", s.handleOfflineDBStats)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/search", s.handleOfflineDBSearch)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/clear", s.handleOfflineDBClear)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/provinces", s.handleOfflineDBProvinces)
	// 审计历史时间序列接口
	s.mux.HandleFunc("/api/v1/brand/history/list", s.handleHistoryList)
	s.mux.HandleFunc("/api/v1/brand/history/get", s.handleHistoryGet)
	s.mux.HandleFunc("/api/v1/brand/history/stats", s.handleHistoryStats)
	s.mux.HandleFunc("/api/v1/brand/history/brands", s.handleHistoryBrands)
	s.mux.HandleFunc("/api/v1/brand/history/clear", s.handleHistoryClear)
	// 定时审计调度器接口
	s.mux.HandleFunc("/api/v1/brand/scheduler/status", s.handleSchedulerStatus)
	s.mux.HandleFunc("/api/v1/brand/scheduler/trigger", s.handleSchedulerTrigger)
	// AI 可见度就绪审计接口
	s.mux.HandleFunc("/api/v1/brand/readiness", s.handleReadinessAudit)
	// AI 可爬取性审计接口（robots.txt/llms.txt/JSON-LD/知识图谱）
	s.mux.HandleFunc("/api/v1/brand/crawlability", s.handleCrawlabilityAudit)
	// diff/drift 回归检测接口（对比两次审计历史，检测各维度漂移）
	s.mux.HandleFunc("/api/v1/brand/drift", s.handleDriftAudit)
	// 社媒情感监控接口（Reddit/微博/YouTube 免鉴权，Twitter/小红书 预留）
	s.mux.HandleFunc("/api/v1/brand/social/monitor", s.handleSocialMonitor)
	// KOL/创作者情报分析接口（从审计结果挖掘被引用最多的媒体/信息源）
	s.mux.HandleFunc("/api/v1/brand/kol/analyze", s.handleKOLAnalyze)
	// Top Source 归因分析接口（识别 LLM 引用的第三方权威域名）
	s.mux.HandleFunc("/api/v1/brand/topsource/analyze", s.handleTopSourceAnalyze)
	// 行业类型自动识别接口
	s.mux.HandleFunc("/api/v1/brand/vertical/detect", s.handleVerticalDetect)
	s.mux.HandleFunc("/api/v1/brand/vertical/list", s.handleVerticalList)
	// Local SEO/GMB 审计接口
	s.mux.HandleFunc("/api/v1/brand/localseo/audit", s.handleLocalSEOAudit)
	// 按量付费第三方数据源接口（DataForSEO / Common Crawl）
	s.mux.HandleFunc("/api/v1/brand/externalsignals/report", s.handleExternalSignals)
	// AutoGEO 规则提取与改写接口
	s.mux.HandleFunc("/api/v1/autorewriter/rules", s.handleAutoRewriteRules)
	s.mux.HandleFunc("/api/v1/autorewriter/rewrite", s.handleAutoRewrite)
	s.mux.HandleFunc("/api/v1/autorewriter/geu", s.handleAutoRewriteGEU)
	// AI 就绪度 CI 闸门接口（扩展 8 维 + 阈值判定）
	s.mux.HandleFunc("/api/v1/brand/readiness/ci-gate", s.handleReadinessCIGate)
	// 关键词发现与自动 GEO 报告
	s.mux.HandleFunc("/api/v1/brand/discover", s.handleBrandDiscover)
	s.mux.HandleFunc("/api/v1/brand/discover/report", s.handleBrandDiscoverReport)
	// 排行榜接口
	s.mux.HandleFunc("/api/v1/leaderboard/categories", s.handleLeaderboardCategories)
	s.mux.HandleFunc("/api/v1/leaderboard/brand/", s.handleLeaderboardBrand)
	s.mux.HandleFunc("/api/v1/leaderboard", s.handleLeaderboard)
	// 竞品对标矩阵接口
	s.mux.HandleFunc("/api/v1/brand/compare", s.handleBrandCompare)
	// 竞品对比报告导出（HTML/JSON）
	s.mux.HandleFunc("/api/v1/brand/compare/export", s.handleBrandCompareExport)
	// CMS 集成接口
	s.mux.HandleFunc("/api/v1/cms/check", s.handleCMSCheck)
	s.mux.HandleFunc("/api/v1/cms/info", s.handleCMSInfo)
	// 安全审计接口
	s.mux.HandleFunc("/api/v1/security/audit", s.handleSecurityAudit)
	// 管理员后台接口（#100）
	s.mux.HandleFunc("/api/v1/admin/tenants", s.handleAdminTenants)
	s.mux.HandleFunc("/api/v1/admin/tenants/", s.handleAdminTenantDetail)
	s.mux.HandleFunc("/api/v1/admin/usage", s.handleAdminUsage)
	s.mux.HandleFunc("/api/v1/admin/announcements", s.handleAdminAnnouncements)
	s.mux.HandleFunc("/api/v1/admin/announcements/", s.handleAdminAnnouncementDelete)
	s.mux.HandleFunc("/api/v1/admin/system", s.handleAdminSystem)
	// 帮助中心与新手引导接口（#101）
	s.mux.HandleFunc("/api/v1/help/articles", s.handleHelpArticles)
	s.mux.HandleFunc("/api/v1/help/articles/", s.handleHelpArticleDetail)
	s.mux.HandleFunc("/api/v1/help/onboarding", s.handleHelpOnboarding)
	s.mux.HandleFunc("/api/v1/help/onboarding/complete", s.handleHelpOnboardingComplete)
	// 工单系统接口（#102）
	s.mux.HandleFunc("/api/v1/tickets", s.handleTickets)
	s.mux.HandleFunc("/api/v1/tickets/", s.handleTicketDetail)
	// 官网/定价接口（#103）
	s.mux.HandleFunc("/api/v1/pricing/plans", s.handlePricingPlans)
	s.mux.HandleFunc("/api/v1/pricing/plans/", s.handlePricingPlanDetail)
	s.mux.HandleFunc("/api/v1/landing/features", s.handleLandingFeatures)
	s.mux.HandleFunc("/api/v1/landing/stats", s.handleLandingStats)
	// ── 法务计划 #80 & #81 合规入口 ──────────────────────────────────
	s.mux.HandleFunc("/legal/bot", s.handleLegalBot)
	s.mux.HandleFunc("/robots.txt", s.handleRobotsTxt)
	s.mux.HandleFunc("/api/v1/legal/data-access", s.handleLegalDataAccess)
	s.mux.HandleFunc("/api/v1/legal/data-export", s.handleLegalDataExport)
	s.mux.HandleFunc("/api/v1/legal/data-delete", s.handleLegalDataDelete)
	s.mux.HandleFunc("/api/v1/meta/compliance", s.handleMetaCompliance)
	// Web SPA 前端（必须放在最后，catch-all 非 API 路径）
	s.mux.HandleFunc("/", s.handleWebSPA)
}

// serveStaticFile 从 embed webFS 读取静态文件并设置正确的 Content-Type。
func serveStaticFile(w http.ResponseWriter, path string) bool {
	subFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return false
	}
	data, err := fs.ReadFile(subFS, path)
	if err != nil {
		return false
	}
	ext := filepath.Ext(path)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		switch strings.ToLower(ext) {
		case ".js":
			ct = "application/javascript; charset=utf-8"
		case ".mjs":
			ct = "application/javascript; charset=utf-8"
		case ".css":
			ct = "text/css; charset=utf-8"
		case ".json":
			ct = "application/json; charset=utf-8"
		case ".svg":
			ct = "image/svg+xml"
		case ".png":
			ct = "image/png"
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".gif":
			ct = "image/gif"
		case ".webp":
			ct = "image/webp"
		case ".ico":
			ct = "image/x-icon"
		case ".woff":
			ct = "font/woff"
		case ".woff2":
			ct = "font/woff2"
		case ".ttf":
			ct = "font/ttf"
		case ".eot":
			ct = "application/vnd.ms-fontobject"
		case ".map":
			ct = "application/json; charset=utf-8"
		default:
			ct = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return true
}

// applyWhitelabelToHTML 对白标 HTML 模板进行占位符替换注入。
func (wl Whitelabel) applyWhitelabelToHTML(html string) string {
	html = strings.ReplaceAll(html, "<!-- WL_INJECT -->", wl.buildInjectBlock())
	html = strings.ReplaceAll(html, "{{WL_BRAND_NAME}}", wl.BrandName)
	html = strings.ReplaceAll(html, "{{WL_PRIMARY_COLOR}}", wl.PrimaryColor)
	html = strings.ReplaceAll(html, "{{WL_LOGO_URL}}", wl.LogoURL)
	html = strings.ReplaceAll(html, "{{WL_DOMAIN}}", wl.Domain)
	return html
}

// buildInjectBlock 构建 <!-- WL_INJECT --> 占位符的实际注入内容（CSS 变量 + Favicon）。
func (wl Whitelabel) buildInjectBlock() string {
	var parts []string
	parts = append(parts, fmt.Sprintf(`<meta name="theme-color" content="%s">`, wl.PrimaryColor))
	if wl.FaviconURL != "" {
		parts = append(parts, fmt.Sprintf(`<link rel="icon" type="image/x-icon" href="%s">`, wl.FaviconURL))
	}
	parts = append(parts, fmt.Sprintf(`<style>
:root {
  --wl-primary-color: %s;
  --wl-brand-name: %q;
}
</style>`, wl.PrimaryColor, wl.BrandName))
	return strings.Join(parts, "\n")
}

// readIndexHTMLData 读取 index.html 原始字节（web/dist/index.html 优先，失败回退 web/index.html）。
func readIndexHTMLData() ([]byte, error) {
	subFS, err := fs.Sub(webFS, "web/dist")
	if err == nil {
		if data, e := fs.ReadFile(subFS, "index.html"); e == nil {
			return data, nil
		}
	}
	return webFS.ReadFile("web/index.html")
}

// serveIndexHTML 返回 SPA 的 index.html 入口文件，并执行白标占位符替换。
func (s *Server) serveIndexHTML(w http.ResponseWriter) bool {
	indexData, err := readIndexHTMLData()
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return false
	}
	html := string(indexData)
	html = s.whitelabel.applyWhitelabelToHTML(html)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, html)
	return true
}

// handleWebSPA Web SPA 前端静态资源服务 + SPA 路由回退。
//
// 路由规则：
//   - /api/* /healthz /readyz 前缀由其他路由处理（此 handler 不处理 API）
//   - 根路径 / 直接返回 web/dist/index.html
//   - 其他路径若在 web/dist/ 下存在静态文件（如 /assets/*），直接返回并带正确 Content-Type
//   - 静态文件不存在且非 API 路径时，回退到 index.html（SPA 前端路由）
//   - 若 web/dist/index.html 也不存在（未执行 npm build），降级返回 web/index.html 作为 dev fallback
func (s *Server) handleWebSPA(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// 排除 API/健康检查路径（双重保险，正常情况下不会进入此 handler）
	if strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/healthz") ||
		strings.HasPrefix(path, "/readyz") {
		http.NotFound(w, r)
		return
	}

	// 根路径：直接返回 index.html
	if path == "/" {
		s.serveIndexHTML(w)
		return
	}

	// 去掉开头的 "/"，构建相对于 web/dist 的路径
	relPath := strings.TrimPrefix(path, "/")

	// 尝试作为静态文件返回
	if serveStaticFile(w, relPath) {
		return
	}

	// 静态文件未命中，SPA 路由回退到 index.html
	s.serveIndexHTML(w)
}

func (s *Server) handleWhitelabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, s.whitelabel)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// liveness：进程存活即可（k8s livenessProbe 用）
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "geo",
		"version": "1.0.0",
	})
}

// handleReady 就绪检查：检查依赖（品牌引擎、历史库、离线库）是否就绪。
// k8s readinessProbe 用，未就绪时返回 503，不接收流量。
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	ready := true
	if s.brandEngine == nil {
		checks["brand_engine"] = "unavailable"
		ready = false
	} else {
		checks["brand_engine"] = "ok"
		if s.brandEngine.HistoryDB() == nil {
			checks["history_db"] = "disabled"
		} else {
			checks["history_db"] = "ok"
		}
		if s.brandEngine.OfflineDB() == nil {
			checks["offline_db"] = "disabled"
		} else {
			checks["offline_db"] = "ok"
		}
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]interface{}{
		"status":  map[bool]string{true: "ready", false: "not_ready"}[ready],
		"checks":  checks,
		"service": "geo",
	})
}

func (s *Server) handleStrategies(w http.ResponseWriter, r *http.Request) {
	infos := s.engine.StrategyInfos()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"strategies": infos,
		"count":      len(infos),
	})
}

type analyzeRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var req analyzeRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content 不能为空"})
		return
	}
	analysis := s.engine.Analyze(req.Content)
	writeJSON(w, http.StatusOK, analysis)
}

func (s *Server) handleScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var req analyzeRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content 不能为空"})
		return
	}
	score, breakdowns := s.engine.Score(req.Content)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"score":      score,
		"breakdowns": breakdowns,
		"grade":      util.ScoreToGrade(score),
	})
}

const geoVersion = "1.0.0"

type cmsCheckRequest struct {
	HTML   string `json:"html"`
	URL    string `json:"url,omitempty"`
	Title  string `json:"title,omitempty"`
	Domain string `json:"domain,omitempty"`
}

type cmsSuggestion struct {
	Category string `json:"category"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

func cmsStripHTMLTags(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			b.WriteRune(' ')
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	decoded := strings.ReplaceAll(b.String(), "&nbsp;", " ")
	decoded = strings.ReplaceAll(decoded, "&amp;", "&")
	decoded = strings.ReplaceAll(decoded, "&lt;", "<")
	decoded = strings.ReplaceAll(decoded, "&gt;", ">")
	decoded = strings.ReplaceAll(decoded, "&quot;", "\"")
	decoded = strings.ReplaceAll(decoded, "&#39;", "'")
	return decoded
}

func cmsGenerateSuggestions(analysis *models.ContentAnalysis, score float64) []cmsSuggestion {
	var suggestions []cmsSuggestion
	if score < 60 {
		suggestions = append(suggestions, cmsSuggestion{
			Category: "overall", Priority: "high",
			Message: "整体 GEO 评分偏低，建议系统应用优化策略以提升 AI 引用可见度。",
		})
	}
	for sig, ok := range analysis.CitabilitySignals {
		if !ok {
			var msg string
			switch sig {
			case "statistics":
				msg = "缺少统计数据与研究数字，建议补充具体百分比、样本量等数据点。"
			case "cite_sources":
				msg = "缺少来源引用标注，建议为关键事实添加 [1] 式脚注或\"来源：\"说明。"
			case "quotation":
				msg = "缺少专家/权威引用语，建议引入行业专家观点或报告原文引述。"
			case "technical_terms":
				msg = "缺少行业专有术语/缩写，建议适度使用以提升主题相关性。"
			case "fluency":
				msg = "句子流畅度欠佳，建议控制句长在 15-60 字/句区间，避免超长或碎片化。"
			case "authoritative":
				msg = "语气权威性不足，建议使用\"研究表明\"、\"专家指出\"等确定性措辞。"
			case "unique_words":
				msg = "词汇多样性偏低，建议避免重复用词，引入同义词与更丰富的表达。"
			default:
				continue
			}
			priority := "medium"
			if sig == "cite_sources" || sig == "statistics" || sig == "quotation" {
				priority = "high"
			}
			suggestions = append(suggestions, cmsSuggestion{
				Category: "citability", Priority: priority, Message: msg,
			})
		}
	}
	for sig, ok := range analysis.StructureSignals {
		if !ok {
			var msg string
			switch sig {
			case "heading_hierarchy":
				msg = "缺少标题层级结构，建议使用 H1-H6 标题组织内容层次。"
			case "lists":
				msg = "缺少列表结构，建议将要点用有序/无序列表呈现以便快速扫描。"
			case "tables":
				msg = "可考虑添加对比表格呈现关键数据差异，提升易引用性。"
			case "front_loading":
				msg = "未采用结论前置，建议首段直接给出核心结论与摘要。"
			case "definition_openings":
				msg = "可采用\"X是指…\"的定义式开头，强化主题相关性。"
			case "faq":
				msg = "建议添加 FAQ 问答模块，覆盖长尾问句提升语义匹配。"
			default:
				continue
			}
			priority := "medium"
			if sig == "heading_hierarchy" || sig == "front_loading" {
				priority = "high"
			}
			suggestions = append(suggestions, cmsSuggestion{
				Category: "structure", Priority: priority, Message: msg,
			})
		}
	}
	for _, neg := range analysis.NegativeSignals {
		var msg string
		switch neg {
		case "thin_content":
			msg = "内容过于单薄（<100 词），建议扩充至 300-2000 词的深度内容。"
		case "cta_overload":
			msg = "CTA（行动号召）过载，可能被判定为营销内容，建议减少至 2 条以内。"
		case "keyword_stuffing":
			msg = "检测到关键词堆砌痕迹，请自然融入关键词避免重复。"
		case "excessive_links":
			msg = "链接数量过多（>10），建议精简外链数量避免 spam 信号。"
		case "no_structure":
			msg = "长内容缺少标题/列表结构，建议分段并使用小标题组织。"
		default:
			continue
		}
		suggestions = append(suggestions, cmsSuggestion{
			Category: "negative", Priority: "high", Message: msg,
		})
	}
	return suggestions
}

func (s *Server) handleCMSCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var req cmsCheckRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.HTML) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "html 不能为空"})
		return
	}
	plain := cmsStripHTMLTags(req.HTML)
	if req.Title != "" {
		plain = req.Title + "\n\n" + plain
	}
	analysis := s.engine.Analyze(plain)
	analysis.URL = req.URL
	score, breakdowns := s.engine.Score(plain)
	grade := util.ScoreToGrade(score)
	suggestions := cmsGenerateSuggestions(analysis, score)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"signals": map[string]interface{}{
			"citability_signals": analysis.CitabilitySignals,
			"structure_signals":  analysis.StructureSignals,
			"negative_signals":   analysis.NegativeSignals,
			"evergreen_score":    analysis.EvergreenScore,
			"word_count":         analysis.WordCount,
		},
		"score":       score,
		"grade":       grade,
		"breakdowns":  breakdowns,
		"suggestions": suggestions,
		"ok":          score >= 60,
	})
}

func (s *Server) handleCMSInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":    geoVersion,
		"api_prefix": "/api/v1",
		"endpoints": map[string]string{
			"check": "/api/v1/cms/check",
			"info":  "/api/v1/cms/info",
		},
		"whitelabel": s.whitelabel,
	})
}

// handleSecurityAudit 安全审计接口，返回当前安全中间件的配置与状态。
//
// GET /api/v1/security/audit
// 用于运维快速确认限流、WAF、CSRF、安全头等防护是否生效。
func (s *Server) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rate_limit": map[string]interface{}{
			"global_per_sec":    rlConfig.globalPerSec,
			"expensive_per_sec": rlConfig.expensivePerSec,
			"expensive_paths":   expensivePathPatterns,
		},
		"waf": map[string]interface{}{
			"max_body_bytes":     defaultMaxBodyBytes,
			"max_body_expensive": 20 * 1024 * 1024,
			"checks":             []string{"sqli", "xss", "path_traversal", "null_byte"},
			"security_headers":   []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "X-XSS-Protection", "Permissions-Policy", "Content-Security-Policy"},
		},
		"csrf": map[string]interface{}{
			"enabled":       corsOrigins != nil,
			"write_methods": []string{"POST", "PUT", "PATCH", "DELETE"},
		},
		"auth": map[string]interface{}{
			"api_key_enabled": apiKey != "",
		},
		"recovery": map[string]interface{}{
			"panic_recovery": true,
		},
		"fallback_cache": map[string]interface{}{
			"enabled":  s.brandEngine != nil,
			"ttl":      "1h",
			"max_size": 1000,
		},
	})
}

func (s *Server) handleOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var req models.OptimizationRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content 不能为空"})
		return
	}
	resp, err := s.engine.Optimize(r.Context(), &req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func readJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 限制 10MB
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("请求体为空")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}
	return nil
}

// scoreToGrade 兼容别名（已迁移至 util.ScoreToGrade）。
func scoreToGrade(score float64) string {
	return util.ScoreToGrade(score)
}

// handleBrandAudit 处理品牌可见度审计请求。
//
// POST /api/v1/brand/audit
// 请求体为品牌画像 JSON（brand.BrandProfile）。
func (s *Server) handleBrandAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var profile brand.BrandProfile
	if err := readJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if profile.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 不能为空"})
		return
	}
	if len(profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompts 不能为空"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "品牌审计引擎未初始化"})
		return
	}
	report, err := s.brandEngine.Audit(r.Context(), profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleBrandMarkets 返回多语言/多市场审计支持的市场列表。
//
// GET /api/v1/brand/markets
//
// 返回 market.SupportedMarkets()，前端据此渲染"目标市场/查询语言"下拉框。
// 该接口不依赖品牌引擎，即便未配置任何 AI 引擎也能正常返回。
func (s *Server) handleBrandMarkets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"markets": market.SupportedMarkets(),
		"count":   len(market.SupportedMarkets()),
	})
}

// handleBrandReport 导出品牌可见度审计报告为自包含 HTML（可打印为 PDF）。
//
// GET /api/v1/brand/report/html?brand=xxx       在浏览器中打开 HTML 报告
// GET /api/v1/brand/report/download?brand=xxx   以附件形式下载 HTML 文件
//
// 从审计历史 DB 取最新一条审计记录的 report_json，调用 report.GenerateHTML 生成。
func (s *Server) handleBrandReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用（无法导出报告）"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "未找到该品牌的审计记录（请先执行一次品牌审计）",
		})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "解析审计报告失败: " + err.Error()})
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 HTML 报告失败: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// download 端点设置附件头，触发浏览器下载
	if strings.HasSuffix(r.URL.Path, "/download") {
		filename := sanitizeFilename(brandName) + "_可见度报告.html"
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, htmlOut)
}

// handleBrandReportPDF 导出品牌可见度审计报告为 PDF。
//
// GET /api/v1/brand/report/pdf?brand=xxx
//
// 服务端使用 headless Chromium（chromedp）渲染 HTML 报告为 A4 PDF。
// 无 Chromium 环境时自动降级：返回 JSON 错误提示并附 HTML 报告下载链接。
func (s *Server) handleBrandReportPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用（无法导出报告）"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该品牌的审计记录"})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "解析审计报告失败: " + err.Error()})
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 HTML 报告失败: " + err.Error()})
		return
	}
	pdfBytes, err := report.GeneratePDF(r.Context(), htmlOut)
	if err != nil {
		// 降级：返回错误 + HTML 报告备用链接
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "PDF 渲染失败，建议使用 HTML 报告后在浏览器打印为 PDF：" + err.Error(),
			"html":  "/api/v1/brand/report/download?brand=" + url.QueryEscape(brandName),
		})
		return
	}
	filename := sanitizeFilename(brandName) + "_可见度报告.pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// handleBrandReportEmail 把品牌可见度审计报告（HTML + PDF 附件）发送邮件。
//
// POST /api/v1/brand/report/email
//
// JSON {"brand":"腾讯","to":["ops@x.com"],"cc":[],"format":"both"}
// format: pdf / html / both（默认 both）
func (s *Server) handleBrandReportEmail(w http.ResponseWriter, r *http.Request) {
	if s.mailSender == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "邮件未启用（请配置 GEO_SMTP_* 环境变量）"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		Brand  string   `json:"brand"`
		To     []string `json:"to"`
		Cc     []string `json:"cc"`
		Format string   `json:"format"` // pdf/html/both
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Brand) == "" || len(body.To) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand 与 to 必填"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.Brand)
	if err != nil || rec == nil || rec.ReportJSON == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该品牌的审计记录"})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "解析报告失败: " + err.Error()})
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成报告失败: " + err.Error()})
		return
	}
	format := body.Format
	if format == "" {
		format = "both"
	}
	msg := &mail.Message{
		To:       body.To,
		Cc:       body.Cc,
		Subject:  fmt.Sprintf("GEO 品牌可见度报告 · %s（BVS %.1f %s）", body.Brand, vr.Score, vr.Grade),
		HTMLBody: htmlOut,
	}
	if format == "both" || format == "pdf" {
		if pdf, err := report.GeneratePDF(r.Context(), htmlOut); err == nil {
			msg.Attachments = append(msg.Attachments, mail.Attachment{
				Filename: sanitizeFilename(body.Brand) + "_可见度报告.pdf",
				Content:  pdf,
			})
		}
		// PDF 失败不阻塞，继续发送 HTML 版
	}
	if err := s.mailSender.Send(msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "发送邮件失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"to":      body.To,
		"format":  format,
		"subject": msg.Subject,
	})
}

// handleMailStatus 返回邮件发送器启用状态。
// GET /api/v1/mail/status
func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.mailSender != nil && s.mailSender.Enabled(),
		"host":    ternary(s.mailSender != nil, s.mailSender.Host, ""),
		"port":    ternary(s.mailSender != nil, s.mailSender.Port, 0),
		"from":    ternary(s.mailSender != nil, s.mailSender.From, ""),
	})
}

// ternary 三目运算符（避免 map 取值啰嗦）。
func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// handleMailSend 通用邮件发送接口（含测试/周报模板）。
//
// POST /api/v1/mail/send
// JSON:
//
//	{
//	  "to": ["a@x.com"], "subject": "...", "text": "...", "html": "...",
//	  "template": "alert|weekly",
//	  "template_data": {...}
//	}
//
// template_data 对应 mail.TemplateAlertData / mail.TemplateWeeklyData。
func (s *Server) handleMailSend(w http.ResponseWriter, r *http.Request) {
	if s.mailSender == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "邮件未启用（请配置 GEO_SMTP_* 环境变量）"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		To           []string          `json:"to"`
		Cc           []string          `json:"cc"`
		Subject      string            `json:"subject"`
		Text         string            `json:"text"`
		HTML         string            `json:"html"`
		Template     string            `json:"template"` // alert / weekly
		TemplateData map[string]any    `json:"template_data"`
		Attachments  []mail.Attachment `json:"attachments"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.To) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to 必填"})
		return
	}
	msg := &mail.Message{
		To:          body.To,
		Cc:          body.Cc,
		Subject:     body.Subject,
		TextBody:    body.Text,
		HTMLBody:    body.HTML,
		Attachments: body.Attachments,
	}
	// 用模板渲染 HTML
	if body.Template != "" && len(body.TemplateData) > 0 {
		raw, _ := json.Marshal(body.TemplateData)
		switch body.Template {
		case "alert":
			var d mail.TemplateAlertData
			_ = json.Unmarshal(raw, &d)
			if d.Subject == "" {
				d.Subject = body.Subject
			}
			if d.ConsoleURL == "" {
				d.ConsoleURL = "http://localhost:" + strings.TrimPrefix(s.addr, ":")
			}
			h, err := mail.RenderAlertHTML(d)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alert 模板渲染失败: " + err.Error()})
				return
			}
			msg.HTMLBody = h
			if msg.Subject == "" {
				msg.Subject = d.Subject
			}
		case "weekly":
			var d mail.TemplateWeeklyData
			_ = json.Unmarshal(raw, &d)
			if d.Subject == "" {
				d.Subject = body.Subject
			}
			if d.ConsoleURL == "" {
				d.ConsoleURL = "http://localhost:" + strings.TrimPrefix(s.addr, ":")
			}
			h, err := mail.RenderWeeklyHTML(d)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "weekly 模板渲染失败: " + err.Error()})
				return
			}
			msg.HTMLBody = h
			if msg.Subject == "" {
				msg.Subject = d.Subject
			}
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未知 template: " + body.Template})
			return
		}
	}
	if msg.Subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subject 或 template_data.subject 必填"})
		return
	}
	if err := s.mailSender.Send(msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "发送失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": body.To, "subject": msg.Subject})
}

// sanitizeFilename 把品牌名转换为安全的文件名片段（去掉路径分隔符等危险字符）。
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	out := repl.Replace(s)
	if out == "" {
		return "brand"
	}
	return out
}

// handleBrandAutocomplete 处理品牌智能补全请求。
//
// POST /api/v1/brand/autocomplete
// 请求体: {"brand_name": "品牌名"}
// 返回: 品牌候选画像（domain/aliases/category/products/competitors/prompts/summary）
func (s *Server) handleBrandAutocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var req brand.AutocompleteRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.BrandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand_name 不能为空"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "品牌审计引擎未初始化"})
		return
	}
	candidate, err := s.brandEngine.Autocomplete(r.Context(), req.BrandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, candidate)
}

// handleBrandProfileAutocomplete 处理品牌画像自动补全请求（GET 版本，返回完整 BrandProfile）。
//
// GET /api/v1/brand/profile/autocomplete?name=品牌名
//
// 与 POST /api/v1/brand/autocomplete 不同的是：
//   - 使用 GET 方法，便于浏览器直接调用与缓存
//   - 返回的是完整 brand.BrandProfile（而非 AutocompleteCandidate），可直接用于后续审计接口
//
// 内部调用 brandEngine.Autocomplete，将 AutocompleteCandidate 转换为 BrandProfile 返回。
func (s *Server) handleBrandProfileAutocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "品牌审计引擎未初始化"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("name"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 不能为空"})
		return
	}
	candidate, err := s.brandEngine.Autocomplete(r.Context(), brandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 将 AutocompleteCandidate 转换为 BrandProfile
	profile := brand.BrandProfile{
		Name:        candidate.Name,
		Aliases:     candidate.Aliases,
		Domain:      candidate.Domain,
		Products:    candidate.Products,
		Company:     candidate.Company,
		Competitors: candidate.Competitors,
		Prompts:     candidate.Prompts,
		Industry:    candidate.Industry,
		Category:    candidate.Category,
	}
	if profile.Name == "" {
		profile.Name = brandName
	}
	if len(profile.Prompts) == 0 {
		// 兜底 prompts，避免返回的 BrandProfile 无法直接用于审计
		profile.Prompts = []string{
			fmt.Sprintf("最好的%s", firstNotEmpty(profile.Category, brandName)),
			fmt.Sprintf("%s推荐", brandName),
		}
	}
	writeJSON(w, http.StatusOK, profile)
}

// handleBrandKnowledgeSearch 搜索本地品牌知识库（SinoFacts CC BY 4.0）。
//
// GET  /api/v1/brand/knowledge/search?q=<query>&limit=5
// POST /api/v1/brand/knowledge/search JSON { "q": "...", "limit": 5 }
//
// 返回来自 383 家中国出海软件公司的离线匹配结果，零延迟。
func (s *Server) handleBrandKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":  "品牌审计引擎未初始化",
			"total":  0,
			"result": []struct{}{},
		})
		return
	}
	kb := s.brandEngine.Knowledge()
	if kb == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"error":  "知识库未加载",
			"total":  0,
			"result": []struct{}{},
		})
		return
	}
	var (
		q     string
		limit = 5
	)
	if r.Method == http.MethodGet {
		q = r.URL.Query().Get("q")
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Q     string `json:"q"`
			Limit int    `json:"limit"`
		}
		if err := readJSON(r, &body); err == nil {
			q = body.Q
			if body.Limit > 0 {
				limit = body.Limit
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	results := kb.Search(q, limit)
	// 转为对前端友好的扁平对象（去掉 brand.AutocompleteCandidate 中 Prompts/Competitors 减少传输量）
	type item struct {
		BrandName     string   `json:"brand_name"`
		BrandDomain   string   `json:"brand_domain,omitempty"`
		BrandAliases  []string `json:"brand_aliases,omitempty"`
		Industry      string   `json:"industry,omitempty"`
		Category      string   `json:"category,omitempty"`
		Products      []string `json:"products,omitempty"`
		CompanyName   string   `json:"company_name,omitempty"`
		CompanyDomain string   `json:"company_domain,omitempty"`
		HQ            string   `json:"hq,omitempty"`
		FoundedYear   int      `json:"founded_year,omitempty"`
		Desc          string   `json:"description,omitempty"`
		Source        string   `json:"source"`       // sinofacts | offlinedb
		SourceLabel   string   `json:"source_label"` // 前端显示的 badge 文案
		Score         float64  `json:"score"`        // 0-100
		// --- offlinedb 专属字段 ---
		CreditCode     string `json:"credit_code,omitempty"`
		LegalPerson    string `json:"legal_person,omitempty"`
		RegisteredDate string `json:"registered_date,omitempty"`
		Capital        string `json:"capital,omitempty"`
		Province       string `json:"province,omitempty"`
		City           string `json:"city,omitempty"`
		Address        string `json:"address,omitempty"`
		CompanyType    string `json:"company_type,omitempty"`
		BusinessScope  string `json:"business_scope,omitempty"`
	}
	out := make([]item, 0, limit*2)
	for _, r := range results {
		out = append(out, item{
			BrandName:     r.Entry.BrandName,
			BrandDomain:   r.Entry.BrandDomain,
			BrandAliases:  r.Entry.BrandAliases,
			Industry:      r.Entry.Industry,
			Category:      r.Entry.Category,
			Products:      r.Entry.Products,
			CompanyName:   r.Entry.CompanyName,
			CompanyDomain: r.Entry.CompanyDomain,
			HQ:            r.Entry.Headquarters,
			FoundedYear:   r.Entry.FoundedYear,
			Desc:          r.Entry.DescriptionZh,
			Source:        "sinofacts",
			SourceLabel:   "📚 品牌知识库（SinoFacts CC BY 4.0）",
			Score:         r.Score,
		})
	}
	// 追加：离线工商 SQLite 库匹配（用剩余配额）
	odbQuota := limit
	if odb := s.brandEngine.OfflineDB(); odb != nil && odbQuota > 0 {
		odbRes, err := odb.Search(r.Context(), offlinedb.SearchOptions{Query: q, TopN: odbQuota})
		if err == nil {
			for _, c := range odbRes {
				desc := c.BusinessScope
				if len(desc) > 120 {
					desc = desc[:120] + "..."
				}
				out = append(out, item{
					BrandName:   c.Name,
					Industry:    c.Province,
					CompanyName: c.Name,
					HQ:          c.City,
					FoundedYear: func() int {
						y := 0
						if len(c.RegistrationDay) >= 4 {
							y, _ = strconv.Atoi(c.RegistrationDay[:4])
						}
						return y
					}(),
					Desc:           desc,
					Source:         "offlinedb",
					SourceLabel:    "💾 离线工商库（1978-2019，guichong/- 种子数据）",
					Score:          c.Score,
					CreditCode:     c.Code,
					LegalPerson:    c.LegalRepresentative,
					RegisteredDate: c.RegistrationDay,
					Capital:        c.Capital,
					Province:       c.Province,
					City:           c.City,
					Address:        c.Address,
					CompanyType:    c.Character,
					BusinessScope:  c.BusinessScope,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":           kb.N,
		"query":           q,
		"result":          out,
		"sinofacts_count": len(results),
		"offlinedb_count": maxInt(0, len(out)-len(results)),
		"license":         "SinoFacts dataset under CC BY 4.0 (https://sinofacts.com); 离线工商数据源自 guichong/- 仓库（国家工商公示系统 1978-2019 公开历史数据）。",
	})
}

// ---------- China-Check MCP 工商核验调试接口 ----------

// handleChinaCheckSearch 搜索工商注册公司（China-Check MCP 调试接口）。
//
// GET  /api/v1/brand/chinacheck/search?q=<query>&limit=5
// POST /api/v1/brand/chinacheck/search JSON { "q": "...", "limit": 5 }
//
// 返回来自国家企业信用信息公示系统（GSXT/SAMR）的公司匹配列表。
func (s *Server) handleChinaCheckSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "品牌审计引擎未初始化",
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	var (
		q     string
		limit = 5
	)
	if r.Method == http.MethodGet {
		q = r.URL.Query().Get("q")
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Q     string `json:"q"`
			Limit int    `json:"limit"`
		}
		if err := readJSON(r, &body); err == nil {
			q = body.Q
			if body.Limit > 0 {
				limit = body.Limit
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q 不能为空"})
		return
	}
	result, err := cc.Search(r.Context(), q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":     fmt.Sprintf("China-Check 搜索失败: %v", err),
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":      q,
		"total":      result.Total,
		"companies":  result.Companies,
		"source":     "国家企业信用信息公示系统（GSXT / SAMR） via China-Check MCP",
		"disclaimer": "本接口返回的数据来自国家企业信用信息公示系统公开信息，仅供参考，请以官方系统最新登记为准。",
	})
}

// handleChinaCheckSnapshot 获取单家公司的工商注册快照（China-Check MCP 调试接口）。
//
// GET  /api/v1/brand/chinacheck/snapshot?company_id=<ID>&q=<名称>
// POST /api/v1/brand/chinacheck/snapshot JSON { "company_id": "...", "q": "..." }
//
// company_id 和 q 至少传一个；同时传时优先 company_id（更精准）。
func (s *Server) handleChinaCheckSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "品牌审计引擎未初始化",
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
		})
		return
	}
	var (
		companyID string
		query     string
	)
	if r.Method == http.MethodGet {
		companyID = r.URL.Query().Get("company_id")
		query = r.URL.Query().Get("q")
	} else if r.Method == http.MethodPost {
		var body struct {
			CompanyID string `json:"company_id"`
			Q         string `json:"q"`
		}
		if err := readJSON(r, &body); err == nil {
			companyID = body.CompanyID
			query = body.Q
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if companyID == "" && strings.TrimSpace(query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "company_id 和 q 至少提供一个"})
		return
	}
	snap, err := cc.GetSnapshot(r.Context(), companyID, query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("China-Check snapshot 失败: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": snap.CompanyID,
		"snapshot":   snap.Snapshot,
		"disclaimer": firstNotEmpty(snap.Disclaimer, "本接口返回的数据来自国家企业信用信息公示系统公开信息，仅供参考，请以官方系统最新登记为准。"),
		"source":     "国家企业信用信息公示系统（GSXT / SAMR） via China-Check MCP",
	})
}

// firstNotEmpty 返回第一个非空字符串（server 内部用，与 brand 包同名函数独立）。
func firstNotEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

// maxInt server 内部辅助。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- 离线工商 SQLite 库调试接口 ----------

// handleOfflineDBStats  GET /api/v1/brand/offlinedb/stats
func (s *Server) handleOfflineDBStats(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "品牌审计引擎未初始化"})
		return
	}
	odb := s.brandEngine.OfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用（GEO_OFFLINE_DB_ENABLED=true）"})
		return
	}
	st, err := odb.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleOfflineDBSearch GET ?q=腾讯&n=10&province=广东  POST JSON {q,n,province,city}
func (s *Server) handleOfflineDBSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "品牌审计引擎未初始化", "result": []struct{}{}})
		return
	}
	odb := s.brandEngine.OfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用", "result": []struct{}{}})
		return
	}
	opt := offlinedb.SearchOptions{TopN: 10}
	if r.Method == http.MethodGet {
		opt.Query = r.URL.Query().Get("q")
		opt.Province = r.URL.Query().Get("province")
		opt.City = r.URL.Query().Get("city")
		if n := r.URL.Query().Get("n"); n != "" {
			if v, err := strconv.Atoi(n); err == nil && v > 0 {
				opt.TopN = v
			}
		}
	} else if r.Method == http.MethodPost {
		var in offlinedb.SearchOptions
		if err := readJSON(r, &in); err == nil {
			opt = in
			if opt.TopN <= 0 {
				opt.TopN = 10
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET/POST"})
		return
	}
	start := time.Now()
	res, err := odb.Search(r.Context(), opt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error(), "result": []struct{}{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":    opt.Query,
		"province": opt.Province,
		"city":     opt.City,
		"count":    len(res),
		"took_ms":  time.Since(start).Milliseconds(),
		"result":   res,
		"source":   "guichong/- JSON 分支（国家工商公示系统 1978-2019 公开历史数据）→ SQLite + FTS5",
	})
}

// handleOfflineDBClear POST /api/v1/brand/offlinedb/clear 清空库（VACUUM 回收空间）
func (s *Server) handleOfflineDBClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅 POST"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.OfflineDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用"})
		return
	}
	before, _ := s.brandEngine.OfflineDB().Stats(r.Context())
	if err := s.brandEngine.OfflineDB().Clear(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	after, _ := s.brandEngine.OfflineDB().Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"before_count": before.Count,
		"after_count":  after.Count,
		"before_size":  before.FileSize,
		"after_size":   after.FileSize,
	})
}

// handleOfflineDBProvinces GET /api/v1/brand/offlinedb/provinces 返回数据库内所有省份（下拉框用）
func (s *Server) handleOfflineDBProvinces(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.OfflineDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用"})
		return
	}
	list, err := s.brandEngine.OfflineDB().Provinces(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"provinces": list})
}

// ---------- handleChinaCheckCache：缓存管理接口 ----------
//
// GET  /api/v1/brand/chinacheck/cache?action=stats               查看缓存统计
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "clear" } 清空缓存
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "compact" } 压缩/去重缓存文件
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "import", "queries": ["腾讯","阿里","字节跳动"] }
//
// import 动作：按列表依次执行 Search+Snapshot 预热缓存（可指定 limit/并发度）。
func (s *Server) handleChinaCheckCache(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "品牌审计引擎未初始化",
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
		})
		return
	}
	ca := cc.Cache()
	if ca == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check 缓存未启用（设 GEO_CHINACHECK_CACHE_ENABLED=true 以启用）",
		})
		return
	}

	// 解析 action
	action := ""
	if r.Method == http.MethodGet {
		action = strings.ToLower(r.URL.Query().Get("action"))
		if action == "" {
			action = "stats"
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Action  string   `json:"action"`
			Queries []string `json:"queries,omitempty"`
			Limit   int      `json:"limit,omitempty"`
		}
		if err := readJSON(r, &body); err == nil {
			action = strings.ToLower(body.Action)
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}

	switch action {
	case "stats":
		writeJSON(w, http.StatusOK, ca.Stats())
	case "clear":
		if err := ca.Clear(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "缓存已清空",
			"stats":   ca.Stats(),
		})
	case "compact":
		if err := ca.Compact(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "缓存已压缩/去重",
			"stats":   ca.Stats(),
		})
	case "import":
		// 预热：读取请求中的 queries 列表
		var body struct {
			Queries []string `json:"queries"`
			Limit   int      `json:"limit"`
		}
		body.Limit = 3
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "解析失败: " + err.Error()})
			return
		}
		if len(body.Queries) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "queries 不能为空"})
			return
		}
		ctx := r.Context()
		done := 0
		errors := map[string]string{}
		for _, q := range body.Queries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			sr, err := cc.Search(ctx, q, body.Limit)
			if err != nil {
				errors[q] = err.Error()
				continue
			}
			// 只对 Top1 拉 snapshot（最常用命中）
			if len(sr.Companies) > 0 {
				best := sr.Companies[0]
				if _, err := cc.GetSnapshot(ctx, best.CompanyID, ""); err != nil {
					errors[q+"/snapshot"] = err.Error()
				}
			}
			done++
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":          true,
			"imported":    done,
			"total":       len(body.Queries),
			"errors":      errors,
			"stats_after": ca.Stats(),
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "未知 action，支持: stats / clear / compact / import",
		})
	}
}

// --- 审计历史时间序列接口 ---

// handleHistoryList 查询指定品牌的审计历史（按时间降序）。
// GET/POST /api/v1/brand/history/list?brand=腾讯&limit=50
func (s *Server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	brandName := r.URL.Query().Get("brand")
	if brandName == "" {
		var body struct {
			Brand string `json:"brand"`
		}
		if r.Method == http.MethodPost {
			_ = readJSON(r, &body)
			brandName = body.Brand
		}
	}
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brand 参数"})
		return
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	records, err := s.brandEngine.HistoryDB().List(r.Context(), brandName, limit+offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 应用 offset（history.List 不支持 offset，取 limit+offset 条后切片）
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	if offset > len(records) {
		offset = len(records)
	}
	paged := records[offset:end]
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"brand":   brandName,
		"count":   len(paged),
		"total":   len(records),
		"offset":  offset,
		"limit":   limit,
		"records": paged,
	})
}

// handleHistoryGet 查询单条审计记录的完整信息（含 report_json）。
// GET /api/v1/brand/history/get?id=123
func (s *Server) handleHistoryGet(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 id 参数"})
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "记录不存在"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleHistoryStats 返回历史库统计信息。
// GET /api/v1/brand/history/stats
func (s *Server) handleHistoryStats(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	st, err := s.brandEngine.HistoryDB().Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleHistoryBrands 列出所有有审计记录的品牌。
// GET /api/v1/brand/history/brands
func (s *Server) handleHistoryBrands(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	names, err := s.brandEngine.HistoryDB().Brands(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":  len(names),
		"brands": names,
	})
}

// handleHistoryClear 清空历史库。
// POST /api/v1/brand/history/clear
func (s *Server) handleHistoryClear(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	if err := s.brandEngine.HistoryDB().Clear(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "历史库已清空"})
}

// --- 定时审计调度器接口 ---

// handleSchedulerStatus 返回调度器状态。
// GET /api/v1/brand/scheduler/status
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
			"message": "调度器未启用（设置 GEO_SCHEDULER_ENABLED=true + GEO_SCHEDULER_CONFIG=/path/to/config.json 启用）",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
	})
}

// handleSchedulerTrigger 手动触发一次指定品牌的定时审计。
// POST /api/v1/brand/scheduler/trigger  body: {"brand_name": "...", "profile": {...}}
func (s *Server) handleSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "品牌引擎未初始化"})
		return
	}
	var body struct {
		BrandName string             `json:"brand_name"`
		Profile   brand.BrandProfile `json:"profile"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if body.BrandName == "" {
		body.BrandName = body.Profile.Name
	}
	if body.BrandName == "" || len(body.Profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brand_name 或 profile.prompts"})
		return
	}
	// 取上一次审计的引擎统计（用于模型分歧告警对比）
	var prevStats []brand.EngineStats
	if s.brandEngine.HistoryDB() != nil {
		if rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName); err == nil && rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var prev brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &prev); err == nil {
				prevStats = prev.EngineStats
			}
		}
	}
	// 直接执行审计
	report, err := s.brandEngine.Audit(r.Context(), body.Profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]interface{}{
		"ok":     true,
		"report": report,
	}
	// 模型分歧告警：对比当前与上次审计，检测 5 类异常信号
	if s.scheduler != nil && len(prevStats) > 0 {
		if mr := s.scheduler.Monitor(report.EngineStats, prevStats); mr != nil {
			resp["monitor_result"] = mr
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- AI 可见度就绪审计接口 ---

// handleReadinessAudit 处理 AI 可见度就绪审计请求。
//
// GET  /api/v1/brand/readiness?url=example.com
// POST /api/v1/brand/readiness  JSON {"url": "example.com"}
//
// 检查目标网站对 AI 搜索引擎的可见度就绪度（robots.txt / llms.txt /
// 结构化数据 / sitemap.xml / TTFB），返回 readiness.AuditResult。
func (s *Server) handleReadinessAudit(w http.ResponseWriter, r *http.Request) {
	var rawURL string
	if r.Method == http.MethodGet {
		rawURL = r.URL.Query().Get("url")
	} else if r.Method == http.MethodPost {
		var body struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		rawURL = body.URL
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url 不能为空"})
		return
	}
	result, err := readiness.Audit(r.Context(), rawURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleCrawlabilityAudit 处理 AI 可爬取性审计请求。
//
// GET  /api/v1/brand/crawlability?url=https://example.com
// POST /api/v1/brand/crawlability  JSON {"url": "https://example.com"}
//
// 审计 27 个 AI 爬虫的 robots.txt 放行状态、JSON-LD schema 丰富度、
// llms.txt 存在性、知识图谱（Wikidata/Wikipedia/百度百科）存在性。
func (s *Server) handleCrawlabilityAudit(w http.ResponseWriter, r *http.Request) {
	var rawURL string
	if r.Method == http.MethodGet {
		rawURL = r.URL.Query().Get("url")
	} else if r.Method == http.MethodPost {
		var body struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		rawURL = body.URL
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url 不能为空"})
		return
	}
	result, err := crawlability.Audit(r.Context(), rawURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleDriftAudit 处理 diff/drift 回归检测请求。
//
// GET  /api/v1/brand/drift?brand_name=腾讯
// GET  /api/v1/brand/drift?brand_name=腾讯&prev_id=10&cur_id=12
// POST /api/v1/brand/drift  JSON {"brand_name":"腾讯","prev_id":10,"cur_id":12}
//
// 对比两次审计历史记录，检测各维度漂移与回归。
// 未指定 prev_id/cur_id 时自动取该品牌最近两条记录对比。
func (s *Server) handleDriftAudit(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	var (
		brandName     string
		prevID, curID int64
	)
	if r.Method == http.MethodGet {
		brandName = r.URL.Query().Get("brand_name")
		if p := r.URL.Query().Get("prev_id"); p != "" {
			prevID, _ = strconv.ParseInt(p, 10, 64)
		}
		if c := r.URL.Query().Get("cur_id"); c != "" {
			curID, _ = strconv.ParseInt(c, 10, 64)
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			BrandName string `json:"brand_name"`
			PrevID    int64  `json:"prev_id"`
			CurID     int64  `json:"cur_id"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		brandName = body.BrandName
		prevID = body.PrevID
		curID = body.CurID
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(brandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}

	// 按 ID 取指定两条记录对比
	if prevID > 0 && curID > 0 {
		prev, err := s.brandEngine.HistoryDB().GetByID(r.Context(), prevID)
		if err != nil || prev == nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("prev_id=%d 记录不存在", prevID)})
			return
		}
		cur, err := s.brandEngine.HistoryDB().GetByID(r.Context(), curID)
		if err != nil || cur == nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("cur_id=%d 记录不存在", curID)})
			return
		}
		writeJSON(w, http.StatusOK, drift.Compare(*prev, *cur))
		return
	}

	// 默认取最近两条对比
	report, err := drift.CompareLatest(r.Context(), s.brandEngine.HistoryDB(), brandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if report == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("品牌 %s 历史记录不足两条，无法对比", brandName)})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// --- 社媒情感监控接口 ---

// handleSocialMonitor 处理社媒情感监控请求。
//
// GET  /api/v1/brand/social/monitor?brand_name=腾讯&platforms=reddit,weibo,youtube&limit=20
// POST /api/v1/brand/social/monitor  JSON {"brand_name": "...", "platforms": ["reddit","weibo","youtube"], "limit": 20}
//
// 在 Reddit / 微博 / YouTube 等社媒平台并行搜索品牌提及，
// 执行规则引擎情感分析，返回提及列表 + 情感评分 + 各平台统计。
// Twitter / 小红书 适配器预留接口，未配置 API Key 时返回提示错误。
func (s *Server) handleSocialMonitor(w http.ResponseWriter, r *http.Request) {
	var (
		brandName string
		platforms []string
		limit     = 20
	)
	if r.Method == http.MethodGet {
		brandName = r.URL.Query().Get("brand_name")
		if p := r.URL.Query().Get("platforms"); p != "" {
			platforms = strings.Split(p, ",")
		}
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			BrandName string   `json:"brand_name"`
			Platforms []string `json:"platforms"`
			Limit     int      `json:"limit"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		brandName = body.BrandName
		platforms = body.Platforms
		if body.Limit > 0 {
			limit = body.Limit
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(brandName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand_name 不能为空"})
		return
	}
	if len(platforms) == 0 {
		// 默认全平台
		platforms = []string{"reddit", "weibo", "youtube", "twitter", "xiaohongshu"}
	}
	// 清理平台标识（去空白、转小写）
	cleaned := make([]string, 0, len(platforms))
	for _, p := range platforms {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	platforms = cleaned

	result, err := social.Monitor(r.Context(), brandName, platforms, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- KOL/创作者情报分析接口 ---

// handleKOLAnalyze 处理 KOL/创作者情报分析请求。
//
// POST /api/v1/brand/kol/analyze
// 请求体: {"brand_name": "...", "results": [...], "competitors": [...]}
//
// results 可从请求体直接传入（前端审计完成后直接传审计结果）；
// 若未传 results 但提供了 brand_name，则从 history DB 最新审计记录中取。
// competitors 可选，用于识别竞品引用源（生成"竞品引用源，需关注"推荐）。
func (s *Server) handleKOLAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		BrandName   string               `json:"brand_name"`
		Results     []brand.PromptResult `json:"results"`
		Competitors []brand.Competitor   `json:"competitors"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand_name 不能为空"})
		return
	}

	results := body.Results
	// results 为空时，从 history DB 最新审计记录中取
	if len(results) == 0 && s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取审计历史失败: " + err.Error()})
			return
		}
		if rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var vr brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err == nil {
				results = vr.Results
			}
		}
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "results 为空且无可用审计历史记录"})
		return
	}

	report := kol.AnalyzeWithCompetitors(body.BrandName, results, body.Competitors)
	writeJSON(w, http.StatusOK, report)
}

// --- Top Source 归因分析接口 ---

// handleTopSourceAnalyze 处理 Top Source 归因分析请求。
//
// POST /api/v1/brand/topsource/analyze
// 请求体: {"brand_name": "...", "results": [...], "brand_domain": "example.com"}
//
// results 可从请求体直接传入；若未传但提供了 brand_name，则从 history DB
// 最新审计记录中取。brand_domain 可选，用于判定品牌是否已在该域名上曝光。
func (s *Server) handleTopSourceAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		BrandName   string               `json:"brand_name"`
		Results     []brand.PromptResult `json:"results"`
		BrandDomain string               `json:"brand_domain"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand_name 不能为空"})
		return
	}

	results := body.Results
	// results 为空时，从 history DB 最新审计记录中取
	if len(results) == 0 && s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取审计历史失败: " + err.Error()})
			return
		}
		if rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var vr brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err == nil {
				results = vr.Results
			}
		}
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "results 为空且无可用审计历史记录"})
		return
	}

	report := topsource.Analyze(body.BrandName, results, body.BrandDomain)
	writeJSON(w, http.StatusOK, report)
}

// --- 行业类型自动识别接口 ---

// handleVerticalDetect 处理行业类型自动识别请求。
//
// POST /api/v1/brand/vertical/detect
// 请求体: 品牌画像字段（industry/category/domain/products/company 等任意组合）
//
// 返回检测到的行业类型、中文标签与差异化评分权重。
func (s *Server) handleVerticalDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var profile map[string]interface{}
	if err := readJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	v := vertical.Detect(profile)
	cfg := vertical.GetConfig(v)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vertical":      v,
		"label":         cfg.Label,
		"description":   cfg.Description,
		"score_weights": cfg.ScoreWeights,
	})
}

// handleVerticalList 返回全部已知的业务垂直行业列表。
//
// GET /api/v1/brand/vertical/list
func (s *Server) handleVerticalList(w http.ResponseWriter, r *http.Request) {
	vs := vertical.AllVerticals()
	out := make([]map[string]interface{}, 0, len(vs))
	for _, v := range vs {
		cfg := vertical.GetConfig(v)
		out = append(out, map[string]interface{}{
			"vertical":      v,
			"label":         cfg.Label,
			"description":   cfg.Description,
			"score_weights": cfg.ScoreWeights,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"verticals": out,
		"count":     len(out),
	})
}

// --- Local SEO/GMB 审计接口 ---

// handleLocalSEOAudit 处理本地 SEO / GMB 审计请求。
//
// POST /api/v1/brand/localseo/audit
// 请求体: {"brand_name": "...", "nap": {"name": "...", "address": "...", "phone": "...", "website": "..."}}
//
// 检查 NAP 一致性、GMB 资料完整度、本地引用收录情况，返回综合评分与建议。
func (s *Server) handleLocalSEOAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		BrandName string                 `json:"brand_name"`
		NAP       localseo.NAPInfo       `json:"nap"`
		Profile   map[string]interface{} `json:"profile,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand_name 不能为空"})
		return
	}
	// nap.name 为空时用 brand_name 兜底
	if strings.TrimSpace(body.NAP.Name) == "" {
		body.NAP.Name = body.BrandName
	}
	report, err := localseo.Audit(r.Context(), body.BrandName, body.NAP)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// --- 按量付费第三方数据源接口 ---

// handleExternalSignals 处理外部信号采集请求。
//
// GET  /api/v1/brand/externalsignals/report?domain=example.com&keywords=kw1,kw2
// POST /api/v1/brand/externalsignals/report  JSON {"domain": "...", "keywords": ["..."]}
//
// 调用 DataForSEO（付费，需 GEO_DFS_APIKEY/GEO_DFS_EMAIL）或 Common Crawl（免费）
// 采集关键词搜索量/难度、反链与 SERP 特性。无 API Key 时返回模拟数据并标注。
func (s *Server) handleExternalSignals(w http.ResponseWriter, r *http.Request) {
	var (
		domain   string
		keywords []string
	)
	if r.Method == http.MethodGet {
		domain = r.URL.Query().Get("domain")
		if k := r.URL.Query().Get("keywords"); k != "" {
			keywords = strings.Split(k, ",")
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Domain   string   `json:"domain"`
			Keywords []string `json:"keywords"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		domain = body.Domain
		keywords = body.Keywords
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(domain) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain 不能为空"})
		return
	}
	client := externalsignals.NewFromEnv()
	report, err := client.FullReport(r.Context(), domain, keywords)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// --- AutoGEO 规则提取与改写接口 ---

// llmManagerAdapter 将 llm.Manager 适配为 autorewriter.LLMClient 接口。
//
// autorewriter 需要 Complete(ctx, prompt) 语义，而 llm.Manager 提供
// Rewrite(ctx, prompt, content) 语义。这里以 prompt 作为改写指令、空内容
// 调用 Rewrite，由首个可用 Provider 完成「补全」。
type llmManagerAdapter struct {
	mgr *llm.Manager
}

func (a *llmManagerAdapter) Available() bool {
	return a.mgr != nil && a.mgr.HasAvailable()
}

func (a *llmManagerAdapter) Complete(ctx context.Context, prompt string) (string, error) {
	if a.mgr == nil {
		return "", fmt.Errorf("LLM 管理器未初始化")
	}
	return a.mgr.Rewrite(ctx, prompt, "")
}

// newAutoRewriter 惰性创建自动改写引擎，复用品牌引擎的 LLM 管理器。
//
// 未初始化品牌引擎时返回基于 StubLLMClient 的降级引擎（规则化改写）。
func (s *Server) newAutoRewriter() *autorewriter.Rewriter {
	if s.brandEngine == nil {
		return autorewriter.New(nil)
	}
	// 复用 brandEngine 内部的 LLM 管理器（通过环境变量重新构建以保持一致）
	mgr := newLLMManagerFromEnv()
	return autorewriter.New(&llmManagerAdapter{mgr: mgr})
}

// handleAutoRewriteRules 返回 AutoGEO 默认规则集（含 Princeton PWC 提升值）。
//
// GET  /api/v1/autorewriter/rules
// POST /api/v1/autorewriter/rules  JSON {"query": "...", "doc": "...", "citation_result": "..."}
//
// POST 时若 LLM 可用，则基于文档与引用结果动态提取规则；否则返回默认规则集。
func (s *Server) handleAutoRewriteRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"rules":  autorewriter.DefaultRules(),
			"source": "princeton",
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	var body struct {
		Query          string `json:"query"`
		Doc            string `json:"doc"`
		CitationResult string `json:"citation_result"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rw := s.newAutoRewriter()
	rs, err := rw.ExtractRules(r.Context(), body.Query, body.Doc, body.CitationResult)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// handleAutoRewrite 依据规则改写内容并执行 GEU 校验。
//
// POST /api/v1/autorewriter/rewrite
// 请求体: {"content": "...", "query": "...", "engine": "...", "preserve_facts": true, "rules": [...]}
//
// rules 为空时使用默认规则。返回改写后内容、应用规则、预估 PWC 提升与 GEU 校验结果。
func (s *Server) handleAutoRewrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		Content       string              `json:"content"`
		Query         string              `json:"query"`
		Engine        string              `json:"engine"`
		PreserveFacts bool                `json:"preserve_facts"`
		Rules         []autorewriter.Rule `json:"rules"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content 不能为空"})
		return
	}
	rw := s.newAutoRewriter()
	req := &autorewriter.RewriteRequest{
		Content:       body.Content,
		Query:         body.Query,
		Engine:        body.Engine,
		PreserveFacts: body.PreserveFacts,
		Rules:         body.Rules,
	}
	result, err := rw.Rewrite(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAutoRewriteGEU 对原文与改写文执行 GEU 校验（标准阈值）。
//
// POST /api/v1/autorewriter/geu
// 请求体: {"original": "...", "rewritten": "..."}
func (s *Server) handleAutoRewriteGEU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		Original  string `json:"original"`
		Rewritten string `json:"rewritten"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Original) == "" || strings.TrimSpace(body.Rewritten) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "original 与 rewritten 均不能为空"})
		return
	}
	rw := s.newAutoRewriter()
	geu, err := rw.CheckGEU(r.Context(), body.Original, body.Rewritten)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, geu)
}

// --- AI 就绪度 CI 闸门接口 ---

// handleReadinessCIGate 处理 AI 就绪度 CI 门禁判定请求。
//
// GET  /api/v1/brand/readiness/ci-gate?url=example.com&threshold=80
// POST /api/v1/brand/readiness/ci-gate  JSON {"url": "example.com", "threshold": 80}
//
// 先执行 8 维就绪审计，再按 threshold（默认 60）判定门禁是否通过。
// 返回 readiness.CIGateResult，含 blocking_issues 与人类可读汇总。
// CI/CD 集成时可直接根据 passed 字段决定流水线是否中断。
func (s *Server) handleReadinessCIGate(w http.ResponseWriter, r *http.Request) {
	var (
		rawURL    string
		threshold = readiness.DefaultCIThreshold()
	)
	if r.Method == http.MethodGet {
		rawURL = r.URL.Query().Get("url")
		if t := r.URL.Query().Get("threshold"); t != "" {
			if n, err := strconv.ParseFloat(t, 64); err == nil {
				threshold = n
			}
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			URL       string  `json:"url"`
			Threshold float64 `json:"threshold"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		rawURL = body.URL
		if body.Threshold > 0 {
			threshold = body.Threshold
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url 不能为空"})
		return
	}
	result, err := readiness.Audit(r.Context(), rawURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	gate := readiness.CIGateReportWithThreshold(result, threshold)
	writeJSON(w, http.StatusOK, gate)
}

// handleBrandDiscover 关键词→公司推断搜索。
// POST /api/v1/brand/discover
// 请求体: {"keyword":"短视频"}
// 响应: {"keyword":"短视频","candidates":[...]}
func (s *Server) handleBrandDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Keyword) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keyword 不能为空"})
		return
	}

	var offlineDB offlinedb.DB
	if s.brandEngine != nil {
		offlineDB = s.brandEngine.OfflineDB()
	}
	var kb *knowledge.Knowledge
	if s.brandEngine != nil {
		kb = s.brandEngine.Knowledge()
	}

	result, err := discover.Discover(r.Context(), body.Keyword, offlineDB, kb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleBrandDiscoverReport 基于选中的公司候选生成完整 GEO 报告。
// POST /api/v1/brand/discover/report
// 请求体: {"candidate":{...},"keyword":"短视频"}
// 响应: 完整 GEOReport（品牌画像 + 审计 + 就绪度 + 建议）
func (s *Server) handleBrandDiscoverReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		Candidate discover.Candidate `json:"candidate"`
		Keyword   string             `json:"keyword"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Candidate.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "candidate.name 不能为空"})
		return
	}

	report, err := discover.GenerateReport(r.Context(), &body.Candidate, body.Keyword, s.brandEngine)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// defaultCompareDimensions 竞品对标默认的 5 个维度占位（当 VisibilityReport 无细分维度时使用）。
var defaultCompareDimensions = []string{"Search", "Content", "Media", "Social", "LocalSEO"}

// dimensionPriority 维度优先级顺序（用于竞品对标维度排序稳定化）。
// 按此列表顺序排列维度；未在列表中的维度按字母序追加在末尾。
var dimensionPriority = []string{
	"ContentQuality", "TechnicalSEO", "OnPageSEO", "Schema",
	"Performance", "AIReadiness", "ImageOptimization",
	"MentionRate", "CitationRate", "ShareOfVoice",
	"CitationPosition", "Sentiment", "EntityRecognition",
	"Search", "Content", "Media", "Social", "LocalSEO",
}

// dimensionPriorityIndex 维度优先级索引（便于 O(1) 查询），未在列表中的维度返回 len(priority)。
var dimensionPriorityIndex = func() map[string]int {
	m := make(map[string]int, len(dimensionPriority))
	for i, d := range dimensionPriority {
		m[d] = i
	}
	return m
}()

// sortDimensionsByPriority 按优先级排序维度（稳定排序）。
// 优先级列表内的维度按列表顺序排列，未在列表中的按字母序追加在末尾。
func sortDimensionsByPriority(dims []string) {
	sort.SliceStable(dims, func(i, j int) bool {
		pi, oki := dimensionPriorityIndex[dims[i]]
		pj, okj := dimensionPriorityIndex[dims[j]]
		if !oki {
			pi = len(dimensionPriority)
		}
		if !okj {
			pj = len(dimensionPriority)
		}
		if pi != pj {
			return pi < pj
		}
		// 同优先级（含均未在列表中）按字母序
		return dims[i] < dims[j]
	})
}

// extractDimensionScores 从 VisibilityReport 提取各维度分数。
//
// 策略：
//  1. 优先使用 ScoreBreakdown 中的 BVS 加权健康 7 维（ContentQuality/TechnicalSEO/OnPageSEO/
//     Schema/Performance/AIReadiness/ImageOptimization），如果这些字段不全为 0。
//  2. 其次使用引擎可见度 6 维（MentionRate/CitationRate/ShareOfVoice/CitationPosition/
//     Sentiment/EntityRecognition），如果这些字段不全为 0。
//  3. 若上述维度字段均不存在/全为 0，使用默认 5 个维度占位（Search/Content/Media/Social/
//     LocalSEO），按 Score 平均分拆，便于画图。
func extractDimensionScores(vr *brand.VisibilityReport) (dimensionNames []string, scores map[string]float64) {
	scores = map[string]float64{}

	if vr == nil {
		return defaultCompareDimensions, scores
	}
	sb := vr.ScoreBreakdown

	hasBVSDims := sb.ContentQuality != 0 || sb.TechnicalSEO != 0 || sb.OnPageSEO != 0 ||
		sb.Schema != 0 || sb.Performance != 0 || sb.AIReadiness != 0 || sb.ImageOptimization != 0
	if hasBVSDims {
		dimensionNames = []string{
			"ContentQuality", "TechnicalSEO", "OnPageSEO",
			"Schema", "Performance", "AIReadiness", "ImageOptimization",
		}
		scores["ContentQuality"] = sb.ContentQuality
		scores["TechnicalSEO"] = sb.TechnicalSEO
		scores["OnPageSEO"] = sb.OnPageSEO
		scores["Schema"] = sb.Schema
		scores["Performance"] = sb.Performance
		scores["AIReadiness"] = sb.AIReadiness
		scores["ImageOptimization"] = sb.ImageOptimization
		return dimensionNames, scores
	}

	hasEngineDims := sb.MentionRate != 0 || sb.CitationRate != 0 || sb.ShareOfVoice != 0 ||
		sb.CitationPosition != 0 || sb.Sentiment != 0 || sb.EntityRecognition != 0
	if hasEngineDims {
		dimensionNames = []string{
			"MentionRate", "CitationRate", "ShareOfVoice",
			"CitationPosition", "Sentiment", "EntityRecognition",
		}
		scores["MentionRate"] = sb.MentionRate
		scores["CitationRate"] = sb.CitationRate
		scores["ShareOfVoice"] = sb.ShareOfVoice
		scores["CitationPosition"] = sb.CitationPosition
		scores["Sentiment"] = sb.Sentiment
		scores["EntityRecognition"] = sb.EntityRecognition
		return dimensionNames, scores
	}

	n := len(defaultCompareDimensions)
	perDim := vr.Score / float64(n)
	dimensionNames = make([]string, len(defaultCompareDimensions))
	copy(dimensionNames, defaultCompareDimensions)
	for _, d := range dimensionNames {
		scores[d] = perDim
	}
	return dimensionNames, scores
}

// compareBrandResult 单个品牌的对标结果。
type compareBrandResult struct {
	Name               string              `json:"name"`
	Score              float64             `json:"score"`
	Grade              string              `json:"grade"`
	Tier               string              `json:"tier,omitempty"`
	EntityCompleteness float64             `json:"entity_completeness,omitempty"`
	CreatedAt          *string             `json:"created_at,omitempty"`
	DimensionScores    map[string]*float64 `json:"dimension_scores"` // nil = 无数据，非 nil = 实际分数
}

// compareDiffResult 两个品牌之间的差异。
// ByDimension 的值类型为 float64（差值）或 string（"n/a" 表示某品牌该维度无数据）。
type compareDiffResult struct {
	BrandA      string                 `json:"brand_a"`
	BrandB      string                 `json:"brand_b"`
	DeltaScore  float64                `json:"delta_score"`
	ByDimension map[string]interface{} `json:"by_dimension"`
}

// handleBrandCompare 竞品对标矩阵接口。
//
// GET /api/v1/brand/compare?brands=A,B,C,D（最多 5 个品牌）
//
// 从 HistoryDB 取各品牌最新审计，返回 JSON：
//
//	{
//	  "brands":     [{name, score, grade, tier, entity_completeness, created_at, dimension_scores:{维度:分数|null}}],
//	  "dimensions": [维度名数组（按优先级排序）],
//	  "diffs":      [{brand_a, brand_b, delta_score, by_dimension:{维度:差值|"n/a"}}],
//	  "errors":     {品牌名: 错误说明}   // 仅在有缺失/失败时存在
//	}
//
// 维度缺失标注：某品牌缺少某维度时 dimension_scores 中对应值为 null（区别于 0 分）。
// diffs 中当任一品牌某维度无数据时，该维度标注 "n/a" 而非计算差值。
// 缺少审计记录的品牌在 brands 数组中对应位置为 null，并在 errors 中说明。
func (s *Server) handleBrandCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}

	brandsRaw := r.URL.Query().Get("brands")
	if strings.TrimSpace(brandsRaw) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brands 参数"})
		return
	}
	parts := strings.Split(brandsRaw, ",")
	brandList := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		brandList = append(brandList, name)
	}
	if len(brandList) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brands 参数为空"})
		return
	}
	if len(brandList) > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("最多支持 5 个品牌同时对比，当前 %d 个", len(brandList)),
		})
		return
	}

	ctx := r.Context()
	data, errorsMap := s.buildBrandCompareData(ctx, brandList)
	resp := map[string]interface{}{
		"brands":     data.Brands,
		"dimensions": data.Dimensions,
		"diffs":      data.Diffs,
	}
	if len(errorsMap) > 0 {
		resp["errors"] = errorsMap
	}
	writeJSON(w, http.StatusOK, resp)
}

// brandCompareData 竞品对标数据（brands/dimensions/diffs）。
// 供 handleBrandCompare 与 handleBrandCompareExport 共享。
type brandCompareData struct {
	Brands     []*compareBrandResult `json:"brands"`
	Dimensions []string              `json:"dimensions"`
	Diffs      []compareDiffResult   `json:"diffs"`
}

// brandCompareEntry 内部构建对标数据时使用的单品牌条目。
type brandCompareEntry struct {
	Name   string
	Record *history.Record
	Report *brand.VisibilityReport
	ErrMsg string
}

// buildBrandCompareData 构建竞品对标数据。
//
// 逻辑要点：
//   - 维度缺失标注：某品牌缺少某维度时 DimensionScores 中对应值为 nil（JSON null），
//     前端可区分"0 分"与"无数据"。
//   - 维度排序稳定化：按 dimensionPriority 优先级排序，未在列表中的按字母序追加。
//   - diffs 中 null 维度跳过：当任一品牌的某维度为 nil 时，diff 中该维度标注 "n/a"。
//   - 透传 tier / entity_completeness。
func (s *Server) buildBrandCompareData(ctx context.Context, brandList []string) (brandCompareData, map[string]string) {
	entries := make([]*brandCompareEntry, len(brandList))
	errorsMap := map[string]string{}

	for i, name := range brandList {
		rec, err := s.brandEngine.HistoryDB().Latest(ctx, name)
		if err != nil {
			entries[i] = &brandCompareEntry{Name: name, ErrMsg: "读取审计历史失败: " + err.Error()}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
			entries[i] = &brandCompareEntry{Name: name, ErrMsg: "未找到该品牌的审计记录（请先执行一次品牌审计）"}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		var vr brand.VisibilityReport
		if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
			entries[i] = &brandCompareEntry{
				Name:   name,
				ErrMsg: "解析审计报告失败: " + err.Error(),
			}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		entries[i] = &brandCompareEntry{Name: name, Record: rec, Report: &vr}
	}

	// 收集所有品牌出现过的维度（去重），并按优先级排序
	unifiedDims := []string{}
	dimSet := map[string]bool{}
	for _, e := range entries {
		if e == nil || e.Report == nil {
			continue
		}
		dims, _ := extractDimensionScores(e.Report)
		for _, d := range dims {
			if !dimSet[d] {
				dimSet[d] = true
				unifiedDims = append(unifiedDims, d)
			}
		}
	}
	if len(unifiedDims) == 0 {
		unifiedDims = make([]string, len(defaultCompareDimensions))
		copy(unifiedDims, defaultCompareDimensions)
	}
	// 维度排序稳定化：按优先级列表排序
	sortDimensionsByPriority(unifiedDims)

	// 构建每个品牌的对标结果（缺失维度填 nil，非缺失填实际分数指针）
	brandsOut := make([]*compareBrandResult, len(entries))
	for i, e := range entries {
		if e == nil || e.Report == nil {
			brandsOut[i] = nil
			continue
		}
		_, dimScores := extractDimensionScores(e.Report)
		finalScores := make(map[string]*float64, len(unifiedDims))
		for _, d := range unifiedDims {
			if v, ok := dimScores[d]; ok {
				// 品牌有该维度，使用实际分数（拷贝避免指针共享）
				vv := v
				finalScores[d] = &vv
			} else {
				// 品牌缺少该维度，nil 表示无数据
				finalScores[d] = nil
			}
		}
		createdAtStr := ""
		if e.Record != nil && e.Record.Generated > 0 {
			createdAtStr = time.Unix(e.Record.Generated, 0).Format(time.RFC3339)
		} else if !e.Report.GeneratedAt.IsZero() {
			createdAtStr = e.Report.GeneratedAt.Format(time.RFC3339)
		}
		var createdAtPtr *string
		if createdAtStr != "" {
			createdAtPtr = &createdAtStr
		}
		brandsOut[i] = &compareBrandResult{
			Name:               e.Name,
			Score:              e.Report.Score,
			Grade:              e.Report.Grade,
			Tier:               e.Report.Tier,
			EntityCompleteness: e.Report.EntityCompletenessScore,
			CreatedAt:          createdAtPtr,
			DimensionScores:    finalScores,
		}
	}

	// 构建两两品牌差异（任一品牌该维度为 nil 时标注 "n/a"）
	diffs := []compareDiffResult{}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a == nil || b == nil || a.Report == nil || b.Report == nil {
				continue
			}
			_, aScores := extractDimensionScores(a.Report)
			_, bScores := extractDimensionScores(b.Report)
			byDim := map[string]interface{}{}
			for _, d := range unifiedDims {
				aVal, aOk := aScores[d]
				bVal, bOk := bScores[d]
				if !aOk || !bOk {
					// 任一品牌该维度无数据，标注 n/a
					byDim[d] = "n/a"
				} else {
					byDim[d] = aVal - bVal
				}
			}
			diffs = append(diffs, compareDiffResult{
				BrandA:      a.Name,
				BrandB:      b.Name,
				DeltaScore:  a.Report.Score - b.Report.Score,
				ByDimension: byDim,
			})
		}
	}

	return brandCompareData{
		Brands:     brandsOut,
		Dimensions: unifiedDims,
		Diffs:      diffs,
	}, errorsMap
}

// compareBrandColors 竞品对比雷达图/卡片用的品牌配色（最多 5 个品牌）。
var compareBrandColors = []string{
	"#4f8cff", // 蓝
	"#16a34a", // 绿
	"#f59e0b", // 橙
	"#dc2626", // 红
	"#8b5cf6", // 紫
}

// compareTierLabel 将 tier 标识转为中文标签（与 report.tierLabel 一致）。
func compareTierLabel(tier string) string {
	switch tier {
	case "household":
		return "头部"
	case "midmarket":
		return "中坚"
	case "niche":
		return "长尾"
	default:
		if tier == "" {
			return "—"
		}
		return tier
	}
}

// handleBrandCompareExport 竞品对比报告导出 API。
//
// GET /api/v1/brand/compare/export?brands=A,B,C&format=html|json
//
// format=html：生成自包含 HTML 报告（内联 CSS + SVG 雷达图 + 对比表格 + 差异分析）
// format=json：直接返回 compare 数据（与 /api/v1/brand/compare 一致）
func (s *Server) handleBrandCompareExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	brandsRaw := r.URL.Query().Get("brands")
	if strings.TrimSpace(brandsRaw) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 brands 参数"})
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "html"
	}
	if format != "html" && format != "json" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "format 仅支持 html 或 json"})
		return
	}
	parts := strings.Split(brandsRaw, ",")
	brandList := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		brandList = append(brandList, name)
	}
	if len(brandList) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brands 参数为空"})
		return
	}
	if len(brandList) > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("最多支持 5 个品牌同时对比，当前 %d 个", len(brandList)),
		})
		return
	}

	data, errorsMap := s.buildBrandCompareData(r.Context(), brandList)

	if format == "json" {
		resp := map[string]interface{}{
			"brands":     data.Brands,
			"dimensions": data.Dimensions,
			"diffs":      data.Diffs,
		}
		if len(errorsMap) > 0 {
			resp["errors"] = errorsMap
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// format == html
	htmlOut := generateCompareHTML(data, errorsMap)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, htmlOut)
}

// generateCompareHTML 生成自包含的竞品对比 HTML 报告。
//
// 报告结构：
//  1. 标题 + 元信息（生成时间 / 品牌数）
//  2. 总分对比卡片（每个品牌一张卡：名称/分数/等级/梯队）
//  3. SVG 雷达图（多品牌叠加，分数归一化到 0-1 映射半径）
//  4. 维度对比表格（行=维度，列=品牌，null 维度显示 n/a）
//  5. 差异分析（两两品牌差值表，null 维度显示 n/a）
//  6. 页脚
func generateCompareHTML(data brandCompareData, errorsMap map[string]string) string {
	var b strings.Builder
	now := time.Now()
	validBrands := make([]*compareBrandResult, 0, len(data.Brands))
	for _, br := range data.Brands {
		if br != nil {
			validBrands = append(validBrands, br)
		}
	}

	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>竞品对标报告 - `)
	b.WriteString(html.EscapeString(now.Format("2006-01-02")))
	b.WriteString(`</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, "PingFang SC", "Microsoft YaHei", "Helvetica Neue", sans-serif;
    color: #e5e7eb;
    background: #0f172a;
    line-height: 1.65;
    -webkit-font-smoothing: antialiased;
  }
  .page {
    max-width: 960px;
    margin: 0 auto;
    padding: 32px 44px 48px;
  }
  h1 { font-size: 28px; font-weight: 700; color: #f1f5f9; margin-bottom: 8px; }
  .meta { font-size: 13px; color: #94a3b8; margin-bottom: 32px; }
  .section-title {
    font-size: 18px; font-weight: 600; color: #f1f5f9;
    margin: 36px 0 16px; padding-bottom: 8px; border-bottom: 1px solid #1e293b;
  }
  /* 总分对比卡片 */
  .score-cards { display: flex; flex-wrap: wrap; gap: 16px; }
  .card {
    flex: 1; min-width: 160px;
    background: #1e293b; border: 1px solid #334155; border-radius: 10px;
    padding: 20px; text-align: center;
  }
  .card h2 { font-size: 15px; font-weight: 600; color: #cbd5e1; margin-bottom: 12px; }
  .card .score { font-size: 36px; font-weight: 700; margin-bottom: 4px; }
  .card .grade { font-size: 13px; color: #94a3b8; margin-bottom: 4px; }
  .card .tier { font-size: 12px; color: #64748b; }
  /* SVG 雷达图 */
  .radar-wrap { display: flex; justify-content: center; margin: 16px 0; }
  svg.radar { max-width: 440px; width: 100%; height: auto; }
  /* 对比表格 */
  table.compare-table {
    width: 100%; border-collapse: collapse;
    background: #1e293b; border-radius: 8px; overflow: hidden;
  }
  table.compare-table th, table.compare-table td {
    padding: 10px 12px; text-align: center; font-size: 13px;
    border-bottom: 1px solid #334155;
  }
  table.compare-table th {
    background: #334155; color: #e2e8f0; font-weight: 600;
  }
  table.compare-table td:first-child {
    text-align: left; color: #cbd5e1; font-weight: 500;
  }
  table.compare-table td.na { color: #64748b; font-style: italic; }
  table.compare-table tr:hover { background: #243044; }
  /* 差异分析 */
  .diffs { display: flex; flex-direction: column; gap: 20px; }
  .diff-item {
    background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 16px 20px;
  }
  .diff-item h3 { font-size: 14px; font-weight: 600; color: #f1f5f9; margin-bottom: 12px; }
  .diff-item table { width: 100%; border-collapse: collapse; }
  .diff-item th, .diff-item td {
    padding: 8px 10px; font-size: 12px; text-align: center;
    border-bottom: 1px solid #334155;
  }
  .diff-item th { color: #94a3b8; font-weight: 500; }
  .diff-item td:first-child { text-align: left; color: #cbd5e1; }
  .diff-item td.pos { color: #16a34a; }
  .diff-item td.neg { color: #dc2626; }
  .diff-item td.na { color: #64748b; font-style: italic; }
  /* 错误提示 */
  .errors {
    background: #4c1d24; border: 1px solid #7f1d1d; border-radius: 8px;
    padding: 12px 16px; margin: 16px 0; font-size: 13px; color: #fca5a5;
  }
  .errors h3 { font-size: 14px; margin-bottom: 8px; }
  .errors ul { margin-left: 18px; }
  /* 图例 */
  .legend { display: flex; flex-wrap: wrap; gap: 12px; justify-content: center; margin: 8px 0; }
  .legend-item { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #cbd5e1; }
  .legend-dot { width: 12px; height: 12px; border-radius: 50%; }
  /* 页脚 */
  footer {
    margin-top: 48px; padding-top: 16px; border-top: 1px solid #1e293b;
    font-size: 12px; color: #64748b; text-align: center;
  }
  @media print {
    body { background: #fff; color: #000; }
    .page { max-width: 100%; padding: 0; }
  }
</style>
</head>
<body>
<div class="page">
  <h1>竞品对标报告</h1>
  <div class="meta">生成时间：`)
	b.WriteString(html.EscapeString(now.Format("2006-01-02 15:04:05")))
	b.WriteString(` | 品牌数：`)
	b.WriteString(strconv.Itoa(len(validBrands)))
	b.WriteString(`</div>
`)

	// 错误提示
	if len(errorsMap) > 0 {
		b.WriteString(`<div class="errors"><h3>⚠ 部分品牌数据缺失</h3><ul>`)
		for name, msg := range errorsMap {
			b.WriteString(`<li><strong>` + html.EscapeString(name) + `</strong>：` + html.EscapeString(msg) + `</li>`)
		}
		b.WriteString(`</ul></div>`)
	}

	// 总分对比卡片
	b.WriteString(`<div class="section-title">总分对比</div>`)
	b.WriteString(`<div class="score-cards">`)
	for i, br := range validBrands {
		color := compareBrandColors[i%len(compareBrandColors)]
		b.WriteString(`<div class="card">`)
		b.WriteString(`<h2>` + html.EscapeString(br.Name) + `</h2>`)
		b.WriteString(fmt.Sprintf(`<div class="score" style="color:%s">%.1f</div>`, color, br.Score))
		b.WriteString(`<div class="grade">等级 ` + html.EscapeString(br.Grade) + `</div>`)
		b.WriteString(`<div class="tier">梯队 ` + html.EscapeString(compareTierLabel(br.Tier)) + `</div>`)
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)

	// SVG 雷达图
	if len(data.Dimensions) >= 3 && len(validBrands) > 0 {
		b.WriteString(`<div class="section-title">维度雷达图</div>`)
		b.WriteString(`<div class="radar-wrap">`)
		b.WriteString(buildCompareRadarSVG(data.Dimensions, validBrands))
		b.WriteString(`</div>`)
		// 图例
		b.WriteString(`<div class="legend">`)
		for i, br := range validBrands {
			color := compareBrandColors[i%len(compareBrandColors)]
			b.WriteString(fmt.Sprintf(`<div class="legend-item"><span class="legend-dot" style="background:%s"></span>%s</div>`,
				color, html.EscapeString(br.Name)))
		}
		b.WriteString(`</div>`)
	}

	// 维度对比表格
	b.WriteString(`<div class="section-title">维度对比明细</div>`)
	b.WriteString(`<table class="compare-table"><thead><tr><th>维度</th>`)
	for _, br := range validBrands {
		b.WriteString(`<th>` + html.EscapeString(br.Name) + `</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, dim := range data.Dimensions {
		b.WriteString(`<tr><td>` + html.EscapeString(dim) + `</td>`)
		for _, br := range validBrands {
			p := br.DimensionScores[dim]
			if p == nil {
				b.WriteString(`<td class="na">n/a</td>`)
			} else {
				b.WriteString(fmt.Sprintf(`<td>%.1f</td>`, *p))
			}
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)

	// 差异分析
	if len(data.Diffs) > 0 {
		b.WriteString(`<div class="section-title">差异分析</div>`)
		b.WriteString(`<div class="diffs">`)
		for _, d := range data.Diffs {
			b.WriteString(`<div class="diff-item">`)
			b.WriteString(fmt.Sprintf(`<h3>%s vs %s (Δ %.1f)</h3>`,
				html.EscapeString(d.BrandA), html.EscapeString(d.BrandB), d.DeltaScore))
			b.WriteString(`<table><thead><tr><th>维度</th><th>差值</th></tr></thead><tbody>`)
			for _, dim := range data.Dimensions {
				val, ok := d.ByDimension[dim]
				if !ok {
					continue
				}
				b.WriteString(`<tr><td>` + html.EscapeString(dim) + `</td>`)
				switch v := val.(type) {
				case string:
					b.WriteString(`<td class="na">` + html.EscapeString(v) + `</td>`)
				case float64:
					cls := ""
					if v > 0 {
						cls = "pos"
					} else if v < 0 {
						cls = "neg"
					}
					b.WriteString(fmt.Sprintf(`<td class="%s">%+.1f</td>`, cls, v))
				default:
					b.WriteString(`<td class="na">n/a</td>`)
				}
				b.WriteString(`</tr>`)
			}
			b.WriteString(`</tbody></table>`)
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`<footer>由 AI 生成，仅供参考 | GEO v` + geoVersion + `</footer>`)
	b.WriteString(`
</div>
</body>
</html>`)
	return b.String()
}

// buildCompareRadarSVG 构建多品牌叠加的 SVG 雷达图。
//
// 画布 400x400，中心 (200,200)，最大半径 140。
// 维度数 N ≥ 3 时绘制 N 边形，每个品牌一条折线，分数归一化到 0-1 映射半径。
// nil 分数（无数据）按半径 0 处理（折线收到中心）。
func buildCompareRadarSVG(dims []string, brands []*compareBrandResult) string {
	const (
		cx, cy     = 200.0, 200.0
		maxRadius  = 140.0
		canvasSize = 400
	)
	n := len(dims)
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="radar" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`, canvasSize, canvasSize))

	// 计算各维度顶点角度（从顶部开始，顺时针）
	angles := make([]float64, n)
	for k := 0; k < n; k++ {
		angles[k] = -math.Pi/2 + 2*math.Pi*float64(k)/float64(n)
	}
	// 顶点坐标辅助函数
	vertex := func(k int, r float64) (float64, float64) {
		return cx + r*math.Cos(angles[k]), cy + r*math.Sin(angles[k])
	}

	// 绘制网格（4 层同心 N 边形：25%/50%/75%/100%）
	for _, ratio := range []float64{0.25, 0.5, 0.75, 1.0} {
		r := maxRadius * ratio
		b.WriteString(`<polygon points="`)
		for k := 0; k < n; k++ {
			x, y := vertex(k, r)
			if k > 0 {
				b.WriteString(" ")
			}
			b.WriteString(fmt.Sprintf("%.1f,%.1f", x, y))
		}
		b.WriteString(`" fill="none" stroke="#334155" stroke-width="1"/>`)
	}

	// 绘制轴线（从中心到各顶点）
	for k := 0; k < n; k++ {
		x, y := vertex(k, maxRadius)
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#334155" stroke-width="1"/>`, cx, cy, x, y))
	}

	// 绘制维度标签
	labelR := maxRadius + 22
	for k, dim := range dims {
		x, y := vertex(k, labelR)
		anchor := "middle"
		if x < cx-10 {
			anchor = "end"
		} else if x > cx+10 {
			anchor = "start"
		}
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="%s" font-size="11" fill="#94a3b8">%s</text>`,
			x, y+4, anchor, html.EscapeString(dim)))
	}

	// 绘制每个品牌的折线
	for i, br := range brands {
		color := compareBrandColors[i%len(compareBrandColors)]
		points := make([]string, 0, n)
		for k := 0; k < n; k++ {
			p := br.DimensionScores[dims[k]]
			var r float64
			if p != nil {
				r = (*p / 100.0) * maxRadius
				if r < 0 {
					r = 0
				}
				if r > maxRadius {
					r = maxRadius
				}
			}
			x, y := vertex(k, r)
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		b.WriteString(`<polygon points="` + strings.Join(points, " ") + `"`)
		b.WriteString(fmt.Sprintf(` fill="%s" fill-opacity="0.12" stroke="%s" stroke-width="2"/>`, color, color))
		// 顶点圆点
		for k := 0; k < n; k++ {
			p := br.DimensionScores[dims[k]]
			if p == nil {
				continue
			}
			x, y := vertex(k, (*p/100.0)*maxRadius)
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="3" fill="%s"/>`, x, y, color))
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

// ---------- 排行榜接口 ----------

// leaderboardItem 排行榜条目。
type leaderboardItem struct {
	Rank      int     `json:"rank"`
	BrandName string  `json:"brand_name"`
	Score     float64 `json:"score"`
	Grade     string  `json:"grade"`
	Tier      string  `json:"tier"`
	Category  string  `json:"category"`
	Industry  string  `json:"industry,omitempty"`
	Generated int64   `json:"generated_at"`
}

// leaderboardBrandHistory 单品牌历史走势与排名。
type leaderboardBrandHistory struct {
	BrandName   string           `json:"brand_name"`
	Category    string           `json:"category"`
	Industry    string           `json:"industry,omitempty"`
	CurrentRank int              `json:"current_rank"`
	History     []history.Record `json:"history"`
	RankHistory []rankPoint      `json:"rank_history,omitempty"`
}

// rankPoint 某个时间点的排名（用于趋势图）。
type rankPoint struct {
	Generated int64   `json:"generated_at"`
	Rank      int     `json:"rank"`
	Score     float64 `json:"score"`
}

// inferCategoryFromReportJSON 从审计报告 JSON 推断 category/industry。
// 解析失败或为空时返回 ("其他", "")。
func inferCategoryFromReportJSON(reportJSON string) (category, industry string) {
	category = "其他"
	industry = ""
	if strings.TrimSpace(reportJSON) == "" {
		return
	}
	var vr struct {
		Category string `json:"category"`
		Industry string `json:"industry"`
	}
	if err := json.Unmarshal([]byte(reportJSON), &vr); err != nil {
		return
	}
	if strings.TrimSpace(vr.Category) != "" {
		category = strings.TrimSpace(vr.Category)
	}
	if strings.TrimSpace(vr.Industry) != "" {
		industry = strings.TrimSpace(vr.Industry)
	}
	return
}

// getAllBrandLatestRecords 获取所有品牌的最新审计记录（每个品牌只保留最新一条），
// 并从 report_json 推断 category。返回的条目按 score 降序排序。
func (s *Server) getAllBrandLatestRecords(ctx context.Context) ([]leaderboardItem, error) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		return nil, fmt.Errorf("审计历史库未启用")
	}
	brands, err := s.brandEngine.HistoryDB().Brands(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]leaderboardItem, 0, len(brands))
	for _, b := range brands {
		rec, err := s.brandEngine.HistoryDB().Latest(ctx, b)
		if err != nil || rec == nil {
			continue
		}
		cat, ind := inferCategoryFromReportJSON(rec.ReportJSON)
		items = append(items, leaderboardItem{
			BrandName: rec.BrandName,
			Score:     rec.Score,
			Grade:     rec.Grade,
			Tier:      rec.Tier,
			Category:  cat,
			Industry:  ind,
			Generated: rec.Generated,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	for i := range items {
		items[i].Rank = i + 1
	}
	return items, nil
}

// handleLeaderboardCategories 返回已有类目列表（去重排序）。
//
// GET /api/v1/leaderboard/categories
func (s *Server) handleLeaderboardCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	items, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	catSet := map[string]int{}
	for _, it := range items {
		catSet[it.Category]++
	}
	categories := make([]map[string]interface{}, 0, len(catSet))
	for cat, cnt := range catSet {
		categories = append(categories, map[string]interface{}{
			"category": cat,
			"count":    cnt,
		})
	}
	sort.Slice(categories, func(i, j int) bool {
		ci := categories[i]["count"].(int)
		cj := categories[j]["count"].(int)
		if ci != cj {
			return ci > cj
		}
		return categories[i]["category"].(string) < categories[j]["category"].(string)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":      len(categories),
		"categories": categories,
	})
}

// handleLeaderboard 排行榜主接口。
//
// GET /api/v1/leaderboard?category=xxx&limit=100
//   - category 可选：空或 "全部" 返回所有类目；否则按类目过滤
//   - limit 可选：默认 50，最大 500
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	items, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var filtered []leaderboardItem
	if category == "" || strings.EqualFold(category, "all") || strings.EqualFold(category, "全部") {
		filtered = items
	} else {
		filtered = make([]leaderboardItem, 0, len(items))
		for _, it := range items {
			if strings.EqualFold(it.Category, category) {
				it.Rank = len(filtered) + 1
				filtered = append(filtered, it)
			}
		}
	}
	if limit < len(filtered) {
		filtered = filtered[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"category":    ternary(category == "", "全部", category),
		"limit":       limit,
		"count":       len(filtered),
		"total":       len(items),
		"leaderboard": filtered,
	})
}

// handleLeaderboardBrand 单品牌历史走势与排名。
//
// GET /api/v1/leaderboard/brand/:brand
// 路径示例：/api/v1/leaderboard/brand/腾讯
//   - 从 URL Path 提取品牌名（去掉 /api/v1/leaderboard/brand/ 前缀）
//   - 返回当前排名 + 该品牌所有历史审计记录（时间序列）
//   - 可选参数 history_limit：历史记录条数，默认 50，最大 500
func (s *Server) handleLeaderboardBrand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计历史库未启用"})
		return
	}
	prefix := "/api/v1/leaderboard/brand/"
	brandName := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
	brandName, _ = url.PathUnescape(brandName)
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少品牌名参数（路径：/api/v1/leaderboard/brand/:brand）"})
		return
	}
	historyLimit := 50
	if l := r.URL.Query().Get("history_limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			historyLimit = n
		}
	}
	if historyLimit > 500 {
		historyLimit = 500
	}
	// 1. 获取该品牌最新记录（用于 category / 当前排名计算）
	latestRec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if latestRec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该品牌的审计记录"})
		return
	}
	category, industry := inferCategoryFromReportJSON(latestRec.ReportJSON)
	// 2. 获取所有品牌最新记录，找到该品牌的当前排名
	allItems, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	currentRank := 0
	for _, it := range allItems {
		if strings.EqualFold(it.BrandName, brandName) {
			currentRank = it.Rank
			break
		}
	}
	// 3. 获取该品牌完整历史（时间序列，按时间降序）
	hist, err := s.brandEngine.HistoryDB().List(r.Context(), brandName, historyLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 4. 构造 rank_history：基于同 category 内历史记录的相对排名
	rankHistory := make([]rankPoint, 0, len(hist))
	if len(hist) > 0 {
		// 为了合理估算历史排名，拿所有品牌的"全量历史"做各时间点快照太重；
		// 这里以该 category 内当前排行榜条目为基准做简单近似：
		// 对每条历史记录，估算它在当前排行榜中的位置（按 score 排序）。
		catItems := make([]leaderboardItem, 0, len(allItems))
		for _, it := range allItems {
			if strings.EqualFold(it.Category, category) {
				catItems = append(catItems, it)
			}
		}
		// 将该品牌的历史分数逐条插入当前分类排行榜，估算历史排名
		for _, h := range hist {
			estRank := 1
			for _, ci := range catItems {
				if !strings.EqualFold(ci.BrandName, brandName) && ci.Score > h.Score {
					estRank++
				}
			}
			rankHistory = append(rankHistory, rankPoint{
				Generated: h.Generated,
				Rank:      estRank,
				Score:     h.Score,
			})
		}
	}
	writeJSON(w, http.StatusOK, leaderboardBrandHistory{
		BrandName:   brandName,
		Category:    category,
		Industry:    industry,
		CurrentRank: currentRank,
		History:     hist,
		RankHistory: rankHistory,
	})
}
