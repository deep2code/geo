// Package diagnostics 提供 GEO 系统的三类诊断能力：
//
//  1. 业务健康检查（business）：探测关键业务路径是否真的能工作
//     （评分 / 分析 / 优化 / LLM 改写 / 各数据库可达性）。
//  2. 配置校验（config）：检查属性、参数、环境变量、DSN、规则集等是否有问题。
//  3. 系统自检（selfcheck）：聚合运行时信息 + 业务健康 + 配置校验，
//     产出整体健康报告（text / json），供 /api/v1/admin/selfcheck 与「系统自检」前端页使用。
//
// 设计为无副作用的只读探针：不修改任何状态、不写库、不发送业务请求
// （LLM 探测为唯一例外，且仅在已配置可用 Provider 时执行一次小调用）。
package diagnostics

import "time"

// Severity 单条检查结论的等级。
type Severity string

const (
	SeverityOK    Severity = "ok"    // 正常
	SeverityInfo  Severity = "info"  // 提示性（如功能未启用、已跳过）
	SeverityWarn  Severity = "warn"  // 配置/环境有隐患，但不阻断运行
	SeverityError Severity = "error" // 业务或配置存在实质性问题
)

// Category 检查所属类别。
type Category string

const (
	CategoryBusiness Category = "business" // 关键业务是否正常
	CategoryConfig   Category = "config"   // 属性/参数/配置是否有问题
	CategoryInfra    Category = "infra"    // 运行时/基础设施
)

// CheckResult 单条检查结果。
type CheckResult struct {
	Name       string   `json:"name"`
	Category   Category `json:"category"`
	Status     Severity `json:"status"`
	Message    string   `json:"message"`
	Detail     string   `json:"detail,omitempty"` // 失败原因 / 上下文，可选
	DurationMs float64  `json:"duration_ms,omitempty"`
}

// Overall 计算一组结果的总体等级：出现 error → error；出现 warn → warn；其余 ok。
// info / ok 不会拉低等级。
func Overall(results []CheckResult) Severity {
	hasWarn := false
	for _, r := range results {
		switch r.Status {
		case SeverityError:
			return SeverityError
		case SeverityWarn:
			hasWarn = true
		}
	}
	if hasWarn {
		return SeverityWarn
	}
	return SeverityOK
}

// CountBySeverity 统计各等级数量。
func CountBySeverity(results []CheckResult) map[Severity]int {
	m := map[Severity]int{SeverityOK: 0, SeverityInfo: 0, SeverityWarn: 0, SeverityError: 0}
	for _, r := range results {
		m[r.Status]++
	}
	return m
}

// sinceMs 返回从 start 到现在的毫秒数（用于探针耗时记录）。
func sinceMs(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}
