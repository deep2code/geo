// Package externalsignals 集成按量付费（pay-per-use）的第三方 SEO 数据源，
// 为品牌 GEO 分析提供关键词难度、SERP 特性与反链情报。
//
// 设计灵感参考 OpenSEO 的做法：不买订阅，而是用用户自带的 API Key 按次调用。
// 目前接入两类数据源：
//   - DataForSEO（付费，按量计费）：关键词搜索量/难度、SERP 高级结果、反链摘要
//   - Common Crawl（免费）：当未配置 DataForSEO Key 或 DFS 反链查询失败时，
//     回退到 Common Crawl 公开索引做轻量反链分析
//
// 容错策略：未配置 API Key 时，所有方法返回带 "模拟数据" 标记的样例数据并打印
// 警告到 stderr，保证整个管线在无 Key 环境下仍可运行。仅依赖标准库，零第三方依赖。
package externalsignals

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"my-geo/internal/config"
)

// Source 标识数据来源。
type Source string

const (
	SourceDataForSEO  Source = "dataforseo"   // DataForSEO 付费接口
	SourceCommonCrawl Source = "common_crawl" // Common Crawl 免费接口
)

// 按量计费费率（USD），用于估算与缺省计费。
// 实际计费优先使用 DataForSEO 响应中返回的真实 cost，缺失时按下列费率估算。
const (
	costPerKeyword      = 0.035       // 关键词：$0.035 / 词
	costPerBacklinkItem = 0.01 / 20.0 // 反链：$0.01 / 20 条（即每条 $0.0005）
	costPerSERPQuery    = 0.02        // SERP：$0.02 / 查询
)

// KeywordInfo 单个关键词的搜索量与难度信息。
type KeywordInfo struct {
	Keyword      string  `json:"keyword"`
	SearchVolume int     `json:"search_volume"`
	Difficulty   float64 `json:"difficulty"` // 0-100
	CPC          float64 `json:"cpc"`
	Intent       string  `json:"intent"` // informational/transactional/navigational/commercial
}

// BacklinkInfo 单条反链信息。
type BacklinkInfo struct {
	SourceDomain    string    `json:"source_domain"`
	TargetURL       string    `json:"target_url"`
	AnchorText      string    `json:"anchor_text"`
	DomainAuthority float64   `json:"domain_authority"`
	FirstSeen       time.Time `json:"first_seen"`
}

// SERPFeature 单个 SERP 特性（AI 概览 / 精选摘要 / 知识面板 / 本地包等）。
type SERPFeature struct {
	Feature  string `json:"feature"` // ai_overview/featured_snippet/knowledge_panel/local_pack
	Present  bool   `json:"present"`
	Position int    `json:"position"`
}

// SignalReport 一次完整的外部信号采集报告。
type SignalReport struct {
	Domain       string         `json:"domain"`
	Keywords     []KeywordInfo  `json:"keywords,omitempty"`
	Backlinks    []BacklinkInfo `json:"backlinks,omitempty"`
	SERPFeatures []SERPFeature  `json:"serp_features,omitempty"`
	Source       Source         `json:"source"`
	FetchedAt    time.Time      `json:"fetched_at"`
	Cost         float64        `json:"cost"` // 估算费用（USD）
}

// Client 外部信号采集客户端。
//
// cost 字段为本次报告构建过程中累计的实际/估算费用，由 FullReport 在开始时清零，
// 各方法（可能并发调用）累加；P2-7 加 costMu 互斥锁保证并发安全。
type Client struct {
	dfsAPIKey  string       // DataForSEO API Key（env GEO_DFS_APIKEY）
	dfsEmail   string       // DataForSEO 邮箱（env GEO_DFS_EMAIL）
	dfsBaseURL string       // DataForSEO 基址，默认 https://api.dataforseo.com/v3
	ccBaseURL  string       // Common Crawl 索引 API 基址
	httpClient *http.Client // 30s 超时
	reportMu   sync.Mutex   // 串行化 FullReport：cost 是实例级累加器，并发调用会互相污染
	costMu     sync.Mutex
	cost       float64 // 本次报告累计费用（FullReport 内累加，costMu 保护）
}

