package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"my-geo/internal/auth"
	"my-geo/internal/billing"
	"my-geo/internal/billing/payment"
	"my-geo/internal/brand"
	"my-geo/internal/config"
	"my-geo/internal/queue"
)

// newBillingFromEnv 初始化计费服务与异步队列。
//
// DSN 解析：统一读取 GEO_MYSQL_DSN（单库架构，全部模块共用 geo 库）。
// 未配置或不可达则计费降级为不可用，不影响其他功能启动。
func newBillingFromEnv(be *brand.Engine, authSvc *auth.Service) (*billing.Service, billing.HandlerSet, *queue.Client, *queue.Server) {
	// 异步队列（Redis 背书，独立于计费库）。
	var qClient *queue.Client
	var qServer *queue.Server
	if be != nil {
		redisAddr := config.Env("GEO_REDIS_ADDR", "127.0.0.1:6379")
		redisPassword := config.Env("GEO_REDIS_PASSWORD", "")
		qc, qerr := queue.NewClient(redisAddr, redisPassword)
		if qerr != nil {
			slog.Warn("异步审计队列未启用：Redis 不可达", slog.String("addr", redisAddr), slog.Any("error", qerr))
		} else {
			qClient = qc
			workers := atoiEnv("GEO_QUEUE_WORKERS", 2)
			if qs, serr := queue.NewServer(redisAddr, redisPassword, be, workers); serr != nil {
				slog.Warn("异步审计队列 worker 启动失败", slog.Any("error", serr))
				_ = qc.Close()
				qClient = nil
			} else {
				qServer = qs
			}
		}
	} else {
		slog.Warn("品牌引擎未初始化，异步审计队列不可用。")
	}

	// 计费（MySQL）：与队列解耦，可独立启停。DSN 统一 GEO_MYSQL_DSN（单库架构共用 geo 库）。
	dsn := config.Env("GEO_MYSQL_DSN", "")
	if dsn == "" {
		slog.Warn("计费未启用：未配置 GEO_MYSQL_DSN。订阅/配额/支付将不可用。")
		return nil, billing.HandlerSet{}, qClient, qServer
	}
	store, err := billing.OpenStore(dsn)
	if err != nil {
		slog.Warn("计费数据库初始化失败（订阅/支付降级为不可用）", slog.Any("error", err))
		return nil, billing.HandlerSet{}, qClient, qServer
	}
	svc := billing.NewService(store)
	h := billing.NewHandlerSet(svc, authSvc)

	// 已配置的在线支付渠道（微信/支付宝/Stripe 并存；手动激活始终独立可用）。
	if provs := payment.ConfiguredProviders(); len(provs) > 0 {
		slog.Info("已启用在线支付渠道（与手动激活轻量版并存）", slog.Any("providers", provs))
	} else {
		slog.Info("未配置在线支付渠道：微信/支付宝/Stripe 待配置；「手动激活」轻量版始终可用。")
	}
	return svc, h, qClient, qServer
}

// ---- 计费 / 订阅 HTTP 路由（在 registerRoutes 中调用） ----

func (s *Server) registerBillingRoutes() {
	if s.billingH.Svc == nil {
		return // 计费未启用时不注册这些端点
	}
	s.mux.HandleFunc("/api/v1/billing/plans", s.billingH.HandlePlans)
	s.mux.HandleFunc("/api/v1/billing/payment-methods", s.billingH.HandlePaymentMethods)
	s.mux.HandleFunc("/api/v1/billing/subscription", s.billingH.HandleSubscription)
	s.mux.HandleFunc("/api/v1/billing/usage", s.billingH.HandleUsage)
	s.mux.HandleFunc("/api/v1/billing/subscription/activate", s.billingH.HandleActivate)
	s.mux.HandleFunc("/api/v1/billing/orders", s.billingH.HandleCreateOrder)
	s.mux.HandleFunc("/api/v1/billing/orders/", s.billingH.HandleGetOrder)
	s.mux.HandleFunc("/api/v1/billing/webhook/", s.billingH.HandleWebhook)
}

// ---- 配额助手 ----

// checkAuditQuota 审计前配额校验。计费未启用或无工作区上下文时放行。
// 返回 true 表示允许执行。
func (s *Server) checkAuditQuota(r *http.Request) bool {
	if s.billingSvc == nil {
		return true
	}
	wsID := ""
	if s.authSvc != nil && s.authSvc.Enabled() {
		wsID = auth.WorkspaceIDFromContext(r.Context())
	}
	if wsID == "" {
		return true // 匿名 / 未启用账号体系：由全局限流兜底
	}
	allowed, _, err := s.billingSvc.CanRun(r.Context(), wsID, billing.MeterAudits)
	if err != nil {
		slog.Warn("配额校验失败，放行", slog.Any("error", err))
		return true
	}
	return allowed
}

