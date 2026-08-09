// Package util 提供跨包复用的通用工具函数，消除各模块重复实现。
//
// 设计原则：
//   - 仅收录被 2 个及以上包重复实现的函数
//   - 优先委托给标准库，不重新发明
//   - 零依赖、无状态、可并行调用
package util

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"
)

// HumanBytes 将字节数格式化为人类可读字符串（如 "1.5 MB"）。
// 采用 1024 进制，单位 B/KB/MB/GB/TB/PB。
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for d := n / unit; d >= unit; d /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// Truncate 截断字节切片 b 至最多 maxBytes 字节，并在末尾加 "…"（若被截断）。
// 注意：按字节截断可能切断多字节 UTF-8 字符，仅在日志/预览场景使用。
func Truncate(b []byte, maxBytes int) []byte {
	if len(b) <= maxBytes {
		return b
	}
	if maxBytes <= 3 {
		return []byte("…")
	}
	out := append([]byte{}, b[:maxBytes-3]...)
	out = append(out, []byte("…")...)
	return out
}

// TruncateStr 截断字符串 s 至最多 maxRunes 个 rune，并在末尾加 "…"（若被截断）。
// 按 rune 截断，不会切断多字节字符，适合中文等文本。
func TruncateStr(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:maxRunes-1]) + "…"
}

// FirstNonEmpty 返回第一个非空字符串，全部为空则返回空串。
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ScoreToGrade 将 0-100 分数转为等级（A-F）。
//
// 区间：A 90+，B 80-89，C 70-79，D 60-69，F <60。
func ScoreToGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// ParseBool 解析常见布尔字符串（"true"/"1"/"yes" 为真，其余为假）。
// 用于统一环境变量布尔解析，避免各包混用 strings.EqualFold。
func ParseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

// IndexOf 返回 sub 在 s 中首次出现的字节索引，未找到返回 -1。
// 直接委托 strings.Index，避免手写循环。
func IndexOf(s, sub string) int {
	if sub == "" {
		return 0
	}
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return i
}

// LastIndexOf 返回 sub 在 s 中最后出现的字节索引，未找到返回 -1。
// 直接委托 strings.LastIndex。
func LastIndexOf(s, sub string) int {
	if sub == "" {
		return len(s)
	}
	i := strings.LastIndex(s, sub)
	if i < 0 {
		return -1
	}
	return i
}

// IsPrivateOrLoopbackHost 判断 URL 的主机是否为内网/回环/链路本地地址。
// 用于防止 SSRF：拒绝访问 127.0.0.0/8、10.0.0.0/8、172.16.0.0/12、
// 192.168.0.0/16、169.254.0.0/16、::1、fc00::/7 等。
//
// hostname 可以是域名或 IP；域名会尝试解析。解析失败返回 false（放行），
// 由调用方决定是否额外限制。
func IsPrivateOrLoopbackHost(hostname string) bool {
	// 去掉端口
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = h
	}
	hostname = strings.TrimSpace(hostname)
	// localhost 直接判为内网
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		// 域名：尝试解析，解析失败不阻断（避免误伤）
		ips, err := net.LookupIP(hostname)
		if err != nil || len(ips) == 0 {
			return false
		}
		ip = ips[0]
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// ValidateExternalURL 校验外部 URL 是否安全（防 SSRF）。
// 返回 nil 表示安全可访问，返回 error 表示应拒绝。
// 规则：必须是 http/https；主机不能为内网/回环。
func ValidateExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("无效 URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("仅允许 http/https 协议，拒绝 %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}
	if IsPrivateOrLoopbackHost(u.Host) {
		return fmt.Errorf("拒绝访问内网地址 %q（SSRF 防护）", u.Host)
	}
	return nil
}