// NewFromEnv 从环境变量构造客户端。
//
// 读取变量：
//   - GEO_DFS_APIKEY / GEO_DFS_EMAIL：DataForSEO 凭据（缺失则退化为模拟数据）
//   - GEO_DFS_BASE_URL（可选）：覆盖 DataForSEO 基址
//   - GEO_CC_BASE_URL（可选，默认 https://index.commoncrawl.org）：覆盖 Common Crawl 基址
func NewFromEnv() *Client {
	c := &Client{
		dfsAPIKey:  strings.TrimSpace(config.Env("GEO_DFS_APIKEY", "")),
		dfsEmail:   strings.TrimSpace(config.Env("GEO_DFS_EMAIL", "")),
		dfsBaseURL: "https://api.dataforseo.com/v3",
		ccBaseURL:  "https://index.commoncrawl.org",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				// 强制 TLS 1.2+，符合 DataForSEO 安全要求
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
	if v := strings.TrimSpace(config.Env("GEO_DFS_BASE_URL", "")); v != "" {
		c.dfsBaseURL = v
	}
	if v := strings.TrimSpace(config.Env("GEO_CC_BASE_URL", "")); v != "" {
		c.ccBaseURL = v
	}
	return c
}

// Available 是否已配置 DataForSEO 凭据（即可走付费接口）。
func (c *Client) Available() bool {
	return c != nil && c.dfsAPIKey != "" && c.dfsEmail != ""
}

// EstimateCost 按费率估算一次采集的总费用（USD）。
//
// 参数：关键词数、反链条数、SERP 查询数。结果四舍五入到分。
func (c *Client) EstimateCost(keywords, backlinks, serpQueries int) float64 {
	cost := float64(keywords)*costPerKeyword +
		float64(backlinks)*costPerBacklinkItem +
		float64(serpQueries)*costPerSERPQuery
	return roundCents(cost)
}

// KeywordResearch 关键词研究：调用 DataForSEO Keywords Data 搜索量接口。
//
// 未配置 Key 或接口异常时返回带 "模拟数据" 标记的样例数据。费用约 $0.035/词。
func (c *Client) KeywordResearch(ctx context.Context, keywords []string) ([]KeywordInfo, error) {
	// 过滤空白关键词
	kws := make([]string, 0, len(keywords))
	for _, k := range keywords {
		if k = strings.TrimSpace(k); k != "" {
			kws = append(kws, k)
		}
	}
	if len(kws) == 0 {
		return nil, fmt.Errorf("关键词列表为空")
	}

	if !c.Available() {
		warnStub("KeywordResearch", "未配置 DataForSEO API Key")
		return stubKeywords(kws), nil
	}

	// DataForSEO 搜索量 live 接口请求体为任务数组
	payload := []map[string]interface{}{{
		"keywords":      kws,
		"location_code": 2840, // 美国
		"language_code": "en",
	}}
	raw, actualCost, err := c.dfsPost(ctx, "/keywords_data/search_volume/live", payload)
	if err != nil {
		warnFallback("KeywordResearch", err)
		return stubKeywords(kws), nil
	}

	// 响应 result 为关键词指标数组
	var results []struct {
		Keyword      string  `json:"keyword"`
		SearchVolume int     `json:"search_volume"`
		Competition  float64 `json:"competition"` // 0-1，乘 100 映射为难度
		CPC          float64 `json:"cpc"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		warnFallback("KeywordResearch", fmt.Errorf("解析响应失败: %w", err))
		return stubKeywords(kws), nil
	}

	out := make([]KeywordInfo, 0, len(results))
	for i, r := range results {
		kw := r.Keyword
		if kw == "" {
			kw = kws[i%len(kws)] // 兜底：接口未回填关键词时按顺序还原
		}
		out = append(out, KeywordInfo{
			Keyword:      kw,
			SearchVolume: r.SearchVolume,
			Difficulty:   clampDifficulty(r.Competition * 100),
			CPC:          r.CPC,
			Intent:       guessIntent(kw),
		})
	}

	cost := actualCost
	if cost <= 0 {
		cost = float64(len(out)) * costPerKeyword
	}
	c.addCost(cost)
	return out, nil
}

// BacklinkAnalysis 反链分析：优先 DataForSEO 反链摘要接口，无 Key 或失败时回退 Common Crawl。
//
// DataForSEO 费用约 $0.01/20 条；Common Crawl 免费。最终无结果时返回模拟数据。
func (c *Client) BacklinkAnalysis(ctx context.Context, domain string, limit int) ([]BacklinkInfo, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}
	if limit <= 0 {
		limit = 100
	}

	// 有 DFS Key：走付费反链接口
	if c.Available() {
		out, err := c.backlinksDFS(ctx, domain, limit)
		if err == nil {
			return out, nil
		}
		warnFallback("BacklinkAnalysis", fmt.Errorf("DataForSEO 反链查询失败，回退 Common Crawl: %w", err))
	}

	// 无 Key 或 DFS 失败：回退 Common Crawl 免费接口
	cc, err := c.backlinksCommonCrawl(ctx, domain, limit)
	if err == nil && len(cc) > 0 {
		return cc, nil
	}
	if err != nil {
		warnFallback("BacklinkAnalysis", fmt.Errorf("Common Crawl 查询失败，返回模拟数据: %w", err))
	} else {
		warnStub("BacklinkAnalysis", "Common Crawl 未返回结果")
	}
	return stubBacklinks(domain, limit), nil
}

// SERPAnalysis SERP 分析：调用 DataForSEO Google Organic Advanced 接口，
// 识别 AI 概览、精选摘要、知识面板、本地包等特性。
//
// 未配置 Key 或接口异常时返回模拟数据。费用约 $0.02/查询。
func (c *Client) SERPAnalysis(ctx context.Context, keyword string) ([]SERPFeature, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}

	if !c.Available() {
		warnStub("SERPAnalysis", "未配置 DataForSEO API Key")
		return stubSERP(keyword), nil
	}

	payload := []map[string]interface{}{{
		"keyword":       keyword,
		"location_code": 2840,
		"language_code": "en",
	}}
	raw, actualCost, err := c.dfsPost(ctx, "/serp/google/organic/live/advanced", payload)
	if err != nil {
		warnFallback("SERPAnalysis", err)
		return stubSERP(keyword), nil
	}

	// 响应 result 为 SERP 概览数组，每个含 items（不同 type 的结果块）
	var results []struct {
		Items []struct {
			Type         string `json:"type"`
			RankAbsolute int    `json:"rank_absolute"`
			RankGroup    int    `json:"rank_group"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		warnFallback("SERPAnalysis", fmt.Errorf("解析响应失败: %w", err))
		return stubSERP(keyword), nil
	}

	// DataForSEO item.type → 本结构 Feature 名称
	typeMap := map[string]string{
		"ai_overview":      "ai_overview",
		"featured_snippet": "featured_snippet",
		"knowledge_graph":  "knowledge_panel",
		"local_pack":       "local_pack",
	}
	out := make([]SERPFeature, 0)
	for _, r := range results {
		for _, it := range r.Items {
			feat, ok := typeMap[it.Type]
			if !ok {
				continue
			}
			pos := it.RankAbsolute
			if pos == 0 {
				pos = it.RankGroup
			}
			out = append(out, SERPFeature{
				Feature:  feat,
				Present:  true,
				Position: pos,
			})
		}
	}

	cost := actualCost
	if cost <= 0 {
		cost = costPerSERPQuery
	}
	c.addCost(cost)
	return out, nil
}

// FullReport 聚合关键词、反链、SERP 三类信号，返回带合并费用的完整报告。
//
// 内部顺序调用三个采集方法并累加实际费用；任一环节失败均自动回退模拟数据，
// 因此只要 domain 非空就会返回可用报告。
func (c *Client) FullReport(ctx context.Context, domain string, keywords []string) (*SignalReport, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}

	// 串行化：cost 是实例级累加器，两个并发 FullReport 会互相清零/污染费用统计
	c.reportMu.Lock()
	defer c.reportMu.Unlock()

	// 重置费用累加器：FullReport 内各方法累加（costMu 保护，并发安全）
	c.costMu.Lock()
	c.cost = 0
	c.costMu.Unlock()

	report := &SignalReport{
		Domain:    domain,
		Source:    SourceDataForSEO,
		FetchedAt: time.Now(),
	}
	if !c.Available() {
		// 无 DFS Key：主要依赖 Common Crawl / 模拟数据
		report.Source = SourceCommonCrawl
	}

	// 1. 关键词研究
	if len(keywords) > 0 {
		if kws, err := c.KeywordResearch(ctx, keywords); err == nil {
			report.Keywords = kws
		}
	}
	// 2. 反链分析
	if bls, err := c.BacklinkAnalysis(ctx, domain, 100); err == nil {
		report.Backlinks = bls
	}
	// 3. SERP 分析（取第一个关键词作为代表查询）
	if len(keywords) > 0 {
		if feats, err := c.SERPAnalysis(ctx, keywords[0]); err == nil {
			report.SERPFeatures = feats
		}
	}

	c.costMu.Lock()
	report.Cost = roundCents(c.cost)
	c.costMu.Unlock()
	return report, nil
}