// recordAuditUsage 审计完成后记录用量。
func (s *Server) recordAuditUsage(r *http.Request) {
	if s.billingSvc == nil {
		return
	}
	wsID := ""
	if s.authSvc != nil && s.authSvc.Enabled() {
		wsID = auth.WorkspaceIDFromContext(r.Context())
	}
	if wsID == "" {
		return
	}
	if err := s.billingSvc.RecordUsage(r.Context(), wsID, billing.MeterAudits); err != nil {
		slog.Warn("记录审计用量失败", slog.Any("error", err))
	}
}

// ---- 异步审计（解耦长任务） ----

type auditAsyncRequest struct {
	Profile brand.BrandProfile `json:"profile"`
}

// handleBrandAuditAsync POST /api/v1/brand/audit/async
// 入队异步审计，立即返回 job_id；前端轮询 /api/v1/brand/audit/jobs/{id} 获取结果。
func (s *Server) handleBrandAuditAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	if s.queueClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "异步审计队列未启用（需配置 GEO_REDIS_ADDR 与品牌引擎）"})
		return
	}
	var req auditAsyncRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Profile.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "profile.name 不能为空"})
		return
	}
	if len(req.Profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "profile.prompts 不能为空"})
		return
	}
	// 配额校验
	if !s.checkAuditQuota(r) {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
			Error: "本月审计次数已达套餐上限，请升级套餐或下月再试",
		})
		return
	}
	profileJSON, err := json.Marshal(req.Profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	wsID := ""
	if s.authSvc != nil && s.authSvc.Enabled() {
		wsID = auth.WorkspaceIDFromContext(r.Context())
	}
	jobID, err := s.queueClient.Enqueue(r.Context(), &queue.Job{
		WorkspaceID: wsID,
		BrandName:   req.Profile.Name,
		ProfileJSON: string(profileJSON),
	})
	if err != nil {
		writeInternalError(w, err, "入队")
		return
	}
	// 入队即记一次用量（配额在提交时扣减）
	s.recordAuditUsage(r)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":  jobID,
		"status":  "pending",
		"message": "审计任务已入队，请轮询 /api/v1/brand/audit/jobs/" + jobID,
	})
}

// handleAuditJob GET /api/v1/brand/audit/jobs/{id}
func (s *Server) handleAuditJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.queueClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "异步审计队列未启用"})
		return
	}
	id := lastBillingSegment(r.URL.Path, "/api/v1/brand/audit/jobs/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 job id"})
		return
	}
	job, err := s.queueClient.GetJob(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "任务不存在"})
		return
	}
	// 工作区归属校验（IDOR 防护）：登录场景下仅允许查询本工作区的任务；
	// 匿名/未启用账号体系时任务 wsID 也为空，放行一致。
	if s.authSvc != nil && s.authSvc.Enabled() {
		if ws := auth.WorkspaceIDFromContext(r.Context()); ws != job.WorkspaceID {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "任务不存在"})
			return
		}
	}
	resp := map[string]any{
		"job_id":      job.ID,
		"status":      job.Status,
		"brand":       job.BrandName,
		"attempts":    job.Attempts,
		"created_at":  job.CreatedAt,
		"finished_at": job.FinishedAt,
	}
	if job.Status == queue.StatusFailed {
		resp["error"] = job.ErrorMsg
	}
	if job.Status == queue.StatusSucceeded && job.ResultJSON != "" {
		var result json.RawMessage
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err == nil {
			resp["report"] = result
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// lastBillingSegment 去除前缀取尾部段（与 billing.lastPathSegment 同义，server 内联避免跨包）。
func lastBillingSegment(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	s := path[len(prefix):]
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// atoiEnv 读取整型环境变量，解析失败或缺失用默认值。
func atoiEnv(key string, def int) int {
	v := config.Env(key, "")
	if v == "" {
		return def
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

// startQueue 在独立 goroutine 中启动 worker 池（随 server 生命周期）。
func (s *Server) startQueue(ctx context.Context) {
	if s.queueServer == nil {
		return
	}
	go s.queueServer.Start(ctx)
}
