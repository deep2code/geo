// Package scorer GEO 评分引擎。
//
// 实现 AutoGEO (ICLR 2026) 的 GEO Score（可见度）+ geo-optimizer-skill
// 的 0-100 多维评分体系。评分综合可引用性信号、结构信号、负向信号，
// 并结合策略效果系数预估优化后的可见度提升。
package scorer

import (
	"my-geo/internal/config"
	"my-geo/internal/models"
)

// 评分维度满分上限，五维合计 100 分。
const (
	maxCitabilityScore   = 35.0 // 可引用性信号
	maxStructureScore    = 25.0 // 结构信号
	maxQualityScore      = 15.0 // 内容质量
	maxRetrievalScore    = 15.0 // 检索友好度（SAGEO Arena 2026）
	maxNegativeScore     = 10.0 // 负向信号（扣分制）
)

// 以下权重/参数默认值对标 AutoGEO 策略效果系数。全部为可覆盖变量（var），
// 支持按行业/引擎偏好调参——调用 OverrideWeights 在启动时一次性覆盖，
// 避免频繁调参需要改代码重编译（对标 AutoGEO rule extraction 的配置化思路）。

// 可引用性信号各信号权重（参考策略效果系数）。
var (
	weightQuotation      = 8.0
	weightStatistics     = 7.0
	weightCiteSources    = 7.0
	weightFluency        = 6.0
	weightAuthoritative  = 5.0
	weightTechnicalTerms = 4.0
	weightUniqueWords    = 3.0
)

// 结构信号各信号权重。
var (
	weightHeadingHierarchy  = 8.0
	weightFrontLoading      = 7.0
	weightLists             = 5.0
	weightDefinitionOpening = 5.0
	weightTables            = 3.0
	weightFAQ               = 2.0
)

// 内容质量评分参数。
var (
	qualityFullWordMin    = 300  // 词数达标（拿满分）下限
	qualityFullWordMax    = 2000 // 词数达标上限
	qualityPartialWordMin = 100  // 词数部分得分下限
	qualityFullScore      = 8.0  // 词数达标得分
	qualityPartialScore   = 5.0  // 词数部分得分
	qualityFloorScore     = 2.0  // 有词即得的最低分
	evergreenScoreWeight  = 12.0 // 常青度（0-100）折合权重
)

// 负向信号扣分参数。
var (
	negativePenaltyPerSignal = 2.5 // 每个负向信号扣分
	negativeFullSignals      = 4   // 达到该数量扣满本维度
)

// scoreRetrieval 检索友好度评分。
func scoreRetrieval(rs *models.RetrievalSignals) float64 {
	if rs == nil {
		return 0
	}
	// 直接使用分析器计算的 RetrievalScore（0-100），折合到满分 15
	return rs.RetrievalScore / 100.0 * maxRetrievalScore
}

// EstimateVisibility 可见度预估参数。
var (
	positionScoreDivisor  = 100.0 // 评分 → 0-1 位置分
	citationFreqBaseDiv   = 20.0  // 评分 → 基准引用频率
	citationFreqBoost     = 5.0   // 每个策略带来的引用频率提升
	citationOrderBase     = 10    // 引用次序基准
	citationOrderDiv      = 12.0  // 评分 → 引用次序改善幅度（float64，参与浮点除法）
	semanticSimilarityDiv = 2.0   // 策略提升折算进语义相似度
)

// EstimateUtility 效用预估参数。
var (
	utilityHighQuality    = 0.9  // 高质量（有引用来源/无负向信号）
	utilityLowQuality     = 0.5  // 低质量（无引用来源）
	keypointCoverageDiv   = 5.0  // 结构信号数 → 关键点覆盖率
	utilityQualityFloor   = 0.3  // 回答质量下限
	utilityQualityPenalty = 0.15 // 每个负向信号的回答质量扣分
	utilityDimensionCount = 3.0  // 效用指标维度数（求均值）
)