// ---------------- DataForSEO 底层 ----------------

// dfsEnvelope DataForSEO 统一响应外壳。
type dfsEnvelope struct {
	StatusCode    int       `json:"status_code"`
	StatusMessage string    `json:"status_message"`
	Cost          float64   `json:"cost"`
	Tasks         []dfsTask `json:"tasks"`
}

// dfsTask 单个任务结果。
type dfsTask struct {
	StatusCode    int             `json:"status_code"`
	StatusMessage string          `json:"status_message"`
	Cost          float64         `json:"cost"`
	Result        json.RawMessage `json:"result"`
}

// dfsPost 向 DataForSEO 发起带 Basic Auth 的 POST 请求，返回首任务 result、真实费用与错误。
func (c *Client) dfsPost(ctx context.Context, path string, payload interface{}) (json.RawMessage, float64, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("构造请求失败: %w", err)
	}
	endpoint := strings.TrimRight(c.dfsBaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	// DataForSEO 使用 HTTP Basic Auth（email:api_key）
	req.SetBasicAuth(c.dfsEmail, c.dfsAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("DataForSEO 请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("DataForSEO HTTP %d: %s", resp.StatusCode, truncateBytes(data, 500))
	}

	var env dfsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, 0, fmt.Errorf("解析 DataForSEO 响应失败: %w (body=%s)", err, truncateBytes(data, 500))
	}
	// 20000 = Ok；0 表示字段缺失，放行
	if env.StatusCode != 0 && env.StatusCode != 20000 {
		return nil, 0, fmt.Errorf("DataForSEO 错误 %d: %s", env.StatusCode, env.StatusMessage)
	}
	if len(env.Tasks) == 0 {
		return nil, 0, fmt.Errorf("DataForSEO 返回无任务")
	}
	t := env.Tasks[0]
	if t.StatusCode != 0 && t.StatusCode != 20000 {
		return nil, 0, fmt.Errorf("DataForSEO 任务错误 %d: %s", t.StatusCode, t.StatusMessage)
	}

	// 优先使用任务级真实费用，其次外壳费用
	cost := t.Cost
	if cost <= 0 {
		cost = env.Cost
	}
	return t.Result, cost, nil
}

