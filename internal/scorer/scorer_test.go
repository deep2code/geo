package scorer

import (
	"testing"

	"my-geo/internal/models"
)

// stubAnalyzer 固定返回预设分析结果，用于隔离评分逻辑做确定性测试。
type stubAnalyzer struct{ ca *models.ContentAnalysis }

func (s stubAnalyzer) Analyze(string) *models.ContentAnalysis { return s.ca }

func newStub(quotation bool) *stubAnalyzer {
	return &stubAnalyzer{ca: &models.ContentAnalysis{
		CitabilitySignals: map[string]bool{"quotation": quotation},
	}}
}

// TestOverrideWeightsChangesScore 验证 P2-5 的配置化权重确实生效：
// 默认 quotation=8 → 总分 18（8 可引用 + 0 结构 + 0 质量 + 10 无负向）；
// 覆盖 quotation=20 → 总分 30。
func TestOverrideWeightsChangesScore(t *testing.T) {
	defer OverrideWeights(map[string]float64{"quotation": 8.0}) // 还原默认
	OverrideWeights(map[string]float64{"quotation": 8.0})

	sc := New(newStub(true))
	before, _ := sc.Score("x")
	if before != 18.0 {
		t.Fatalf("默认分 = %v, want 18.0", before)
	}

	OverrideWeights(map[string]float64{"quotation": 20.0})
	after, _ := sc.Score("x")
	if after != 30.0 {
		t.Fatalf("覆盖后分 = %v, want 30.0", after)
	}
}

// TestOverrideWeightsIgnoresInvalid 验证非法键与负值被忽略，不破坏既有权重。
func TestOverrideWeightsIgnoresInvalid(t *testing.T) {
	defer OverrideWeights(map[string]float64{"quotation": 8.0})
	OverrideWeights(map[string]float64{"quotation": 8.0})

	OverrideWeights(map[string]float64{"bogus_key": 99.0, "quotation": -5.0})
	sc := New(newStub(true))
	got, _ := sc.Score("x")
	if got != 18.0 {
		t.Errorf("非法覆盖后分 = %v, want 18.0（应被忽略）", got)
	}
}

// TestScoreDeterministic 验证相同输入分数稳定且在 0-100 区间。
func TestScoreDeterministic(t *testing.T) {
	sc := New(newStub(true))
	s1, _ := sc.Score("anything")
	s2, _ := sc.Score("anything")
	if s1 != s2 {
		t.Errorf("分数不稳定: %v vs %v", s1, s2)
	}
	if s1 < 0 || s1 > 100 {
		t.Errorf("分数越界: %v", s1)
	}
}
