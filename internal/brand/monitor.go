package brand

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/brand/roi"
	"my-geo/internal/models"
)

// Monitor 品牌可见度监控引擎。
//
// 查询多个 AI 引擎，检测品牌提及、引用、情感、位置、竞品、幽灵引用。
// 设计参考 ai-brand-monitor-mcp 与 oneglanse 的多引擎并行查询。
type Monitor struct {
	adapters       map[models.EngineType]adapter.Adapter
	maxConcurrency int              // 并行查询的最大并发数（默认 5）
	roiTracker     *roi.Tracker     // 可选：token 用量与成本追踪
}

const defaultMaxConcurrency = 5

// NewMonitor 创建监控引擎。
//
// adapters 为目标引擎适配器映射；若为 nil 则使用全部内置引擎（未配置 key 的返回模拟响应）。
// 并发度默认为 5，可用 WithMaxConcurrency 调整。
func NewMonitor(adapters map[models.EngineType]adapter.Adapter) *Monitor {
	m := &Monitor{
		adapters:       map[models.EngineType]adapter.Adapter{},
		maxConcurrency: defaultMaxConcurrency,
	}
	if adapters != nil {
		for k, v := range adapters {
			m.adapters[k] = v
		}
	}
	return m
}

// WithMaxConcurrency 设置并行查询的最大并发数。
//
// 高批次查询（prompt×引擎 组合很多）时调高可提升速度；需注意避免引擎限流。
// n <= 0 时使用默认值 5。
func (m *Monitor) WithMaxConcurrency(n int) *Monitor {
	if n <= 0 {
		n = defaultMaxConcurrency
	}
	m.maxConcurrency = n
	return m
}

// WithROITracker 注入 token 用量与成本追踪器。
//
// 注入后，每次引擎查询的 token 用量与估算成本会被自动记录。
func (m *Monitor) WithROITracker(t *roi.Tracker) *Monitor {
	m.roiTracker = t
	return m
}

// ROITracker 返回当前 ROI 追踪器（可能为 nil）。
func (m *Monitor) ROITracker() *roi.Tracker { return m.roiTracker }

// NewMonitorFromConfigs 从配置批量创建适配器。
//
// configs 为各引擎的 API 配置，未配置 key 的引擎也会创建（返回模拟响应）。
// 若某引擎适配器创建失败，会在 errs 中返回该引擎的错误，不影响其他引擎正常创建。
// 调用方应检查 len(m.adapters) > 0 确保至少一个引擎可用。
func NewMonitorFromConfigs(configs map[models.EngineType]adapter.Config) (*Monitor, map[models.EngineType]error) {
	m := &Monitor{
		adapters:       map[models.EngineType]adapter.Adapter{},
		maxConcurrency: defaultMaxConcurrency,
	}
	errs := map[models.EngineType]error{}
	for engine, cfg := range configs {
		a, err := adapter.NewAdapter(engine, cfg)
		if err != nil {
			errs[engine] = err
			continue
		}
		m.adapters[engine] = a
	}
	if len(errs) == 0 {
		errs = nil
	}
	return m, errs
}

// Run 执行品牌可见度监控。
//
// 对每个 prompt × 每个目标引擎发起查询，并行执行，收集检测结果。
// 参考 LLM Brand Tracker 的并行查询设计。
func (m *Monitor) Run(ctx context.Context, profile BrandProfile) ([]PromptResult, error) {
	engines := profile.TargetEngines
	if len(engines) == 0 {
		for e := range m.adapters {
			engines = append(engines, e)
		}
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("未配置任何引擎适配器")
	}
	if len(profile.Prompts) == 0 {
		return nil, fmt.Errorf("未提供查询词（prompts）")
	}

	type job struct {
		prompt string
		engine models.EngineType
	}
	jobs := make([]job, 0, len(engines)*len(profile.Prompts))
	for _, p := range profile.Prompts {
		for _, e := range engines {
			jobs = append(jobs, job{prompt: p, engine: e})
		}
	}

	results := make([]PromptResult, len(jobs))
	var wg sync.WaitGroup
	concurrency := m.maxConcurrency
	if concurrency <= 0 {
		concurrency = defaultMaxConcurrency
	}
	sem := make(chan struct{}, concurrency) // 并发限制，避免触发引擎限流

	for i, j := range jobs {
		wg.Add(1)
		go func(idx int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = m.queryOne(ctx, profile, j.prompt, j.engine)
		}(i, j)
	}
	wg.Wait()
	return results, nil
}

