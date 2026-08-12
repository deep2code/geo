package brand

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"my-geo/internal/brand/vertical"
	"my-geo/internal/models"
)

// Reporter 报告生成器。
//
// 将监控结果与评分聚合为运营可读的报告，生成内容缺口、竞品声量、
// 负面提及摘要，并产出按优先级排序的运营行动建议。
type Reporter struct{}

// NewReporter 创建报告生成器。
func NewReporter() *Reporter { return &Reporter{} }

// Build 生成完整报告。
func (r *Reporter) Build(profile BrandProfile, results []PromptResult, stats []EngineStats, score float64, grade, tier string, breakdown ScoreBreakdown) VisibilityReport {
	entCompleteness := EntityCompleteness(profile)
	report := VisibilityReport{
		BrandName:               profile.Name,
		Industry:                profile.Industry,
		Category:                profile.Category,
		Company:                 profile.Company,
		EntityCompletenessScore: entCompleteness,
		GeneratedAt:             time.Now(),
		Score:                   score,
		Grade:                   grade,
		Tier:                    tier,
		ScoreBreakdown:          breakdown,
		EngineStats:             stats,
		Results:                 results,
	}
	report.ContentGaps = r.findContentGaps(results, profile)
	report.CompetitorSOV = r.calcCompetitorSOV(results, profile)
	report.NegativeMentions = r.findNegativeMentions(results, profile)
	report.SeverityIssues = BuildSeverityIssues(breakdown)
	// 业务类型→策略自动联动：检测行业并生成权重覆盖与运营建议
	vl := LinkVertical(profile, breakdown, score)
	if vl.Detected != "" && vl.Detected != "unknown" {
		report.VerticalLink = &vl
	}
	report.Actions = r.generateActions(profile, stats, report.ContentGaps, report.NegativeMentions, breakdown, tier)
	// 合并行业差异化建议到 Actions
	if report.VerticalLink != nil && len(report.VerticalLink.Recommendations) > 0 {
		report.Actions = appendVerticalRecommendations(report.Actions, report.VerticalLink.Recommendations)
	}
	return report
}

// appendVerticalRecommendations 将行业差异化建议转换为 ActionItem 并追加到 actions。
func appendVerticalRecommendations(actions []ActionItem, recs []vertical.Recommendation) []ActionItem {
	for _, rec := range recs {
		actions = append(actions, ActionItem{
			Priority:   rec.Priority,
			Category:   rec.Category,
			Title:      rec.Title,
			Detail:     rec.Detail,
		})
	}
	return actions
}

// findContentGaps 识别内容缺口：竞品被提及而品牌未被提及的 prompt。
//
// 参考 ai-visibility-audit 的 "missed high-intent queries" 功能。
// 这些是高优先级运营机会：运营人员应针对这些查询创建内容。
// 同一 prompt 在不同引擎上的缺口分别记录，便于定位引擎特定的内容盲点。
func (r *Reporter) findContentGaps(results []PromptResult, profile BrandProfile) []ContentGap {
	type gapKey struct {
		prompt string
		engine models.EngineType
	}
	gapMap := map[gapKey]*ContentGap{}
	for _, res := range results {
		if res.Error != "" || res.BrandMentioned {
			continue
		}
		if len(res.CompetitorMentions) == 0 {
			continue
		}
		key := gapKey{prompt: res.Prompt, engine: res.Engine}
		gap, ok := gapMap[key]
		if !ok {
			gap = &ContentGap{
				Prompt:         res.Prompt,
				Engine:         res.Engine,
				SuggestedTopic: suggestTopic(res.Prompt),
			}
			gapMap[key] = gap
		}
		for _, cm := range res.CompetitorMentions {
			gap.CompetitorNamed = appendUnique(gap.CompetitorNamed, cm.Name)
		}
	}
	gaps := make([]ContentGap, 0, len(gapMap))
	for _, g := range gapMap {
		gaps = append(gaps, *g)
	}
	// 按竞品数量降序（竞品越多，说明该 prompt 竞争越激烈，品牌缺席越严重）
	sort.Slice(gaps, func(i, j int) bool {
		return len(gaps[i].CompetitorNamed) > len(gaps[j].CompetitorNamed)
	})
	return gaps
}

