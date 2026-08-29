// Package crawlability 审计网站对 AI 爬虫的可见性（AI Crawlability Audit）。
//
// 这是 GEO 的技术基础层：在评估"内容是否被 AI 引用"之前，先确认
// "网站是否允许 AI 爬虫访问"。数据显示 65% 的网站在不知情的情况下屏蔽了
// AI 爬虫，屏蔽 GPTBot 的网站被引用率降低 73%。
//
// 借鉴 rankweave-geo-audit 与 geo-optimizer-skill 的设计：
//   - robots.txt 对 27 个 AI 爬虫的放行状态（3 级分类：训练/搜索/用户代理）
//   - llms.txt 存在性与结构深度（H1+blockquote+分区+链接+配套 llms-full.txt）
//   - JSON-LD schema 丰富度（WebSite/Organization/FAQPage/Article + 属性数）
//   - 知识图谱存在性（Wikidata/Wikipedia/百度百科）
//
// 评分公式（透明公开）：
//
//	爬虫访问 40% + 结构化数据 30% + llms.txt 20% + 知识图谱 10%
package crawlability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"my-geo/internal/util"
	"my-geo/internal/config"
)

// ── 合规：审计用请求一律使用避风港 MyGEOBot UA，并在请求前对每主机做礼貌限频。 ──
// 说明：crawlability 是对第三方网站"AI 可爬性"的审计（只读、不会训练模型），
// 但为了满足避风港与爬虫合规要求，同样使用统一身份、限频与联系信息。

// AIBotTier AI 爬虫分级。
type AIBotTier string

const (
	TierTraining AIBotTier = "training" // 训练用爬虫（GPTBot/ClaudeBot/CCBot 等）
	TierSearch   AIBotTier = "search"   // 搜索用爬虫（OAI-SearchBot/PerplexityBot 等）
	TierUser     AIBotTier = "user"     // 用户代理（ChatGPT-User/Claude-Web 等）
)

// AIBot 描述一个 AI 爬虫。
type AIBot struct {
	UserAgent string    `json:"user_agent"` // robots.txt 中的 User-agent
	Name      string    `json:"name"`       // 人类可读名称
	Tier      AIBotTier `json:"tier"`       // 分级
	Vendor    string    `json:"vendor"`     // 厂商（OpenAI/Anthropic/Google 等）
}

// 27 个主流 AI 爬虫（借鉴 geo-optimizer-skill 的分类）。
// AIBots 导出供访问监控等模块复用（UA 识别）。
var AIBots = []AIBot{
	// 训练用爬虫（用于模型训练数据采集）
	{"GPTBot", "GPTBot", TierTraining, "OpenAI"},
	{"ChatGPT-User", "ChatGPT User", TierUser, "OpenAI"},
	{"OAI-SearchBot", "OAI SearchBot", TierSearch, "OpenAI"},
	{"ClaudeBot", "ClaudeBot", TierTraining, "Anthropic"},
	{"Claude-Web", "Claude Web", TierUser, "Anthropic"},
	{"anthropic-ai", "Anthropic AI", TierTraining, "Anthropic"},
	{"Google-Extended", "Google Extended", TierTraining, "Google"},
	{"PerplexityBot", "PerplexityBot", TierSearch, "Perplexity"},
	{"Perplexity-User", "Perplexity User", TierUser, "Perplexity"},
	{"CCBot", "CCBot", TierTraining, "Common Crawl"},
	{"Bytespider", "Bytespider", TierTraining, "ByteDance"},
	{"Amazonbot", "Amazonbot", TierTraining, "Amazon"},
	{"AI2Bot", "AI2Bot", TierTraining, "Allen AI"},
	{"Applebot-Extended", "Applebot Extended", TierTraining, "Apple"},
	{"cohere-ai", "Cohere AI", TierTraining, "Cohere"},
	{"Meta-ExternalAgent", "Meta External Agent", TierTraining, "Meta"},
	{"meta-externalfetcher", "Meta External Fetcher", TierUser, "Meta"},
	{"FacebookBot", "FacebookBot", TierTraining, "Meta"},
	{"FriendlyCrawler", "Friendly Crawler", TierTraining, "Friendly"},
	{"ImagesiftBot", "ImagesiftBot", TierTraining, "Imagesift"},
	{"img2dataset", "img2dataset", TierTraining, "img2dataset"},
	{"Diffbot", "Diffbot", TierTraining, "Diffbot"},
	{"Omgilibot", "Omgilibot", TierTraining, "Omgili"},
	{"Omgili", "Omgili", TierTraining, "Omgili"},
	{"YouBot", "YouBot", TierTraining, "You.com"},
	{"PiplBot", "PiplBot", TierTraining, "Pipl"},
	{"webzio-extended", "Webzio Extended", TierTraining, "Webzio"},
}

