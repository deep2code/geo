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

	// 可引用性信号（满分 40）
	citScore, citDetail := scoreCitability(a.CitabilitySignals)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "citability", Score: citScore, MaxScore: 40, Detail: citDetail,
	})

	// 结构信号（满分 30）
	struScore, struDetail := scoreStructure(a.StructureSignals)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "structure", Score: struScore, MaxScore: 30, Detail: struDetail,
	})

	// 内容质量（满分 20）—— 基于词数与常青度
	qualityScore := scoreQuality(a)
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "quality", Score: qualityScore, MaxScore: 20,
	})

	// 负向信号扣分（满分 10）
	negScore := 10.0 - float64(len(a.NegativeSignals))*2.5
	if negScore < 0 {
		negScore = 0
	}
	breakdowns = append(breakdowns, models.ScoreBreakdown{
		Category: "negative_penalty", Score: negScore, MaxScore: 10,
	})

	total := citScore + struScore + qualityScore + negScore
	if total > 100 {
		total = 100
	}
	return total, breakdowns
}

// scoreCitability 可引用性评分。
func scoreCitability(signals map[string]bool) (float64, string) {
	// 每个信号权重不同（参考效果系数）
	weights := map[string]float64{
		"quotation":       8,
		"statistics":      7,
		"cite_sources":    7,
		"fluency":         6,
		"authoritative":   5,
		"technical_terms": 4,
		"unique_words":    3,
	}
	var score float64
	for sig, w := range weights {
		if signals[sig] {
			score += w
		}
	}
	if score > 40 {
		score = 40
	}
	return score, ""
}

// scoreStructure 结构评分。
func scoreStructure(signals map[string]bool) (float64, string) {
	weights := map[string]float64{
		"heading_hierarchy":    8,
		"front_loading":        7,
		"lists":                5,
		"definition_openings":  5,
		"tables":               3,
		"faq":                  2,
	}
	var score float64
	for sig, w := range weights {
		if signals[sig] {
			score += w
		}
	}
	if score > 30 {
		score = 30
	}
	return score, ""
}

// scoreQuality 内容质量评分。
func scoreQuality(a *models.ContentAnalysis) float64 {
	var score float64
	// 词数：100-2000 词区间得分高
	switch {
	case a.WordCount >= 300 && a.WordCount <= 2000:
		score += 8
	case a.WordCount >= 100:
		score += 5
	case a.WordCount > 0:
		score += 2
	}
	// 常青度
	score += float64(a.EvergreenScore) / 100 * 12
	if score > 20 {
		score = 20
	}
	return score
}

// EstimateVisibility 基于当前评分与已应用策略预估可见度指标。
//
// 使用 Princeton 论文的策略效果系数累加预估提升。
func (s *Scorer) EstimateVisibility(scoreBefore float64, applied []models.StrategyType) models.VisibilityMetrics {
	visibility := models.VisibilityMetrics{
		PositionScore: scoreBefore / 100,
	}
	// 累加策略效果
	var improvement float64
	for _, st := range applied {
		improvement += config.StrategyEffectiveness[st]
	}
	// 预估引用频率（基于评分与提升）
	visibility.CitationFrequency = int(scoreBefore/20 + improvement*5)
	visibility.CitationOrder = max(1, 10-int(scoreBefore/12))
	visibility.RelativeCitationScore = scoreBefore/100 + improvement
	visibility.SemanticSimilarity = min(1.0, scoreBefore/100+improvement/2)
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
		m.CitationQuality = 0.9
	} else {
		m.CitationQuality = 0.5
	}
	// 关键点覆盖：有结构化内容则高
	keypoints := 0
	for _, v := range analysis.StructureSignals {
		if v {
			keypoints++
		}
	}
	m.KeypointCoverage = min(1.0, float64(keypoints)/5)
	// 回答质量：无负向信号则高
	if len(analysis.NegativeSignals) == 0 {
		m.ResponseQuality = 0.9
	} else {
		m.ResponseQuality = max(0.3, 0.9-float64(len(analysis.NegativeSignals))*0.15)
	}
	m.OverallScore = (m.CitationQuality + m.KeypointCoverage + m.ResponseQuality) / 3
	return m
}


