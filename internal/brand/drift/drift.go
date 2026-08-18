// Package drift 对比两次品牌审计结果，检测各维度漂移与回归。
//
// 功能定位：CI/CD 回归检测 + 运营趋势监控。输入两条 history.Record
// （或可选的完整 VisibilityReport JSON），输出：
//   - 各标量维度漂移（BVS 总分、提及率、引用率、声量、位置、情感等）
//   - 引擎级漂移（各引擎的提及率/引用率/SOV/位置，需完整报告）
//   - 回归项（regression，显著负向漂移，按严重度分级）
//   - 改善项（improvement，显著正向漂移）
//   - 整体结论（improved / regressed / stable）
//
// 借鉴 readiness 的 CI 闸门设计：Compare 产出的 Report 可通过 CIGate
// 方法作为 CI/CD 回归闸门——存在 critical 回归或 BVS 下降超阈值时阻断。
package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/history"
	"my-geo/internal/models"
)

// DimDelta 单个维度的漂移。
type DimDelta struct {
	Name           string  `json:"name"`
	Label          string  `json:"label"`
	Previous       float64 `json:"previous"`
	Current        float64 `json:"current"`
	Delta          float64 `json:"delta"`     // current - previous
	Direction      string  `json:"direction"` // up/down/stable
	Severity       string  `json:"severity"`  // critical/warning/info/none（仅回归方向升级）
	HigherIsBetter bool    `json:"higher_is_better"`
}

// Regression 显著负向漂移项（需运营关注）。
type Regression struct {
	Dim      string  `json:"dim"`
	Label    string  `json:"label"`
	Previous float64 `json:"previous"`
	Current  float64 `json:"current"`
	Delta    float64 `json:"delta"`
	Severity string  `json:"severity"`
}

// EngineDelta 单引擎漂移。
type EngineDelta struct {
	Engine models.EngineType `json:"engine"`
	Dims   []DimDelta        `json:"dims"`
}

// Report 完整的漂移对比报告。
type Report struct {
	BrandName    string        `json:"brand_name"`
	PreviousAt   int64         `json:"previous_at"`
	CurrentAt    int64         `json:"current_at"`
	PreviousID   int64         `json:"previous_id"`
	CurrentID    int64         `json:"current_id"`
	ScoreDelta   float64       `json:"score_delta"`
	Verdict      string        `json:"verdict"` // improved/regressed/stable
	Dimensions   []DimDelta    `json:"dimensions"`
	Engines      []EngineDelta `json:"engines,omitempty"`
	Regressions  []Regression  `json:"regressions,omitempty"`
	Improvements []DimDelta    `json:"improvements,omitempty"`
	CheckedAt    time.Time     `json:"checked_at"`
}

// dimSpec 维度规格：方向、告警阈值、稳定容忍度。
type dimSpec struct {
	name           string
	label          string
	higherIsBetter bool
	warn           float64 // 回归方向达到此幅度 → warning
	crit           float64 // 回归方向达到此幅度 → critical
	stableTol      float64 // 浮动小于此值视为 stable
}

// 维度阈值表（数值借鉴 scheduler 的告警阈值并细化）。
var dimSpecs = []dimSpec{
	{"score", "BVS 总分", true, 5, 10, 0.5},
	{"mention_rate", "提及率", true, 8, 15, 1},
	{"citation_rate", "引用率", true, 8, 15, 1},
	{"share_of_voice", "声量份额", true, 5, 10, 1},
	{"citation_position", "引用位置", false, 1.5, 3, 0.5}, // 位置数值增大=后退
	{"sentiment", "情感正面率", true, 10, 20, 1},
	{"entity_recognition", "实体识别", true, 8, 15, 1},
	{"entity_completeness", "实体完备度", true, 5, 10, 1},
}