// calcCompetitorSOV 计算各竞品的整体声量份额。
func (r *Reporter) calcCompetitorSOV(results []PromptResult, profile BrandProfile) []CompetitorSOV {
	mentionCount := map[string]int{}
	brandCount := 0
	for _, res := range results {
		if res.Error != "" {
			continue
		}
		if res.BrandMentioned {
			brandCount++
		}
		for _, cm := range res.CompetitorMentions {
			mentionCount[cm.Name]++
		}
	}
	total := brandCount
	for _, c := range mentionCount {
		total += c
	}
	sovs := []CompetitorSOV{{Name: profile.Name, MentionCount: brandCount}}
	if total > 0 {
		sovs[0].SOV = float64(brandCount) / float64(total) * 100
	}
	for name, c := range mentionCount {
		sov := CompetitorSOV{Name: name, MentionCount: c}
		if total > 0 {
			sov.SOV = float64(c) / float64(total) * 100
		}
		sovs = append(sovs, sov)
	}
	sort.Slice(sovs, func(i, j int) bool { return sovs[i].MentionCount > sovs[j].MentionCount })
	return sovs
}

// findNegativeMentions 提取负面提及摘要。
func (r *Reporter) findNegativeMentions(results []PromptResult, profile BrandProfile) []NegativeMention {
	var negs []NegativeMention
	// 合并品牌别名 + 公司名/公司别名，扩大负面上下文匹配范围
	aliases := append([]string{}, profile.Aliases...)
	if profile.Company != nil {
		if profile.Company.Name != "" {
			aliases = append(aliases, profile.Company.Name)
		}
		aliases = append(aliases, profile.Company.Aliases...)
	}
	for _, res := range results {
		if res.Error != "" || res.Sentiment != "negative" {
			continue
		}
		snippet := extractSnippet(res.Answer, profile.Name, aliases, 80)
		category, severity := ClassifyNegative(snippet)
		negs = append(negs, NegativeMention{
			Prompt:   res.Prompt,
			Engine:   res.Engine,
			Snippet:  snippet,
			Category: category,
			Severity: severity,
		})
	}
	return negs
}