// backlinksDFS 调用 DataForSEO 反链列表接口（/backlinks/backlinks/live，逐条反链；
// /backlinks/summary/live 只返回聚合指标不含 items，勿混用）。
func (c *Client) backlinksDFS(ctx context.Context, domain string, limit int) ([]BacklinkInfo, error) {
	payload := []map[string]interface{}{{
		"target":   domain,
		"limit":    limit,
		"order_by": []string{"domain_from_rank,desc"},
	}}
	raw, actualCost, err := c.dfsPost(ctx, "/backlinks/backlinks/live", payload)
	if err != nil {
		return nil, err
	}

	// result 为摘要数组，每个含 items（单条反链）
	var results []struct {
		Items []struct {
			DomainFrom     string `json:"domain_from"`
			URLFrom        string `json:"url_from"`
			URLTo          string `json:"url_to"`
			Anchor         string `json:"anchor"`
			FirstSeen      string `json:"first_seen"`
			DomainFromRank int    `json:"domain_from_rank"` // 0-1000 量级
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("解析反链响应失败: %w", err)
	}

	out := make([]BacklinkInfo, 0)
	for _, r := range results {
		for _, it := range r.Items {
			bl := BacklinkInfo{
				SourceDomain:    it.DomainFrom,
				TargetURL:       it.URLTo,
				AnchorText:      it.Anchor,
				DomainAuthority: clampDifficulty(float64(it.DomainFromRank) / 10.0), // 0-1000 → 0-100
			}
			if t, err := parseTime(it.FirstSeen); err == nil {
				bl.FirstSeen = t
			}
			out = append(out, bl)
		}
	}

	cost := actualCost
	if cost <= 0 {
		cost = float64(len(out)) * costPerBacklinkItem
	}
	c.addCost(cost)
	return out, nil
}

// ---------------- Common Crawl 回退 ----------------

// backlinksCommonCrawl 通过 Common Crawl 公开索引做轻量反链分析（免费）。
//
// 流程：collinfo.json 取最新索引 → {index}/search?url={domain} 取抓取记录 →
// 将抓取页所在域名作为反链来源（近似；CC 不提供锚文本与 DA）。
func (c *Client) backlinksCommonCrawl(ctx context.Context, domain string, limit int) ([]BacklinkInfo, error) {
	idx, err := c.ccLatestIndex(ctx)
	if err != nil {
		return nil, err
	}
	searchURL := fmt.Sprintf("%s/%s/search?url=%s&output=json&limit=%d",
		strings.TrimRight(c.ccBaseURL, "/"), idx, domain, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Common Crawl 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Common Crawl HTTP %d", resp.StatusCode)
	}

	// CC search 返回换行分隔的 JSON（NDJSON），每行一条抓取记录
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 Common Crawl 响应失败: %w", err)
	}

	out := make([]BacklinkInfo, 0)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 跳过无法解析的行
		}
		srcDomain := extractDomain(rec.URL)
		if srcDomain == "" {
			continue
		}
		bl := BacklinkInfo{
			SourceDomain:    srcDomain,
			TargetURL:       "https://" + domain + "/",
			AnchorText:      "", // Common Crawl 不提供锚文本
			DomainAuthority: 0,  // 免费接口无 DA
		}
		if t, err := parseTime(rec.Timestamp); err == nil {
			bl.FirstSeen = t
		}
		out = append(out, bl)
	}
	// Common Crawl 免费：不计费
	return out, nil
}