// OverrideWeights 覆盖评分权重（按名称）。零值/非法名称忽略，仅覆盖合法键。
// 应在启动早期调用一次（Scorer 创建前），非并发安全。
// 支持的键：quotation / statistics / cite_sources / fluency / authoritative /
// technical_terms / unique_words / heading_hierarchy / front_loading / lists /
// definition_opening / tables / faq / negative_penalty / evergreen / retrieval。
func OverrideWeights(w map[string]float64) {
	if len(w) == 0 {
		return
	}
	set := func(name string, dst *float64) {
		if v, ok := w[name]; ok && v >= 0 {
			*dst = v
		}
	}
	set("quotation", &weightQuotation)
	set("statistics", &weightStatistics)
	set("cite_sources", &weightCiteSources)
	set("fluency", &weightFluency)
	set("authoritative", &weightAuthoritative)
	set("technical_terms", &weightTechnicalTerms)
	set("unique_words", &weightUniqueWords)
	set("heading_hierarchy", &weightHeadingHierarchy)
	set("front_loading", &weightFrontLoading)
	set("lists", &weightLists)
	set("definition_opening", &weightDefinitionOpening)
	set("tables", &weightTables)
	set("faq", &weightFAQ)
	set("negative_penalty", &negativePenaltyPerSignal)
	set("evergreen", &evergreenScoreWeight)
}

// Scorer 评分引擎。
type Scorer struct {
	analyzer AnalysisProvider
}

// AnalysisProvider 内容分析提供者接口（解耦 analyzer 包）。
type AnalysisProvider interface {
	Analyze(content string) *models.ContentAnalysis
}

// New 创建评分引擎。
func New(a AnalysisProvider) *Scorer {
	return &Scorer{analyzer: a}
}

// Score 对内容评分，返回 0-100 分与明细。
func (s *Scorer) Score(content string) (float64, []models.ScoreBreakdown) {
	analysis := s.analyzer.Analyze(content)
	return s.ScoreFromAnalysis(analysis)
}

// Analyze 分析内容，委托给底层分析器。
func (s *Scorer) Analyze(content string) *models.ContentAnalysis {
	return s.analyzer.Analyze(content)
}

// ScoreFromAnalysis 基于已有分析结果评分。
func (s *Scorer) ScoreFromAnalysis(a *models.ContentAnalysis) (float64, []models.ScoreBreakdown) {
	var breakdowns []models.ScoreBreakdown

	// 可引用性信号（满分 35）
	citScore, citDetail := scoreCitability(a.CitabilitySignals)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "citability", Score: citScore, MaxScore: maxCitabilityScore, Detail: citDetail,
	})

	// 结构信号（满分 25）
	struScore, struDetail := scoreStructure(a.StructureSignals)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "structure", Score: struScore, MaxScore: maxStructureScore, Detail: struDetail,
	})

	// 内容质量（满分 15）—— 基于词数与常青度
	qualityScore := scoreQuality(a)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "quality", Score: qualityScore, MaxScore: maxQualityScore,
	})

	// 检索友好度（满分 15）—— SAGEO Arena 2026
	retrievalScore := scoreRetrieval(a.RetrievalSignals)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "retrieval_friendliness", Score: retrievalScore, MaxScore: maxRetrievalScore,
	})

	// 负向信号扣分（满分 10）
	negScore := maxNegativeScore - float64(len(a.NegativeSignals))*negativePenaltyPerSignal
	if negScore < 0 {
		negScore = 0
	}
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "negative_penalty", Score: negScore, MaxScore: maxNegativeScore,
	})

	total := citScore + struScore + qualityScore + retrievalScore + negScore
	maxTotal := maxCitabilityScore + maxStructureScore + maxQualityScore + maxRetrievalScore + maxNegativeScore
	if total > maxTotal {
		total = maxTotal
	}
	return total, breakdowns
}