// generateActions 生成运营行动建议，按优先级排序，指导运营人员工作方向。
//
// 四类行动：content（内容创作）/ engine（引擎优化）/ reputation（声誉管理）/ entity（实体建设）
func (r *Reporter) generateActions(profile BrandProfile, stats []EngineStats, gaps []ContentGap, negs []NegativeMention, breakdown ScoreBreakdown, tier string) []ActionItem {
	var actions []ActionItem

	// 高优先级：内容缺口（竞品出现、品牌缺席的高意图查询）
	if len(gaps) > 0 {
		tasks := make([]string, 0, len(gaps))
		top := gaps
		if len(top) > 5 {
			top = top[:5]
		}
		for _, g := range top {
			tasks = append(tasks, fmt.Sprintf("针对「%s」创建内容（当前竞品 %s 已被 AI 推荐）", g.Prompt, strings.Join(g.CompetitorNamed, "、")))
		}
		actions = append(actions, ActionItem{
			Priority:    "high",
			Category:    "content",
			Title:       "抢占高意图查询的内容缺口",
			Detail:      fmt.Sprintf("发现 %d 个高意图查询中竞品被提及而品牌缺席，这是最直接的流量流失点。运营应优先针对这些查询创作 GEO 优化内容。", len(gaps)),
			Tasks:       tasks,
			ExpectedImpact: "预计提及率提升 10-25%",
		})
	}

	// 高优先级：弱引擎补强（提及率显著低于均值的引擎）
	weakEngines := findWeakEngines(stats)
	if len(weakEngines) > 0 {
		names := make([]string, 0, len(weakEngines))
		tasks := make([]string, 0, len(weakEngines))
		for _, we := range weakEngines {
			names = append(names, string(we.Engine))
			tasks = append(tasks, fmt.Sprintf("在 %s 上提及率仅 %.0f%%，重点优化该引擎偏好内容（查看策略推荐）", we.Engine, we.MentionRate))
		}
		actions = append(actions, ActionItem{
			Priority:    "high",
			Category:    "engine",
			Title:       "补强弱表现引擎",
			Detail:      fmt.Sprintf("品牌在 %s 上可见度偏低，不同引擎检索机制差异巨大（Perplexity 引用率约 13%%，ChatGPT 仅 0.59%%），需针对性优化。", strings.Join(names, "、")),
			Tasks:       tasks,
			ExpectedImpact: "预计该引擎提及率提升 15-30%",
		})
	}

	// 高优先级：公司实体信息缺失（完备度 < 40）
	entCompleteness := EntityCompleteness(profile)
	if entCompleteness < 40 {
		detail := fmt.Sprintf("当前品牌实体信息完备度仅 %.0f/100，缺少关键的公司实体信息。AI 无法建立「品牌名 ↔ 公司 ↔ 官网」的强关联，会导致幽灵引用和品牌错配。", entCompleteness)
		tasks := []string{
			"补充品牌所属母公司/集团全称（Company.name）",
			"补充公司官网域名，用于引用关联检测（Company.domain）",
			"补充公司简介（1-2 句话），帮助 AI 建立实体关联（Company.description）",
		}
		if profile.Company == nil || profile.Company.Industry == "" {
			tasks = append(tasks, "填写公司所属行业，避免 AI 将品牌归入错误品类（Company.industry）")
		}
		actions = append(actions, ActionItem{
			Priority:       "high",
			Category:       "entity",
			Title:          "补齐公司实体信息，建立品牌-公司实体关联",
			Detail:         detail,
			Tasks:          tasks,
			ExpectedImpact: "预计实体识别维度得分 +15-25 分",
		})
	}

	// 中优先级：实体识别（幽灵引用率高 或 公司实体完备度不足）
	if breakdown.EntityRecognition < 60 || entCompleteness < 70 {
		detail := fmt.Sprintf("实体识别得分 %.0f 分，实体完备度 %.0f/100。", breakdown.EntityRecognition, entCompleteness)
		if breakdown.EntityRecognition < 60 {
			detail += "存在较多幽灵引用（官网被引用但品牌名未被提及），说明 AI 尚未建立品牌名与官网的实体关联。"
		} else {
			detail += "当前公司实体信息仍不够丰富，可进一步完善以增强跨引擎实体关联一致性。"
		}
		tasks := []string{
			"在官网首页显著位置强化「品牌名 + 所属公司 + 核心业务」三者关联表述",
			"部署 Organization + Brand 类型的 JSON-LD 结构化数据（明确 brand.parentOrganization 指向公司）",
		}
		if profile.Company != nil && profile.Company.Domain != "" {
			tasks = append(tasks, fmt.Sprintf("确保公司官网（%s）通过链接或提及方式引用品牌官网，强化双向关联", profile.Company.Domain))
		}
		tasks = append(tasks,
			"在 Wikipedia、百度百科等知识库建立品牌词条，并在词条中声明所属公司",
			"确保品牌名在第三方评测、榜单文章中被准确使用，并明确所属公司")
		actions = append(actions, ActionItem{
			Priority:       "medium",
			Category:       "entity",
			Title:          "提升品牌实体识别度（品牌+公司双实体）",
			Detail:         detail,
			Tasks:          tasks,
			ExpectedImpact: "预计幽灵引用率下降 30-50%",
		})
	}

	// 中优先级：声量份额提升（SOV 偏低）
	if breakdown.ShareOfVoice < 30 && len(profile.Competitors) > 0 {
		actions = append(actions, ActionItem{
			Priority: "medium",
			Category: "content",
			Title:    "提升品类声量份额",
			Detail:   fmt.Sprintf("当前声量份额仅 %.0f 分，落后于竞品。需增加品牌在「最佳/推荐/对比」类内容中的曝光。", breakdown.ShareOfVoice),
			Tasks: []string{
				"争取进入行业「Top N 工具/产品」榜单文章",
				"与权威媒体合作发布品类对比评测",
				"在知乎、CSDN 等平台发布对比类内容，自然植入品牌",
				"鼓励用户在 Reddit、小红书等社区讨论品牌",
			},
			ExpectedImpact: "预计 SOV 提升 10-20%",
		})
	}

	// 中优先级：引用位置优化
	if breakdown.CitationPosition < 50 {
		actions = append(actions, ActionItem{
			Priority: "medium",
			Category: "content",
			Title:    "优化引用位置",
			Detail:   fmt.Sprintf("品牌平均提及位置靠后（得分 %.0f），靠前位置更易被采纳。AI 回答首段提及的品牌转化率显著更高。", breakdown.CitationPosition),
			Tasks: []string{
				"创作结论前置的内容（核心观点放首段）",
				"在内容开头明确建立品牌与品类的关联",
				"补充权威引用与统计数据增强首段可信度",
			},
			ExpectedImpact: "预计平均位置提升 1-2 位",
		})
	}

	// 低优先级：情感管理
	if breakdown.Sentiment < 60 && len(negs) > 0 {
		actions = append(actions, ActionItem{
			Priority: "low",
			Category: "reputation",
			Title:    "管理品牌情感倾向",
			Detail:   fmt.Sprintf("正面提及率仅 %.0f%%，存在 %d 条负面提及。AI 会综合网络信息形成品牌印象，负面内容会影响推荐。", breakdown.Sentiment, len(negs)),
			Tasks: []string{
				"排查负面提及对应的网络信息源，进行澄清或优化",
				"增加正面用户评价、获奖、权威背书等内容的发布",
				"监控并回应第三方平台的负面评价",
			},
			ExpectedImpact: "预计正面提及率提升至 70%+",
		})
	}

	// 低优先级：长尾品牌梯队提升
	if tier == "niche" {
		actions = append(actions, ActionItem{
			Priority: "low",
			Category: "content",
			Title:    "从长尾向中坚梯队跃迁",
			Detail:   "品牌当前处于长尾梯队（提及率 < 30%）。参考行业基准，需系统性增加品牌在 AI 训练语料中的密度。",
			Tasks: []string{
				"制定月度 GEO 内容发布计划，覆盖核心品类查询",
				"在权威行业媒体建立长期内容专栏",
				"建立品牌官方 llms.txt，引导 AI 爬虫理解品牌",
				"定期复测本报告，追踪梯队跃迁进度",
			},
			ExpectedImpact: "目标 6 个月内提及率突破 30%",
		})
	}

	return actions
}

