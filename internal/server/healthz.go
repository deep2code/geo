// 健康检查端点：/healthz (liveness) 与 /readyz (readiness)。
//
// 设计参考 Kubernetes + Google Cloud Run /healthz、/readyz 规范：
//
//   - liveness（/healthz）：只检查进程自身是否存活，快速、无副作用。
//     k8s 连续失败会触发重启。
//   - readiness（/readyz）：检查所有依赖（DB/LLM/SMTP）是否可访问，
//     k8s / LB 连续失败会把节点从后端池摘掉，不再接收流量。
//
// 参考开源实现：
//   - https://github.com/heptiolabs/healthcheck （经典 Go healthcheck 库）
//   - kubernetes/kubernetes/pkg/probe/{http,tcp} 规范
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// startedAt 进程启动时间，方便运维算 uptime。
var startedAt = time.Now()

// pid 缓存一次，避免每次健康检查都 syscall。
var pid = os.Getpid()

// healthChecks 就绪检查清单（可扩展、可注入）。
type healthChecks struct {
	mu    sync.RWMutex
	funcs map[string]func(context.Context) checkResult
}

// checkResult 单项检查结果。
type checkResult struct {
	Status    string `json:"status"`              // ok | warn | fail | disabled
	Message   string `json:"message,omitempty"`   // 人类可读错误/说明
	LatencyMs int64  `json:"latency_ms"`          // 本次检查耗时
	Detail    string `json:"detail,omitempty"`    // DSN 脱敏 / host:port 等
}

func newHealthChecks() *healthChecks {
	return &healthChecks{funcs: map[string]func(context.Context) checkResult{}}
}

func (h *healthChecks) Register(name string, fn func(context.Context) checkResult) {
	if h == nil || fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.funcs[name] = fn
}

func (h *healthChecks) Run(ctx context.Context) (map[string]checkResult, bool) {
	h.mu.RLock()
	names := make([]string, 0, len(h.funcs))
	for k := range h.funcs {
		names = append(names, k)
	}
	// 拷贝函数引用，释放锁，避免并发执行时持锁
	fns := make(map[string]func(context.Context) checkResult, len(h.funcs))
	for _, n := range names {
		fns[n] = h.funcs[n]
	}
	h.mu.RUnlock()

	type outEntry struct {
		name string
		res  checkResult
	}
	ch := make(chan outEntry, len(names))
	wg := sync.WaitGroup{}
	// 并发跑：每个依赖独立超时
	for _, n := range names {
		wg.Add(1)
		go func(name string, fn func(context.Context) checkResult) {
			defer wg.Done()
			tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			start := time.Now()
			r := fn(tctx)
			if r.LatencyMs == 0 {
				r.LatencyMs = time.Since(start).Milliseconds()
			}
			ch <- outEntry{name: name, res: r}
		}(n, fns[n])
	}
	wg.Wait()
	close(ch)

	allOk := true
	result := make(map[string]checkResult, len(names))
	for e := range ch {
		result[e.name] = e.res
		if e.res.Status == "fail" {
			allOk = false
		}
	}
	return result, allOk
}

// maskDSN 把 DSN 里的密码替换成 ***，避免在健康检查 JSON 中泄露。
// user:pass@tcp(host:3306)/db -> user:***@tcp(host:3306)/db
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	if at := stringsLastIndexByte(dsn, '@'); at > 0 {
		prefix := dsn[:at]
		suffix := dsn[at:]
		if colon := stringsFirstColon(prefix); colon > 0 {
			return prefix[:colon+1] + "***" + suffix
		}
	}
	return dsn
}

func stringsLastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
func stringsFirstColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