// queryOne 查询单个引擎并检测品牌信号。
func (m *Monitor) queryOne(ctx context.Context, profile BrandProfile, prompt string, engine models.EngineType) PromptResult {
	start := time.Now()
	pr := PromptResult{Prompt: prompt, Engine: engine}

	ad, ok := m.adapters[engine]
	if !ok {
		pr.Error = "引擎未配置适配器"
		return pr
	}

	resp, err := ad.Query(ctx, prompt)
	pr.Duration = time.Since(start)
	if err != nil {
		pr.Error = err.Error()
		return pr
	}
	pr.Answer = resp.Answer
	pr.Citations = resp.Citations

	// 记录 token 用量与成本（若已注入 ROI 追踪器）
	if m.roiTracker != nil {
		m.roiTracker.RecordFromResponse(engine, "query", resp)
	}

	// 收集品牌所有匹配别名（含关联公司名称/别名，扩大匹配范围）
	brandNames := append([]string{}, profile.Aliases...)
	companyNames := []string(nil)
	if profile.Company != nil {
		if profile.Company.Name != "" {
			companyNames = append(companyNames, profile.Company.Name)
		}
		companyNames = append(companyNames, profile.Company.Aliases...)
		brandNames = append(brandNames, companyNames...)
	}
	// 检测品牌提及
	pr.BrandMentioned, pr.BrandPosition = detectMention(pr.Answer, profile.Name, brandNames)
	// 检测品牌产品提及
	for _, prod := range profile.Products {
		if mentioned, _ := detectMention(pr.Answer, prod, nil); mentioned {
			pr.BrandMentioned = true
			break
		}
	}
	// 检测品牌官网引用（品牌域名 + 公司域名，均视为品牌引用）
	pr.BrandCited = detectCitation(resp.Citations, profile.Domain)
	if profile.Company != nil && profile.Company.Domain != "" && !pr.BrandCited {
		pr.BrandCited = detectCitation(resp.Citations, profile.Company.Domain)
	}
	// 幽灵引用：官网被引用但品牌名未在文本中出现
	pr.GhostCitation = pr.BrandCited && !pr.BrandMentioned
	// 情感分析（包含公司名作为匹配词）
	pr.Sentiment = analyzeSentiment(pr.Answer, profile.Name, append(brandNames, companyNames...))
	// 竞品提及
	pr.CompetitorMentions = detectCompetitors(pr.Answer, resp.Citations, profile.Competitors)

	return pr
}

// detectMention 检测文本中是否提及目标名称，返回是否提及与首次出现的段落位置。
//
// 使用词边界匹配避免子串误判（如 "Apple" 不匹配 "Snapple"）。
// 段落按空行分割，位置从 1 开始；未提及返回 0。
func detectMention(text, name string, aliases []string) (bool, int) {
	if name == "" {
		return false, 0
	}
	// 预计算所有名称的小写形式，避免在循环内重复 ToLower
	names := make([]string, 0, 1+len(aliases))
	for _, n := range append([]string{name}, aliases...) {
		if n != "" {
			names = append(names, strings.ToLower(n))
		}
	}
	paragraphs := strings.Split(text, "\n\n")
	for i, para := range paragraphs {
		lower := strings.ToLower(para)
		for _, n := range names {
			if containsWord(lower, n) {
				return true, i + 1
			}
		}
	}
	// 段落未命中时做全文检测（可能无空行分段）
	lower := strings.ToLower(text)
	for _, n := range names {
		if containsWord(lower, n) {
			return true, 1
		}
	}
	return false, 0
}

