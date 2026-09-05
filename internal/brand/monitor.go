package brand

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/brand/llmanalysis"
	"my-geo/internal/brand/roi"
	"my-geo/internal/models"
)

// Monitor 品牌可见度监控引擎。
//
// 查询多个 AI 引擎，检测品牌提及、引用、情感、位置、竞品、幽灵引用。
// 设计参考 ai-brand-monitor-mcp 与 oneglanse 的多引擎并行查询。
type Monitor struct {
	adapters       map[models.EngineType]adapter.Adapter
	maxConcurrency int          // 并行查询的最大并发数（默认 5）
	roiTracker     *roi.Tracker // 可选：token 用量与成本追踪
	judge          *llmanalysis.Analyzer // 可选：LLM 判定层（情感/源情报/准确性）
	samples        int          // 采样次数：每 prompt×engine 重复查询 N 次，多数票判定（默认 1）
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

// Adapters 返回引擎适配器映射（供模拟器等模块使用）。
func (m *Monitor) Adapters() map[models.EngineType]adapter.Adapter {
	return m.adapters
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

// WithJudge 注入 LLM 判定层。
//
// judge 适配器（建议强推理模型）用于把情感/源情报/准确性从"词典法"升级为 LLM 推理。
// 未配置（或适配器未 Configured）时，判定层自动降级到词典法，系统行为不变。
func (m *Monitor) WithJudge(judge adapter.Adapter) *Monitor {
	if judge != nil {
		m.judge = llmanalysis.New(judge)
	}
	return m
}

// WithSamples 设置采样次数：每个「查询词×引擎」重复查询 N 次，多数票判定。
//
// 目的：LLM 回答有随机方差，单次查询结果不可复现。N=3 时取多数票，
// 提及/引用/情感判定更稳定，并输出一致性（consistency）供置信度参考。
// n <= 1 时保持单次查询（默认，与旧版行为一致）。
func (m *Monitor) WithSamples(n int) *Monitor {
	if n > 1 {
		m.samples = n
	}
	return m
}

// Samples 返回当前采样次数（默认 1）。
func (m *Monitor) Samples() int {
	if m.samples < 1 {
		return 1
	}
	return m.samples
}

// Judge 返回 LLM 判定层（可能为 nil）。
func (m *Monitor) Judge() *llmanalysis.Analyzer { return m.judge }

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

// queryOne 查询单个引擎并检测品牌信号（支持多次采样）。
//
// 采样次数取 max(profile.Samples, monitor.Samples)，≥2 且引擎已配置时对同一
// 查询重复 N 次，经 mergeSampled 多数票合并（提及/引用/情感），并输出一致性。
// 单次查询（默认）或未配置 API Key（模拟响应）时行为与旧版完全一致。
func (m *Monitor) queryOne(ctx context.Context, profile BrandProfile, prompt string, engine models.EngineType) PromptResult {
	samples := m.samples
	if profile.Samples > 1 {
		samples = profile.Samples
	}
	if samples < 1 {
		samples = 1
	}
	ad, ok := m.adapters[engine]
	if !ok {
		return PromptResult{Prompt: prompt, Engine: engine, Error: "引擎未配置适配器"}
	}
	if samples <= 1 || !ad.Configured() {
		return m.queryOnce(ctx, profile, prompt, engine)
	}
	prs := make([]PromptResult, 0, samples)
	for i := 0; i < samples; i++ {
		prs = append(prs, m.queryOnce(ctx, profile, prompt, engine))
	}
	return mergeSampled(prs)
}

// mergeSampled 对 N 次采样的结果做多数票合并。
//
// 规则：
//   - BrandMentioned / BrandCited：票数 > 有效采样数一半
//   - BrandPosition：被提及采样中的最早位置
//   - Sentiment：票数最多的倾向
//   - CompetitorMentions：并集去重（任一采样引用即 Cited）
//   - Consistency：提及票数 / 有效采样数（1=完全一致）
//   - Answer / ExtractedSources 取第一个有效采样
func mergeSampled(prs []PromptResult) PromptResult {
	n := len(prs)
	if n == 0 {
		return PromptResult{}
	}
	out := prs[0]
	mentionVotes, citedVotes, errors := 0, 0, 0
	sentimentVotes := map[string]int{}
	earliestPos := 0
	compSeen := map[string]CompetitorMention{}

	for _, p := range prs {
		if p.Error != "" {
			errors++
			continue
		}
		if p.BrandMentioned {
			mentionVotes++
			if earliestPos == 0 || (p.BrandPosition > 0 && p.BrandPosition < earliestPos) {
				earliestPos = p.BrandPosition
			}
		}
		if p.BrandCited {
			citedVotes++
		}
		sentimentVotes[p.Sentiment]++
		for _, cm := range p.CompetitorMentions {
			prev, ok := compSeen[cm.Name]
			if !ok {
				compSeen[cm.Name] = cm
				continue
			}
			if cm.Cited && !prev.Cited {
				prev.Cited = true
			}
			if prev.Position == 0 && cm.Position > 0 {
				prev.Position = cm.Position
			}
			compSeen[cm.Name] = prev
		}
	}
	if errors == n {
		out.Error = prs[0].Error
		return out
	}
	valid := n - errors
	half := float64(valid) / 2

	out.Error = ""
	out.Samples = n
	out.MentionVotes = mentionVotes
	out.CitedVotes = citedVotes
	out.BrandMentioned = float64(mentionVotes) > half
	out.BrandCited = float64(citedVotes) > half
	out.GhostCitation = out.BrandCited && !out.BrandMentioned
	out.BrandPosition = earliestPos
	out.Consistency = float64(mentionVotes) / float64(valid)

	// 情感多数票
	best, bestN := "neutral", 0
	for s, c := range sentimentVotes {
		if c > bestN {
			best, bestN = s, c
		}
	}
	out.Sentiment = best

	comps := make([]CompetitorMention, 0, len(compSeen))
	for _, cm := range compSeen {
		comps = append(comps, cm)
	}
	out.CompetitorMentions = comps

	var dur time.Duration
	for _, p := range prs {
		dur += p.Duration
	}
	out.Duration = dur / time.Duration(n)
	return out
}

// queryOnce 执行单次引擎查询并检测品牌信号。
func (m *Monitor) queryOnce(ctx context.Context, profile BrandProfile, prompt string, engine models.EngineType) PromptResult {
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
	// 情感分析（包含公司名作为匹配词）。
	// 优先用 LLM 判定层；未配置时降级到词典法（analyzeSentiment）。
	if m.judge != nil && m.judge.Enabled() {
		label, reason, conf := m.judge.Sentiment(ctx, profile.Name, append(brandNames, companyNames...), pr.Answer)
		pr.Sentiment = label
		pr.SentimentConfidence = conf
		pr.LLMJudged = true
		_ = reason
	} else {
		pr.Sentiment = analyzeSentiment(pr.Answer, profile.Name, append(brandNames, companyNames...))
	}
	// 源情报：LLM 识别回答"采信了谁"（降级为正则 URL）。
	if m.judge != nil && m.judge.Enabled() {
		if srcs, err := m.judge.ExtractSources(ctx, pr.Answer, profile.Domain); err == nil && len(srcs) > 0 {
			pr.ExtractedSources = srcs
		}
	}
	// 竞品提及
	pr.CompetitorMentions = detectCompetitors(pr.Answer, resp.Citations, profile.Competitors)
	// 单次查询明确标注 Samples=1（未采样），与多次采样结果字段对齐。
	pr.Samples = 1

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
// 只做主机名精确/子域匹配，不做子串匹配——避免 "acme.com" 误判命中 "acme.com.cn"。
func detectCitation(citations []models.Citation, domain string) bool {
	if domain == "" || len(citations) == 0 {
		return false
	}
	domainLower := strings.TrimPrefix(strings.ToLower(domain), "www.")
	for _, c := range citations {
		host := hostOf(c.URL)
		if host == domainLower || strings.HasSuffix(host, "."+domainLower) {
			return true
		}
	}
	return false
}

// hostOf 提取 URL 的小写主机名（去协议、userinfo、端口、路径、www. 前缀）。
func hostOf(raw string) string {
	u := strings.ToLower(strings.TrimSpace(raw))
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "//")
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimPrefix(u, "www.")
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