// buildReadinessChecks 把 Server 上持有的各个依赖注册成 ready 检查函数。
func (s *Server) buildReadinessChecks() *healthChecks {
	h := newHealthChecks()

	// 1) Auth DB（若启用）：拿一个空用户查询，本质是让 MySQL 走一条查询。
	if s.authSvc != nil && s.authSvc.Enabled() && s.authSvc.Store() != nil {
		h.Register("auth_db", func(ctx context.Context) checkResult {
			st := s.authSvc.Store()
			start := time.Now()
			_, err := st.GetUserByEmail("__healthcheck_does_not_exist__@invalid.local")
			lat := time.Since(start).Milliseconds()
			if err != nil {
				return checkResult{
					Status:    "fail",
					Message:   err.Error(),
					LatencyMs: lat,
					Detail:    "auth_mysql",
				}
			}
			return checkResult{Status: "ok", LatencyMs: lat, Detail: "auth_mysql"}
		})
	} else {
		h.Register("auth_db", func(_ context.Context) checkResult {
			return checkResult{Status: "disabled", Message: "GEO_AUTH_ENABLED=false 或未初始化"}
		})
	}

	// 2) Audit History DB：Stats() 内部有简单查询。
	h.Register("history_db", func(ctx context.Context) checkResult {
		if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
			return checkResult{Status: "warn", Message: "审计历史库未初始化/未连接"}
		}
		start := time.Now()
		st, err := s.brandEngine.HistoryDB().Stats(ctx)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			return checkResult{
				Status:    "fail",
				Message:   err.Error(),
				LatencyMs: lat,
				Detail:    maskDSN(st.Path),
			}
		}
		return checkResult{
			Status:    "ok",
			LatencyMs: lat,
			Detail:    maskDSN(st.Path) + " records=" + strconv.FormatInt(st.Records, 10) + " brands=" + strconv.FormatInt(st.Brands, 10),
		}
	})

	// 3) Offline Company DB：Stats() 内部有 SELECT COUNT。
	h.Register("offline_db", func(ctx context.Context) checkResult {
		if s.brandEngine == nil || s.brandEngine.OfflineDB() == nil {
			return checkResult{Status: "warn", Message: "离线工商库未初始化/未连接"}
		}
		start := time.Now()
		st, err := s.brandEngine.OfflineDB().Stats(ctx)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			return checkResult{
				Status:    "fail",
				Message:   err.Error(),
				LatencyMs: lat,
				Detail:    maskDSN(st.Path),
			}
		}
		return checkResult{
			Status:    "ok",
			LatencyMs: lat,
			Detail:    maskDSN(st.Path) + " companies=" + strconv.FormatInt(st.Count, 10),
		}
	})

	// 4) LLM Providers：Manager.Status()（多 Provider 熔断 + 计数）
	h.Register("llm_providers", func(_ context.Context) checkResult {
		if s.brandEngine == nil || s.brandEngine.LLM() == nil {
			return checkResult{Status: "warn", Message: "LLM Manager 未初始化（品牌补全不可用，其余功能正常）"}
		}
		mgr := s.brandEngine.LLM()
		if !mgr.HasAvailable() {
			return checkResult{Status: "warn", Message: "无已配置的 LLM Provider（品牌补全不可用，其余功能正常）"}
		}
		list := mgr.Status()
		b, _ := json.Marshal(list)
		// 如果有任何 provider 处于熔断状态，标记 warn（不是 fail：还有其他 provider 可用）
		hasOpen := false
		configured := 0
		for _, ps := range list {
			if ps.Available {
				configured++
			}
			if ps.OpenUntil != "" {
				hasOpen = true
			}
		}
		status := "ok"
		msg := ""
		if configured == 0 {
			status = "warn"
			msg = "所有 LLM Provider 均未配置 API Key"
		} else if hasOpen {
			status = "warn"
			msg = "部分 LLM Provider 触发熔断冷却（已自动切换到备用）"
		}
		return checkResult{
			Status:  status,
			Message: msg,
			Detail:  "configured=" + strconv.Itoa(configured) + "/" + strconv.Itoa(len(list)) + " statuses=" + string(b),
		}
	})

	// 5) Adapters（外部搜索 / SERP 引擎）：看 Configured，加一个 1 字节 PingContext？
	//    没有统一 Ping 接口，这里只做"配置检查 + 缓存状态"。
	h.Register("search_adapters", func(_ context.Context) checkResult {
		if s.brandEngine == nil {
			return checkResult{Status: "fail", Message: "品牌审计引擎未初始化"}
		}
		adapters := s.brandEngine.Adapters()
		total := len(adapters)
		configured := 0
		for _, ok := range adapters {
			if ok {
				configured++
			}
		}
		b, _ := json.Marshal(adapters)
		detail := "total=" + strconv.Itoa(total) + " configured=" + strconv.Itoa(configured)
		if total == 0 {
			return checkResult{Status: "warn", Message: "无任何搜索适配器", Detail: detail}
		}
		if configured == 0 {
			return checkResult{Status: "warn", Message: "搜索适配器均未配置 API Key（将使用 Stub 响应）", Detail: detail + " adapters=" + string(b)}
		}
		return checkResult{Status: "ok", Detail: detail + " adapters=" + string(b)}
	})

	// 6) SMTP：TCP 拨号（不发 EHLO/MAIL），验证 host:port 可达。
	h.Register("smtp", func(ctx context.Context) checkResult {
		if s.mailSender == nil || !s.mailSender.Enabled() {
			return checkResult{Status: "disabled", Message: "SMTP 未配置（邮件接口将返回未启用）"}
		}
		host := s.mailSender.Host
		port := s.mailSender.Port
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		start := time.Now()
		d := &net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			return checkResult{
				Status:    "fail",
				Message:   err.Error(),
				LatencyMs: lat,
				Detail:    addr,
			}
		}
		_ = conn.Close()
		return checkResult{Status: "ok", LatencyMs: lat, Detail: addr}
	})

	return h
}

