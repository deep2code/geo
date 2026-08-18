package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"my-geo/pkg/geo"
)

// RuntimeInfo 系统运行时快照。
type RuntimeInfo struct {
	GoVersion  string  `json:"go_version"`
	OS         string  `json:"os"`
	Arch       string  `json:"arch"`
	NumCPU     int     `json:"num_cpu"`
	Goroutines int     `json:"goroutines"`
	AllocMB    float64 `json:"alloc_mb"`
}

// SelfCheckReport 系统自检总报告。
type SelfCheckReport struct {
	GeneratedAt string        `json:"generated_at"`
	Overall     Severity      `json:"overall"`
	Runtime     RuntimeInfo   `json:"runtime"`
	Business    []CheckResult `json:"business"`
	Config      []CheckResult `json:"config"`
	Summary     SummaryCounts `json:"summary"`
}

// SummaryCounts 各等级计数（business + config 合并）。
type SummaryCounts struct {
	OK    int `json:"ok"`
	Info  int `json:"info"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
}

// SelfCheck 聚合运行时信息、关键业务健康与配置校验，产出整体自检报告。
//
// engine 用于业务探针（评分/分析/优化/LLM）；rulesPath 可选，传入则额外校验外部规则集。
// 该调用只读、无副作用。
func SelfCheck(ctx context.Context, engine *geo.Engine, rulesPath string) *SelfCheckReport {
	business := BusinessHealth(ctx, engine)
	cfg := ConfigCheck(rulesPath)

	all := append([]CheckResult{}, business...)
	all = append(all, cfg...)

	counts := CountBySeverity(all)
	report := &SelfCheckReport{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Overall:     Overall(all),
		Runtime:     collectRuntime(),
		Business:    business,
		Config:      cfg,
		Summary: SummaryCounts{
			OK:    counts[SeverityOK],
			Info:  counts[SeverityInfo],
			Warn:  counts[SeverityWarn],
			Error: counts[SeverityError],
		},
	}
	return report
}

// collectRuntime 采集运行时快照。
func collectRuntime() RuntimeInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return RuntimeInfo{
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(),
		AllocMB:    float64(m.Alloc) / (1024 * 1024),
	}
}

// HasError 是否存在 error 级问题（决定是否以非零退出码结束）。
func (r *SelfCheckReport) HasError() bool { return r.Overall == SeverityError }

// RenderJSON 序列化为 JSON（含缩进）。
func (r *SelfCheckReport) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// RenderText 将报告渲染为可读文本写入 w。
func (r *SelfCheckReport) RenderText(w io.Writer) {
	var b strings.Builder
	title := "GEO 系统自检报告"
	b.WriteString(strings.Repeat("═", 56) + "\n")
	b.WriteString(" " + title + "\n")
	b.WriteString(strings.Repeat("═", 56) + "\n")
	b.WriteString(fmt.Sprintf(" 生成时间：%s\n", r.GeneratedAt))
	b.WriteString(fmt.Sprintf(" 总体状态：%s\n", statusLabel(r.Overall)))
	b.WriteString(fmt.Sprintf(" 运行时：Go %s · %s/%s · %d CPU · %d goroutines · %.1f MB\n",
		r.Runtime.GoVersion, r.Runtime.OS, r.Runtime.Arch,
		r.Runtime.NumCPU, r.Runtime.Goroutines, r.Runtime.AllocMB))
	b.WriteString(fmt.Sprintf(" 统计：✓ %d  ·  ℹ %d  ·  ⚠ %d  ·  ✗ %d\n\n",
		r.Summary.OK, r.Summary.Info, r.Summary.Warn, r.Summary.Error))

	b.WriteString(sectionTitle("关键业务健康"))
	for _, c := range r.Business {
		b.WriteString(renderLine(c))
	}
	b.WriteString("\n")

	b.WriteString(sectionTitle("属性 / 参数 / 配置"))
	for _, c := range r.Config {
		b.WriteString(renderLine(c))
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("═", 56) + "\n")

	_, _ = io.WriteString(w, b.String())
}

// statusLabel 将等级转为中文标签。
func statusLabel(s Severity) string {
	switch s {
	case SeverityOK:
		return "✓ 健康"
	case SeverityInfo:
		return "ℹ 正常（含提示项）"
	case SeverityWarn:
		return "⚠ 存在隐患"
	case SeverityError:
		return "✗ 存在问题"
	default:
		return string(s)
	}
}

// sectionTitle 渲染小节标题。
func sectionTitle(t string) string {
	return "【" + t + "】\n"
}

// renderLine 渲染单条检查结果行。
func renderLine(c CheckResult) string {
	mark := "?"
	switch c.Status {
	case SeverityOK:
		mark = "✓"
	case SeverityInfo:
		mark = "ℹ"
	case SeverityWarn:
		mark = "⚠"
	case SeverityError:
		mark = "✗"
	}
	line := fmt.Sprintf("  %s %s：%s\n", mark, c.Name, c.Message)
	if c.Detail != "" {
		line += fmt.Sprintf("      └ %s\n", c.Detail)
	}
	return line
}