// suggestTopic 根据查询词建议运营内容主题。
//
// 策略：去除常见的引导词（"最好的"、"推荐"、"top"等）和尾部标点，
// 剩余核心名词短语作为内容主题；若无法抽取则返回原 prompt。
func suggestTopic(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	// 中/英文停用词前缀：常见问题引导词，去除后保留核心主题
	prefixStops := []string{
		"最好的", "最佳", "推荐", "什么是", "如何选择", "怎么选", "哪些",
		"求推荐", "对比", "评测", "排名", "排行榜", "top", "Top", "TOP",
		"best", "Best", "BEST", "recommend", "Recommend", "what is", "how to",
		"what are", "which", "vs", "VS", "Vs",
	}
	// 中/英文停用词后缀
	suffixStops := []string{
		"有哪些", "哪个好", "怎么样", "推荐一下", "排行榜", "对比", "评测",
		"排名", "好用吗", "比较好", "哪个比较好", "哪家好",
		"?", "？", "!", "！", ".", "。", " ", "	",
	}

	topic := strings.TrimSpace(prompt)
	// 先去尾部，反复迭代直到无变化
	changed := true
	for changed {
		changed = false
		for _, suf := range suffixStops {
			if strings.HasSuffix(topic, suf) {
				topic = strings.TrimSpace(strings.TrimSuffix(topic, suf))
				changed = true
			}
		}
	}
	// 再去前缀
	for _, pre := range prefixStops {
		if strings.HasPrefix(topic, pre) {
			topic = strings.TrimSpace(strings.TrimPrefix(topic, pre))
		}
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return prompt
	}
	return topic
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// findWeakEngines 找出提及率显著低于均值的引擎。
func findWeakEngines(stats []EngineStats) []EngineStats {
	var valid []EngineStats
	sum := 0.0
	for _, s := range stats {
		if s.TotalPrompts > 0 {
			valid = append(valid, s)
			sum += s.MentionRate
		}
	}
	if len(valid) == 0 {
		return nil
	}
	avg := sum / float64(len(valid))
	var weak []EngineStats
	for _, s := range valid {
		if s.MentionRate < avg*0.6 && s.Configured {
			weak = append(weak, s)
		}
	}
	return weak
}

// extractSnippet 提取品牌名附近的上下文片段。
func extractSnippet(text, name string, aliases []string, maxLen int) string {
	lower := strings.ToLower(text)
	names := append([]string{name}, aliases...)
	for _, n := range names {
		if n == "" {
			continue
		}
		idx := strings.Index(lower, strings.ToLower(n))
		if idx >= 0 {
			start := idx - maxLen/2
			if start < 0 {
				start = 0
			}
			end := idx + len(n) + maxLen/2
			if end > len(text) {
				end = len(text)
			}
			snippet := text[start:end]
			if start > 0 {
				snippet = "..." + snippet
			}
			if end < len(text) {
				snippet = snippet + "..."
			}
			return snippet
		}
	}
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}
