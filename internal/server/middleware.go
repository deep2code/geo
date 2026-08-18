package server

import (
	"compress/gzip"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"my-geo/internal/auth"
	"my-geo/internal/httputil"
	"my-geo/internal/util"
)

// CORS 与认证中间件配置。
//
// 安全模型：
//   - GEO_CORS_ORIGINS：允许的跨域源（逗号分隔），默认仅 localhost。设为 "*" 表示全开放（仅限本地开发）。
//   - GEO_API_KEY：API 密钥。设置后，除 health/web 外的所有接口需 Bearer token。未设置则不鉴权（向后兼容）。
//   - 写操作（POST/DELETE/clear/import）在 CORS 设为具体源时始终校验 Origin（CSRF 防护）。
//   - GEO_TRUSTED_PROXIES：可信代理列表（逗号分隔的 IP/CIDR）。设置后 X-Forwarded-For 仅在
//     RemoteAddr 属于可信代理时才解析；否则直接用 RemoteAddr，避免 IP 伪造绕过限流/WAF。
var (
	corsOrigins = parseCORSOrigins()
	apiKey      = strings.TrimSpace(os.Getenv("GEO_API_KEY"))
)

func parseCORSOrigins() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("GEO_CORS_ORIGINS"))
	m := map[string]bool{}
	if raw == "" {
		// 默认仅允许本地开发常用源
		m["http://localhost"] = true
		m["http://localhost:8080"] = true
		m["http://127.0.0.1:8080"] = true
		return m
	}
	if raw == "*" {
		return nil // nil 表示全开放
	}
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			m[o] = true
		}
	}
	return m
}

// ===== 请求日志 =====

// requestIDContextKey 是 request ID 注入 context 的唯一键类型，避免与其他包冲突。
type requestIDContextKey struct{}

// RequestIDFromContext 从 context 取出当前请求的 request ID（未设置返回空串）。
// 供业务 handler 做日志关联 / 任务追踪使用。
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

// withRequestID 生成/继承 request ID：
//   - 优先复用客户端传入的 X-Request-Id（便于链路追踪）；
//   - 否则生成新的加密安全随机 ID（默认 16 位 hex）；
//   - 注入 context + 回写响应头（前端可调取，上报工单便于后端检索）。
func (s *Server) withRequestID(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if rid == "" {
			rid = util.RandomHexID(8)
		}
		w.Header().Set("X-Request-Id", rid)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, rid))
		h.ServeHTTP(w, r)
	})
}

// requestLogger 结构化请求日志中间件（method/path/status/耗时/client_ip/request_id）。
func (s *Server) requestLogger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		observeRequest(r.URL.Path, rw.status)
		rid := RequestIDFromContext(r.Context())
		if rid == "" {
			slog.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("client_ip", clientIP(r)),
				slog.String("remote", r.RemoteAddr),
			)
			return
		}
		slog.Info("http request",
			slog.String("request_id", rid),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("client_ip", clientIP(r)),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// ===== 中间件链组装 =====

// withMiddleware 应用完整中间件链（顺序：外层先，然后层层嵌套向内）：
// recovery → gzip → requestLogger → withRequestID → rateLimitGlobal → waf →
// cors(+CSRF) → auth(JWT+legacy API Key) → aiGeneratedHeaders → handler
func (s *Server) withMiddleware(h http.Handler) http.Handler {
	cfg := auth.MiddlewareConfig{
		Svc:          s.authSvc,
		LegacyAPIKey: apiKey, // GEO_API_KEY 作为账号体系未启用时的回退
		PublicPaths: map[string]bool{
			"/":              true,
			"/legal/bot":     true,
			"/robots.txt":    true,
			"/index.html":    true,
			"/favicon.ico":   true,
			"/healthz":       true, // 探活端点必须公开，避免鉴权误判宕机
			"/readyz":        true,
			"/metrics":       true, // Prometheus 抓取端点（无敏感数据）
			"/api/v1/health": true,
			"/api/v1/ready":  true,
		},
	}
	return s.recovery(
		s.withGzip(
			s.requestLogger(
				s.withRequestID(
					s.rateLimitGlobal(
						s.withWAF(
							s.withCSRF(
								s.withCORS(
									auth.WithAuthN(cfg)(
										s.withAIGeneratedHeaders(h),
									),
								),
							),
						),
					),
				),
			),
		),
	)
}

// ===== AI 生成合规头（法务 #81） =====

// withAIGeneratedHeaders 给"内容可能由 AI 生成"的响应统一追加机器可读标识：
//   - X-AI-Generated: true            （通用声明）
//   - X-AI-Disclaimer: ...            （人类可读短声明）
//   - X-Content-Source: ai-llm, refs  （内容来源：AI 生成 + 附引用）
//   - X-Compliance-Contact: compliance@mygeo.ai
//
// 覆盖范围（详见 shouldMarkAIGenerated 判定函数）：
//   - 所有 SPA 页面（页面内可能含 AI 产出区块，已在 UI 层标注）
//   - 报告 HTML/PDF/邮件 导出
//   - /analyze /score /optimize /audit /readiness /crawlability /autorewriter 等 LLM 接口
func (s *Server) withAIGeneratedHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldMarkAIGenerated(r.URL.Path) {
			w.Header().Set("X-AI-Generated", "true")
			w.Header().Set("X-AI-Disclaimer", aiGeneratedDisclaimerShort)
			w.Header().Set("X-Content-Source", "ai-llm, refs")
			w.Header().Set("X-Compliance-Contact", util.MyGEOComplianceEmail)
		}
		h.ServeHTTP(w, r)
	})
}