// 全局 readiness 检查器。初始化一次，后续复用。
var readinessOnce sync.Once
var readinessChecks atomic.Value // *healthChecks

func (s *Server) getReadinessChecks() *healthChecks {
	// 因为 readiness 依赖 s 字段，我们不能简单 sync.Once 做全局；
	// 这里用 per-Server 初始化（每个 New Server 一个进程通常就一个）。
	// 用指针字段更直观，但我们不想改 Server struct（保持 ABI 兼容），
	// 所以存到一个 Server 级 map。
	readinessPerServerMu.RLock()
	h, ok := readinessPerServer[s]
	readinessPerServerMu.RUnlock()
	if ok && h != nil {
		return h
	}
	readinessPerServerMu.Lock()
	defer readinessPerServerMu.Unlock()
	h, ok = readinessPerServer[s]
	if ok && h != nil {
		return h
	}
	h = s.buildReadinessChecks()
	readinessPerServer[s] = h
	return h
}

var (
	readinessPerServer   = map[*Server]*healthChecks{}
	readinessPerServerMu sync.RWMutex
)

// handleLiveness /healthz, /api/v1/health：只回答进程存活（无副作用、毫秒级）。
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	up := time.Since(startedAt).Truncate(time.Second)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "geo",
		"version": geoVersion,
		"pid":     pid,
		"uptime":  up.String(),
		"started": startedAt.Format(time.RFC3339),
	})
}

// handleReadiness /readyz, /api/v1/ready：并发检查所有依赖，503 任一 fail。
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	deadline := time.Now().Add(7 * time.Second)
	if d, ok := ctx.Deadline(); !ok || d.After(deadline) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	checks, allOk := s.getReadinessChecks().Run(ctx)
	code := http.StatusOK
	statusStr := "ready"
	if !allOk {
		code = http.StatusServiceUnavailable
		statusStr = "not_ready"
	}
	// 日志仅在 NOT_READY 时打 WARN（ready 正常时不刷日志，避免噪声）
	if !allOk {
		slog.Warn("readiness NOT_READY", slog.Any("checks", checks))
	}
	writeJSON(w, code, map[string]interface{}{
		"status":  statusStr,
		"service": "geo",
		"version": geoVersion,
		"checks":  checks,
	})
}
