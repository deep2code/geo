// Package crawler 提供官网爬虫能力，自动从品牌官网提取标题、描述、关键词、
// 产品线索等信息，用于品牌画像自动补全。
//
// 仅依赖标准库 net/http + regexp，零第三方依赖。
// HTTP 请求超时 10s；GuessDomain 并行尝试多个候选域名（brandname.com、
// getbrandname.com、brandname.cn、brandname.io），取第一个返回 200 的域名。
package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"my-geo/internal/util"
)

// httpTimeout HTTP 请求超时时间。
const httpTimeout = 10 * time.Second

// maxBodySize 单页内容最大读取字节数（1MB），避免超大页面耗内存。
const maxBodySize = 1 << 20

// maxProductHints 从 H1/H2/nav 中提取的产品线索上限。
const maxProductHints = 20

// WebsiteCrawler 官网爬虫，自动提取品牌信息。
type WebsiteCrawler struct {
	httpClient *http.Client
	maxDepth   int
}

// New 创建官网爬虫实例（默认配置：10s 超时、不深入子页面）。
func New() *WebsiteCrawler {
	return &WebsiteCrawler{
		httpClient: &http.Client{Timeout: httpTimeout},
		maxDepth:   1,
	}
}

// WebsiteInfo 官网提取信息。
type WebsiteInfo struct {
	Domain       string    `json:"domain"`
	Title        string    `json:"title"`         // <title> 标签内容
	Description  string    `json:"description"`   // meta description
	Keywords     []string  `json:"keywords"`      // 从 meta keywords + 页面文本提取
	ProductHints []string  `json:"product_hints"` // 疑似产品名（从 H1/H2/nav 提取）
	Language     string    `json:"language"`      // html lang 属性
	StatusCode   int       `json:"status_code"`
	FetchedAt    time.Time `json:"fetched_at"`
	Error        string    `json:"error,omitempty"`
}

// 预编译正则：大小写不敏感，DOTALL 模式让 . 匹配换行
var (
	// <html lang="zh-CN"> 或 <html lang='en'>
	reHTMLLang = regexp.MustCompile(`(?i)<html[^>]*\blang\s*=\s*["']([^"']+)["']`)
	// <title>...</title>
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	// <meta name="description" content="...">
	reMetaDesc = regexp.MustCompile(`(?is)<meta[^>]+name\s*=\s*["']description["'][^>]*>`)
	// <meta name="keywords" content="...">
	reMetaKeywords = regexp.MustCompile(`(?is)<meta[^>]+name\s*=\s*["']keywords["'][^>]*>`)
	// meta 标签中的 content 属性值
	reMetaContent = regexp.MustCompile(`(?i)content\s*=\s*["']([^"']*)["']`)
	// <h1>...</h1>
	reH1 = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	// <h2>...</h2>
	reH2 = regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>`)
	// <nav>...</nav>
	reNav = regexp.MustCompile(`(?is)<nav[^>]*>(.*?)</nav>`)
	// <a href="...">text</a>，提取链接文本
	reAnchorText = regexp.MustCompile(`(?is)<a[^>]*>(.*?)</a>`)
	// HTML 标签清理
	reTag = regexp.MustCompile(`(?is)<[^>]+>`)
	// 连续空白
	reWhitespace = regexp.MustCompile(`\s+`)
	// HTML 实体（仅处理常见几种）
	reEntityNbsp = regexp.MustCompile(`(?i)&nbsp;`)
	reEntityAmp  = regexp.MustCompile(`(?i)&amp;`)
	reEntityLt   = regexp.MustCompile(`(?i)&lt;`)
	reEntityGt   = regexp.MustCompile(`(?i)&gt;`)
	reEntityQuot = regexp.MustCompile(`(?i)&quot;`)
	reEntity39   = regexp.MustCompile(`(?i)&#39;`)
)

