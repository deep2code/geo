// Package readiness AI 可见度就绪审计。
//
// 检查一个网站对 AI 搜索引擎（ChatGPT / Claude / Perplexity / Gemini 等）的
// "可见度就绪度"，覆盖 8 个维度（参考 foglift-scan 的就绪审计思路）：
//   - robots.txt 是否屏蔽 AI 爬虫（GPTBot/ClaudeBot/PerplexityBot/CCBot/Googlebot-Ext）
//   - 是否有 /llms.txt 文件（面向大语言模型的站点摘要）
//   - 页面是否有结构化数据（JSON-LD / Microdata）
//   - 是否有 sitemap.xml
//   - 页面加载性能（首字节时间 TTFB）
//   - 标题清晰度（H1 唯一性 + H2/H3 层级）
//   - FAQ 质量（FAQPage schema 或问答文本模式）
//   - 实体身份（Organization schema / sameAs / logo）
//
// 每个检查项带有 severity（critical/high/medium/low）与 ci_blocking 标记，
// 可通过 CIGateScore / CIGateReport 在 CI/CD 流水线中作为发布门禁使用。
//
// 仅依赖 net/http 标准库，请求超时 10 秒。
package readiness

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"time"

	"my-geo/internal/util"
)

// CheckResult 单项检查结果。
type CheckResult struct {
	Name       string  `json:"name"`                  // 检查项名称
	Status     string  `json:"status"`                // "pass" / "fail" / "warn"
	Score      float64 `json:"score"`                 // 0-100
	Detail     string  `json:"detail"`                // 详细说明
	Evidence   string  `json:"evidence"`              // 证据（如 robots.txt 内容片段）
	Severity   string  `json:"severity,omitempty"`    // critical/high/medium/low
	CIBlocking bool    `json:"ci_blocking,omitempty"` // 若为 true，该项失败时 CI 门禁应失败
}

// AuditResult 一次就绪审计的完整结果。
type AuditResult struct {
	URL        string        `json:"url"`
	TotalScore float64       `json:"total_score"` // 0-100 综合就绪度
	Grade      string        `json:"grade"`       // A-F
	Checks     []CheckResult `json:"checks"`
	AuditedAt  time.Time     `json:"audited_at"`
}

// CIGateResult CI 门禁判定结果，供 CI/CD 流水线使用。
type CIGateResult struct {
	Passed         bool          `json:"passed"`          // 是否通过门禁
	Score          float64       `json:"score"`           // 综合得分
	Threshold      float64       `json:"threshold"`       // 通过阈值
	BlockingIssues []CheckResult `json:"blocking_issues"` // 触发 ci_blocking 的失败项
	Summary        string        `json:"summary"`         // 人类可读汇总
}

// 检查项名称常量（同时用于权重映射）。
const (
	checkNameRobots     = "robots.txt AI 爬虫检查"
	checkNameLlmsTxt    = "llms.txt"
	checkNameStructured = "结构化数据"
	checkNameSitemap    = "sitemap.xml"
	checkNameTTFB       = "页面性能 (TTFB)"
	checkNameHeading    = "标题清晰度"
	checkNameFAQ        = "FAQ 质量"
	checkNameEntity     = "实体身份"
)

// 检查项权重（8 维度合计 1.0）。
const (
	weightRobots     = 0.15 // robots.txt AI 爬虫检查
	weightLlmsTxt    = 0.15 // llms.txt 存在性
	weightStructured = 0.15 // 结构化数据
	weightSitemap    = 0.10 // sitemap.xml
	weightTTFB       = 0.10 // 页面性能 TTFB
	weightHeading    = 0.10 // 标题清晰度
	weightFAQ        = 0.10 // FAQ 质量
	weightEntity     = 0.15 // 实体身份
)

// 默认 CI 门禁阈值。
const defaultCIThreshold = 60

// DefaultCIThreshold 返回默认 CI 门禁阈值（60 分），供外部包复用。
func DefaultCIThreshold() float64 { return defaultCIThreshold }