// ccLatestIndex 获取 Common Crawl 最新索引 ID（列表首项）。
func (c *Client) ccLatestIndex(ctx context.Context) (string, error) {
	url := strings.TrimRight(c.ccBaseURL, "/") + "/collinfo.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 Common Crawl 索引列表失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Common Crawl collinfo HTTP %d", resp.StatusCode)
	}
	var indexes []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&indexes); err != nil {
		return "", fmt.Errorf("解析 Common Crawl 索引列表失败: %w", err)
	}
	if len(indexes) == 0 {
		return "", fmt.Errorf("Common Crawl 无可用索引")
	}
	return indexes[0].ID, nil
}

// ---------------- 工具函数 ----------------

// addCost 累加费用（costMu 保护，并发安全）。
func (c *Client) addCost(v float64) {
	c.costMu.Lock()
	c.cost += v
	c.costMu.Unlock()
}

// guessIntent 根据关键词启发式推断搜索意图。
func guessIntent(kw string) string {
	k := strings.ToLower(kw)
	switch {
	case strings.Contains(k, "buy") || strings.Contains(k, "price") ||
		strings.Contains(k, "discount") || strings.Contains(k, "coupon") ||
		strings.Contains(k, "购买") || strings.Contains(k, "价格") || strings.Contains(k, "优惠"):
		return "transactional"
	case strings.Contains(k, "best") || strings.Contains(k, "vs") ||
		strings.Contains(k, "compare") || strings.Contains(k, "对比") || strings.Contains(k, "推荐"):
		return "commercial"
	case strings.Contains(k, "login") || strings.Contains(k, "官网") ||
		strings.Contains(k, "登录") || strings.Contains(k, "sign in"):
		return "navigational"
	default:
		return "informational"
	}
}

