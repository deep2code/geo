// Package trend 可见度趋势追踪：存储时序数据并生成趋势图表数据。
//
// 在 scheduler 的定时审计基础上，增加：
//   - 时序数据存储（每次审计结果追加到趋势队列）
//   - 趋势图表数据生成（折线图/柱状图数据点）
//   - 变化率计算（环比/同比）
//   - 异常检测（突变/持续下滑/恢复）
package trend

import (
	"math"
	"sort"
	"time"

	"my-geo/internal/models"
)

// TrendPoint 单个时间点的趋势数据。
type TrendPoint struct {
	Timestamp   time.Time              `json:"timestamp"`
	BVSScore    float64                `json:"bvs_score"`
	Engines     []EngineTrendPoint     `json:"engines,omitempty"`
	MentionRate float64                `json:"mention_rate"`  // 提及率 0-1
	CitationRate float64               `json:"citation_rate"` // 引用率 0-1
}

// EngineTrendPoint 单个引擎的趋势数据。
type EngineTrendPoint struct {
	Engine    models.EngineType `json:"engine"`
	Mentioned bool              `json:"mentioned"`
	Cited     bool              `json:"cited"`
	Position  int               `json:"position"`
	Sentiment string            `json:"sentiment"`
}

// TrendSeries 趋势序列。
type TrendSeries struct {
	BrandName string       `json:"brand_name"`
	Points    []TrendPoint `json:"points"`
	Summary   TrendSummary `json:"summary"`
}

// TrendSummary 趋势摘要。
type TrendSummary struct {
	TotalChecks    int     `json:"total_checks"`
	AvgBVSScore    float64 `json:"avg_bvs_score"`
	MinBVSScore    float64 `json:"min_bvs_score"`
	MaxBVSScore    float64 `json:"max_bvs_score"`
	ScoreChange    float64 `json:"score_change"`     // 最近一次 vs 第一次
	ChangePercent  float64 `json:"change_percent"`   // 变化百分比
	TrendDirection string  `json:"trend_direction"`  // up/down/stable
	Alerts         []TrendAlert `json:"alerts,omitempty"`
}

// TrendAlert 趋势异常告警。
type TrendAlert struct {
	Type        string    `json:"type"` // sudden_drop / continuous_decline / recovery / sudden_rise
	Severity    string    `json:"severity"` // high/medium/low
	Message     string    `json:"message"`
	DetectedAt  time.Time `json:"detected_at"`
	ScoreDelta  float64   `json:"score_delta"`
}

// Tracker 趋势追踪器。
type Tracker struct {
	brandName string
	points    []TrendPoint
	maxPoints int // 最大保留点数（滚动窗口）
}

// NewTracker 创建趋势追踪器。
func NewTracker(brandName string, maxPoints int) *Tracker {
	if maxPoints <= 0 {
		maxPoints = 365 // 默认保留一年
	}
	return &Tracker{
		brandName: brandName,
		maxPoints: maxPoints,
	}
}

// AddPoint 添加一个趋势数据点。
func (t *Tracker) AddPoint(point TrendPoint) {
	t.points = append(t.points, point)
	// 滚动窗口：超出最大点数时删除最旧的
	if len(t.points) > t.maxPoints {
		t.points = t.points[len(t.points)-t.maxPoints:]
	}
}

// AddFromPromptResult 从 PromptResult 构建趋势点并添加。
func (t *Tracker) AddFromPromptResult(prs []PromptResult) {
	if len(prs) == 0 {
		return
	}
	point := TrendPoint{
		Timestamp: time.Now(),
	}
	// 聚合多个 prompt 的结果
	mentioned, cited := 0, 0
	for _, pr := range prs {
		if pr.BrandMentioned {
			mentioned++
		}
		if pr.BrandCited {
			cited++
		}
	}
	n := float64(len(prs))
	if n > 0 {
		point.MentionRate = float64(mentioned) / n
		point.CitationRate = float64(cited) / n
	}
	t.AddPoint(point)
}

// PromptResult 简化的审计结果（避免循环导入）。
type PromptResult struct {
	BrandMentioned bool
	BrandCited     bool
	Engine         models.EngineType
	BrandPosition  int
	Sentiment      string
}

// GetSeries 获取趋势序列。
func (t *Tracker) GetSeries() TrendSeries {
	series := TrendSeries{
		BrandName: t.brandName,
		Points:    t.points,
		Summary:   t.calcSummary(),
	}
	return series
}

// calcSummary 计算趋势摘要。
func (t *Tracker) calcSummary() TrendSummary {
	if len(t.points) == 0 {
		return TrendSummary{}
	}
	scores := make([]float64, 0, len(t.points))
	for _, p := range t.points {
		scores = append(scores, p.BVSScore)
	}
	sort.Float64s(scores)

	summary := TrendSummary{
		TotalChecks: len(t.points),
		AvgBVSScore: avg(scores),
		MinBVSScore: scores[0],
		MaxBVSScore: scores[len(scores)-1],
	}

	if len(t.points) >= 2 {
		first := t.points[0].BVSScore
		last := t.points[len(t.points)-1].BVSScore
		summary.ScoreChange = last - first
		if first > 0 {
			summary.ChangePercent = (last - first) / first * 100
		}
		if last > first*1.05 {
			summary.TrendDirection = "up"
		} else if last < first*0.95 {
			summary.TrendDirection = "down"
		} else {
			summary.TrendDirection = "stable"
		}
	}

	// 异常检测
	summary.Alerts = t.detectAlerts()
	return summary
}

