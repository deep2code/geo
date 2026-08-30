// Package util 提供跨包复用的通用工具函数，消除各模块重复实现。
//
// 设计原则：
//   - 仅收录被 2 个及以上包重复实现的函数
//   - 优先委托给标准库，不重新发明
//   - 零依赖、无状态、可并行调用
package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ── 爬虫合规：共享 User-Agent（带避风港格式） ────────────────────────

const (
	// MyGEOUserAgent 所有对外爬虫请求统一使用的 User-Agent。
	// 格式符合 RFC 7231 及主流爬虫避风港约定：产品名/版本 (+信息页URL; 联系邮箱)。
	// 信息页 /legal/bot 由服务端注册，说明爬虫目的、频次、退出联系与 robots 遵循声明。
	MyGEOUserAgent = "MyGEOBot/1.0 (+/legal/bot; compliance@mygeo.ai)"

	// MyGEOCrawlInfoURL 对外可访问的爬虫说明页路径。
	MyGEOCrawlInfoURL = "/legal/bot"

	// MyGEOComplianceEmail 合规/退出联系邮箱。
	MyGEOComplianceEmail = "compliance@mygeo.ai"

	// defaultCrawlMinInterval 同一主机两次请求之间的最小间隔（礼貌爬取）。
	defaultCrawlMinInterval = 600 * time.Millisecond
)

// ── 每主机限频（礼貌爬取） ──────────────────────────────────────────

type hostRateLimiter struct {
	mu      sync.Mutex
	lastReq map[string]time.Time
	minInt  time.Duration
}

var sharedHostLimiter = &hostRateLimiter{
	lastReq: map[string]time.Time{},
	minInt:  defaultCrawlMinInterval,
}

// HostThrottle 阻塞直到当前 host 距离上次请求已至少 minInterval，用于跨包礼貌爬取。
// host 可为域名（含端口时取 host 部分）。线程安全。
func HostThrottle(host string) {
	host = cleanHost(host)
	if host == "" {
		return
	}
	sharedHostLimiter.mu.Lock()
	last := sharedHostLimiter.lastReq[host]
	sharedHostLimiter.lastReq[host] = time.Now()
	sharedHostLimiter.mu.Unlock()
	sleepFor := sharedHostLimiter.minInt - time.Since(last)
	if sleepFor > 0 {
		time.Sleep(sleepFor)
	}
}

// SetHostThrottleInterval 覆盖默认限频（仅用于测试/本地加速；生产环境不建议改）。
func SetHostThrottleInterval(d time.Duration) {
	if d > 0 {
		sharedHostLimiter.mu.Lock()
		sharedHostLimiter.minInt = d
		sharedHostLimiter.mu.Unlock()
	}
}

func cleanHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// ── robots.txt 简易允许判断（遵循 RFC 9309 基础子集） ─────────────

// RobotsAllows 拉取 scheme://host/robots.txt，判断 MyGEOBot 与 * 是否允许访问 path。
// 拉取失败 / robots 缺失 / 格式异常时默认返回 true（不阻塞业务），调用方应记录日志。
// 只做基础支持：User-Agent: MyGEOBot/* 行 + Disallow: 前缀匹配；不处理 Allow、Crawl-delay、Sitemap。
func RobotsAllows(ctx context.Context, rawBaseURL, path string) bool {
	u, err := url.Parse(strings.TrimRight(rawBaseURL, "/"))
	if err != nil || u.Host == "" {
		return true
	}
	robotURL := u.Scheme + "://" + u.Host + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotURL, nil)
	if err != nil {
		return true
	}
	req.Header.Set("User-Agent", MyGEOUserAgent)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return true
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false // 服务端报错：保守不爬
	}
	if resp.StatusCode == 404 {
		return true
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return robotsAllows(string(body), "MyGEOBot", path)
}