// ===== CORS =====

// withCORS 添加 CORS 头。
// corsOrigins 为 nil 时：
//   - 本地开发（localhost/127.0.0.1）允许跨域
//   - 生产环境默认拒绝跨域（安全姿态：deny by default）
//
// corsOrigins 非 nil 时：校验 Origin 白名单。
func (s *Server) withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if corsOrigins == nil {
			// 本地开发放行；非 localhost 默认拒绝
			if isLocalhostOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			// 非 localhost 且未配置白名单：不设 ACAO 头 → 浏览器拒绝跨域
		} else if corsOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin 判断 Origin 是否为本地开发地址。
func isLocalhostOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, prefix := range []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"http://[::1]",
		"https://[::1]",
	} {
		if strings.HasPrefix(origin, prefix) {
			return true
		}
	}
	return false
}

// ===== CSRF 防护 =====

// withCSRF 对写操作（POST/PUT/PATCH/DELETE）校验 Origin。
//
// 安全策略（deny by default）：
//   - corsOrigins 非 nil：写操作的 Origin 必须在白名单中
//   - corsOrigins 为 nil：localhost 放行，非 localhost 写操作拒绝
//   - Origin 缺失：允许（浏览器同源导航不携带 Origin，但非浏览器客户端也无 Origin）
func (s *Server) withCSRF(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWriteMethod(r.Method) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				if corsOrigins != nil {
					allowed = corsOrigins[origin]
				} else {
					allowed = isLocalhostOrigin(origin)
				}
				if !allowed {
					writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "CSRF 校验失败：Origin 不在白名单", Code: "CSRF_ORIGIN_MISMATCH"})
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

func isWriteMethod(method string) bool {
	return method == http.MethodPost ||
		method == http.MethodPut ||
		method == http.MethodPatch ||
		method == http.MethodDelete
}

// ===== 错误响应结构 =====

// ErrorResponse 统一错误响应结构。
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

// ===== 全局限流 =====

// rateLimitConfig 全局限流配置（从环境变量读取，带默认值）。
type rateLimitConfig struct {
	globalPerSec    int // 每个 IP 每秒最大请求数（全局）
	expensivePerSec int // 每个 IP 每秒最大昂贵接口请求数
}

var rlConfig = loadRateLimitConfig()

func loadRateLimitConfig() rateLimitConfig {
	return rateLimitConfig{
		globalPerSec:    envInt("GEO_RATE_LIMIT_GLOBAL", 20),
		expensivePerSec: envInt("GEO_RATE_LIMIT_EXPENSIVE", 2),
	}
}

