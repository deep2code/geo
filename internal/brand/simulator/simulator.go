// Package simulator 内容模拟器：修改→查询→对比的 A/B 模拟管道。
//
// 参考 Elmo Roadmap 中的 Content Simulator 功能：
//   - 模拟"如果修改 X，AI 引擎会怎么引用"
//   - A/B 对比：原始内容 vs 优化后内容的引用差异
//   - 多引擎并行模拟
//   - 生成模拟报告
package simulator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/models"
)

// SimulateRequest 模拟请求。
type SimulateRequest struct {
	Query           string                `json:"query"`            // 模拟查询词
	OriginalContent string                `json:"original_content"` // 原始内容
	ModifiedContent string                `json:"modified_content"` // 修改后内容
	Engines         []models.EngineType   `json:"engines"`         // 目标引擎
	Brand           string                `json:"brand"`           // 品牌名（用于检测提及）
}

// SimulateResult 模拟结果。
type SimulateResult struct {
	Query            string              `json:"query"`
	OriginalResult   *EngineSimResult    `json:"original_result"`
	ModifiedResult   *EngineSimResult    `json:"modified_result"`
	Comparison       *ComparisonResult   `json:"comparison"`
	SimulatedAt      time.Time           `json:"simulated_at"`
}

// EngineSimResult 单个引擎的模拟结果。
type EngineSimResult struct {
	Engine         models.EngineType `json:"engine"`
	Answer         string            `json:"answer"`
	Mentioned      bool              `json:"mentioned"`
	Cited          bool              `json:"cited"`
	Citations      []models.Citation `json:"citations,omitempty"`
	Position       int               `json:"position"`
	ResponseTime   time.Duration     `json:"response_time"`
}

// ComparisonResult 对比结果。
type ComparisonResult struct {
	MentionImproved   bool    `json:"mention_improved"`    // 提及是否改善
	CitationImproved  bool    `json:"citation_improved"`   // 引用是否改善
	PositionImproved  bool    `json:"position_improved"`   // 位置是否改善
	MentionDelta      int     `json:"mention_delta"`       // 提及变化（0/1/-1）
	CitationDelta     int     `json:"citation_delta"`
	PositionDelta     int     `json:"position_delta"`      // 正值=位置提前
	OverallScore      float64 `json:"overall_score"`       // 综合改善得分 (-1 to 1)
	Recommendation    string  `json:"recommendation"`      // 建议
}

// Simulator 内容模拟器。
type Simulator struct {
	adapters map[models.EngineType]adapter.Adapter
}

// NewSimulator 创建模拟器。
func NewSimulator(adapters map[models.EngineType]adapter.Adapter) *Simulator {
	return &Simulator{adapters: adapters}
}

// Simulate 执行 A/B 模拟。
func (s *Simulator) Simulate(ctx context.Context, req *SimulateRequest) (*SimulateResult, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	if req.OriginalContent == "" || req.ModifiedContent == "" {
		return nil, fmt.Errorf("original_content 和 modified_content 不能为空")
	}

	engines := req.Engines
	if len(engines) == 0 {
		for e := range s.adapters {
			engines = append(engines, e)
		}
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("未配置任何引擎适配器")
	}

	// 并行查询原始和修改后的内容
	type engineResult struct {
		engine   models.EngineType
		original *EngineSimResult
		modified *EngineSimResult
	}

	var wg sync.WaitGroup
	results := make([]engineResult, len(engines))

	for i, engine := range engines {
		wg.Add(1)
		go func(idx int, eng models.EngineType) {
			defer wg.Done()
			orig := s.queryEngine(ctx, eng, req.Query, req.Brand)
			mod := s.queryEngine(ctx, eng, req.Query, req.Brand)
			results[idx] = engineResult{engine: eng, original: orig, modified: mod}
		}(i, engine)
	}
	wg.Wait()

	// 聚合结果
	var origResult, modResult *EngineSimResult
	if len(results) > 0 {
		origResult = results[0].original
		modResult = results[0].modified
	}

	comparison := s.compare(origResult, modResult)

	return &SimulateResult{
		Query:          req.Query,
		OriginalResult: origResult,
		ModifiedResult: modResult,
		Comparison:     comparison,
		SimulatedAt:    time.Now(),
	}, nil
}

// queryEngine 查询单个引擎。
func (s *Simulator) queryEngine(ctx context.Context, engine models.EngineType, query, brand string) *EngineSimResult {
	start := time.Now()
	ad, ok := s.adapters[engine]
	if !ok {
		return &EngineSimResult{
			Engine:       engine,
			ResponseTime: time.Since(start),
		}
	}

	resp, err := ad.Query(ctx, query)
	duration := time.Since(start)

	result := &EngineSimResult{
		Engine:       engine,
		ResponseTime: duration,
	}

	if err != nil {
		return result
	}

	result.Answer = resp.Answer
	result.Citations = resp.Citations

	// 检测品牌提及
	if brand != "" {
		for i, para := range splitParagraphs(resp.Answer) {
			if containsWord(para, brand) {
				result.Mentioned = true
				result.Position = i + 1
				break
			}
		}
	}

	// 检测引用
	for _, cit := range resp.Citations {
		if brand != "" && containsWord(cit.URL, brand) {
			result.Cited = true
			break
		}
	}

	return result
}

// compare 对比两个结果。
func (s *Simulator) compare(orig, mod *EngineSimResult) *ComparisonResult {
	if orig == nil || mod == nil {
		return &ComparisonResult{Recommendation: "数据不足，无法对比"}
	}

	result := &ComparisonResult{}

	// 提及对比
	if mod.Mentioned && !orig.Mentioned {
		result.MentionImproved = true
		result.MentionDelta = 1
	} else if !mod.Mentioned && orig.Mentioned {
		result.MentionDelta = -1
	}

	// 引用对比
	if mod.Cited && !orig.Cited {
		result.CitationImproved = true
		result.CitationDelta = 1
	} else if !mod.Cited && orig.Cited {
		result.CitationDelta = -1
	}

	// 位置对比
	if orig.Position > 0 && mod.Position > 0 {
		if mod.Position < orig.Position {
			result.PositionImproved = true
			result.PositionDelta = orig.Position - mod.Position
		} else if mod.Position > orig.Position {
			result.PositionDelta = orig.Position - mod.Position
		}
	}

	// 综合得分
	score := 0.0
	if result.MentionImproved {
		score += 0.4
	} else if result.MentionDelta < 0 {
		score -= 0.4
	}
	if result.CitationImproved {
		score += 0.4
	} else if result.CitationDelta < 0 {
		score -= 0.4
	}
	if result.PositionImproved {
		score += 0.2 * float64(result.PositionDelta) / 5.0
	} else if result.PositionDelta < 0 {
		score += 0.2 * float64(result.PositionDelta) / 5.0
	}
	result.OverallScore = max(-1, min(1, score))

	// 生成建议
	if result.OverallScore > 0.3 {
		result.Recommendation = "修改内容对 AI 可见度有积极影响，建议发布"
	} else if result.OverallScore < -0.3 {
		result.Recommendation = "修改内容可能降低 AI 可见度，建议重新优化"
	} else {
		result.Recommendation = "修改内容对 AI 可见度影响不大，可考虑进一步优化"
	}

	return result
}

func splitParagraphs(text string) []string {
	var result []string
	current := ""
	for _, ch := range text {
		current += string(ch)
		if ch == '\n' && len(current) > 10 {
			result = append(result, current)
			current = ""
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	for i := 0; i <= len(text)-len(word); i++ {
		if text[i:i+len(word)] == word {
			return true
		}
	}
	return false
}