// BotCheckResult 单个爬虫的检查结果。
type BotCheckResult struct {
	Bot      AIBot  `json:"bot"`
	Allowed  bool   `json:"allowed"`   // 是否被允许
	RuleType string `json:"rule_type"` // "explicit_allow"/"explicit_disallow"/"no_rule"(默认允许)
	Evidence string `json:"evidence"`  // 证据（匹配的 robots.txt 规则）
}

// SchemaCheck JSON-LD schema 检查结果。
type SchemaCheck struct {
	HasJSONLD   bool     `json:"has_json_ld"`
	SchemaTypes []string `json:"schema_types"` // 发现的 @type
	AttrCount   int      `json:"attr_count"`   // 属性总数（丰富度）
	Richness    string   `json:"richness"`     // rich/medium/poor/none
}

// LlmsTxtCheck llms.txt 检查结果。
type LlmsTxtCheck struct {
	Present    bool   `json:"present"`
	HasH1      bool   `json:"has_h1"`       // 有 H1 标题
	HasQuote   bool   `json:"has_quote"`    // 有 blockquote 摘要
	Sections   int    `json:"sections"`     // 分区数（## 标题）
	Links      int    `json:"links"`        // 链接数
	HasFullTxt bool   `json:"has_full_txt"` // 配套 llms-full.txt
	Depth      string `json:"depth"`        // deep/medium/shallow/none
}

// KnowledgeGraphCheck 知识图谱存在性检查结果。
type KnowledgeGraphCheck struct {
	Wikidata    bool `json:"wikidata"`
	WikipediaEN bool `json:"wikipedia_en"`
	WikipediaZH bool `json:"wikipedia_zh"`
	BaiduBaike  bool `json:"baidu_baike"` // 百度百科（中国本土化）
}

// AuditResult 完整的可爬取性审计结果。
type AuditResult struct {
	URL             string              `json:"url"`
	TotalScore      float64             `json:"total_score"` // 0-100
	Grade           string              `json:"grade"`       // A-F
	BotChecks       []BotCheckResult    `json:"bot_checks"`
	SchemaCheck     SchemaCheck         `json:"schema_check"`
	LlmsTxtCheck    LlmsTxtCheck        `json:"llms_txt_check"`
	KGCheck         KnowledgeGraphCheck `json:"knowledge_graph_check"`
	Recommendations []string            `json:"recommendations"`
	AuditedAt       time.Time           `json:"audited_at"`
}

// httpClient 共享 HTTP 客户端。
var httpClient = &http.Client{Timeout: 10 * time.Second}

// insecureTLS 控制是否跳过 TLS 验证（与 readiness 一致）。
var insecureTLS = parseInsecureTLS()

func parseInsecureTLS() bool {
	v := strings.ToLower(strings.TrimSpace(config.Env("GEO_READINESS_INSECURE_TLS", "")))
	return v == "true" || v == "1" || v == "yes"
}

