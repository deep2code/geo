// Package billing 提供 GEO 商业化的核心能力：套餐目录、订阅、用量配额、
// 支付订单与发票。设计为「免费 + 手动激活」轻量版优先——即使未配置任何
// 支付渠道（微信/支付宝/Stripe），订阅与配额逻辑也能完整运行；配置渠道后
// 自动切换为在线支付闭环（详见 internal/billing/payment）。
package billing

import "time"

// PlanID 套餐标识。
type PlanID string

const (
	PlanFree       PlanID = "free"
	PlanPro        PlanID = "pro"
	PlanTeam       PlanID = "team"
	PlanEnterprise PlanID = "enterprise"
)

// Meter 计量维度（配额按自然月滚动）。
type Meter string

const (
	// MeterAudits 每月品牌可见度审计次数。
	MeterAudits Meter = "audits"
	// MeterBrands 可管理品牌数（预留，用于未来品牌库上限）。
	MeterBrands Meter = "brands"
	// MeterScheduledAudits 定时审计任务数。
	MeterScheduledAudits Meter = "scheduled_audits"
	// MeterExports 每月报告导出（PDF / 邮件）次数。
	MeterExports Meter = "exports"
	// MeterHistoryRetentionDays 审计历史保留天数。
	MeterHistoryRetentionDays Meter = "history_retention_days"
)

// Unlimited 表示某计量维度无上限（plan.Limits 中设为该值）。
const Unlimited int64 = -1

// Plan 套餐定义（带配额的真源）。landing.go 的 pricingPlans 营销展示后续可收敛到此。
type Plan struct {
	ID          PlanID
	Name        string
	PriceCents  int64  // 月价（分，人民币）；0 表示免费
	Currency    string // CNY / USD
	Description string
	// Limits 各计量维度月度上限；缺失或 Unlimited(-1) 表示不限制。
	Limits map[Meter]int64
	// Features 营销特性列表（用于 /api/v1/billing/plans 展示）。
	Features []string
}

// Catalog 返回全部套餐（按价格升序）。
func Catalog() []Plan {
	return []Plan{
		{
			ID:          PlanFree,
			Name:        "免费版",
			PriceCents:  0,
			Currency:    "CNY",
			Description: "个人试用，核心审计能力开放，适合验证 GEO 价值。",
			Limits: map[Meter]int64{
				MeterAudits:               10,
				MeterBrands:               3,
				MeterScheduledAudits:      0,
				MeterExports:              5,
				MeterHistoryRetentionDays: 30,
			},
			Features: []string{
				"每月 10 次品牌审计",
				"3 个品牌",
				"报告 HTML 导出",
				"30 天历史保留",
			},
		},
		{
			ID:          PlanPro,
			Name:        "专业版",
			PriceCents:  9900, // ¥99/月
			Currency:    "CNY",
			Description: "成长型团队，高频审计 + 定时监控 + PDF 邮件投递。",
			Limits: map[Meter]int64{
				MeterAudits:               200,
				MeterBrands:               20,
				MeterScheduledAudits:      5,
				MeterExports:              100,
				MeterHistoryRetentionDays: 365,
			},
			Features: []string{
				"每月 200 次审计",
				"20 个品牌",
				"5 个定时审计任务",
				"PDF + 邮件投递",
				"365 天历史保留",
			},
		},
		{
			ID:          PlanTeam,
			Name:        "团队版",
			PriceCents:  29900, // ¥299/月
			Currency:    "CNY",
			Description: "多成员协作，更大审计额度与历史窗口。",
			Limits: map[Meter]int64{
				MeterAudits:               1000,
				MeterBrands:               100,
				MeterScheduledAudits:      20,
				MeterExports:              500,
				MeterHistoryRetentionDays: 365,
			},
			Features: []string{
				"每月 1000 次审计",
				"100 个品牌",
				"20 个定时审计任务",
				"无限成员（RBAC）",
				"365 天历史保留",
			},
		},
		{
			ID:          PlanEnterprise,
			Name:        "企业版",
			PriceCents:  0, // 价格面议，按合同激活
			Currency:    "CNY",
			Description: "白标、SLA、私有部署、无限额度。",
			Limits: map[Meter]int64{
				MeterAudits:               Unlimited,
				MeterBrands:               Unlimited,
				MeterScheduledAudits:      Unlimited,
				MeterExports:              Unlimited,
				MeterHistoryRetentionDays: Unlimited,
			},
			Features: []string{
				"无限审计与品牌",
				"白标定制",
				"SLA 与企业支持",
				"私有部署选项",
			},
		},
	}
}

// PlanByID 按 ID 取套餐；不存在返回零值与 false。
func PlanByID(id PlanID) (Plan, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// MonthKey 返回当前自然月的 YYYY-MM（用于配额滚动）。
func MonthKey(t time.Time) string {
	return t.Format("2006-01")
}