// AIBots 需要检查是否被屏蔽的 AI 爬虫列表。
// 评分依据前 4 个（GPTBot/ClaudeBot/PerplexityBot/CCBot），Googlebot-Ext 仅作证据展示。
var AIBots = []string{"GPTBot", "ClaudeBot", "PerplexityBot", "CCBot", "Googlebot-Ext"}

// mainAIBots 用于评分的 4 个核心 AI 爬虫（与权重说明一致）。
var mainAIBots = []string{"GPTBot", "ClaudeBot", "PerplexityBot", "CCBot"}

// socialDomains 用于实体身份检查的常见社交主页域名。
var socialDomains = []string{
	"twitter.com", "x.com", "facebook.com", "linkedin.com",
	"github.com", "youtube.com", "instagram.com", "weibo.com",
	"tiktok.com", "threads.net",
}

// maxBodyBytes 单次响应体读取上限（避免拉取超大页面耗尽内存）。
const maxBodyBytes = 2 << 20 // 2MB

// insecureTLS 控制是否跳过 TLS 证书验证。
// 默认 false（安全，启用证书验证）；通过环境变量 GEO_READINESS_INSECURE_TLS=true
// 显式开启，仅用于内网/自签名证书测试场景。生产环境务必保持默认。
var insecureTLS = parseInsecureTLS()

func parseInsecureTLS() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GEO_READINESS_INSECURE_TLS")))
	return v == "true" || v == "1" || v == "yes"
}

// httpClient 共享 HTTP 客户端（10 秒超时）。
// 默认启用 TLS 证书验证；GEO_READINESS_INSECURE_TLS=true 时跳过（仅限测试）。
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureTLS},
	},
}

const userAgent = "geo-readiness-auditor/1.0 (+https://github.com/my-geo)"

// Audit 对指定 URL 执行 AI 可见度就绪审计。
//
// rawURL 无 scheme 时自动补 https://。返回综合评分与各检查项明细。
func Audit(ctx context.Context, rawURL string) (*AuditResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("readiness: URL 不能为空")
	}
	// 自动补全 scheme
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("readiness: 无效 URL %q: %w", rawURL, err)
	}
	// SSRF 防护：拒绝审计内网/回环地址（除非显式开启 GEO_READINESS_INSECURE_TLS）
	if !insecureTLS {
		if err := util.ValidateExternalURL(rawURL); err != nil {
			return nil, fmt.Errorf("readiness: %w", err)
		}
	}
	baseURL := u.Scheme + "://" + u.Host

	result := &AuditResult{
		URL:       rawURL,
		AuditedAt: time.Now(),
		Checks:    make([]CheckResult, 0, 8),
	}

	// 1. robots.txt 检查（15%）
	robotsCh := checkRobots(ctx, baseURL)
	// 2. llms.txt 检查（15%）
	llmsCh := checkLlmsTxt(ctx, baseURL)
	// 3. 结构化数据 + 4. TTFB + 6. 标题清晰度 + 7. FAQ 质量 + 8. 实体身份
	// （共用一次主页请求，减少开销）
	structuredCh, ttfbCh, headingCh, faqCh, entityCh := checkMainPage(ctx, rawURL)
	// 5. sitemap.xml 检查（10%）
	sitemapCh := checkSitemap(ctx, baseURL)

	// 按规范顺序追加（与权重说明一致）
	result.Checks = append(result.Checks,
		robotsCh, llmsCh, structuredCh, sitemapCh, ttfbCh,
		headingCh, faqCh, entityCh,
	)

	// 加权汇总总分
	var total float64
	for _, c := range result.Checks {
		total += c.Score * weightOf(c.Name)
	}
	result.TotalScore = total
	result.Grade = scoreToGrade(total)

	return result, nil
}

// weightOf 按检查项名称返回权重。
func weightOf(name string) float64 {
	switch name {
	case checkNameRobots:
		return weightRobots
	case checkNameLlmsTxt:
		return weightLlmsTxt
	case checkNameStructured:
		return weightStructured
	case checkNameSitemap:
		return weightSitemap
	case checkNameTTFB:
		return weightTTFB
	case checkNameHeading:
		return weightHeading
	case checkNameFAQ:
		return weightFAQ
	case checkNameEntity:
		return weightEntity
	}
	return 0
}

