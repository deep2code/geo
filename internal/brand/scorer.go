package brand

import (
	"cmp"
	"math"
	"slices"

	"my-geo/internal/models"
)

// Scorer 品牌可见度评分器。
//
// 基于 Ranqo 论文 5 维指标 + Amicited 实体识别维度，
// 计算 0-100 的品牌可见度评分（BVS）。
type Scorer struct{}

// NewScorer 创建评分器。
func NewScorer() *Scorer { return &Scorer{} }

// EntityCompleteness 评估品牌实体信息完备度（0-100）。
//
// 公司信息越完整（公司名、域名、别名、行业等字段填写率越高），
// AI 越容易将品牌名 ↔ 公司 ↔ 官网建立实体关联，减少幽灵引用。
func EntityCompleteness(profile BrandProfile) float64 {
	score := 0.0
	total := 8.0
	// 基础品牌字段
	if profile.Name != "" {
		score++
	}
	if profile.Domain != "" {
		score++
	}
	if profile.Industry != "" {
		score += 0.5
	}
	if len(profile.Products) > 0 {
		score++
	}
	// 公司字段（权重加倍：公司实体是关联的核心）
	if profile.Company != nil {
		if profile.Company.Name != "" {
			score += 1.5
		}
		if profile.Company.Domain != "" {
			score += 1
		}
		if len(profile.Company.Aliases) > 0 {
			score += 0.5
		}
		if profile.Company.Industry != "" {
			score += 0.5
		}
		if profile.Company.Description != "" {
			score += 0.5
		}
	}
	return min(score/total*100, 100)
}

// Aggregate 聚合检测结果，生成各引擎统计。
func (s *Scorer) Aggregate(results []PromptResult, profile BrandProfile, configuredEngines map[models.EngineType]bool) []EngineStats {
	statsByEngine := map[models.EngineType]*EngineStats{}
	for _, r := range results {
		st, ok := statsByEngine[r.Engine]
		if !ok {
			st = &EngineStats{Engine: r.Engine, Configured: configuredEngines[r.Engine]}
			statsByEngine[r.Engine] = st
		}
		if r.Error != "" {
			st.TotalPrompts++
			continue
		}
		st.TotalPrompts++
		if st.competitorMentions == nil {
			st.competitorMentions = map[string]int{}
		}
		if r.BrandMentioned {
			st.MentionCount++
			st.brandPositions = append(st.brandPositions, r.BrandPosition)
			switch r.Sentiment {
			case "positive":
				st.SentimentPositive++
			case "negative":
				st.SentimentNegative++
			default:
				st.SentimentNeutral++
			}
		}
		if r.BrandCited {
			st.CitationCount++
		}
		if r.GhostCitation {
			st.GhostCitationCount++
		}
		// 竞品提及计数（用于 SOV）
		for _, cm := range r.CompetitorMentions {
			st.competitorMentions[cm.Name]++
		}
	}

	// 转换为切片并计算比率
	stats := make([]EngineStats, 0, len(statsByEngine))
	for _, st := range statsByEngine {
		if st.TotalPrompts > 0 {
			st.MentionRate = float64(st.MentionCount) / float64(st.TotalPrompts) * 100
			st.CitationRate = float64(st.CitationCount) / float64(st.TotalPrompts) * 100
			if len(st.brandPositions) > 0 {
				sum := 0
				for _, p := range st.brandPositions {
					sum += p
				}
				st.AvgPosition = float64(sum) / float64(len(st.brandPositions))
			}
			if st.MentionCount > 0 {
				st.PositiveRate = float64(st.SentimentPositive) / float64(st.MentionCount) * 100
			}
			// SOV = 品牌提及 / (品牌提及 + 竞品提及)
			totalMentions := st.MentionCount
			for _, c := range st.competitorMentions {
				totalMentions += c
			}
			if totalMentions > 0 {
				st.ShareOfVoice = float64(st.MentionCount) / float64(totalMentions) * 100
			}
		}
		stats = append(stats, *st)
	}
	slices.SortFunc(stats, func(a, b EngineStats) int { return cmp.Compare(string(a.Engine), string(b.Engine)) })
	return stats
}

// BVS 7 维权重常量（参考 Claude SEO 权重体系，合计 1.0）。
const (
	WeightContentQuality    = 0.23 // 内容质量 23%
	WeightTechnicalSEO      = 0.22 // 技术 SEO 22%
	WeightOnPageSEO         = 0.20 // 站内 SEO 20%
	WeightSchema            = 0.10 // Schema 结构化 10%
	WeightPerformance       = 0.10 // 页面性能 10%
	WeightAIReadiness       = 0.10 // AI 搜索就绪 10%
	WeightImageOptimization = 0.05 // 图像优化 5%
)

