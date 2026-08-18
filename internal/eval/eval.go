// Package eval GEO 评测体系：中文评测集 + 改前/改后引用率与评分对比的可复现报告。
//
// 对标 AutoGEO (ICLR 2026) 的 GEO-Bench + evaluate 命令——这是证明产品价值的核心资产
// （战略级改进方向 #1，最值得投入）。
//
// 设计要点：
//   - 评测集为 JSON，包含若干 Case（query × 待优化页面内容 × 目标引擎 × 期望引用）。
//   - Evaluate 对每个 Case 计算：基线评分、LLM 实际优化后评分、以及"若应用推荐策略"的
//     预期可见度提升（EstimateVisibility，与 LLM 无关，离线即有意义的投影）。
//   - 产出可复现报告（Markdown / JSON）：逐 Case 明细 + 聚合均值。
//
// 关于"引用率"：生成式引擎的真实引用率需联网查询引擎（依赖 API Key 与网络）。
// 此处以 RelativeCitationScore（基于评分与策略系数的投影）作为可复现的离线代理指标，
// 并在报告中明确标注；接入真实引擎查询后，可替换为实测引用率。
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"my-geo/internal/models"
	"my-geo/pkg/geo"
)

// Case 单个评测用例。
type Case struct {
	Query            string               `json:"query"`                       // 评测查询词
	TargetURL        string               `json:"target_url,omitempty"`        // 待优化页面 URL（报告展示用）
	TargetContent    string               `json:"target_content"`              // 待优化页面内容（离线评测直接分析）
	Engines          []models.EngineType  `json:"engines,omitempty"`           // 目标生成式引擎
	DomainType       models.DomainType    `json:"domain_type,omitempty"`       // 领域类型
	ExpectedCitations []string            `json:"expected_citations,omitempty"` // 期望被引用的来源/实体（人工标注）
}

// Benchmark 评测集。
type Benchmark struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Cases       []Case `json:"cases"`
}

// CaseResult 单个用例的评测结果。
type CaseResult struct {
	Index             int     `json:"index"`
	Query             string  `json:"query"`
	TargetURL         string  `json:"target_url,omitempty"`
	ScoreBefore       float64 `json:"score_before"`
	ScoreAfter        float64 `json:"score_after"` // LLM 实际优化后评分（无 LLM 时≈基线）
	RelCitBefore      float64 `json:"rel_cit_before"`  // 相对可见度得分（scorer 加性指标，可>1）
	RelCitProjected   float64 `json:"rel_cit_projected"` // 推荐策略投影（离线有意义）
	RelCitActual      float64 `json:"rel_cit_actual"`    // LLM 实际（=resp.GeoScore）
	CitRateBefore     float64 `json:"cit_rate_before"`   // 有界引用率代理 0..1（用于提升%）
	CitRateProjected  float64 `json:"cit_rate_projected"`
	CitRateActual     float64 `json:"cit_rate_actual"`
	LiftPct           float64 `json:"lift_pct"` // 引用率代理提升%（基于投影）
	AppliedStrategies int     `json:"applied_strategies"`
	ExpectedCitations int     `json:"expected_citations"`
}

// Aggregate 聚合指标。
type Aggregate struct {
	CaseCount        int     `json:"case_count"`
	AvgScoreBefore   float64 `json:"avg_score_before"`
	AvgScoreAfter    float64 `json:"avg_score_after"`
	AvgRelCitBefore  float64 `json:"avg_rel_cit_before"`
	AvgRelCitProjected float64 `json:"avg_rel_cit_projected"`
	AvgRelCitActual  float64 `json:"avg_rel_cit_actual"`
	AvgCitRateBefore  float64 `json:"avg_cit_rate_before"`  // 有界引用率代理均值
	AvgCitRateProjected float64 `json:"avg_cit_rate_projected"`
	AvgCitRateActual  float64 `json:"avg_cit_rate_actual"`
	AvgLiftPct       float64 `json:"avg_lift_pct"`
}

// Report 评测报告。
type Report struct {
	BenchmarkName string         `json:"benchmark_name"`
	GeneratedAt   string         `json:"generated_at"`
	Mode          string         `json:"mode"` // "offline-proxy" | "live"
	Cases         []CaseResult   `json:"cases"`
	Aggregate     Aggregate      `json:"aggregate"`
}