// expensivePaths 高成本接口路径前缀匹配（审计/PDF/邮件/自动改写等消耗 LLM 或 CPU 的接口）。
var expensivePathPatterns = []string{
	"/api/v1/brand/audit",
	"/api/v1/brand/report/pdf",
	"/api/v1/brand/report/email",
	"/api/v1/brand/report/download",
	"/api/v1/mail/send",
	"/api/v1/autorewriter/rewrite",
	"/api/v1/autorewriter/geu",
	"/api/v1/optimize",
	"/api/v1/brand/discover",
	"/api/v1/brand/scheduler/trigger",
	"/api/v1/brand/readiness",
	"/api/v1/brand/crawlability",
	"/api/v1/brand/drift",
	"/api/v1/brand/social/monitor",
	"/api/v1/brand/kol/analyze",
	"/api/v1/brand/topsource/analyze",
	"/api/v1/brand/localseo/audit",
	"/api/v1/brand/externalsignals",
}

func isExpensivePath(path string) bool {
	for _, p := range expensivePathPatterns {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// tokenBucket 令牌桶限流器。
type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	rate     float64 // 每秒补充令牌数
	burst    int     // 桶容量
	mu       sync.Mutex
}

// allow 尝试消费 1 个令牌，成功返回 true。
func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now
	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}
	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}

// rateLimiter 多级令牌桶限流器（按 IP 维度）。
// 用读写锁：读路径（已存在的桶）完全并发，仅首次创建新桶与周期清理走写锁。
type rateLimiter struct {
	mu            sync.RWMutex
	globalBuckets map[string]*tokenBucket // IP → 全局桶
	expBuckets    map[string]*tokenBucket // IP → 昂贵接口桶
	lastCleanup   time.Time
}

var globalLimiter = newRateLimiter()

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		globalBuckets: map[string]*tokenBucket{},
		expBuckets:    map[string]*tokenBucket{},
		lastCleanup:   time.Now(),
	}
}

// getGlobalBucket 获取或创建 IP 的全局令牌桶（惰性创建）。
// 快路径 RLock 命中即返回；未命中才升级写锁 double-check 后创建。
func (rl *rateLimiter) getGlobalBucket(ip string) *tokenBucket {
	rl.mu.RLock()
	b, ok := rl.globalBuckets[ip]
	rl.mu.RUnlock()
	if ok {
		return b
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok = rl.globalBuckets[ip]; ok {
		return b
	}
	b = &tokenBucket{
		tokens:   float64(rlConfig.globalPerSec),
		lastTime: time.Now(),
		rate:     float64(rlConfig.globalPerSec),
		burst:    rlConfig.globalPerSec,
	}
	rl.globalBuckets[ip] = b
	rl.cleanup()
	return b
}

// getExpensiveBucket 获取或创建 IP 的昂贵接口令牌桶。
func (rl *rateLimiter) getExpensiveBucket(ip string) *tokenBucket {
	rl.mu.RLock()
	b, ok := rl.expBuckets[ip]
	rl.mu.RUnlock()
	if ok {
		return b
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok = rl.expBuckets[ip]; ok {
		return b
	}
	b = &tokenBucket{
		tokens:   float64(rlConfig.expensivePerSec),
		lastTime: time.Now(),
		rate:     float64(rlConfig.expensivePerSec),
		burst:    rlConfig.expensivePerSec,
	}
	rl.expBuckets[ip] = b
	return b
}

// cleanup 定期清理过期桶（超过 10 分钟未使用的 IP 条目）。
// 仅在写锁路径内调用（新桶创建时触发），内部自带 10 分钟窗口节流。
func (rl *rateLimiter) cleanup() {
	if time.Since(rl.lastCleanup) < 10*time.Minute {
		return
	}
	rl.lastCleanup = time.Now()
	cutoff := time.Now().Add(-10 * time.Minute)
	for ip, b := range rl.globalBuckets {
		b.mu.Lock()
		expired := b.lastTime.Before(cutoff)
		b.mu.Unlock()
		if expired {
			delete(rl.globalBuckets, ip)
		}
	}
	for ip, b := range rl.expBuckets {
		b.mu.Lock()
		expired := b.lastTime.Before(cutoff)
		b.mu.Unlock()
		if expired {
			delete(rl.expBuckets, ip)
		}
	}
}

// rateLimitGlobal 全局限流中间件：按 IP 限制每秒请求数，昂贵接口有更严格的独立配额。
func (s *Server) rateLimitGlobal(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		// 全局限流
		if !globalLimiter.getGlobalBucket(ip).allow() {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
				Error: "请求过于频繁，请稍后重试",
				Code:  "RATE_LIMIT_GLOBAL",
			})
			return
		}
		// 昂贵接口额外限流
		if isExpensivePath(r.URL.Path) && !globalLimiter.getExpensiveBucket(ip).allow() {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
				Error: "高成本接口请求过于频繁，请降低频率",
				Code:  "RATE_LIMIT_EXPENSIVE",
			})
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ===== WAF 中间件 =====