// Compare 对比两条历史记录，生成漂移报告。
//
// 若记录的 ReportJSON 可解析为完整 VisibilityReport，则追加引擎级漂移对比。
// prev/cur 顺序无强制要求，内部按时间语义标注 Previous/Current。
func Compare(prev, cur history.Record) *Report {
	r := &Report{
		BrandName:  cur.BrandName,
		PreviousAt: prev.Generated,
		CurrentAt:  cur.Generated,
		PreviousID: prev.ID,
		CurrentID:  cur.ID,
		CheckedAt:  time.Now(),
	}

	// 标量维度对比
	vals := map[string][2]float64{
		"score":               {prev.Score, cur.Score},
		"mention_rate":        {prev.MentionRate, cur.MentionRate},
		"citation_rate":       {prev.CitationRate, cur.CitationRate},
		"share_of_voice":      {prev.ShareOfVoice, cur.ShareOfVoice},
		"citation_position":   {prev.CitationPosition, cur.CitationPosition},
		"sentiment":           {prev.Sentiment, cur.Sentiment},
		"entity_recognition":  {prev.EntityRecognition, cur.EntityRecognition},
		"entity_completeness": {prev.EntityCompleteness, cur.EntityCompleteness},
	}
	specByName := map[string]dimSpec{}
	for _, s := range dimSpecs {
		specByName[s.name] = s
	}
	for _, spec := range dimSpecs {
		pair := vals[spec.name]
		dd := computeDelta(spec, pair[0], pair[1])
		r.Dimensions = append(r.Dimensions, dd)
		if dd.Severity == "critical" || dd.Severity == "warning" {
			r.Regressions = append(r.Regressions, Regression{
				Dim: dd.Name, Label: dd.Label,
				Previous: dd.Previous, Current: dd.Current,
				Delta: dd.Delta, Severity: dd.Severity,
			})
		} else if isImprovement(spec, dd) {
			r.Improvements = append(r.Improvements, dd)
		}
	}
	r.ScoreDelta = cur.Score - prev.Score

	// 完整报告对比（引擎级）
	if prev.ReportJSON != "" && cur.ReportJSON != "" {
		r.Engines = compareEngines(prev.ReportJSON, cur.ReportJSON)
	}

	r.Verdict = verdictOf(r)
	return r
}

// CompareLatest 从历史库取指定品牌最近两条记录对比。
// 历史库为 nil 或不足两条记录时返回 (nil, nil)。
func CompareLatest(ctx context.Context, db history.DB, brandName string) (*Report, error) {
	if db == nil {
		return nil, nil
	}
	records, err := db.List(ctx, brandName, 2, 0)
	if err != nil {
		return nil, fmt.Errorf("drift: 读取历史记录失败: %w", err)
	}
	if len(records) < 2 {
		return nil, nil
	}
	// records[0] 最新，records[1] 上一次
	return Compare(records[1], records[0]), nil
}

// CIGate 以阈值判定是否通过 CI 闸门。
//
// 判定规则（任一不满足即不通过）：
//   - 不存在 critical 级回归项
//   - BVS 下降幅度 < threshold
//
// threshold <= 0 时仅以 critical 回归为准。
func (r *Report) CIGate(threshold float64) bool {
	for _, reg := range r.Regressions {
		if reg.Severity == "critical" {
			return false
		}
	}
	if threshold > 0 && r.ScoreDelta < -threshold {
		return false
	}
	return true
}

// Summary 返回人类可读的结论摘要。
func (r *Report) Summary() string {
	verdictLabel := map[string]string{
		"improved":  "改善",
		"regressed": "回归",
		"stable":    "持平",
	}[r.Verdict]
	if len(r.Regressions) == 0 {
		return fmt.Sprintf("品牌 %s 漂移结论：%s（BVS %+.1f），无显著回归", r.BrandName, verdictLabel, r.ScoreDelta)
	}
	parts := make([]string, 0, len(r.Regressions))
	for _, reg := range r.Regressions {
		parts = append(parts, fmt.Sprintf("%s %.1f→%.1f(%+.1f)[%s]",
			reg.Label, reg.Previous, reg.Current, reg.Delta, reg.Severity))
	}
	return fmt.Sprintf("品牌 %s 漂移结论：%s（BVS %+.1f），回归项：%s",
		r.BrandName, verdictLabel, r.ScoreDelta, join(parts, "；"))
}