// Audit 对指定 URL 执行 AI 可爬取性审计。
func Audit(ctx context.Context, rawURL string) (*AuditResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("crawlability: URL 不能为空")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("crawlability: 无效 URL %q: %w", rawURL, err)
	}
	if !insecureTLS {
		if err := util.ValidateExternalURL(rawURL); err != nil {
			return nil, fmt.Errorf("crawlability: %w", err)
		}
	}
	baseURL := u.Scheme + "://" + u.Host

	result := &AuditResult{URL: rawURL, AuditedAt: time.Now()}

	// 并发执行 4 类检查
	var wg sync.WaitGroup
	var robotsTxt string
	var robotsUnreachable bool
	var botResults []BotCheckResult
	var schemaChk SchemaCheck
	var llmsChk LlmsTxtCheck
	var kgChk KnowledgeGraphCheck

	// 1. robots.txt + 爬虫检查
	wg.Add(1)
	go func() {
		defer wg.Done()
		robotsTxt, robotsUnreachable = fetchRobotsTxt(ctx, baseURL)
		botResults = checkBotsEx(robotsTxt, robotsUnreachable)
	}()

	// 2. 主页 schema 检查
	wg.Add(1)
	go func() {
		defer wg.Done()
		schemaChk = checkSchema(ctx, rawURL)
	}()

	// 3. llms.txt 检查
	wg.Add(1)
	go func() {
		defer wg.Done()
		llmsChk = checkLlmsTxt(ctx, baseURL)
	}()

	// 4. 知识图谱检查（用 host 名作为品牌名）
	wg.Add(1)
	go func() {
		defer wg.Done()
		kgChk = checkKnowledgeGraph(ctx, u.Hostname())
	}()

	wg.Wait()

	result.BotChecks = botResults
	result.SchemaCheck = schemaChk
	result.LlmsTxtCheck = llmsChk
	result.KGCheck = kgChk

	// 评分（透明公式）：爬虫 40% + schema 30% + llms.txt 20% + 知识图谱 10%
	botScore := scoreBots(botResults)
	schemaScore := scoreSchema(schemaChk)
	llmsScore := scoreLlmsTxt(llmsChk)
	kgScore := scoreKG(kgChk)
	result.TotalScore = botScore*0.4 + schemaScore*0.3 + llmsScore*0.2 + kgScore*0.1
	result.Grade = util.ScoreToGrade(result.TotalScore)
	result.Recommendations = buildRecommendations(result)

	return result, nil
}

// fetchRobotsTxt 获取 robots.txt 内容。
//
// 三态：200 返回内容；404/410 返回 ""（无文件，按无规则处理）；
// 5xx / 网络错误返回 unreachable=true（RFC 9309 保守语义：视为全禁而非全允许）。
func fetchRobotsTxt(ctx context.Context, baseURL string) (content string, unreachable bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/robots.txt", nil)
	if err != nil {
		return "", true
	}
	req.Header.Set("User-Agent", util.MyGEOUserAgent)
	u, _ := url.Parse(baseURL)
	if u != nil {
		util.HostThrottle(u.Host)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", true
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return "", false
	default:
		// 403 / 5xx 等：服务器存在但拒绝提供，保守视为不可达
		return "", true
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", true
	}
	return string(data), false
}

// checkBots 解析 robots.txt，判断每个 AI 爬虫是否被允许。
func checkBots(robotsTxt string) []BotCheckResult {
	return checkBotsEx(robotsTxt, false)
}