// Score 计算品牌可见度评分（BVS 0-100）。
//
// 采用 7 维加权健康评分体系（参考 Claude SEO 权重）：
//   - 内容质量 23%：引用率反映内容被 AI 引擎引用的频率与质量
//   - 技术 SEO 22%：实体识别度反映品牌实体结构化基础
//   - 站内 SEO 20%：提及率反映站内优化在 AI 回答中的曝光效果
//   - Schema 10%：实体完备度与幽灵引用率反映结构化数据质量
//   - 页面性能 10%：引用位置间接反映页面加载与用户体验
//   - AI 搜索就绪 10%：正面情感率与低幽灵引用反映 AI 友好度
//   - 图像优化 5%：无图片数据时取中性默认分
//
// 同时保留引擎可见度 6 维（MentionRate 等）用于历史兼容与引擎级分析，
// 以及 E-E-A-T 四维（对齐 Google 质量评估准则）反映品牌可信度信号。
// entityCompleteness (0-100) 来自 EntityCompleteness()，为 0 表示无公司信息。
func (s *Scorer) Score(stats []EngineStats, entityCompleteness float64) (float64, string, string, ScoreBreakdown) {
	return s.ScoreWithProfile(stats, nil, entityCompleteness)
}

// ScoreWithProfile 计算品牌可见度评分，支持传入 BrandProfile 以计算 E-E-A-T 维度。
//
// profile 为 nil 时 E-E-A-T 维度退化为中性默认分（50），不影响 BVS 7 维计算。
func (s *Scorer) ScoreWithProfile(stats []EngineStats, profile *BrandProfile, entityCompleteness float64) (float64, string, string, ScoreBreakdown) {
	if len(stats) == 0 {
		return 0, "F", "niche", ScoreBreakdown{}
	}

	// 跨引擎平均（各引擎等权；可扩展为按引擎用户量加权）
	var avgMention, avgCitation, avgSOV, avgPos, avgSentiment, avgGhost float64
	validEngines := 0
	for _, st := range stats {
		if st.TotalPrompts == 0 {
			continue
		}
		validEngines++
		avgMention += st.MentionRate
		avgCitation += st.CitationRate
		avgSOV += st.ShareOfVoice
		// 位置得分：1=100分，越靠后越低（位置 5 约为 20 分）
		if st.MentionCount > 0 && st.AvgPosition > 0 {
			avgPos += positionScore(st.AvgPosition)
		}
		avgSentiment += st.PositiveRate
		// 幽灵引用率（越低越好）
		if st.CitationCount > 0 {
			avgGhost += float64(st.GhostCitationCount) / float64(st.CitationCount) * 100
		}
	}
	if validEngines == 0 {
		return 0, "F", "niche", ScoreBreakdown{}
	}
	avgMention /= float64(validEngines)
	avgCitation /= float64(validEngines)
	avgSOV /= float64(validEngines)
	avgPos /= float64(validEngines)
	avgSentiment /= float64(validEngines)
	avgGhost /= float64(validEngines)

	// 实体识别得分 = (100 - 幽灵引用率) × 0.8 + 实体完备度 × 0.2
	ghostBase := 100 - avgGhost
	if ghostBase < 0 {
		ghostBase = 0
	}
	entityScore := ghostBase*0.8 + math.Max(entityCompleteness, 0)*0.2
	if entityScore > 100 {
		entityScore = 100
	}
	// 引擎可见度 6 维归一化（保留用于历史兼容）
	mentionScore := math.Min(avgMention/75*100, 100)
	citationScore := math.Min(avgCitation/15*100, 100)
	sovScore := math.Min(avgSOV/30*100, 100)
	sentimentScore := math.Min(avgSentiment/80*100, 100)

	// --- BVS 7 维计算（从引擎可见度指标映射）---
	// 内容质量：引用率为主（60%），辅以声量份额（40%）
	contentQuality := citationScore*0.6 + sovScore*0.4
	if contentQuality > 100 {
		contentQuality = 100
	}
	// 技术 SEO：实体识别度为主（70%），实体完备度辅之（30%）
	techSEO := entityScore*0.7 + math.Max(entityCompleteness, 0)*0.3
	if techSEO > 100 {
		techSEO = 100
	}
	// 站内 SEO：提及率为主（70%），引用位置辅之（30%）
	onPageSEO := mentionScore*0.7 + avgPos*0.3
	if onPageSEO > 100 {
		onPageSEO = 100
	}
	// Schema：实体完备度为主（60%），幽灵引用率辅之（40%）
	schemaScore := math.Max(entityCompleteness, 0)*0.6 + ghostBase*0.4
	if schemaScore > 100 {
		schemaScore = 100
	}
	// 页面性能：引用位置为主（80%），提及率辅之（20%）
	perfScore := avgPos*0.8 + mentionScore*0.2
	if perfScore > 100 {
		perfScore = 100
	}
	// AI 搜索就绪：情感正面率为主（50%），低幽灵引用辅之（50%）
	aiReadyScore := sentimentScore*0.5 + ghostBase*0.5
	if aiReadyScore > 100 {
		aiReadyScore = 100
	}
	// 图像优化：无图片数据时取中性默认分 60
	imageScore := 60.0

	breakdown := ScoreBreakdown{
		// 引擎可见度 6 维（历史兼容）
		MentionRate:       round(mentionScore),
		CitationRate:      round(citationScore),
		ShareOfVoice:      round(sovScore),
		CitationPosition:  round(avgPos),
		Sentiment:         round(sentimentScore),
		EntityRecognition: round(entityScore),
		// BVS 7 维
		ContentQuality:    round(contentQuality),
		TechnicalSEO:      round(techSEO),
		OnPageSEO:         round(onPageSEO),
		Schema:            round(schemaScore),
		Performance:       round(perfScore),
		AIReadiness:       round(aiReadyScore),
		ImageOptimization: round(imageScore),
	}

	// E-E-A-T 四维评分（对齐 Google 质量评估准则）
	if profile != nil {
		eeat := ScoreEEAT(*profile, avgPos, avgGhost, entityCompleteness)
		breakdown.Experience = round(eeat.Experience)
		breakdown.Expertise = round(eeat.Expertise)
		breakdown.Authoritativeness = round(eeat.Authoritativeness)
		breakdown.Trustworthiness = round(eeat.Trustworthiness)
	} else {
		// 无 profile 时取中性默认分
		breakdown.Experience = 50
		breakdown.Expertise = 50
		breakdown.Authoritativeness = 50
		breakdown.Trustworthiness = 50
	}

	// BVS = 7 维加权求和
	bvs := breakdown.ContentQuality*WeightContentQuality +
		breakdown.TechnicalSEO*WeightTechnicalSEO +
		breakdown.OnPageSEO*WeightOnPageSEO +
		breakdown.Schema*WeightSchema +
		breakdown.Performance*WeightPerformance +
		breakdown.AIReadiness*WeightAIReadiness +
		breakdown.ImageOptimization*WeightImageOptimization

	bvs = round(bvs)
	grade := gradeOf(bvs)
	tier := tierOf(avgMention)
	return bvs, grade, tier, breakdown
}