// computeDelta 计算单维度漂移并判定方向与严重度。
func computeDelta(spec dimSpec, prev, cur float64) DimDelta {
	delta := cur - prev
	dir := "stable"
	if delta > spec.stableTol {
		dir = "up"
	} else if delta < -spec.stableTol {
		dir = "down"
	}
	// 严重度：仅对回归方向判定
	// higherIsBetter 且下降=回归；lowerIsBetter(位置) 且上升=回归
	severity := "none"
	regressed := (spec.higherIsBetter && delta < 0) || (!spec.higherIsBetter && delta > 0)
	if regressed {
		// 回归幅度（正数）：higherIsBetter 下降取 -delta，lowerIsBetter 上升取 delta
		mag := delta
		if spec.higherIsBetter {
			mag = -delta
		}
		switch {
		case mag >= spec.crit:
			severity = "critical"
		case mag >= spec.warn:
			severity = "warning"
		default:
			severity = "info"
		}
	}
	return DimDelta{
		Name: spec.name, Label: spec.label,
		Previous: prev, Current: cur, Delta: delta,
		Direction: dir, Severity: severity, HigherIsBetter: spec.higherIsBetter,
	}
}

// isImprovement 判定是否为显著改善（正向且非回归）。
func isImprovement(spec dimSpec, dd DimDelta) bool {
	if dd.Severity != "none" {
		return false
	}
	return (spec.higherIsBetter && dd.Direction == "up") ||
		(!spec.higherIsBetter && dd.Direction == "down")
}

// verdictOf 综合判定整体结论。
func verdictOf(r *Report) string {
	for _, reg := range r.Regressions {
		if reg.Severity == "critical" || reg.Severity == "warning" {
			return "regressed"
		}
	}
	if r.ScoreDelta > 0 {
		return "improved"
	}
	return "stable"
}

// compareEngines 解析两份完整报告，按引擎对比核心指标。
func compareEngines(prevJSON, curJSON string) []EngineDelta {
	var prev, cur brand.VisibilityReport
	if err := json.Unmarshal([]byte(prevJSON), &prev); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(curJSON), &cur); err != nil {
		return nil
	}
	prevByEng := engineMap(prev.EngineStats)
	curByEng := engineMap(cur.EngineStats)
	out := make([]EngineDelta, 0, len(curByEng))
	for eng, cs := range curByEng {
		ps, ok := prevByEng[eng]
		if !ok {
			continue
		}
		dims := []DimDelta{
			computeDelta(dimSpec{name: "mention_rate", label: "提及率", higherIsBetter: true, warn: 8, crit: 15, stableTol: 1}, ps.MentionRate, cs.MentionRate),
			computeDelta(dimSpec{name: "citation_rate", label: "引用率", higherIsBetter: true, warn: 8, crit: 15, stableTol: 1}, ps.CitationRate, cs.CitationRate),
			computeDelta(dimSpec{name: "share_of_voice", label: "声量份额", higherIsBetter: true, warn: 5, crit: 10, stableTol: 1}, ps.ShareOfVoice, cs.ShareOfVoice),
			computeDelta(dimSpec{name: "avg_position", label: "平均位置", higherIsBetter: false, warn: 1.5, crit: 3, stableTol: 0.5}, ps.AvgPosition, cs.AvgPosition),
		}
		out = append(out, EngineDelta{Engine: eng, Dims: dims})
	}
	return out
}

// engineMap 将引擎统计切片转为按引擎类型索引的 map。
func engineMap(stats []brand.EngineStats) map[models.EngineType]brand.EngineStats {
	m := make(map[models.EngineType]brand.EngineStats, len(stats))
	for _, s := range stats {
		m[s.Engine] = s
	}
	return m
}

// join 简易字符串连接（避免引入 strings 仅为一次调用）。
func join(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += sep + s
	}
	return out
}