// detectAlerts 检测趋势异常。
func (t *Tracker) detectAlerts() []TrendAlert {
	if len(t.points) < 3 {
		return nil
	}
	var alerts []TrendAlert
	n := len(t.points)

	// 1. 突变检测：最近一次 vs 前一次的变化超过 15%
	if n >= 2 {
		prev := t.points[n-2].BVSScore
		last := t.points[n-1].BVSScore
		if prev > 0 {
			change := (last - prev) / prev * 100
			if change < -15 {
				alerts = append(alerts, TrendAlert{
					Type:       "sudden_drop",
					Severity:   "high",
					Message:    "BVS 评分突降 " + formatPercent(change),
					DetectedAt: time.Now(),
					ScoreDelta: last - prev,
				})
			} else if change > 20 {
				alerts = append(alerts, TrendAlert{
					Type:       "sudden_rise",
					Severity:   "medium",
					Message:    "BVS 评分突升 " + formatPercent(change),
					DetectedAt: time.Now(),
					ScoreDelta: last - prev,
				})
			}
		}
	}

	// 2. 持续下滑检测：最近 5 次连续下降
	if n >= 5 {
		continuous := true
		for i := n - 5; i < n-1; i++ {
			if t.points[i+1].BVSScore >= t.points[i].BVSScore {
				continuous = false
				break
			}
		}
		if continuous {
			delta := t.points[n-1].BVSScore - t.points[n-5].BVSScore
			alerts = append(alerts, TrendAlert{
				Type:       "continuous_decline",
				Severity:   "high",
				Message:    "BVS 评分连续 5 次下滑，累计下降 " + formatFloat(math.Abs(delta)),
				DetectedAt: time.Now(),
				ScoreDelta: delta,
			})
		}
	}

	// 3. 恢复检测：从低谷回升
	if n >= 3 {
		minIdx := 0
		for i, p := range t.points {
			if p.BVSScore < t.points[minIdx].BVSScore {
				minIdx = i
			}
		}
		if minIdx > 0 && minIdx < n-1 {
			recovery := t.points[n-1].BVSScore - t.points[minIdx].BVSScore
			if recovery > 10 {
				alerts = append(alerts, TrendAlert{
					Type:       "recovery",
					Severity:   "medium",
					Message:    "BVS 评分从低谷恢复 +" + formatFloat(recovery),
					DetectedAt: time.Now(),
					ScoreDelta: recovery,
				})
			}
		}
	}
	return alerts
}

// ChartData 生成图表数据（适配常见图表库）。
func (t *Tracker) ChartData() map[string]interface{} {
	if len(t.points) == 0 {
		return map[string]interface{}{"labels": []string{}, "datasets": []interface{}{}}
	}
	labels := make([]string, 0, len(t.points))
	scores := make([]float64, 0, len(t.points))
	mentionRates := make([]float64, 0, len(t.points))
	citationRates := make([]float64, 0, len(t.points))

	for _, p := range t.points {
		labels = append(labels, p.Timestamp.Format("2006-01-02 15:04"))
		scores = append(scores, p.BVSScore)
		mentionRates = append(mentionRates, p.MentionRate*100)
		citationRates = append(citationRates, p.CitationRate*100)
	}

	return map[string]interface{}{
		"labels": labels,
		"datasets": []map[string]interface{}{
			{
				"label":       "BVS 评分",
				"data":        scores,
				"borderColor": "#7c3aed",
				"fill":        false,
			},
			{
				"label":       "提及率 (%)",
				"data":        mentionRates,
				"borderColor": "#0ea5e9",
				"fill":        false,
			},
			{
				"label":       "引用率 (%)",
				"data":        citationRates,
				"borderColor": "#10b981",
				"fill":        false,
			},
		},
	}
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func formatFloat(v float64) string {
	if v == math.Trunc(v) {
		return formatInt(int(v))
	}
	abs := math.Abs(v)
	intPart := int(abs)
	decPart := int((abs - float64(intPart)) * 10)
	return formatInt(intPart) + "." + formatInt(decPart) + "0"
}

func formatInt(n int) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return formatInt(n/10) + string(rune('0'+n%10))
}

func formatPercent(v float64) string {
	abs := math.Abs(v)
	intPart := int(abs)
	decPart := int((abs - float64(intPart)) * 10)
	result := formatInt(intPart) + "." + formatInt(decPart) + "0%"
	if v > 0 {
		return "+" + result
	} else if v < 0 {
		return "-" + result
	}
	return result
}