// Crawl 爬取官网，提取品牌信息。
// 自动从 HTML 中提取：title、meta description、域名、产品/服务关键词。
// 出错时返回带 Error 字段的 WebsiteInfo（不返回 error），便于调用方降级处理。
//
// 合规：请求前检查 robots.txt（MyGEOBot/* Disallow），并应用跨包每主机限频（默认 600ms），
// 使用合规 User-Agent MyGEOBot/1.0（避风港格式，含信息页 + 联系邮箱）。
func (c *WebsiteCrawler) Crawl(ctx context.Context, domain string) (*WebsiteInfo, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("crawler: domain 不能为空")
	}
	// 规范化：去掉协议前缀和路径，仅保留主机名
	domain = stripSchemeAndPath(domain)
	if domain == "" {
		return nil, fmt.Errorf("crawler: 无效的 domain %q", domain)
	}

	info := &WebsiteInfo{
		Domain:    domain,
		FetchedAt: time.Now(),
	}

	// 构建 URL（默认 https，失败回退 http）
	rawURL := "https://" + domain + "/"
	if err := util.ValidateExternalURL(rawURL); err != nil {
		info.Error = err.Error()
		return info, nil
	}

	// ── 合规：robots + 限频 ────────────────────────────────────
	if !util.RobotsAllows(ctx, rawURL, "/") {
		info.Error = fmt.Sprintf("crawler: robots.txt 禁止 MyGEOBot 访问 %s", domain)
		return info, nil
	}
	util.HostThrottle(domain)

	body, status, err := c.fetchHTML(ctx, rawURL)
	// https 失败或返回非 2xx（WAF 反爬拦截页）同样回退 http：
	// 否则拦截页会被解析成品牌 Title/Description 污染画像
	if err != nil || status < 200 || status >= 300 {
		rawURL = "http://" + domain + "/"
		if util.RobotsAllows(ctx, rawURL, "/") {
			util.HostThrottle(domain)
			body2, status2, err2 := c.fetchHTML(ctx, rawURL)
			if err2 != nil || status2 < 200 || status2 >= 300 {
				info.StatusCode = status
				info.Error = fmt.Sprintf("https 与 http 均不可用: %v(状态 %d); %v(状态 %d)", err, status, err2, status2)
				return info, nil
			}
			body, status = body2, status2
		} else {
			info.StatusCode = status
			info.Error = fmt.Sprintf("https 不可用 %v；http robots 禁止访问", err)
			return info, nil
		}
	}
	info.StatusCode = status

	// 解析 HTML
	info.Language = extractFirstGroup(reHTMLLang, body)
	info.Title = decodeEntities(stripTags(extractFirstGroup(reTitle, body)))
	info.Description = decodeEntities(stripTags(extractMetaContent(reMetaDesc, body)))
	info.Keywords = parseKeywords(decodeEntities(stripTags(extractMetaContent(reMetaKeywords, body))))
	info.ProductHints = extractProductHints(body)
	return info, nil
}

// GuessDomain 猜测品牌域名。
// 并行尝试多个候选域名（brandname.com、getbrandname.com、brandname.cn、brandname.io），
// 返回第一个返回 HTTP 200 的域名。全部失败时返回 error。
func (c *WebsiteCrawler) GuessDomain(ctx context.Context, brandName string) (string, error) {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		return "", fmt.Errorf("crawler: brandName 不能为空")
	}
	base := sanitizeBrandName(brandName)
	if base == "" {
		return "", fmt.Errorf("crawler: 品牌名 sanitize 后为空")
	}
	candidates := []string{
		base + ".com",
		"get" + base + ".com",
		base + ".cn",
		base + ".io",
	}

	type result struct {
		domain string
		ok     bool
	}

	// 用 channel 控制并发，第一个返回 200 的胜出
	resultCh := make(chan result, len(candidates))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for _, dom := range candidates {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			rawURL := "https://" + d + "/"
			// SSRF 防护
			if err := util.ValidateExternalURL(rawURL); err != nil {
				resultCh <- result{domain: d, ok: false}
				return
			}
			// 合规：robots 预检 + 跨包限频（避免并发打爆域名）
			if !util.RobotsAllows(ctx, rawURL, "/") {
				resultCh <- result{domain: d, ok: false}
				return
			}
			util.HostThrottle(d)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				resultCh <- result{domain: d, ok: false}
				return
			}
			req.Header.Set("User-Agent", util.MyGEOUserAgent)
			resp, err := c.httpClient.Do(req)
			if err != nil {
				resultCh <- result{domain: d, ok: false}
				return
			}
			defer resp.Body.Close()
			// 读取并丢弃 body，避免连接复用问题
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resultCh <- result{domain: d, ok: resp.StatusCode == http.StatusOK}
		}(dom)
	}

	// 等所有请求完成
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果，返回第一个 ok 的
	var tried []string
	for r := range resultCh {
		if r.ok {
			// 取消其他在途请求
			cancel()
			return r.domain, nil
		}
		tried = append(tried, r.domain)
	}
	return "", fmt.Errorf("crawler: 所有候选域名均不可达: %s", strings.Join(tried, ", "))
}

