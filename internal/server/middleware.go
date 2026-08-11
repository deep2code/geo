package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CORS 与认证中间件配置。
//
// 安全模型：
//   - GEO_CORS_ORIGINS：允许的跨域源（逗号分隔），默认仅 localhost。设为 "*" 表示全开放（仅限本地开发）。
//   - GEO_API_KEY：API 密钥。设置后，除 health/web 外的所有接口需 Bearer token。未设置则不鉴权（向后兼容）。
//   - 写操作（POST/DELETE/clear/import）在 CORS 设为具体源时始终校验 Origin（CSRF 防护）。
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

// requestLogger 结构化请求日志中间件（method/path/status/耗时）。
func (s *Server) requestLogger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		slog.Info("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
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

// withMiddleware 应用完整中间件链：
// recovery → requestLogger → rateLimitGlobal → waf → cors(+CSRF) → auth → handler
func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return s.recovery(
		s.requestLogger(
			s.rateLimitGlobal(
				s.withWAF(
					s.withCSRF(
						s.withCORS(
							s.withAuth(h),
						),
					),
				),
			),
		),
	)
}

// ===== CORS =====

// withCORS 添加 CORS 头。
// corsOrigins 为 nil 时全开放（兼容本地开发）；否则校验 Origin 白名单。
func (s *Server) withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if corsOrigins == nil {
			w.Header().Set("Access-Control-Allow-Origin", "*")
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

// ===== CSRF 防护 =====

// withCSRF 对写操作（POST/PUT/PATCH/DELETE）校验 Origin。
//
// 当 CORS 白名单非 nil（非全开放模式）时：
//   - 写操作的 Origin 必须在白名单中，否则拒绝（CSRF 防护）。
//   - Origin 缺失时允许同源请求（浏览器同源导航不携带 Origin，但也不应发写请求）。
//
// CORS 为 "*" 全开放模式时跳过（本地开发场景）。
func (s *Server) withCSRF(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsOrigins == nil {
			h.ServeHTTP(w, r)
			return
		}
		if isWriteMethod(r.Method) {
			origin := r.Header.Get("Origin")
			if origin != "" && !corsOrigins[origin] {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "CSRF 校验失败：Origin 不在白名单", Code: "CSRF_ORIGIN_MISMATCH"})
				return
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

// ===== 鉴权 =====

// withAuth API Key 鉴权中间件。
// 未配置 GEO_API_KEY 时跳过鉴权（向后兼容）；配置后除白名单路径外需 Bearer token。
func (s *Server) withAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" || isPublicPath(r.URL.Path) {
			h.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(auth[7:]) != apiKey {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "未授权：无效或缺失的 API Key"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

// isPublicPath 判断路径是否为公开路径（无需鉴权）。
func isPublicPath(path string) bool {
	return path == "/" || path == "/api/v1/health" || path == "/api/v1/ready"
}

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
type rateLimiter struct {
	mu             sync.Mutex
	globalBuckets  map[string]*tokenBucket // IP → 全局桶
	expBuckets     map[string]*tokenBucket // IP → 昂贵接口桶
	lastCleanup    time.Time
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
func (rl *rateLimiter) getGlobalBucket(ip string) *tokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.cleanup()
	b, ok := rl.globalBuckets[ip]
	if !ok {
		b = &tokenBucket{
			tokens:   float64(rlConfig.globalPerSec),
			lastTime: time.Now(),
			rate:     float64(rlConfig.globalPerSec),
			burst:    rlConfig.globalPerSec,
		}
		rl.globalBuckets[ip] = b
	}
	return b
}

// getExpensiveBucket 获取或创建 IP 的昂贵接口令牌桶。
func (rl *rateLimiter) getExpensiveBucket(ip string) *tokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.expBuckets[ip]
	if !ok {
		b = &tokenBucket{
			tokens:   float64(rlConfig.expensivePerSec),
			lastTime: time.Now(),
			rate:     float64(rlConfig.expensivePerSec),
			burst:    rlConfig.expensivePerSec,
		}
		rl.expBuckets[ip] = b
	}
	return b
}

// cleanup 定期清理过期桶（超过 10 分钟未使用的 IP 条目）。
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
		setSecurityHeaders(w, r.URL.Path)

		h.ServeHTTP(w, r)
	})
}

// setSecurityHeaders 设置安全响应头。
func setSecurityHeaders(w http.ResponseWriter, path string) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("X-XSS-Protection", "1; mode=block")
	h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
	// CSP 仅对 API 路径设置；SPA 页面由 HTML 自身的 meta 标签控制
	if strings.HasPrefix(path, "/api/") {
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

// ===== 工具函数 =====

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.Index(fwd, ","); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return fwd
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

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
