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
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"math"
	"mime"
	"my-geo/internal/adapter"
	"my-geo/internal/auth"
	"my-geo/internal/billing"
	"my-geo/internal/brand"
	"my-geo/internal/brand/chinacheck"
	"my-geo/internal/brand/history"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/scheduler"
	"my-geo/internal/config"
	"my-geo/internal/dbprovider"
	"my-geo/internal/httputil"
	"my-geo/internal/queue"
	"my-geo/internal/llm"
	"my-geo/internal/mail"
	"my-geo/internal/models"
	"my-geo/internal/optimizer/autorewriter"
	"my-geo/pkg/geo"
	"net/http"
	_ "net/http/pprof" // /debug/pprof/* 性能剖析（注册到 DefaultServeMux，由 registerRoutes 转发）
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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
	mailSender  *mail.Sender  // SMTP 邮件发送器（未配置时为 nil）
	authSvc     *auth.Service // 账号体系（未启用时为 nil；但 Service.Enabled() 才为 false）
	authH       auth.HandlerSet
	billingSvc  *billing.Service // 计费 / 订阅 / 支付（未启用时为 nil）
	billingH    billing.HandlerSet
	queueClient *queue.Client // 异步审计队列客户端（未启用时为 nil）
	queueServer *queue.Server // 异步审计 worker 池（未启用时为 nil）
	whitelabel  Whitelabel
	llmMgr      *llm.Manager // 全局 LLM 管理器（/metrics 与品牌引擎复用同一实例）
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
	// 全局 LLM 管理器：品牌引擎与 /metrics 复用同一实例，避免多实例计数漂移。
	llmMgr := newLLMManagerFromEnv()
	be := newBrandEngineFromEnv(llmMgr)
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
	// 初始化账号体系（GEO_AUTH_ENABLED=true 时启用，缺省降级为 legacy API Key）
	var authSvc *auth.Service
	if as, err := auth.NewService(); err != nil {
		slog.Warn("账号体系初始化失败（将不启用 JWT/工作区/RBAC）", slog.Any("error", err))
	} else {
		authSvc = as
	}
	s := &Server{
		engine:      engine,
		brandEngine: be,
		whitelabel:  loadWhitelabelFromEnv(),
		addr:        addr,
		mux:         http.NewServeMux(),
		authSvc:     authSvc,
		authH:       auth.NewHandlerSet(authSvc),
		llmMgr:      llmMgr,
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
	// 初始化计费 / 订阅 / 支付 与 异步队列
	bsvc, bh, qc, qs := newBillingFromEnv(be, authSvc)
	s.billingSvc = bsvc
	s.billingH = bh
	s.queueClient = qc
	s.queueServer = qs
	if qs != nil {
		s.startQueue(context.Background())
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
// 离线工商 MySQL 库默认启用，即便空库也会打开以便后续写入。
func newBrandEngineFromEnv(llmMgr *llm.Manager) *brand.Engine {
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
	if llmMgr == nil {
		llmMgr = newLLMManagerFromEnv()
	}
	opts := []brand.Option{
		brand.WithAdapters(adapters),
		brand.WithLLM(llmMgr),
	}
	// 注入 China-Check 工商核验客户端（默认启用，可通过环境变量关闭）
	if cc := newChinaCheckFromEnv(); cc != nil {
		opts = append(opts, brand.WithChinaCheck(cc))
		slog.Info("China-Check MCP 工商核验已启用（GSXT/SAMR 官方数据，免鉴权免费）。")
	}
	// 注入离线工商 MySQL 库（默认启用，空库也打开）
	if odb := newOfflineDBFromEnv(); odb != nil {
		opts = append(opts, brand.WithOfflineDB(odb))
		st, err := odb.Stats(context.Background())
		if err != nil {
			slog.Warn("离线工商库打开成功但统计失败", slog.Any("error", err))
		} else {
			slog.Info("离线工商 MySQL 库已启用",
				slog.String("dsn", maskDSN(st.Path)),
				slog.Int64("count", st.Count),
				slog.String("seed_source", "guichong/- 仓库 json 分支"))
		}
	}
	// 注入审计历史 MySQL 库（默认启用）
	if hdb := newHistoryDBFromEnv(); hdb != nil {
		opts = append(opts, brand.WithHistoryDB(hdb))
		slog.Info("审计历史 MySQL 库已启用", slog.String("dsn", maskDSN(hdb.Path())))
	}
	return brand.New(opts...)
}

// BuildBrandEngineFromEnv 从环境变量构建品牌可见度引擎（导出版本，供 MCP Server 等复用）。
//
// 内部逻辑与 newBrandEngineFromEnv 完全一致，仅做导出封装，避免在 MCP Server
// 命令中重复实现 ChinaCheck / OfflineDB / LLM / HistoryDB 的环境变量解析逻辑。
func BuildBrandEngineFromEnv() *brand.Engine {
	return newBrandEngineFromEnv(nil)
}

// newHistoryDBFromEnv 连接审计历史 MySQL 库。
//
// 环境变量：
//
//	GEO_HISTORY_DB_ENABLED=true/false     总开关（默认 true）
//	GEO_HISTORY_MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/geo_history?...  DSN（优先）
//	GEO_HISTORY_DB_PATH=...                兼容旧变量：若形如 user:pass@tcp(...) 则作为 DSN
func newHistoryDBFromEnv() history.DB {
	enabled := config.Env("GEO_HISTORY_DB_ENABLED", "true")
	if strings.EqualFold(enabled, "false") || strings.EqualFold(enabled, "0") || strings.EqualFold(enabled, "off") {
		slog.Info("审计历史 MySQL 库已通过 GEO_HISTORY_DB_ENABLED=false 禁用。")
		return nil
	}
	dsn := dbprovider.PathFor(dbprovider.ModuleAuditHistory)
	db, err := history.Open(dsn)
	if err != nil {
		slog.Warn("审计历史 MySQL 库打开失败（将无历史记录）", slog.Any("error", err))
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

// newChinaCheckFromEnv 从环境变量构建 China-Check MCP 客户端（默认启用 + 默认启用 MySQL 缓存）。
//
// 环境变量：
//
//	GEO_CHINACHECK_ENABLED=true/false            总开关（默认 true）
//	GEO_CHINACHECK_URL=https://...               自定义 MCP endpoint
//	GEO_CHINACHECK_LANG=zh/en/ja/...             enum 字段翻译语言（默认 zh）
//	GEO_CHINACHECK_CACHE_ENABLED=true/false      缓存开关（默认 true）
//	GEO_CHINACHECK_MYSQL_DSN=user:pass@tcp(...)/geo_cache?...  MySQL 缓存 DSN（优先）
//	GEO_CHINACHECK_CACHE_PATH=...                兼容旧变量（若含 @tcp(...) 则视为 DSN）
//	GEO_CHINACHECK_CACHE_MAX_ITEMS=20000         最大缓存条目（默认 10000）
//	GEO_CHINACHECK_CACHE_TTL_HOURS=720           单条目 TTL 小时（默认 720=30 天）
//	GEO_CHINACHECK_CACHE_TYPE=mysql/redis        后端类型（默认 mysql）
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

	// ---------- 缓存层（默认启用 MySQL K/V）----------
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
		cacheDSN := dbprovider.PathFor(dbprovider.ModuleChinaCheckCache)
		ca, err := chinacheck.NewCache(cacheDSN, cacheOpts...)
		if err != nil {
			slog.Warn("China-Check MySQL 缓存初始化失败（将无缓存运行）", slog.Any("error", err))
		} else {
			st := ca.Stats()
			slog.Info("China-Check MCP 缓存已启用（MySQL）",
				slog.String("backend", st.Backend),
				slog.String("dsn", st.File),
				slog.Int("count", st.Count),
				slog.Int("max", st.MaxItems),
				slog.Int("ttl_h", int(st.TTLSeconds/3600)))
			opts = append(opts, chinacheck.WithCache(ca))
		}
	}

	return chinacheck.New(opts...)
}

// newOfflineDBFromEnv 连接离线工商 MySQL 库。
//
// 环境变量：
//
//	GEO_OFFLINE_DB_ENABLED=true/false         总开关（默认 true）
//	GEO_OFFLINE_MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/geo_offline?...  DSN（优先）
//	GEO_OFFLINE_DB_PATH=...                   兼容旧变量（形如 user:pass@tcp(...) 视为 DSN）
func newOfflineDBFromEnv() offlinedb.DB {
	enabled := config.Env("GEO_OFFLINE_DB_ENABLED", "true")
	if strings.EqualFold(enabled, "false") || strings.EqualFold(enabled, "0") || strings.EqualFold(enabled, "off") {
		slog.Info("离线工商库已通过 GEO_OFFLINE_DB_ENABLED=false 禁用。")
		return nil
	}
	dsn := dbprovider.PathFor(dbprovider.ModuleOfflineCompanies)
	db, err := offlinedb.Open(dsn)
	if err != nil {
		slog.Warn("离线工商 MySQL 库打开失败（将无离线库运行）", slog.Any("error", err))
		return nil
	}
	return db
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
	if s.queueServer != nil {
		s.queueServer.Stop()
	}
	if s.brandEngine != nil {
		s.brandEngine.Close()
	}
	if s.authSvc != nil {
		_ = s.authSvc.Close()
	}
	if s.billingSvc != nil {
		_ = s.billingSvc.Store().Close()
	}
}

// Handler 返回 HTTP Handler（便于测试）。
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

func (s *Server) registerRoutes() {
	// Kubernetes 规范健康检查路径别名（liveness / readiness）
	s.mux.HandleFunc("/healthz", s.handleLiveness)
	s.mux.HandleFunc("/readyz", s.handleReadiness)
	// 可观测性：Prometheus 指标（免鉴权）+ pprof 性能剖析。
	// pprof 不加入鉴权白名单——启用 GEO_API_KEY / GEO_AUTH 时受保护；
	// 未配置任何密钥时与 /healthz 一样公开（仅限单机内网部署）。
	s.mux.HandleFunc("/metrics", s.handleMetrics)
	s.mux.Handle("/debug/pprof/", http.DefaultServeMux)
	// REST API（/api/v1/health, /api/v1/ready 复用同一实现，保持向后兼容）
	s.mux.HandleFunc("/api/v1/health", s.handleLiveness)
	s.mux.HandleFunc("/api/v1/ready", s.handleReadiness)
	s.mux.HandleFunc("/api/v1/meta/whitelabel", s.handleWhitelabel)
	s.mux.HandleFunc("/api/v1/strategies", s.handleStrategies)
	s.mux.HandleFunc("/api/v1/analyze", s.handleAnalyze)
	s.mux.HandleFunc("/api/v1/score", s.handleScore)
	s.mux.HandleFunc("/api/v1/optimize", s.handleOptimize)
	// 规则集外部化管理（替代原 CLI `geo rules`）
	s.mux.HandleFunc("/api/v1/rules", s.handleRulesList)              // GET 列出可用规则集
	s.mux.HandleFunc("/api/v1/rules/default", s.handleRulesDefault)   // GET 默认规则集 JSON
	s.mux.HandleFunc("/api/v1/rules/validate", s.handleRulesValidate) // POST 校验规则集 JSON
	// GEO 评测集运行（替代原 CLI `geo evaluate`）
	s.mux.HandleFunc("/api/v1/evaluate", s.handleEvaluate) // POST 运行评测集
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
	// 离线工商 MySQL 库调试接口
	s.mux.HandleFunc("/api/v1/brand/offlinedb/stats", s.handleOfflineDBStats)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/search", s.handleOfflineDBSearch)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/clear", s.handleOfflineDBClear)
	s.mux.HandleFunc("/api/v1/brand/offlinedb/provinces", s.handleOfflineDBProvinces)
	// 离线工商库导入（替代原 CLI `geo brand db import-*`）
	s.mux.HandleFunc("/api/v1/brand/offlinedb/import", s.handleOfflineDBImport)             // POST 上传 JSON 文件导入
	s.mux.HandleFunc("/api/v1/brand/offlinedb/import-github", s.handleOfflineDBImportGitHub) // POST 直连 GitHub 下载并导入
	// 审计历史时间序列接口
	s.mux.HandleFunc("/api/v1/brand/history/list", s.handleHistoryList)
	s.mux.HandleFunc("/api/v1/brand/history/get", s.handleHistoryGet)
	s.mux.HandleFunc("/api/v1/brand/history/stats", s.handleHistoryStats)
	s.mux.HandleFunc("/api/v1/brand/history/stats/daily", s.handleHistoryStatsDaily)
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
	s.mux.HandleFunc("/api/v1/brand/compare/export", s.handleBrandCompareExport)
	// CMS 集成接口
	s.mux.HandleFunc("/api/v1/cms/check", s.handleCMSCheck)
	s.mux.HandleFunc("/api/v1/cms/info", s.handleCMSInfo)
	// 安全审计接口
	s.mux.HandleFunc("/api/v1/security/audit", s.handleSecurityAudit)
	// ── 计费 / 订阅 / 支付（# 商业化） ──
	s.registerBillingRoutes()
	// 异步审计（长任务解耦）
	s.mux.HandleFunc("/api/v1/brand/audit/async", s.handleBrandAuditAsync)
	s.mux.HandleFunc("/api/v1/brand/audit/jobs/", s.handleAuditJob)
	// 管理员后台接口（#100）
	s.mux.HandleFunc("/api/v1/admin/tenants", s.handleAdminTenants)
	s.mux.HandleFunc("/api/v1/admin/tenants/", s.handleAdminTenantDetail)
	s.mux.HandleFunc("/api/v1/admin/usage", s.handleAdminUsage)
	s.mux.HandleFunc("/api/v1/admin/announcements", s.handleAdminAnnouncements)
	s.mux.HandleFunc("/api/v1/admin/announcements/", s.handleAdminAnnouncementDelete)
	s.mux.HandleFunc("/api/v1/admin/system", s.handleAdminSystem)
	s.mux.HandleFunc("/api/v1/admin/cost", s.handleAdminCost) // LLM 成本仪表盘
	s.mux.HandleFunc("/api/v1/admin/selfcheck", s.handleAdminSelfCheck) // 系统自检报告
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

	// ── 账号体系 #1-4：注册 / 登录 / 刷新 / 登出 / 工作区切换 / 成员管理 / 审计日志 ──
	s.mux.HandleFunc("/api/v1/auth/register", s.authH.Register)
	s.mux.HandleFunc("/api/v1/auth/login", s.authH.Login)
	s.mux.HandleFunc("/api/v1/auth/refresh", s.authH.Refresh)
	s.mux.HandleFunc("/api/v1/auth/logout", s.authH.Logout)
	s.mux.HandleFunc("/api/v1/auth/me", s.authH.Me)
	s.mux.HandleFunc("/api/v1/auth/change-password", s.authH.ChangePassword)
	s.mux.HandleFunc("/api/v1/auth/workspace/switch", s.authH.SwitchWorkspace)
	s.mux.HandleFunc("/api/v1/auth/workspace/members/add", s.authH.AddMember)
	s.mux.HandleFunc("/api/v1/auth/workspace/members/change-role", s.authH.ChangeRole)
	s.mux.HandleFunc("/api/v1/auth/workspace/members/remove", s.authH.RemoveMember)
	s.mux.HandleFunc("/api/v1/auth/admin/audit", s.authH.AdminAuditLog)
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
	// 静态资源分级缓存策略：
	//   - hashed 资源（.js/.css/.woff2/.png 等）：immutable，浏览器永久缓存
	//   - HTML 入口文件：no-cache，每次必须回源校验
	if ext != ".html" && ext != ".htm" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return true
}

// applyWhitelabelToHTML 对白标 HTML 模板进行占位符替换注入。
// 所有占位符值一律 HTML 转义后再注入，防止 XSS。
func (wl Whitelabel) applyWhitelabelToHTML(page string) string {
	page = strings.ReplaceAll(page, "<!-- WL_INJECT -->", wl.buildInjectBlock())
	page = strings.ReplaceAll(page, "{{WL_BRAND_NAME}}", html.EscapeString(wl.BrandName))
	page = strings.ReplaceAll(page, "{{WL_PRIMARY_COLOR}}", html.EscapeString(wl.PrimaryColor))
	page = strings.ReplaceAll(page, "{{WL_LOGO_URL}}", html.EscapeString(wl.LogoURL))
	page = strings.ReplaceAll(page, "{{WL_DOMAIN}}", html.EscapeString(wl.Domain))
	return page
}

// buildInjectBlock 构建 <!-- WL_INJECT --> 占位符的实际注入内容（CSS 变量 + Favicon）。
// 所有来自环境变量的值一律 HTML 转义，防止注入 <script> 等（XSS）。
func (wl Whitelabel) buildInjectBlock() string {
	var parts []string
	// 主题色仅允许 CSS 安全格式：校验为 hex/rgb 基础格式，非法则忽略
	parts = append(parts, fmt.Sprintf(`<meta name="theme-color" content="%s">`, html.EscapeString(wl.PrimaryColor)))
	if wl.FaviconURL != "" {
		// favicon 仅允许 http(s) 协议，其余一律忽略（防 javascript: 等协议注入）
		if safe, ok := safeURL(wl.FaviconURL); ok {
			parts = append(parts, fmt.Sprintf(`<link rel="icon" type="image/x-icon" href="%s">`, html.EscapeString(safe)))
		}
	}
	parts = append(parts, fmt.Sprintf(`<style>
:root {
  --wl-primary-color: %s;
  --wl-brand-name: %q;
}
</style>`, html.EscapeString(wl.PrimaryColor), html.EscapeString(wl.BrandName)))
	return strings.Join(parts, "\n")
}

// safeURL 校验 URL 仅允许 http/https 协议，返回规范化后值。
func safeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return raw, true
}