// scoreCitability 可引用性评分。
func scoreCitability(signals map[string]bool) (float64, string) {
	// 每个信号权重不同（参考效果系数）
	weights := map[string]float64{
		"quotation":       weightQuotation,
		"statistics":      weightStatistics,
		"cite_sources":    weightCiteSources,
		"fluency":         weightFluency,
		"authoritative":   weightAuthoritative,
		"technical_terms": weightTechnicalTerms,
		"unique_words":    weightUniqueWords,
	}
	var score float64
	for sig, w := range weights {
		if signals[sig] {
			score += w
		}
	}
	if score > maxCitabilityScore {
		score = maxCitabilityScore
	}
	return score, ""
}

// scoreStructure 结构评分。
func scoreStructure(signals map[string]bool) (float64, string) {
	weights := map[string]float64{
		"heading_hierarchy":   weightHeadingHierarchy,
		"front_loading":       weightFrontLoading,
		"lists":               weightLists,
		"definition_openings": weightDefinitionOpening,
		"tables":              weightTables,
		"faq":                 weightFAQ,
	}
	var score float64
	for sig, w := range weights {
		if signals[sig] {
			score += w
		}
	}
	if score > maxStructureScore {
		score = maxStructureScore
	}
	return score, ""
}

// scoreQuality 内容质量评分。
func scoreQuality(a *models.ContentAnalysis) float64 {
	var score float64
	// 词数：300-2000 词区间得分高
	switch {
	case a.WordCount >= qualityFullWordMin && a.WordCount <= qualityFullWordMax:
		score += qualityFullScore
	case a.WordCount >= qualityPartialWordMin:
		score += qualityPartialScore
	case a.WordCount > 0:
		score += qualityFloorScore
	}
	// 常青度
	score += float64(a.EvergreenScore) / 100 * evergreenScoreWeight
	if score > maxQualityScore {
		score = maxQualityScore
	}
	return score
}

// EstimateVisibility 基于当前评分与已应用策略预估可见度指标。
//
// 使用 Princeton 论文的策略效果系数累加预估提升。
func (s *Scorer) EstimateVisibility(scoreBefore float64, applied []models.StrategyType) models.VisibilityMetrics {
	visibility := models.VisibilityMetrics{
		PositionScore: scoreBefore / positionScoreDivisor,
	}
	// 累加策略效果
	var improvement float64
	for _, st := range applied {
		improvement += config.StrategyEffectiveness[st]
	}
	// 预估引用频率（基于评分与提升）
	visibility.CitationFrequency = int(scoreBefore/citationFreqBaseDiv + improvement*citationFreqBoost)
	visibility.CitationOrder = max(1, citationOrderBase-int(scoreBefore/citationOrderDiv))
	visibility.RelativeCitationScore = scoreBefore/positionScoreDivisor + improvement
	visibility.SemanticSimilarity = min(1.0, scoreBefore/positionScoreDivisor+improvement/semanticSimilarityDiv)
	visibility.OverallScore = min(100.0, scoreBefore*(1+improvement))
	return visibility
}

// EstimateUtility 预估效用指标（确保优化不破坏 AI 回答质量）。
//
// 合作式优化（学习 GE 偏好）保持高质量；对抗式（关键词堆砌）降低质量。
func (s *Scorer) EstimateUtility(analysis *models.ContentAnalysis) models.UtilityMetrics {
	var m models.UtilityMetrics
	// 引用质量：有真实引用来源则高
	if analysis.CitabilitySignals["cite_sources"] {
		m.CitationQuality = utilityHighQuality
	} else {
		m.CitationQuality = utilityLowQuality
	}
	// 关键点覆盖：有结构化内容则高
	keypoints := 0
	for _, v := range analysis.StructureSignals {
		if v {
			keypoints++
		}
	}
	m.KeypointCoverage = min(1.0, float64(keypoints)/keypointCoverageDiv)
	// 回答质量：无负向信号则高
	if len(analysis.NegativeSignals) == 0 {
		m.ResponseQuality = utilityHighQuality
	} else {
		m.ResponseQuality = max(utilityQualityFloor, utilityHighQuality-float64(len(analysis.NegativeSignals))*utilityQualityPenalty)
	}
	m.OverallScore = (m.CitationQuality + m.KeypointCoverage + m.ResponseQuality) / utilityDimensionCount
	return m
}