// SeverityOf 将维度得分映射为严重级别。
//
// <40 → Critical（阻断级），40-60 → High，60-80 → Medium，≥80 → Low。
func SeverityOf(score float64) Severity {
	switch {
	case score < 40:
		return SeverityCritical
	case score < 60:
		return SeverityHigh
	case score < 80:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// CriticalDimensions 返回 BVS 7 维中 Critical/High 级别的维度名列表，
// 供报告与告警使用，帮助运营快速定位最需修复的维度。
func CriticalDimensions(bd ScoreBreakdown) []string {
	var critical []string
	dims := []struct {
		name  string
		score float64
	}{
		{"内容质量", bd.ContentQuality},
		{"技术SEO", bd.TechnicalSEO},
		{"站内SEO", bd.OnPageSEO},
		{"Schema", bd.Schema},
		{"页面性能", bd.Performance},
		{"AI就绪", bd.AIReadiness},
		{"图像优化", bd.ImageOptimization},
	}
	for _, d := range dims {
		sev := SeverityOf(d.score)
		if sev == SeverityCritical || sev == SeverityHigh {
			critical = append(critical, d.name)
		}
	}
	return critical
}

// positionScore 将平均提及位置转换为 0-100 得分。
//
// 位置 1（首个提及）= 100 分，位置每+1 扣 20 分，最低 0 分。
func positionScore(avgPos float64) float64 {
	score := 100 - (avgPos-1)*20
	if score < 0 {
		return 0
	}
	return score
}

func gradeOf(score float64) string {
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

// tierOf 根据 mention rate 判定品牌梯队（参考 Ranqo 论文三梯队）。
//
// household（头部）：提及率 ≥ 60%
// midmarket（中坚）：提及率 ≥ 30%
// niche（长尾）：提及率 < 30%
func tierOf(mentionRate float64) string {
	switch {
	case mentionRate >= 60:
		return "household"
	case mentionRate >= 30:
		return "midmarket"
	default:
		return "niche"
	}
}

func round(v float64) float64 {
	return math.Round(v*10) / 10
}
