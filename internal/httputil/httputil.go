// Package httputil 提供 HTTP 辅助工具的统一实现。
//
// 收敛历史遗留的重复代码：internal/server 与 internal/auth 各有一份
// writeJSON/readJSON、clientIP/requestIP、可信代理解析与分页解析，
// 且行为存在漂移（请求体上限 10MB vs 1MB；可信代理默认策略不一致）。
// 统一到本包后，各模块只保留一行转发，杜绝行为漂移。
package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"my-geo/internal/config"
)

// DefaultMaxBody 默认请求体上限（10MB，与历史 server 实现一致；
// auth 原 1MB 一并统一到该值——JSON API 请求体远小于此，仅防 DoS）。
const DefaultMaxBody = 10 << 20

// WriteJSON 统一 JSON 响应输出。
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// ReadJSON 解析 JSON 请求体，上限 DefaultMaxBody。
func ReadJSON(r *http.Request, v any) error {
	return ReadJSONLimit(r, v, DefaultMaxBody)
}

// ReadJSONLimit 解析 JSON 请求体，显式指定上限 maxBytes（<=0 时用默认）。
// 相比 io.LimitReader 静默截断，这里多读 1 字节以区分"超限"与"解析失败"。
func ReadJSONLimit(r *http.Request, v any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(body) == 0 {
		return errors.New("请求体为空")
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("请求体过大（上限 %d 字节）", maxBytes)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}
	return nil
}

// ---- 可信代理与真实客户端 IP ----
//
// 策略：仅当 RemoteAddr 属于可信代理时才解析 X-Forwarded-For / X-Real-IP，
// 避免这些头被任意客户端伪造绕过审计/限流/WAF。

var (
	trustedProxyOnce sync.Once
	trustedProxyNets []netip.Prefix
)

// TrustedProxies 解析 GEO_TRUSTED_PROXIES（逗号分隔的 IP/CIDR）。
// 未设置时默认信任回环与私有子网（本机单机部署 + VPC Nginx Ingress / Cloudflare
// LB 常见场景），与历史 server 中间件行为一致；企业生产应显式精确配置。
func TrustedProxies() []netip.Prefix {
	trustedProxyOnce.Do(func() {
		raw := strings.TrimSpace(config.Env("GEO_TRUSTED_PROXIES", ""))
		if raw == "" {
			raw = "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,fc00::/7"
		}
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if !strings.Contains(p, "/") { // 纯 IP 自动补全掩码
				if addr, err := netip.ParseAddr(p); err == nil {
					trustedProxyNets = append(trustedProxyNets, netip.PrefixFrom(addr, addr.BitLen()))
				}
				continue
			}
			if prefix, err := netip.ParsePrefix(p); err == nil {
				trustedProxyNets = append(trustedProxyNets, prefix)
			} else {
				slog.Warn("GEO_TRUSTED_PROXIES 跳过非法条目",
					slog.String("entry", p), slog.String("error", err.Error()))
			}
		}
	})
	return trustedProxyNets
}

// IsTrustedProxy 判断一个 IP 是否属于可信代理列表。
func IsTrustedProxy(ipStr string) bool {
	if ipStr == "" {
		return false
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return false
	}
	for _, p := range TrustedProxies() {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// StripPort 从 "IP:port" / "[IPv6]:port" / "host:port" 中提取裸地址。
// 无端口字面（如裸 IPv6 "::1"）原样返回。
func StripPort(addr string) string {
	if addr == "" {
		return ""
	}
	if a, err := netip.ParseAddr(addr); err == nil { // 无端口字面（IPv4/IPv6）
		return a.String()
	}
	if strings.HasPrefix(addr, "[") { // [IPv6]:port 或 [IPv6]
		if i := strings.LastIndex(addr, "]"); i > 0 {
			return addr[1:i]
		}
		return addr
	}
	if i := strings.LastIndex(addr, ":"); i > 0 { // host:port
		return addr[:i]
	}
	return addr
}

// ClientIP 获取真实客户端 IP（仅信任来自可信代理的转发头）。
// 解析顺序：X-Forwarded-For 从左到右第一个非可信代理地址 → X-Real-IP → RemoteAddr。
func ClientIP(r *http.Request) string {
	remoteIP := StripPort(r.RemoteAddr)
	if !IsTrustedProxy(remoteIP) {
		return remoteIP
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		for i := range parts {
			ip := strings.TrimSpace(parts[i])
			if ip == "" {
				continue
			}
			if !IsTrustedProxy(ip) {
				return ip
			}
		}
		if len(parts) > 0 { // 所有段都是可信代理（极少数多层 LB），取第一个
			return strings.TrimSpace(parts[0])
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	return remoteIP
}

// ---- 分页 ----

// PageLimit 解析 page/limit 分页参数（page 1-based）。
// 非法或缺失回落默认；limit 超过 maxLimit 截断（maxLimit<=0 不截断）。
// page 设上限防止 (page-1)*limit 整型溢出为负导致切片越界 panic。
func PageLimit(r *http.Request, defaultLimit, maxLimit int) (page, limit int) {
	page = atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	if page > 1_000_000 {
		page = 1_000_000
	}
	limit = atoiDefault(r.URL.Query().Get("limit"), defaultLimit)
	if limit < 1 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}

// OffsetLimit 解析 offset/limit 分页参数（offset 0-based）。
// 同时兼容 page 参数（换算 offset=(page-1)*limit），便于前端统一。
// 非法或缺失回落默认；limit 超过 maxLimit 截断（maxLimit<=0 不截断）。
func OffsetLimit(r *http.Request, defaultLimit, maxLimit int) (offset, limit int) {
	limit = atoiDefault(r.URL.Query().Get("limit"), defaultLimit)
	if limit < 1 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			return n, limit
		}
	}
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			// page 设上限：超大 page 换算 (n-1)*limit 会整型溢出为负，
			// 负 OFFSET 直接打挂 SQL（与 PageLimit 同款防护）
			if n > 1_000_000 {
				n = 1_000_000
			}
			return (n - 1) * limit, limit
		}
	}
	return 0, limit
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