// readIndexHTMLData 读取 index.html 原始字节（web/dist/index.html 优先，失败回退 web/index.html）。
// embed.FS 内容在编译期固定、运行期不可变，因此结果用 sync.Once 缓存，避免每个请求重复解析 fs.Sub。
var (
	indexHTMLOnce sync.Once
	indexHTMLData []byte
	indexHTMLErr  error
)

func readIndexHTMLData() ([]byte, error) {
	indexHTMLOnce.Do(func() {
		subFS, err := fs.Sub(webFS, "web/dist")
		if err == nil {
			if data, e := fs.ReadFile(subFS, "index.html"); e == nil {
				indexHTMLData = data
				return
			}
		}
		indexHTMLData, indexHTMLErr = webFS.ReadFile("web/index.html")
	})
	return indexHTMLData, indexHTMLErr
}

type analyzeRequest struct {
	Content string `json:"content"`
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

// writeJSON 统一 JSON 响应（实现见 httputil）。
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	httputil.WriteJSON(w, status, data)
}

// writeInternalError 记录完整错误到日志（含 request_id），但对客户端只返回通用提示，
// 避免泄露 SQL / 连接串 / 表结构 / 堆栈等内部实现。
// msg 为额外上下文（如"生成报告失败"），可为空。
func writeInternalError(w http.ResponseWriter, err error, msg string) {
	slog.Error("internal error",
		slog.String("message", msg),
		slog.Any("error", err))
	m := "内部错误，请稍后重试"
	if msg != "" {
		m = msg + "失败，请稍后重试"
	}
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: m})
}