// Evaluate 运行评测集，返回可复现报告。
//
// engine 已按需要应用规则集（见 geo.Engine.ApplyRuleSet）。无 LLM 时进入 offline-proxy 模式：
// ScoreAfter≈基线，但 RelCitProjected 仍给出"应用推荐策略"的预期提升，体现评测框架价值。
func Evaluate(ctx context.Context, engine *geo.Engine, bench *Benchmark) (*Report, error) {
	if len(bench.Cases) == 0 {
		return nil, fmt.Errorf("评测集为空")
	}
	report := &Report{
		BenchmarkName: bench.Name,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Mode:          "offline-proxy",
	}
	var agg Aggregate
	agg.CaseCount = len(bench.Cases)

	for i, c := range bench.Cases {
		scoreBefore, _ := engine.Score(c.TargetContent)
		resp, err := engine.Optimize(ctx, &models.OptimizationRequest{
			Content:       c.TargetContent,
			TargetEngines: c.Engines,
			DomainType:    c.DomainType,
		})
		if err != nil {
			return nil, fmt.Errorf("用例 %d 优化失败: %w", i+1, err)
		}
		// 推荐策略（与 optimizer 内部推荐一致），用于离线投影预期提升。
		recommended := engine.RecommendStrategies(c.DomainType, c.Engines)
		relBefore := engine.EstimateVisibility(scoreBefore, nil).RelativeCitationScore
		relProjected := engine.EstimateVisibility(scoreBefore, recommended).RelativeCitationScore
		relActual := resp.GeoScore.RelativeCitationScore
		rateBefore := citationRate(relBefore)
		rateProjected := citationRate(relProjected)
		rateActual := citationRate(relActual)

		applied := 0
		for _, s := range resp.AppliedStrategies {
			if s.Applied {
				applied++
			}
		}
		r := CaseResult{
			Index:             i + 1,
			Query:             c.Query,
			TargetURL:         c.TargetURL,
			ScoreBefore:       scoreBefore,
			ScoreAfter:        resp.ScoreAfter,
			RelCitBefore:      relBefore,
			RelCitProjected:   relProjected,
			RelCitActual:      relActual,
			CitRateBefore:     rateBefore,
			CitRateProjected:  rateProjected,
			CitRateActual:     rateActual,
			AppliedStrategies: applied,
			ExpectedCitations: len(c.ExpectedCitations),
		}
		if rateBefore > 0 {
			r.LiftPct = (rateProjected - rateBefore) / rateBefore * 100
		}
		report.Cases = append(report.Cases, r)

		agg.AvgScoreBefore += scoreBefore
		agg.AvgScoreAfter += resp.ScoreAfter
		agg.AvgRelCitBefore += relBefore
		agg.AvgRelCitProjected += relProjected
		agg.AvgRelCitActual += relActual
		agg.AvgCitRateBefore += rateBefore
		agg.AvgCitRateProjected += rateProjected
		agg.AvgCitRateActual += rateActual
		agg.AvgLiftPct += r.LiftPct
	}
	n := float64(agg.CaseCount)
	agg.AvgScoreBefore /= n
	agg.AvgScoreAfter /= n
	agg.AvgRelCitBefore /= n
	agg.AvgRelCitProjected /= n
	agg.AvgRelCitActual /= n
	agg.AvgCitRateBefore /= n
	agg.AvgCitRateProjected /= n
	agg.AvgCitRateActual /= n
	agg.AvgLiftPct /= n
	report.Aggregate = agg
	return report, nil
}

// RenderJSON 渲染 JSON 报告。
func RenderJSON(r *Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// RenderMarkdown 渲染 Markdown 报告。
func RenderMarkdown(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# GEO 评测报告：%s\n\n", r.BenchmarkName)
	fmt.Fprintf(&b, "- 生成时间：%s\n", r.GeneratedAt)
	fmt.Fprintf(&b, "- 模式：%s（相对引用得分为离线代理指标；接入真实引擎查询后可替换为实测引用率）\n", r.Mode)
	a := r.Aggregate
	fmt.Fprintf(&b, "- 用例数：%d\n", a.CaseCount)
	fmt.Fprintf(&b, "- 平均评分：基线 %.1f → 实际 %.1f\n", a.AvgScoreBefore, a.AvgScoreAfter)
	fmt.Fprintf(&b, "- 平均相对可见度得分（加性，仅供透明参考）：基线 %.3f → 投影 %.3f → 实际 %.3f\n",
		a.AvgRelCitBefore, a.AvgRelCitProjected, a.AvgRelCitActual)
	fmt.Fprintf(&b, "- 平均引用率代理（有界 0–1）：基线 %.1f%% → 投影 %.1f%% → 实际 %.1f%%\n",
		a.AvgCitRateBefore*100, a.AvgCitRateProjected*100, a.AvgCitRateActual*100)
	fmt.Fprintf(&b, "- **平均预期引用率提升：%.1f%%**（基于有界代理投影 vs 基线）\n\n", a.AvgLiftPct)

	b.WriteString("| # | 查询 | 基线分 | 实际分 | 引用率代理(基线→投影→实际) | 预期提升 | 应用策略 |\n")
	b.WriteString("|---|------|-------:|-------:|------------------------------:|--------:|--------:|\n")
	for _, c := range r.Cases {
		fmt.Fprintf(&b, "| %d | %s | %.1f | %.1f | %.1f%% → %.1f%% → %.1f%% | %+.1f%% | %d |\n",
			c.Index, truncate(c.Query, 24), c.ScoreBefore, c.ScoreAfter,
			c.CitRateBefore*100, c.CitRateProjected*100, c.CitRateActual*100, c.LiftPct, c.AppliedStrategies)
	}
	fmt.Fprintf(&b, "\n> 说明：引用率代理 = 1 − e^(−相对可见度得分)，将加性得分映射到 0–1；"+
		"提升%% 为投影 vs 基线。"+
		"相对可见度得分为加性指标（可>1），仅在 JSON 报告中保留以作透明参考。\n")
	return b.String()
}

// truncate 截断长文本用于表格展示。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// citationRate 将加性的相对可见度得分（可>1）映射为 0–1 的引用率代理。
//
// 相对可见度得分 = 评分/100 + Σ策略效果系数，是无上界的"引用增益"；
// 真实引用率天然有界，故用 1−e^(−rel) 将其饱和到 [0,1)，且 rel 较小时近似线性，
// 保证提升% 有界、可解释，不会出现原始比率那种 >1000% 的失真。
func citationRate(rel float64) float64 {
	if rel < 0 {
		return 0
	}
	return 1 - math.Exp(-rel)
}