func robotsAllows(robotsTxt, bot, path string) bool {
	if strings.TrimSpace(robotsTxt) == "" {
		return true
	}
	lines := strings.Split(robotsTxt, "\n")
	var (
		inMyGroup    bool
		inStarGroup  bool
		myDisallow   []string
		starDisallow []string
	)
	flushStar := func() { inStarGroup = false }
	flushMy := func() { inMyGroup = false }
	// RFC 9309：连续多条 User-agent 行属于同一组。只有当前一组已出现
	// 规则行（Disallow/Allow）后，新 UA 行才开启新组——否则
	// "User-agent: MyGEOBot\nUser-agent: *\nDisallow: /private/" 这种
	// 同组等价写法会把规则漏记到错误的组，导致抓取被禁路径。
	sawRule := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "user-agent:") {
			if sawRule {
				flushStar()
				flushMy()
				sawRule = false
			}
			val := strings.TrimSpace(line[len("user-agent:"):])
			val = strings.SplitN(val, "#", 2)[0]
			val = strings.TrimSpace(val)
			if val == "*" {
				inStarGroup = true
			} else if strings.EqualFold(val, bot) {
				inMyGroup = true
			}
			continue
		}
		if strings.HasPrefix(low, "disallow:") {
			sawRule = true
			val := strings.TrimSpace(line[len("disallow:"):])
			val = strings.SplitN(val, "#", 2)[0]
			val = strings.TrimSpace(val)
			if val == "" {
				continue
			}
			if inMyGroup {
				myDisallow = append(myDisallow, val)
			} else if inStarGroup {
				starDisallow = append(starDisallow, val)
			}
			continue
		}
		// Allow / Crawl-delay / Sitemap 等当前忽略，不影响最小合规
	}
	disallowed := func(rules []string) bool {
		for _, r := range rules {
			// robots disallow 前缀匹配；支持末尾 * 通配，遇到 % 先不转码以保守匹配
			if strings.HasSuffix(r, "*") {
				prefix := strings.TrimSuffix(r, "*")
				if strings.HasPrefix(path, prefix) {
					return true
				}
				continue
			}
			if strings.HasPrefix(path, r) {
				return true
			}
		}
		return false
	}
	if len(myDisallow) > 0 {
		if disallowed(myDisallow) {
			return false
		}
		return true
	}
	if disallowed(starDisallow) {
		return false
	}
	return true
}

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

// RandomHexID 生成加密安全的 2n 位十六进制请求/追踪 ID。
// 默认 n=8 → 16 字符（64bit 熵，足够单机请求唯一）。
// 用于 requestID 中间件与异步任务关联。
func RandomHexID(n int) string {
	if n <= 0 {
		n = 8
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		// 极端回退：时间戳（不唯一但不应阻塞请求）
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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

// ── 句子切分工具 ─────────────────────────────────────────────────────

// sentenceSplitRe 匹配中英文句末标点（句号/感叹号/问号），捕获组含标点本身。
var sentenceSplitRe = regexp.MustCompile(`([。！？!?])`)

// SplitSentences 按中英文句末标点（。！？!?）切分单行为句子，保留标点在句尾。
// 返回的句子拼接后与原行等价；空行返回 []string{""} 以保持"每行至少一个元素"的语义。
func SplitSentences(line string) []string {
	if line == "" {
		return []string{""}
	}
	indices := sentenceSplitRe.FindAllStringSubmatchIndex(line, -1)
	if len(indices) == 0 {
		return []string{line}
	}
	var sentences []string
	prev := 0
	for _, idx := range indices {
		// idx[2]:idx[3] 为捕获组（标点）的范围
		end := idx[3]
		sentences = append(sentences, line[prev:end])
		prev = end
	}
	if prev < len(line) {
		sentences = append(sentences, line[prev:])
	}
	return sentences
}

// CountSentences 统计内容中的句子数量：优先按句末标点计数；
// 无标点时按非空行数兜底，两者皆无时返回 1。
func CountSentences(content string) int {
	matches := sentenceSplitRe.FindAllString(content, -1)
	if len(matches) == 0 {
		count := 0
		for _, line := range strings.Split(content, "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		if count == 0 {
			return 1
		}
		return count
	}
	return len(matches)
}