// readJSON 解析 JSON 请求体（实现见 httputil，默认上限 10MB）。
func readJSON(r *http.Request, v interface{}) error {
	return httputil.ReadJSON(r, v)
}

// requireDataAdmin 校验"数据清理类"接口的权限（P2-9）。
// 双模式：账号体系启用时要求 PermManageData（Owner/Admin），否则 403；
// legacy GEO_API_KEY 模式中 API Key 鉴权已通过即为全权，直接放行。
func (s *Server) requireDataAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.authSvc != nil && s.authSvc.Enabled() {
		if err := auth.RequirePermission(r.Context(), auth.PermManageData); err != nil {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: err.Error(), Code: "PERMISSION_DENIED"})
			return false
		}
	}
	return true
}

// ternary 三目运算符（避免 map 取值啰嗦）。
func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
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

// ---------- China-Check MCP 工商核验调试接口 ----------

// firstNotEmpty 返回第一个非空字符串（server 内部用，与 brand 包同名函数独立）。
func firstNotEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---------- 离线工商 MySQL 库调试接口 ----------

// --- 审计历史时间序列接口 ---

// --- 定时审计调度器接口 ---

// --- AI 可见度就绪审计接口 ---

// --- 社媒情感监控接口 ---

// --- KOL/创作者情报分析接口 ---

// --- Top Source 归因分析接口 ---

// --- 行业类型自动识别接口 ---

// --- Local SEO/GMB 审计接口 ---

// --- 按量付费第三方数据源接口 ---

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

// newAutoRewriter 惰性创建自动改写引擎，复用全局 LLM 管理器（与品牌引擎同实例）。
//
// 未初始化 LLM 时返回基于 nil 客户端的降级引擎（纯规则化改写）。
func (s *Server) newAutoRewriter() *autorewriter.Rewriter {
	if s.llmMgr == nil || !s.llmMgr.HasAvailable() {
		return autorewriter.New(nil)
	}
	return autorewriter.New(&llmManagerAdapter{mgr: s.llmMgr})
}

// --- AI 就绪度 CI 闸门接口 ---

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
	slices.SortStableFunc(dims, func(a, b string) int {
		pi, oki := dimensionPriorityIndex[a]
		pj, okj := dimensionPriorityIndex[b]
		if !oki {
			pi = len(dimensionPriority)
		}
		if !okj {
			pj = len(dimensionPriority)
		}
		if c := cmp.Compare(pi, pj); c != 0 {
			return c
		}
		// 同优先级（含均未在列表中）按字母序
		return cmp.Compare(a, b)
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