// 请求体大小上限（默认 10MB，审计接口可适当放大）
const defaultMaxBodyBytes = 10 * 1024 * 1024 // 10MB

// sqliPattern 常见 SQL 注入模式（UNION SELECT / OR 1=1 / 注释符 / 堆叠注入）。
var sqliPattern = regexp.MustCompile(`(?i)(?:'|--|/\*|\*/|;)\s*(?:union|select|insert|update|delete|drop|or\s+1\s*=\s*1|sleep\s*\(|benchmark\s*\(|load_file\s*\()|'\s*or\s*'1'\s*=\s*'1`)

// xssPattern 常见 XSS 攻击模式（<script> / on*= / javascript: / <iframe>）。
var xssPattern = regexp.MustCompile(`(?i)<\s*script|<\s*iframe|<\s*img[^>]+onerror|javascript:\s*\w|on(load|error|click|mouseover)\s*=\s*["']?[^"'\s>]`)

// pathTraversalPattern 路径遍历攻击模式（../ / ..\ / %2e%2e / %252e）。
var pathTraversalPattern = regexp.MustCompile(`(?:\.\.[\\/]|%2e%2e[%2f/\\]|%252e%252e)`)

// nullBytePattern null 字节注入。
var nullBytePattern = regexp.MustCompile(`%00|\x00`)

// withWAF Web 应用防火墙中间件。
//
// 防护能力：
//   - 请求体大小限制（防资源耗尽）
//   - URL 路径与查询参数注入检测（SQLi / XSS / 路径遍历 / null 字节）
//   - 安全响应头（X-Content-Type-Options / X-Frame-Options / Referrer-Policy / CSP）
func (s *Server) withWAF(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. 请求体大小限制
		maxBody := defaultMaxBodyBytes
		if isExpensivePath(r.URL.Path) {
			maxBody = 20 * 1024 * 1024 // 昂贵接口放宽到 20MB
		}
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxBody))

		// 2. URL 路径安全检查
		rawPath := r.URL.EscapedPath()
		if pathTraversalPattern.MatchString(rawPath) || nullBytePattern.MatchString(rawPath) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: "请求路径包含非法字符",
				Code:  "WAF_PATH_TRAVERSAL",
			})
			return
		}

		// 3. 查询参数安全检查（SQLi / XSS）
		rawQuery := r.URL.RawQuery
		if rawQuery != "" {
			decodedQuery, err := url.QueryUnescape(rawQuery)
			if err != nil {
				decodedQuery = rawQuery
			}
			if sqliPattern.MatchString(decodedQuery) {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{
					Error: "查询参数疑似包含 SQL 注入攻击",
					Code:  "WAF_SQLI",
				})
				return
			}
			if xssPattern.MatchString(decodedQuery) {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{
					Error: "查询参数疑似包含 XSS 攻击",
					Code:  "WAF_XSS",
				})
				return
			}
			if pathTraversalPattern.MatchString(decodedQuery) || nullBytePattern.MatchString(decodedQuery) {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{
					Error: "查询参数包含非法路径字符",
					Code:  "WAF_PATH_TRAVERSAL",
				})
				return
			}
		}

		// 4. 安全响应头
		setSecurityHeaders(w, r)

		h.ServeHTTP(w, r)
	})
}

// setSecurityHeaders 设置安全响应头。
func setSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("X-XSS-Protection", "1; mode=block")
	h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
	// HSTS：仅当请求经 HTTPS 到达（反代或直连）时设置，避免本地 HTTP 被锁死
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
	}
	// CSP 仅对 API 路径设置；SPA 页面由 HTML 自身的 meta 标签控制
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	}
}