// fetchHTML 抓取指定 URL 的 HTML 文本与状态码。
func (c *WebsiteCrawler) fetchHTML(ctx context.Context, rawURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", util.MyGEOUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(data), resp.StatusCode, nil
}

// stripSchemeAndPath 去掉 URL 的协议与路径，仅保留主机名（含端口若有）。
// 例如 "https://www.example.com/path" → "www.example.com"
func stripSchemeAndPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 去掉协议
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// 去掉 path
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	// 去掉 query
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sanitizeBrandName 品牌名清理：转小写、去空格与特殊字符。
// 例如 "My Brand!" → "mybrand"
func sanitizeBrandName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	// 仅保留 a-z 0-9，其余字符全部移除
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// extractFirstGroup 用正则提取第一个匹配的第一个捕获组内容。
func extractFirstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractMetaContent 从 meta 标签片段中提取 content 属性值。
func extractMetaContent(re *regexp.Regexp, s string) string {
	tag := re.FindString(s)
	if tag == "" {
		return ""
	}
	return extractFirstGroup(reMetaContent, tag)
}

// stripTags 移除所有 HTML 标签，并将连续空白压缩为单空格。
func stripTags(s string) string {
	if s == "" {
		return ""
	}
	s = reTag.ReplaceAllString(s, " ")
	s = reWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// decodeEntities 解码常见 HTML 实体（与 server.go 中 cmsStripHTMLTags 一致）。
func decodeEntities(s string) string {
	s = reEntityNbsp.ReplaceAllString(s, " ")
	s = reEntityAmp.ReplaceAllString(s, "&")
	s = reEntityLt.ReplaceAllString(s, "<")
	s = reEntityGt.ReplaceAllString(s, ">")
	s = reEntityQuot.ReplaceAllString(s, "\"")
	s = reEntity39.ReplaceAllString(s, "'")
	return s
}

// parseKeywords 将 meta keywords 字符串拆分为去重的小写关键词切片。
// 输入形如 "CRM, SaaS, 项目管理" → ["crm","saas","项目管理"]。
func parseKeywords(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 保留原始大小写（产品名/专有名词可能含大写）
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// extractProductHints 从 H1/H2/nav 中提取疑似产品名/导航项。
func extractProductHints(html string) []string {
	if html == "" {
		return nil
	}
	var fragments []string

	// 收集所有 H1/H2 文本
	for _, m := range reH1.FindAllStringSubmatch(html, -1) {
		if len(m) >= 2 {
			fragments = append(fragments, m[1])
		}
	}
	for _, m := range reH2.FindAllStringSubmatch(html, -1) {
		if len(m) >= 2 {
			fragments = append(fragments, m[1])
		}
	}
	// 收集 nav 内的 a 标签文本
	for _, m := range reNav.FindAllStringSubmatch(html, -1) {
		if len(m) >= 2 {
			navHTML := m[1]
			for _, am := range reAnchorText.FindAllStringSubmatch(navHTML, -1) {
				if len(am) >= 2 {
					fragments = append(fragments, am[1])
				}
			}
		}
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(fragments))
	for _, f := range fragments {
		t := decodeEntities(stripTags(f))
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// 过滤过短或过长文本
		if len([]rune(t)) < 2 || len([]rune(t)) > 40 {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= maxProductHints {
			break
		}
	}
	return out
}