// scoreToGrade 将综合分数转为等级。
// 映射：>=80=A, >=60=B, >=40=C, >=20=D, <20=F。
func scoreToGrade(s float64) string {
	switch {
	case s >= 80:
		return "A"
	case s >= 60:
		return "B"
	case s >= 40:
		return "C"
	case s >= 20:
		return "D"
	default:
		return "F"
	}
}

// CIGateScore 判断审计结果是否通过 CI 门禁。
//
// 通过条件：综合得分 >= threshold，且不存在任何 ci_blocking 标记的失败项
// （ci_blocking=true 且 status=fail 视为硬性阻断）。
func CIGateScore(result *AuditResult, threshold float64) bool {
	if result == nil {
		return false
	}
	if result.TotalScore < threshold {
		return false
	}
	for _, c := range result.Checks {
		if c.CIBlocking && c.Status == "fail" {
			return false
		}
	}
	return true
}

// CIGateReport 生成 CI 门禁报告（默认阈值 60）。
//
// 返回是否通过、综合得分、阈值、阻断项列表与人类可读汇总。
func CIGateReport(result *AuditResult) *CIGateResult {
	return CIGateReportWithThreshold(result, defaultCIThreshold)
}

// CIGateReportWithThreshold 按指定阈值生成 CI 门禁报告。
func CIGateReportWithThreshold(result *AuditResult, threshold float64) *CIGateResult {
	gate := &CIGateResult{
		Threshold:      threshold,
		BlockingIssues: []CheckResult{},
	}
	if result == nil {
		gate.Passed = false
		gate.Summary = "审计结果为空，门禁未通过"
		return gate
	}
	gate.Score = result.TotalScore
	gate.Passed = CIGateScore(result, threshold)

	for _, c := range result.Checks {
		if c.CIBlocking && c.Status == "fail" {
			gate.BlockingIssues = append(gate.BlockingIssues, c)
		}
	}

	switch {
	case !gate.Passed && result.TotalScore < threshold && len(gate.BlockingIssues) > 0:
		gate.Summary = fmt.Sprintf(
			"门禁未通过：综合得分 %.1f 低于阈值 %.1f，且存在 %d 项阻断问题。",
			result.TotalScore, threshold, len(gate.BlockingIssues))
	case !gate.Passed && result.TotalScore < threshold:
		gate.Summary = fmt.Sprintf(
			"门禁未通过：综合得分 %.1f 低于阈值 %.1f。",
			result.TotalScore, threshold)
	case !gate.Passed:
		gate.Summary = fmt.Sprintf(
			"门禁未通过：综合得分 %.1f 达标，但存在 %d 项 ci_blocking 失败项。",
			result.TotalScore, len(gate.BlockingIssues))
	default:
		gate.Summary = fmt.Sprintf(
			"门禁通过：综合得分 %.1f（阈值 %.1f），无 ci_blocking 失败项。",
			result.TotalScore, threshold)
	}
	return gate
}

// ---------- 检查项实现 ----------

