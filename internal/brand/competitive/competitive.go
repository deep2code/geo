// Package competitive 竞品引用对比分析：跨 prompt 聚合的竞品声量份额趋势视图。
//
// 扩展 brand 的 CompetitorSOV 能力：
//   - 跨 prompt 聚合竞品声量份额
//   - 多维度竞品对比矩阵
//   - 声量份额趋势（时间序列）
//   - 竞品涌现/消失检测
package competitive

import (
	"sort"
	"time"

	"my-geo/internal/models"
)

// CompetitorOverview 竞品总览。
type CompetitorOverview struct {
	BrandName       string              `json:"brand_name"`
	Competitors     []CompetitorEntry   `json:"competitors"`
	TotalPrompts    int                 `json:"total_prompts"`
	TotalEngines    int                 `json:"total_engines"`
	AnalysisTime    time.Time           `json:"analysis_time"`
}

// CompetitorEntry 单个竞品条目。
type CompetitorEntry struct {
	Name           string             `json:"name"`
	MentionRate    float64            `json:"mention_rate"`    // 被提及的 prompt 占比
	CitationRate   float64            `json:"citation_rate"`   // 被引用的 prompt 占比
	AvgPosition    float64            `json:"avg_position"`    // 平均提及位置
	SentimentDist  map[string]int     `json:"sentiment_dist"`  // 情感分布
	EnginePresence map[string]bool    `json:"engine_presence"` // 在哪些引擎中出现
	MentionCount   int                `json:"mention_count"`
	CitationCount  int                `json:"citation_count"`
}

// ComparisonMatrix 竞品对比矩阵。
type ComparisonMatrix struct {
	Metrics []string            `json:"metrics"` // 对比维度
	Brands  []BrandComparison   `json:"brands"`
}

// BrandComparison 单个品牌的对比数据。
type BrandComparison struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"` // 与 Metrics 一一对应
}

// SOVTrendPoint 声量份额趋势点。
type SOVTrendPoint struct {
	Date      string             `json:"date"`
	BrandSOV  float64            `json:"brand_sov"`
	Competitors []CompetitorSOV  `json:"competitors"`
}

// CompetitorSOV 单个竞品的声量份额。
type CompetitorSOV struct {
	Name string  `json:"name"`
	SOV  float64 `json:"sov"`
}

// DetectEmergence 检测竞品涌现：本次出现但上次未出现的竞品。
func DetectEmergence(current, previous []string) []string {
	prevSet := make(map[string]bool)
	for _, name := range previous {
		prevSet[name] = true
	}
	var emerged []string
	for _, name := range current {
		if !prevSet[name] {
			emerged = append(emerged, name)
		}
	}
	return emerged
}

// DetectDisappearance 检测竞品消失：上次出现但本次未出现的竞品。
func DetectDisappearance(current, previous []string) []string {
	currSet := make(map[string]bool)
	for _, name := range current {
		currSet[name] = true
	}
	var gone []string
	for _, name := range previous {
		if !currSet[name] {
			gone = append(gone, name)
		}
	}
	return gone
}

// AggregateSOV 聚合多 prompt 的竞品声量份额。
func AggregateSOV(results []PromptCompetitorResult) []CompetitorEntry {
	type agg struct {
		mentionCount  int
		citationCount int
		totalPosition int
		positionCount int
		sentimentDist map[string]int
		enginePresent map[string]bool
	}
	aggs := make(map[string]*agg)
	totalPrompts := make(map[string]bool)

	for _, r := range results {
		totalPrompts[r.Prompt] = true
		for _, cm := range r.CompetitorMentions {
			a, ok := aggs[cm.Name]
			if !ok {
				a = &agg{
					sentimentDist: make(map[string]int),
					enginePresent: make(map[string]bool),
				}
				aggs[cm.Name] = a
			}
			if cm.Mentioned {
				a.mentionCount++
			}
			if cm.Cited {
				a.citationCount++
			}
			if cm.Position > 0 {
				a.totalPosition += cm.Position
				a.positionCount++
			}
			if cm.Sentiment != "" {
				a.sentimentDist[cm.Sentiment]++
			}
			a.enginePresent[string(cm.Engine)] = true
		}
	}

	promptsCount := float64(len(totalPrompts))
	if promptsCount == 0 {
		promptsCount = 1
	}

	var entries []CompetitorEntry
	for name, a := range aggs {
		avgPos := 0.0
		if a.positionCount > 0 {
			avgPos = float64(a.totalPosition) / float64(a.positionCount)
		}
		entries = append(entries, CompetitorEntry{
			Name:           name,
			MentionRate:    float64(a.mentionCount) / promptsCount,
			CitationRate:   float64(a.citationCount) / promptsCount,
			AvgPosition:    avgPos,
			SentimentDist:  a.sentimentDist,
			EnginePresence: a.enginePresent,
			MentionCount:   a.mentionCount,
			CitationCount:  a.citationCount,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CitationRate > entries[j].CitationRate
	})

	return entries
}

// PromptCompetitorResult 单个 prompt 的竞品检测结果。
type PromptCompetitorResult struct {
	Prompt            string
	CompetitorMentions []CompetitorMentionResult
}

// CompetitorMentionResult 单个竞品的检测结果。
type CompetitorMentionResult struct {
	Name      string
	Mentioned bool
	Cited     bool
	Position  int
	Sentiment string
	Engine    models.EngineType
}

// BuildComparisonMatrix 构建竞品对比矩阵。
func BuildComparisonMatrix(brand CompetitorEntry, competitors []CompetitorEntry) *ComparisonMatrix {
	metrics := []string{
		"提及率",
		"引用率",
		"平均位置",
		"提及次数",
		"引用次数",
	}

	var brands []BrandComparison

	// 品牌自身
	brandValues := []float64{
		brand.MentionRate * 100,
		brand.CitationRate * 100,
		brand.AvgPosition,
		float64(brand.MentionCount),
		float64(brand.CitationCount),
	}
	brands = append(brands, BrandComparison{Name: brand.Name, Values: brandValues})

	// 竞品
	for _, comp := range competitors {
		values := []float64{
			comp.MentionRate * 100,
			comp.CitationRate * 100,
			comp.AvgPosition,
			float64(comp.MentionCount),
			float64(comp.CitationCount),
		}
		brands = append(brands, BrandComparison{Name: comp.Name, Values: values})
	}

	return &ComparisonMatrix{
		Metrics: metrics,
		Brands:  brands,
	}
}