// checkBotsEx 同 checkBots，额外处理 robots.txt 不可达（5xx/网络错误）场景：
// RFC 9309 / Google 保守语义 —— 不可达时视为全禁，而非误判"全部允许"。
func checkBotsEx(robotsTxt string, unreachable bool) []BotCheckResult {
	results := make([]BotCheckResult, 0, len(AIBots))
	if unreachable {
		for _, bot := range AIBots {
			results = append(results, BotCheckResult{
				Bot: bot, Allowed: false, RuleType: "unreachable",
				Evidence: "robots.txt 不可达（服务器错误或网络故障），按保守策略视为禁止",
			})
		}
		return results
	}
	if robotsTxt == "" {
		// 无 robots.txt：默认全部允许
		for _, bot := range AIBots {
			results = append(results, BotCheckResult{
				Bot: bot, Allowed: true, RuleType: "no_rule",
				Evidence: "robots.txt 不存在，默认允许所有爬虫",
			})
		}
		return results
	}
	// 解析 robots.txt 为 User-agent -> [Disallow paths]
	rules := parseRobots(robotsTxt)
	for _, bot := range AIBots {
		// 查找精确匹配的 User-agent（大小写不敏感）
		disallows, found := findRules(rules, bot.UserAgent)
		if !found {
			results = append(results, BotCheckResult{
				Bot: bot, Allowed: true, RuleType: "no_rule",
				Evidence: fmt.Sprintf("robots.txt 无 %s 专属规则，默认允许", bot.UserAgent),
			})
			continue
		}
		// 检查是否有 Disallow: / （全站禁止）
		fullBlock := false
		evidence := ""
		for _, d := range disallows {
			if d == "/" {
				fullBlock = true
				evidence = fmt.Sprintf("Disallow: / （全站禁止 %s）", bot.UserAgent)
				break
			}
		}
		if fullBlock {
			results = append(results, BotCheckResult{
				Bot: bot, Allowed: false, RuleType: "explicit_disallow", Evidence: evidence,
			})
		} else {
			allow := true
			if len(disallows) > 0 {
				allow = true // 部分路径禁止，但整体仍允许
				evidence = fmt.Sprintf("有 %d 条 Disallow 规则但未全站禁止", len(disallows))
			} else {
				evidence = fmt.Sprintf("User-agent: %s 无 Disallow，明确允许", bot.UserAgent)
			}
			results = append(results, BotCheckResult{
				Bot: bot, Allowed: allow, RuleType: "explicit_allow", Evidence: evidence,
			})
		}
	}
	return results
}

// parseRobots 解析 robots.txt 为 map[useragent-lower] -> []disallow-path。
//
// RFC 9309：连续多条 User-agent 行属于同一组，共享该组后续的 Disallow/Allow
// （最常见写法 "User-agent: GPTBot\nUser-agent: OAI-SearchBot\nDisallow: /"）。
func parseRobots(txt string) map[string][]string {
	rules := map[string][]string{}
	var group []string // 当前组内的所有 agent
	sawRule := false   // 当前组内是否已出现规则行（出现后再遇 User-agent 即开新组）
	for _, line := range strings.Split(txt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去掉注释
		if i := strings.Index(line, "#"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "user-agent":
			if sawRule {
				group = nil
				sawRule = false
			}
			a := strings.ToLower(val)
			if _, ok := rules[a]; !ok {
				rules[a] = []string{}
			}
			group = append(group, a)
		case "disallow":
			sawRule = true
			for _, a := range group {
				rules[a] = append(rules[a], val)
			}
		case "allow":
			// Allow 规则记录但不影响判断（简化处理）
			sawRule = true
		}
	}
	return rules
}

// findRules 查找爬虫的规则，支持通配符 * 与大小写。
func findRules(rules map[string][]string, agent string) ([]string, bool) {
	agentLower := strings.ToLower(agent)
	if d, ok := rules[agentLower]; ok {
		return d, true
	}
	// 通配符
	if d, ok := rules["*"]; ok {
		return d, true
	}
	return nil, false
}