// checkRobots 检查 robots.txt 是否屏蔽 AI 爬虫。
// 对 AI 可见度而言：未被屏蔽 = pass（高分），被屏蔽 = fail（低分）。
func checkRobots(ctx context.Context, baseURL string) CheckResult {
	const name = checkNameRobots
	body, status, err := fetchText(ctx, baseURL+"/robots.txt")
	if err != nil {
		// 无法获取 robots.txt → 视为未屏蔽（中性 warn）
		return CheckResult{
			Name:   name,
			Status: "warn",
			Score:  50,
			Detail: fmt.Sprintf("无法获取 robots.txt（%v），假定未屏蔽 AI 爬虫", err),
		}
	}
	if status != http.StatusOK {
		return CheckResult{
			Name:     name,
			Status:   "pass",
			Score:    100,
			Detail:   fmt.Sprintf("robots.txt 返回 HTTP %d（视为未屏蔽 AI 爬虫）", status),
			Evidence: truncate(body, 500),
		}
	}
	blocked := parseRobotsBlocked(body, AIBots)
	// 按核心 4 个爬虫计算评分
	blockedCount := 0
	var blockedNames, allowedNames []string
	for _, b := range mainAIBots {
		if blocked[b] {
			blockedCount++
			blockedNames = append(blockedNames, b)
		} else {
			allowedNames = append(allowedNames, b)
		}
	}
	// Googlebot-Ext 仅作证据展示
	geBlocked := blocked["Googlebot-Ext"]

	score := float64(len(mainAIBots)-blockedCount) / float64(len(mainAIBots)) * 100
	var checkStatus, detail, severity string
	var ciBlocking bool
	switch {
	case blockedCount == 0:
		checkStatus = "pass"
		detail = "未屏蔽任何核心 AI 爬虫（GPTBot/ClaudeBot/PerplexityBot/CCBot 均可抓取）"
	case blockedCount == len(mainAIBots):
		checkStatus = "fail"
		detail = "全部核心 AI 爬虫被屏蔽：" + strings.Join(blockedNames, "、")
		// 完全屏蔽 AI 爬虫 → 关键阻断，CI 门禁应失败
		severity = "critical"
		ciBlocking = true
	default:
		checkStatus = "warn"
		detail = fmt.Sprintf("部分 AI 爬虫被屏蔽：%s；放行：%s",
			strings.Join(blockedNames, "、"), strings.Join(allowedNames, "、"))
	}
	if geBlocked {
		detail += "；Googlebot-Ext 也被屏蔽"
	}
	return CheckResult{
		Name:       name,
		Status:     checkStatus,
		Score:      score,
		Detail:     detail,
		Evidence:   truncate(body, 500),
		Severity:   severity,
		CIBlocking: ciBlocking,
	}
}