// containsWord 词边界匹配，避免子串误判。
func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	// 中文无需词边界，直接子串匹配
	hasCJK := func(s string) bool {
		for _, r := range s {
			if r >= 0x4e00 && r <= 0x9fff {
				return true
			}
		}
		return false
	}
	if hasCJK(word) {
		return strings.Contains(text, word)
	}
	// 英文使用词边界
	idx := strings.Index(text, word)
	for idx >= 0 {
		before := byte(' ')
		if idx > 0 {
			before = text[idx-1]
		}
		after := byte(' ')
		end := idx + len(word)
		if end < len(text) {
			after = text[end]
		}
		if !isAlnum(before) && !isAlnum(after) {
			return true
		}
		next := idx + 1
		if next >= len(text) {
			break
		}
		idx = strings.Index(text[next:], word)
		if idx < 0 {
			break
		}
		idx = next + idx
	}
	return false
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// detectCitation 检测引用列表中是否包含目标域名。
func detectCitation(citations []models.Citation, domain string) bool {
	if domain == "" || len(citations) == 0 {
		return false
	}
	domainLower := strings.ToLower(domain)
	for _, c := range citations {
		urlLower := strings.ToLower(c.URL)
		// 去掉协议后比较
		u := strings.TrimPrefix(urlLower, "https://")
		u = strings.TrimPrefix(u, "http://")
		u = strings.TrimPrefix(u, "www.")
		if strings.HasPrefix(u, domainLower) {
			return true
		}
		if strings.Contains(urlLower, domainLower) {
			return true
		}
	}
	return false
}

// detectCompetitors 检测回答中提及的竞争对手。
func detectCompetitors(text string, citations []models.Citation, competitors []Competitor) []CompetitorMention {
	var mentions []CompetitorMention
	seen := map[string]bool{}
	for _, c := range competitors {
		if seen[c.Name] {
			continue
		}
		// 合并竞品自身别名 + 所属公司名称/别名，扩大匹配范围
		extraAliases := append([]string{}, c.Aliases...)
		if c.Company != nil {
			if c.Company.Name != "" {
				extraAliases = append(extraAliases, c.Company.Name)
			}
			extraAliases = append(extraAliases, c.Company.Aliases...)
		}
		mentioned, pos := detectMention(text, c.Name, extraAliases)
		cited := detectCitation(citations, c.Domain)
		if !cited && c.Company != nil && c.Company.Domain != "" {
			cited = detectCitation(citations, c.Company.Domain)
		}
		if mentioned || cited {
			mentions = append(mentions, CompetitorMention{
				Name:     c.Name,
				Position: pos,
				Cited:    cited,
			})
			seen[c.Name] = true
		}
	}
	return mentions
}

// analyzeSentiment 分析品牌提及上下文的情感倾向。
//
// 采用关键词词典法（未配置 LLM 时的降级方案）。
// 在品牌名附近窗口内统计正负面词频，判定倾向。
func analyzeSentiment(text, name string, aliases []string) string {
	names := append([]string{name}, aliases...)
	lower := strings.ToLower(text)

	// 中英文情感词典（精简版，覆盖常见表达）
	positiveWords := []string{
		"best", "top", "leading", "recommend", "recommended", "excellent", "great",
		"popular", "trusted", "reliable", "powerful", "innovative", "favorite",
		"prefer", "preferred", "standout", "notable", "award", "leader",
		"优秀", "领先", "推荐", "最佳", "最好", "首选", "知名", "值得信赖",
		"强大", "创新", "受欢迎", "首选", "杰出", "权威", "出色",
	}
	negativeWords := []string{
		"worst", "avoid", "poor", "bad", "expensive", "limited", "outdated",
		"buggy", "slow", "complaint", "issue", "problem", "controversy",
		"差", "糟糕", "避免", "问题", "投诉", "过时", "昂贵", "局限", "负面",
	}

	posScore, negScore := 0, 0
	for _, w := range positiveWords {
		posScore += strings.Count(lower, w)
	}
	for _, w := range negativeWords {
		negScore += strings.Count(lower, w)
	}

	// 若品牌未被提及，情感中性
	brandMentioned := false
	for _, n := range names {
		if n != "" && containsWord(lower, strings.ToLower(n)) {
			brandMentioned = true
			break
		}
	}
	if !brandMentioned {
		return "neutral"
	}

	if negScore > posScore {
		return "negative"
	}
	if posScore > negScore && posScore > 0 {
		return "positive"
	}
	return "neutral"
}