// checkSchema 检查主页的 JSON-LD 结构化数据。
func checkSchema(ctx context.Context, pageURL string) SchemaCheck {
	html := fetchPage(ctx, pageURL)
	if html == "" {
		return SchemaCheck{}
	}
	// 提取 <script type="application/ld+json"> 内容
	re := regexp.MustCompile(`(?s)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	matches := re.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return SchemaCheck{HasJSONLD: false, Richness: "none"}
	}
	schemaTypes := []string{}
	attrTotal := 0
	for _, m := range matches {
		var obj map[string]any
		if err := json.Unmarshal([]byte(m[1]), &obj); err != nil {
			continue
		}
		if t, ok := obj["@type"].(string); ok {
			schemaTypes = append(schemaTypes, t)
		} else if ts, ok := obj["@type"].([]any); ok {
			for _, x := range ts {
				if s, ok := x.(string); ok {
					schemaTypes = append(schemaTypes, s)
				}
			}
		}
		attrTotal += len(obj)
	}
	richness := "poor"
	if attrTotal >= 5 {
		richness = "rich"
	} else if attrTotal >= 2 {
		richness = "medium"
	}
	return SchemaCheck{
		HasJSONLD:   true,
		SchemaTypes: schemaTypes,
		AttrCount:   attrTotal,
		Richness:    richness,
	}
}

// checkLlmsTxt 检查 llms.txt 与 llms-full.txt。
func checkLlmsTxt(ctx context.Context, baseURL string) LlmsTxtCheck {
	chk := LlmsTxtCheck{}
	txt := fetchPage(ctx, baseURL+"/llms.txt")
	if txt == "" {
		return chk
	}
	chk.Present = true
	chk.HasH1 = strings.Contains(txt, "# ") && strings.Index(txt, "# ") == 0
	chk.HasQuote = strings.Contains(txt, "> ")
	// 统计 ## 分区
	for _, line := range strings.Split(txt, "\n") {
		if strings.HasPrefix(line, "## ") {
			chk.Sections++
		}
	}
	// 统计链接
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	chk.Links = len(linkRe.FindAllString(txt, -1))
	// 检查 llms-full.txt
	if fetchPage(ctx, baseURL+"/llms-full.txt") != "" {
		chk.HasFullTxt = true
	}
	chk.Depth = "shallow"
	if chk.Sections >= 3 && chk.Links >= 5 {
		chk.Depth = "deep"
	} else if chk.Sections >= 1 || chk.Links >= 2 {
		chk.Depth = "medium"
	}
	return chk
}

// checkKnowledgeGraph 检查品牌在知识图谱中的存在性。
// 用 HTTP 查询各知识库的搜索 API，brand 作为关键词。
// 注意：这些 API 对"无结果"同样返回 200 + 空 JSON，必须解析响应体判断。
func checkKnowledgeGraph(ctx context.Context, brand string) KnowledgeGraphCheck {
	chk := KnowledgeGraphCheck{}
	if brand == "" {
		return chk
	}
	// Wikidata：wbsearchentities → {"search":[...]}
	chk.Wikidata = checkSearchAPI(ctx,
		"https://www.wikidata.org/w/api.php?action=wbsearchentities&search="+url.QueryEscape(brand)+"&language=en&format=json&limit=1",
		func(r map[string]any) bool { return arrayNonEmpty(r, "search") })
	// Wikipedia EN：list=search → {"query":{"search":[...]}}
	chk.WikipediaEN = checkSearchAPI(ctx,
		"https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch="+url.QueryEscape(brand)+"&format=json&srlimit=1",
		func(r map[string]any) bool { return arrayNonEmpty(r, "query", "search") })
	// Wikipedia ZH
	chk.WikipediaZH = checkSearchAPI(ctx,
		"https://zh.wikipedia.org/w/api.php?action=query&list=search&srsearch="+url.QueryEscape(brand)+"&format=json&srlimit=1",
		func(r map[string]any) bool { return arrayNonEmpty(r, "query", "search") })
	// 百度百科：无结果时返回空对象/ errorCode；有结果时含 title/key
	chk.BaiduBaike = checkSearchAPI(ctx,
		"https://baike.baidu.com/api/openapi/BaikeLemmaCardApi?scope=103&format=json&appid=379020&bk_key="+url.QueryEscape(brand)+"&bk_length=600",
		func(r map[string]any) bool {
			if _, bad := r["errorCode"]; bad {
				return false
			}
			t, _ := r["title"].(string)
			if strings.TrimSpace(t) != "" {
				return true
			}
			k, _ := r["key"].(string)
			return strings.TrimSpace(k) != ""
		})
	return chk
}

// checkSearchAPI 请求搜索 API 并按 hasResult 判断响应是否含有效结果。
func checkSearchAPI(ctx context.Context, rawURL string, hasResult func(root map[string]any) bool) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", util.MyGEOUserAgent)
	if u, err := url.Parse(rawURL); err == nil {
		util.HostThrottle(u.Host)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	return hasResult(root)
}

// arrayNonEmpty 按 path 取嵌套数组并判断非空。
func arrayNonEmpty(root map[string]any, path ...string) bool {
	var cur any = root
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[k]; !ok {
			return false
		}
	}
	arr, ok := cur.([]any)
	return ok && len(arr) > 0
}

// fetchPage 获取页面 HTML/文本内容。
func fetchPage(ctx context.Context, pageURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", util.MyGEOUserAgent)
	if u, err := url.Parse(pageURL); err == nil {
		util.HostThrottle(u.Host)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ""
	}
	return string(data)
}

// 评分函数（透明公式）。

func scoreBots(results []BotCheckResult) float64 {
	if len(results) == 0 {
		return 0
	}
	allowed := 0
	for _, r := range results {
		if r.Allowed {
			allowed++
		}
	}
	return float64(allowed) / float64(len(results)) * 100
}

func scoreSchema(chk SchemaCheck) float64 {
	if !chk.HasJSONLD {
		return 0
	}
	score := 30.0 // 有 JSON-LD 基础分
	switch chk.Richness {
	case "rich":
		score += 70
	case "medium":
		score += 40
	case "poor":
		score += 15
	}
	if score > 100 {
		score = 100
	}
	return score
}

func scoreLlmsTxt(chk LlmsTxtCheck) float64 {
	if !chk.Present {
		return 0
	}
	score := 40.0 // 存在即得 40 分
	if chk.HasH1 {
		score += 15
	}
	if chk.HasQuote {
		score += 15
	}
	if chk.Sections >= 3 {
		score += 15
	}
	if chk.HasFullTxt {
		score += 15
	}
	if score > 100 {
		score = 100
	}
	return score
}

func scoreKG(chk KnowledgeGraphCheck) float64 {
	// Wikidata 40 + WikiEN 25 + WikiZH 20 + 百科 15
	score := 0.0
	if chk.Wikidata {
		score += 40
	}
	if chk.WikipediaEN {
		score += 25
	}
	if chk.WikipediaZH {
		score += 20
	}
	if chk.BaiduBaike {
		score += 15
	}
	return score
}

// buildRecommendations 根据检查结果生成可操作建议。
func buildRecommendations(r *AuditResult) []string {
	recs := []string{}
	// 爬虫建议
	blocked := []string{}
	for _, b := range r.BotChecks {
		if !b.Allowed && b.Bot.Tier == TierTraining {
			blocked = append(blocked, b.Bot.UserAgent)
		}
	}
	if len(blocked) > 0 {
		recs = append(recs, fmt.Sprintf("robots.txt 屏蔽了 %d 个训练爬虫（%s），建议放行以提升 AI 可见度",
			len(blocked), strings.Join(blocked, "/")))
	}
	// schema 建议
	if !r.SchemaCheck.HasJSONLD {
		recs = append(recs, "页面无 JSON-LD 结构化数据，建议添加 WebSite/Organization schema（可使 GPT-4 提取准确率从 16% 提升到 54%）")
	} else if r.SchemaCheck.Richness != "rich" {
		recs = append(recs, fmt.Sprintf("JSON-LD 属性数 %d，建议补充至 5+ 属性提升丰富度", r.SchemaCheck.AttrCount))
	}
	// llms.txt 建议
	if !r.LlmsTxtCheck.Present {
		recs = append(recs, "缺少 /llms.txt 文件，建议生成（面向 LLM 的站点摘要，已有 84.4 万站点部署）")
	} else if r.LlmsTxtCheck.Depth != "deep" {
		recs = append(recs, "llms.txt 深度不足，建议补充 H1 标题、blockquote 摘要、3+ 分区与配套 llms-full.txt")
	}
	// 知识图谱建议
	if !r.KGCheck.Wikidata {
		recs = append(recs, "品牌未在 Wikidata 收录，建议创建词条（AI 模型将其作为事实来源）")
	}
	if !r.KGCheck.BaiduBaike {
		recs = append(recs, "品牌未在百度百科收录，建议补充（中国本土 AI 引擎的重要事实来源）")
	}
	return recs
}