// extractDomain 从 URL 中提取规范化域名（无 net/url 依赖的最小实现）。
func extractDomain(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	// 去掉 scheme
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rawURL = rawURL[i+3:]
	}
	// 去掉 path / query
	if i := strings.IndexAny(rawURL, "/?"); i >= 0 {
		rawURL = rawURL[:i]
	}
	// 去掉端口
	if i := strings.LastIndex(rawURL, ":"); i >= 0 {
		rawURL = rawURL[:i]
	}
	rawURL = strings.ToLower(strings.TrimPrefix(rawURL, "www."))
	return rawURL
}

// parseTime 解析时间字符串，兼容 RFC3339（DFS）与 YYYYMMDDhhmmss（CC）。
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("空时间")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("20060102150405", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("无法解析时间: %s", s)
}

// clampDifficulty 将数值限制在 0-100 区间（用于难度/DA 归一化）。
func clampDifficulty(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// roundCents 四舍五入到分。
func roundCents(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// truncateBytes 截断字节切片用于错误信息展示。
func truncateBytes(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// warnStub 打印"返回模拟数据"警告到 stderr。
func warnStub(method, reason string) {
	fmt.Fprintf(os.Stderr, "[externalsignals] %s: %s，返回模拟数据\n", method, reason)
}

// warnFallback 打印"回退"警告到 stderr。
func warnFallback(method string, err error) {
	fmt.Fprintf(os.Stderr, "[externalsignals] %s: %v，回退到模拟数据\n", method, err)
}

// ---------------- 模拟数据生成 ----------------

// stubKeywords 生成带 "模拟数据" 标记的关键词样例。
func stubKeywords(keywords []string) []KeywordInfo {
	out := make([]KeywordInfo, 0, len(keywords))
	for i, kw := range keywords {
		vol := 5400 - i*320
		if vol < 120 {
			vol = 120 + (i%7)*30
		}
		diff := 28.0 + float64(i*7)
		if diff > 92 {
			diff = 92 - float64(i%5)
		}
		out = append(out, KeywordInfo{
			Keyword:      "[模拟数据] " + kw,
			SearchVolume: vol,
			Difficulty:   clampDifficulty(diff),
			CPC:          0.85 + float64(i%4)*0.35,
			Intent:       guessIntent(kw),
		})
	}
	return out
}

// stubBacklinks 生成带 "模拟数据" 标记的反链样例。
func stubBacklinks(domain string, limit int) []BacklinkInfo {
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	sources := []string{
		"blog.example.com", "news.sample.org", "forum.demo.net",
		"docs.ref.io", "review.site.co", "medium.com", "dev.to",
	}
	out := make([]BacklinkInfo, 0, limit)
	for i := 0; i < limit; i++ {
		src := sources[i%len(sources)]
		out = append(out, BacklinkInfo{
			SourceDomain:    "[模拟数据]" + src,
			TargetURL:       "https://" + domain + "/",
			AnchorText:      domain,
			DomainAuthority: 25 + float64(i*8),
			FirstSeen:       time.Now().AddDate(-1, 0, -i*12),
		})
	}
	return out
}

// stubSERP 生成带 "模拟数据" 标记的 SERP 特性样例。
func stubSERP(keyword string) []SERPFeature {
	_ = keyword
	return []SERPFeature{
		{Feature: "[模拟数据]ai_overview", Present: true, Position: 0},
		{Feature: "[模拟数据]featured_snippet", Present: true, Position: 1},
		{Feature: "[模拟数据]knowledge_panel", Present: true, Position: 0},
		{Feature: "[模拟数据]local_pack", Present: false, Position: 0},
	}
}