// ===== Panic Recovery =====

// recovery panic 恢复中间件，捕获 handler 中的 panic 并返回 500 JSON。
func (s *Server) recovery(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					slog.Any("error", rec),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("remote", r.RemoteAddr),
				)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{
					Error: "服务器内部错误（panic 已恢复）",
					Code:  "INTERNAL_PANIC",
				})
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// ===== Gzip 压缩 =====

// gzipResponseWriter 包装 http.ResponseWriter，对满足条件的响应自动 gzip 压缩。
// 跳过条件（不压缩）：
//   - 客户端不支持 gzip（Accept-Encoding 未含 gzip）
//   - 响应已设置 Content-Encoding（例如已是压缩格式）
//   - 响应体 < 512 字节（压缩开销 > 收益）
//   - SSE / WebSocket / 二进制流（Content-Type 为 text/event-stream、application/zip 等）
type gzipResponseWriter struct {
	http.ResponseWriter
	gz            *gzip.Writer
	contentType   string
	headerWritten bool
	statusCode    int
	buf           []byte // 未达到 minSize 前缓存的响应体
}

const gzipMinSize = 512 // 小于此值不压缩

var gzipSkipTypes = map[string]bool{
	"application/zip":              true,
	"application/gzip":             true,
	"application/x-gzip":           true,
	"application/x-tar":            true,
	"application/x-rar-compressed": true,
	"application/pdf":              true, // PDF 已压缩
	"image/png":                    true, // 二进制图片已压缩
	"image/jpeg":                   true,
	"image/gif":                    true,
	"image/webp":                   true,
	"font/woff":                    true, // 字体已压缩
	"font/woff2":                   true,
	"text/event-stream":            true, // SSE
	"application/octet-stream":     true, // 可能是二进制流
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.gz != nil {
		return g.gz.Write(b)
	}
	g.buf = append(g.buf, b...)
	if len(g.buf) >= gzipMinSize && g.shouldCompress() {
		g.startGzip()
		n, err := g.gz.Write(g.buf)
		g.buf = nil
		return n, err
	}
	return len(b), nil
}

func (g *gzipResponseWriter) shouldCompress() bool {
	if g.contentType == "" {
		return true
	}
	mime := g.contentType
	if i := strings.Index(mime, ";"); i > 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return !gzipSkipTypes[strings.ToLower(mime)]
}

func (g *gzipResponseWriter) startGzip() {
	g.Header().Del("Content-Length")
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Set("Vary", "Accept-Encoding")
	if !g.headerWritten {
		g.ResponseWriter.WriteHeader(g.statusCode)
		g.headerWritten = true
	}
	g.gz = gzip.NewWriter(g.ResponseWriter)
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.headerWritten {
		return
	}
	g.statusCode = code
	g.contentType = g.Header().Get("Content-Type")
	if g.contentType != "" && !g.shouldCompress() {
		g.ResponseWriter.WriteHeader(code)
		g.headerWritten = true
		return
	}
}

func (g *gzipResponseWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withGzip 对响应做 gzip 压缩。
func (s *Server) withGzip(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		if w.Header().Get("Content-Encoding") != "" {
			h.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			if gw.gz != nil {
				_ = gw.gz.Close()
			} else if !gw.headerWritten {
				if len(gw.buf) > 0 {
					w.Header().Set("Content-Length", strconv.Itoa(len(gw.buf)))
					w.WriteHeader(gw.statusCode)
					_, _ = w.Write(gw.buf)
				} else {
					w.WriteHeader(gw.statusCode)
				}
			}
		}()
		h.ServeHTTP(gw, r)
	})
}

// ===== 工具函数 =====

// clientIP 获取真实客户端 IP（实现见 httputil：仅信任 GEO_TRUSTED_PROXIES
// 才解析 X-Forwarded-For / X-Real-IP，避免 IP 伪造绕过限流/WAF）。
func clientIP(r *http.Request) string { return httputil.ClientIP(r) }

// envInt 读取整型环境变量，未设置或解析失败返回 fallback。
func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
