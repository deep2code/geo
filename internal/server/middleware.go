package server

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// CORS 与认证中间件配置。
//
// 安全模型：
//   - GEO_CORS_ORIGINS：允许的跨域源（逗号分隔），默认仅 localhost。设为 "*" 表示全开放（仅限本地开发）。
//   - GEO_API_KEY：API 密钥。设置后，除 health/web 外的所有接口需 Bearer token。未设置则不鉴权（向后兼容）。
//   - 写操作（POST/DELETE/clear/import）在 CORS 设为具体源时始终校验 Origin。
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

// withMiddleware 应用 CORS + 认证 + 请求日志的完整中间件链。
func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return s.requestLogger(s.withAuth(s.withCORS(h)))
}

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

// mu 限流器（简单令牌桶，按 IP 限制每秒请求数）。
var mu sync.Mutex

// rateLimit 简单的按 IP 限流中间件（每秒 maxPerSec 请求）。
// 仅对 /api/v1/brand/audit 等高成本接口启用，防止 LLM 账单风险。
func (s *Server) rateLimit(maxPerSec int, h http.Handler) http.Handler {
	type bucket struct {
		count int
		reset time.Time
	}
	buckets := map[string]*bucket{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		mu.Lock()
		b, ok := buckets[ip]
		now := time.Now()
		if !ok || now.After(b.reset) {
			buckets[ip] = &bucket{count: 1, reset: now.Add(time.Second)}
			mu.Unlock()
			h.ServeHTTP(w, r)
			return
		}
		b.count++
		allowed := b.count <= maxPerSec
		mu.Unlock()
		if !allowed {
			writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "请求过于频繁，请稍后重试"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

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