// checkLlmsTxt 检查 /llms.txt 是否存在且非空。
func checkLlmsTxt(ctx context.Context, baseURL string) CheckResult {
	const name = checkNameLlmsTxt
	body, status, err := fetchText(ctx, baseURL+"/llms.txt")
	if err != nil {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     fmt.Sprintf("无法获取 /llms.txt: %v", err),
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	if status != http.StatusOK {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     fmt.Sprintf("/llms.txt 返回 HTTP %d（未提供）", status),
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	if strings.TrimSpace(body) == "" {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     "/llms.txt 存在但内容为空",
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	return CheckResult{
		Name:     name,
		Status:   "pass",
		Score:    100,
		Detail:   fmt.Sprintf("/llms.txt 存在且非空（%d 字节）", len(body)),
		Evidence: truncate(body, 500),
	}
}

// checkMainPage 检查主页结构化数据、TTFB、标题清晰度、FAQ 质量与实体身份（共用一次请求）。
func checkMainPage(ctx context.Context, pageURL string) (structured, ttfb, heading, faq, entity CheckResult) {
	structured.Name = checkNameStructured
	ttfb.Name = checkNameTTFB
	heading.Name = checkNameHeading
	faq.Name = checkNameFAQ
	entity.Name = checkNameEntity

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		structured.Status, structured.Score = "fail", 0
		structured.Detail = fmt.Sprintf("请求构建失败: %v", err)
		ttfb.Status, ttfb.Score = "fail", 0
		ttfb.Detail = fmt.Sprintf("请求构建失败: %v", err)
		heading.Status, heading.Score = "fail", 0
		heading.Detail = fmt.Sprintf("请求构建失败: %v", err)
		faq.Status, faq.Score = "fail", 0
		faq.Detail = fmt.Sprintf("请求构建失败: %v", err)
		entity.Status, entity.Score = "fail", 0
		entity.Detail = fmt.Sprintf("请求构建失败: %v", err)
		return
	}
	req.Header.Set("User-Agent", userAgent)

	// 用 httptrace 捕获首字节时间（从发起请求到收到第一个响应字节）
	reqStart := time.Now()
	var ttfbDur time.Duration
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			ttfbDur = time.Since(reqStart)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := httpClient.Do(req)
	if err != nil {
		structured.Status, structured.Score = "fail", 0
		structured.Detail = fmt.Sprintf("主页请求失败: %v", err)
		ttfb.Status, ttfb.Score = "fail", 0
		ttfb.Detail = fmt.Sprintf("主页请求失败: %v", err)
		heading.Status, heading.Score = "fail", 0
		heading.Detail = fmt.Sprintf("主页请求失败: %v", err)
		faq.Status, faq.Score = "fail", 0
		faq.Detail = fmt.Sprintf("主页请求失败: %v", err)
		entity.Status, entity.Score = "fail", 0
		entity.Detail = fmt.Sprintf("主页请求失败: %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		structured.Status, structured.Score = "fail", 0
		structured.Detail = fmt.Sprintf("读取响应体失败: %v", err)
		ttfb.Status, ttfb.Score = "fail", 0
		ttfb.Detail = fmt.Sprintf("读取响应体失败: %v", err)
		heading.Status, heading.Score = "fail", 0
		heading.Detail = fmt.Sprintf("读取响应体失败: %v", err)
		faq.Status, faq.Score = "fail", 0
		faq.Detail = fmt.Sprintf("读取响应体失败: %v", err)
		entity.Status, entity.Score = "fail", 0
		entity.Detail = fmt.Sprintf("读取响应体失败: %v", err)
		return
	}
	html := string(body)

	// --- 结构化数据检查 ---
	hasJSONLD := strings.Contains(html, "application/ld+json")
	hasMicrodata := strings.Contains(html, "itemtype=")
	switch {
	case hasJSONLD:
		structured.Status = "pass"
		structured.Score = 100
		structured.Detail = "页面包含 JSON-LD 结构化数据（application/ld+json）"
		structured.Evidence = extractSnippet(html, "application/ld+json", 300)
	case hasMicrodata:
		structured.Status = "warn"
		structured.Score = 70
		structured.Detail = "页面包含 Microdata（itemtype）但未发现 JSON-LD，建议补充 JSON-LD 以提升 AI 解析友好度"
		structured.Evidence = "Microdata itemtype detected"
	default:
		structured.Status = "fail"
		structured.Score = 0
		structured.Detail = "页面未发现结构化数据（JSON-LD / Microdata 均缺失）"
		// 缺失结构化数据 → 高严重度，CI 门禁应失败
		structured.Severity = "high"
		structured.CIBlocking = true
	}

	// --- TTFB 检查 ---
	secs := ttfbDur.Seconds()
	switch {
	case secs < 1:
		ttfb.Status = "pass"
		ttfb.Score = 100
	case secs < 3:
		ttfb.Status = "warn"
		ttfb.Score = 50
	default:
		ttfb.Status = "fail"
		ttfb.Score = 0
		// TTFB > 3s → 高严重度，CI 门禁应失败
		ttfb.Severity = "high"
		ttfb.CIBlocking = true
	}
	ttfb.Detail = fmt.Sprintf("TTFB = %.3f 秒（阈值: <1s pass, <3s warn, ≥3s fail）", secs)
	ttfb.Evidence = fmt.Sprintf("HTTP %d | TTFB %dms", resp.StatusCode, ttfbDur.Milliseconds())

	// --- 标题清晰度检查 ---
	heading = checkHeadingClarity(html)

	// --- FAQ 质量检查 ---
	faq = checkFAQQuality(html)

	// --- 实体身份检查 ---
	entity = checkEntityIdentity(html)

	return
}

// checkHeadingClarity 检查页面标题层级清晰度。
//   - 恰好 1 个 H1 + 多个 H2 → 100
//   - 多个 H1 → 70
//   - 无 H1（但有其他标题）→ 30
//   - 完全无标题 → 0
func checkHeadingClarity(html string) CheckResult {
	const name = checkNameHeading
	h1 := countTagCI(html, "h1")
	h2 := countTagCI(html, "h2")
	h3 := countTagCI(html, "h3")
	total := h1 + h2 + h3

	var status, detail, severity string
	var score float64
	switch {
	case total == 0:
		status = "fail"
		score = 0
		detail = "页面未发现任何标题标签（h1/h2/h3 均缺失）"
		severity = "high"
	case h1 == 0:
		status = "warn"
		score = 30
		detail = fmt.Sprintf("页面缺少 H1（h2=%d, h3=%d），AI 与搜索引擎难以识别主题", h2, h3)
		severity = "medium"
	case h1 > 1:
		status = "warn"
		score = 70
		detail = fmt.Sprintf("页面存在多个 H1（h1=%d），建议仅保留 1 个作为主标题", h1)
		severity = "medium"
	case h1 == 1 && h2 >= 2:
		status = "pass"
		score = 100
		detail = fmt.Sprintf("标题层级良好（1 个 H1，%d 个 H2，%d 个 H3）", h2, h3)
		severity = "low"
	default: // h1 == 1 && h2 < 2
		status = "pass"
		score = 80
		detail = fmt.Sprintf("有 1 个 H1，但 H2 较少（h2=%d, h3=%d），层级略单薄", h2, h3)
		severity = "low"
	}
	return CheckResult{
		Name:     name,
		Status:   status,
		Score:    score,
		Detail:   detail,
		Evidence: fmt.Sprintf("h1=%d h2=%d h3=%d", h1, h2, h3),
		Severity: severity,
	}
}

// checkFAQQuality 检查页面是否包含 FAQ 内容。
//   - FAQPage JSON-LD schema → 100
//   - 问答文本模式（多个问号）→ 70
//   - 仅 FAQ 相关标题 → 30
//   - 无任何 FAQ 信号 → 0
func checkFAQQuality(html string) CheckResult {
	const name = checkNameFAQ
	lower := strings.ToLower(html)

	// 1. FAQPage 结构化数据
	hasFAQSchema := strings.Contains(lower, "application/ld+json") &&
		strings.Contains(lower, "faqpage")

	// 2. 问答文本模式：全角"？"≥2 或半角"?"≥5（半角易出现在 URL/JS 中，阈值更高）
	fullWidthQ := strings.Count(html, "？")
	halfWidthQ := strings.Count(html, "?")
	hasQAPattern := fullWidthQ >= 2 || halfWidthQ >= 5

	// 3. FAQ 相关标题
	headingText := extractHeadingsTextCI(html)
	hasFAQHeading := false
	for _, kw := range []string{"faq", "常见问题", "常见问答", "问答", "q&a", "q & a"} {
		if strings.Contains(headingText, kw) {
			hasFAQHeading = true
			break
		}
	}

	var status, detail, severity string
	var score float64
	var evidence string
	switch {
	case hasFAQSchema:
		status = "pass"
		score = 100
		detail = "页面包含 FAQPage JSON-LD 结构化数据，AI 可直接解析问答"
		evidence = extractSnippet(html, "application/ld+json", 200)
		severity = "low"
	case hasQAPattern:
		status = "pass"
		score = 70
		detail = fmt.Sprintf("检测到问答文本模式（？=%d, ?=%d），建议补充 FAQPage schema", fullWidthQ, halfWidthQ)
		evidence = fmt.Sprintf("？=%d ?=%d", fullWidthQ, halfWidthQ)
		severity = "low"
	case hasFAQHeading:
		status = "warn"
		score = 30
		detail = "仅发现 FAQ 相关标题，未检测到问答正文或 FAQPage schema"
		evidence = "FAQ heading detected"
		severity = "low"
	default:
		status = "fail"
		score = 0
		detail = "未检测到任何 FAQ 信号（无 FAQPage schema、无问答文本、无 FAQ 标题）"
		severity = "medium"
	}
	return CheckResult{
		Name:     name,
		Status:   status,
		Score:    score,
		Detail:   detail,
		Evidence: evidence,
		Severity: severity,
	}
}

// checkEntityIdentity 检查页面是否具备清晰的实体身份信号。
//   - Organization schema + sameAs + logo → 100
//   - Organization schema（缺 sameAs 或 logo）→ 70
//   - 仅 logo → 40
//   - 无任何实体信号 → 0
func checkEntityIdentity(html string) CheckResult {
	const name = checkNameEntity
	lower := strings.ToLower(html)

	// Organization schema（JSON-LD 中出现 Organization 类型）
	hasOrgSchema := strings.Contains(lower, "application/ld+json") &&
		strings.Contains(lower, "organization")

	// sameAs 字段或社交主页链接
	hasSameAs := strings.Contains(lower, "sameas")
	if !hasSameAs {
		for _, d := range socialDomains {
			if strings.Contains(lower, d) {
				hasSameAs = true
				break
			}
		}
	}

	// logo 信号（JSON-LD logo 字段 或 <img> 含 logo）
	hasLogo := strings.Contains(lower, "logo")

	var status, detail, severity string
	var score float64
	var evidence string
	switch {
	case hasOrgSchema && hasSameAs && hasLogo:
		status = "pass"
		score = 100
		detail = "实体身份完整：Organization schema + sameAs 社交链接 + logo"
		evidence = "Organization schema + sameAs + logo"
		severity = "low"
	case hasOrgSchema:
		status = "warn"
		score = 70
		detail = "存在 Organization schema，但 sameAs 或 logo 不完整"
		missing := []string{}
		if !hasSameAs {
			missing = append(missing, "sameAs")
		}
		if !hasLogo {
			missing = append(missing, "logo")
		}
		evidence = "Organization schema; 缺少 " + strings.Join(missing, "、")
		severity = "medium"
	case hasLogo:
		status = "warn"
		score = 40
		detail = "仅检测到 logo，缺少 Organization schema 与 sameAs 社交链接"
		evidence = "logo only"
		severity = "medium"
	default:
		status = "fail"
		score = 0
		detail = "未检测到任何实体身份信号（无 Organization schema、无 sameAs、无 logo）"
		severity = "high"
	}
	return CheckResult{
		Name:     name,
		Status:   status,
		Score:    score,
		Detail:   detail,
		Evidence: evidence,
		Severity: severity,
	}
}

// checkSitemap 检查 /sitemap.xml 是否存在且为有效的 sitemap 格式。
func checkSitemap(ctx context.Context, baseURL string) CheckResult {
	const name = checkNameSitemap
	body, status, err := fetchText(ctx, baseURL+"/sitemap.xml")
	if err != nil {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     fmt.Sprintf("无法获取 /sitemap.xml: %v", err),
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	if status != http.StatusOK {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     fmt.Sprintf("/sitemap.xml 返回 HTTP %d（未提供）", status),
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	if strings.TrimSpace(body) == "" {
		return CheckResult{
			Name:       name,
			Status:     "fail",
			Score:      0,
			Detail:     "/sitemap.xml 存在但内容为空",
			Severity:   "medium",
			CIBlocking: false,
		}
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<urlset") || strings.Contains(lower, "<sitemapindex") {
		urlCount := strings.Count(lower, "<url>")
		return CheckResult{
			Name:     name,
			Status:   "pass",
			Score:    100,
			Detail:   fmt.Sprintf("/sitemap.xml 有效（包含 %d 条 <url>）", urlCount),
			Evidence: truncate(body, 500),
		}
	}
	return CheckResult{
		Name:     name,
		Status:   "warn",
		Score:    50,
		Detail:   "/sitemap.xml 存在但不是有效的 sitemap 格式（缺少 urlset/sitemapindex 根元素）",
		Evidence: truncate(body, 500),
	}
}

// ---------- 工具函数 ----------

// fetchText 发起 GET 请求并返回响应体文本与状态码（限制 1MB）。
func fetchText(ctx context.Context, target string) (body string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 限制 1MB
	return string(b), resp.StatusCode, err
}

// parseRobotsBlocked 解析 robots.txt，返回被完全屏蔽（Disallow: /）的爬虫集合。
//
// 兼容标准 robots.txt 语法：
//   - 连续的 User-agent 行归为同一组（共享其后继的 Disallow/Allow 规则）
//   - User-agent: * 匹配所有爬虫（包括 AI 爬虫）
//   - 仅 Disallow: / 或 Disallow: /* 视为完全屏蔽
//   - 空行 / 注释（#）分隔组
func parseRobotsBlocked(body string, bots []string) map[string]bool {
	blocked := make(map[string]bool, len(bots))
	for _, b := range bots {
		blocked[b] = false
	}
	applyBlock := func(agents []string) {
		for _, a := range agents {
			if a == "*" {
				for _, b := range bots {
					blocked[b] = true
				}
				continue
			}
			for _, b := range bots {
				if strings.EqualFold(a, b) {
					blocked[b] = true
				}
			}
		}
	}
	lines := strings.Split(body, "\n")
	var currentAgents []string
	seenRule := false // 当前组是否已出现 Disallow/Allow（用于判断新 User-agent 是否开启新组）
	hasDisallowRoot := false
	flush := func() {
		if hasDisallowRoot && len(currentAgents) > 0 {
			applyBlock(currentAgents)
		}
		currentAgents = nil
		seenRule = false
		hasDisallowRoot = false
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			flush()
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "user-agent:"):
			if seenRule {
				// 前一组已有规则，此 User-agent 开启新组
				flush()
			}
			ua := strings.TrimSpace(line[len("user-agent:"):])
			currentAgents = append(currentAgents, ua)
		case strings.HasPrefix(lower, "disallow:"):
			seenRule = true
			val := strings.TrimSpace(line[len("disallow:"):])
			if val == "/" || val == "/*" {
				hasDisallowRoot = true
			}
		case strings.HasPrefix(lower, "allow:"):
			seenRule = true
		}
	}
	flush()
	return blocked
}

// truncate 截断字符串到指定长度并追加省略号。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// extractSnippet 从 html 中提取 marker 出现位置开始的片段（用于 JSON-LD 证据展示）。
func extractSnippet(html, marker string, n int) string {
	idx := strings.Index(html, marker)
	if idx < 0 {
		return ""
	}
	end := idx + n
	if end > len(html) {
		end = len(html)
	}
	return truncate(html[idx:end], n)
}

// countTagCI 不区分大小写统计某个 HTML 起始标签（如 "h1"、"h2"）的数量。
// 仅匹配 "<tag" 后紧跟空白、">" 或 "/" 的位置，避免误匹配 "<h1x" 之类的字符串。
func countTagCI(html, tag string) int {
	lower := strings.ToLower(html)
	target := "<" + strings.ToLower(tag)
	count := 0
	idx := 0
	for {
		pos := strings.Index(lower[idx:], target)
		if pos < 0 {
			break
		}
		realIdx := idx + pos
		after := realIdx + len(target)
		if after >= len(lower) {
			count++
			break
		}
		c := lower[after]
		if c == ' ' || c == '>' || c == '\t' || c == '\n' || c == '\r' || c == '/' {
			count++
		}
		idx = realIdx + len(target)
	}
	return count
}

// extractHeadingsTextCI 提取所有 h1-h6 标签内的文本内容（已转小写），
// 用于在标题中检测关键词（如 FAQ / 常见问题）。
func extractHeadingsTextCI(html string) string {
	lower := strings.ToLower(html)
	var buf strings.Builder
	for _, tag := range []string{"h1", "h2", "h3", "h4", "h5", "h6"} {
		buf.WriteString(extractTagTextCI(lower, tag))
		buf.WriteByte(' ')
	}
	return buf.String()
}

// extractTagTextCI 从（已小写的）html 中提取指定标签的内部文本。
func extractTagTextCI(lowerHTML, tag string) string {
	var buf strings.Builder
	open := "<" + tag
	closeTag := "</" + tag + ">"
	idx := 0
	for {
		pos := strings.Index(lowerHTML[idx:], open)
		if pos < 0 {
			break
		}
		realIdx := idx + pos
		// 跳过起始标签的属性，找到 ">"
		gt := strings.Index(lowerHTML[realIdx:], ">")
		if gt < 0 {
			break
		}
		contentStart := realIdx + gt + 1
		ct := strings.Index(lowerHTML[contentStart:], closeTag)
		if ct < 0 {
			break
		}
		buf.WriteString(lowerHTML[contentStart : contentStart+ct])
		buf.WriteByte(' ')
		idx = contentStart + ct + len(closeTag)
	}
	return buf.String()
}
