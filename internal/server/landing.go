package server

import (
	"net/http"
	"strings"
)

// PricingPlan 定价方案。
type PricingPlan struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`         // 免费版/专业版/企业版
	Price       float64    `json:"price"`        // 月价格（CNY）
	PriceYearly float64    `json:"price_yearly"` // 年价格（优惠后）
	Currency    string     `json:"currency"`     // CNY
	Features    []string   `json:"features"`     // 功能列表
	Limits      PlanLimits `json:"limits"`
	Popular     bool       `json:"popular"` // 是否推荐
	CTA         string     `json:"cta"`      // 按钮文案
}

// PlanLimits 方案权益上限。
type PlanLimits struct {
	Brands          int  `json:"brands"`            // 最大品牌数，-1=无限
	AuditsPerMonth  int  `json:"audits_per_month"`  // 每月审计次数
	EmailsPerMonth  int  `json:"emails_per_month"`  // 每月邮件数
	ReportsPerMonth int  `json:"reports_per_month"` // 每月报告数
	ConcurrentAudits int  `json:"concurrent_audits"` // 并发审计数
	APIAccess       bool `json:"api_access"`
	Whitelabel      bool `json:"whitelabel"`
	PrioritySupport bool `json:"priority_support"`
}

// FeatureShowcase 功能亮点。
type FeatureShowcase struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	Icon     string `json:"icon"`     // emoji
	Category string `json:"category"` // audit/optimize/report/monitor/integrate
}

// pricingPlans 定价方案（硬编码）。
var pricingPlans = []PricingPlan{
	{
		ID:          "free",
		Name:        "免费版",
		Price:       0,
		PriceYearly: 0,
		Currency:    "CNY",
		Features: []string{
			"1 个品牌",
			"每月 10 次审计",
			"基础 BVS 分数",
			"HTML 报告导出",
			"社区支持",
		},
		Limits: PlanLimits{
			Brands:           1,
			AuditsPerMonth:   10,
			EmailsPerMonth:   0,
			ReportsPerMonth:  5,
			ConcurrentAudits: 1,
			APIAccess:        false,
			Whitelabel:       false,
			PrioritySupport:  false,
		},
		Popular: false,
		CTA:     "免费开始",
	},
	{
		ID:          "pro",
		Name:        "专业版",
		Price:       999,
		PriceYearly: 9990, // 约 8.3 折
		Currency:    "CNY",
		Features: []string{
			"最多 10 个品牌",
			"每月 500 次审计",
			"全维度 BVS 分析",
			"PDF 报告导出",
			"邮件告警与周报",
			"竞品对标矩阵",
			"API 访问",
			"工作日客服支持",
		},
		Limits: PlanLimits{
			Brands:           10,
			AuditsPerMonth:   500,
			EmailsPerMonth:   200,
			ReportsPerMonth:  100,
			ConcurrentAudits: 3,
			APIAccess:        true,
			Whitelabel:       false,
			PrioritySupport:  false,
		},
		Popular: true,
		CTA:     "立即升级",
	},
	{
		ID:          "enterprise",
		Name:        "企业版",
		Price:       4999,
		PriceYearly: 49990, // 约 8.3 折
		Currency:    "CNY",
		Features: []string{
			"无限品牌",
			"无限审计次数",
			"全维度 BVS 分析",
			"PDF/Excel 报告导出",
			"邮件告警与周报",
			"竞品对标矩阵",
			"API 访问（更高配额）",
			"白标定制",
			"专属客户成功经理",
			"7×24 优先支持",
		},
		Limits: PlanLimits{
			Brands:           -1, // 无限
			AuditsPerMonth:   -1,
			EmailsPerMonth:   -1,
			ReportsPerMonth:  -1,
			ConcurrentAudits: 10,
			APIAccess:        true,
			Whitelabel:       true,
			PrioritySupport:  true,
		},
		Popular: false,
		CTA:     "联系销售",
	},
}

// featureShowcases 功能亮点（硬编码，至少 8 个）。
var featureShowcases = []FeatureShowcase{
	{
		ID:       "ai-visibility-audit",
		Title:    "AI 可见度审计",
		Desc:     "模拟真实用户提问，检测品牌在 GLM、通义千问、Doubao 等主流 AI 引擎中的提及率与引用率",
		Icon:     "🔍",
		Category: "audit",
	},
	{
		ID:       "bvs-score",
		Title:    "BVS 品牌可见度分数",
		Desc:     "综合提及率、引用率、情感分、实体识别等多维度，生成 0-100 的品牌 AI 可见度评分",
		Icon:     "📊",
		Category: "audit",
	},
	{
		ID:       "content-optimizer",
		Title:    "内容优化引擎",
		Desc:     "自动识别内容信号缺口，给出可执行的优化建议，提升 AI 引擎引用概率",
		Icon:     "✨",
		Category: "optimize",
	},
	{
		ID:       "multi-engine-compare",
		Title:    "多引擎对标",
		Desc:     "同一品牌在多个 AI 引擎中的表现对比，发现模型分歧与优化机会",
		Icon:     "⚖️",
		Category: "audit",
	},
	{
		ID:       "report-export",
		Title:    "报告导出",
		Desc:     "支持 HTML 在线报告与 PDF 下载，一键邮件发送给团队成员",
		Icon:     "📄",
		Category: "report",
	},
	{
		ID:       "trend-monitor",
		Title:    "趋势监控",
		Desc:     "审计历史时间序列，可视化 BVS 分数变化趋势，及时发现异常",
		Icon:     "📈",
		Category: "monitor",
	},
	{
		ID:       "alert-email",
		Title:    "告警邮件",
		Desc:     "分数下降自动告警，定时周报/月报，模型分歧异常推送",
		Icon:     "🔔",
		Category: "monitor",
	},
	{
		ID:       "competitor-benchmark",
		Title:    "竞品对标矩阵",
		Desc:     "与竞品并排对比 AI 可见度表现，识别优势与劣势维度",
		Icon:     "🎯",
		Category: "audit",
	},
	{
		ID:       "api-access",
		Title:    "API 接入",
		Desc:     "专业版及以上提供 REST API，支持集成到自有系统与自动化流程",
		Icon:     "🔌",
		Category: "integrate",
	},
	{
		ID:       "whitelabel",
		Title:    "白标定制",
		Desc:     "企业版支持品牌名称、Logo、配色等白标定制，打造专属 AI 可见度平台",
		Icon:     "🎨",
		Category: "integrate",
	},
}

// handlePricingPlans 定价方案列表。
// GET /api/v1/pricing/plans
func (s *Server) handlePricingPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": len(pricingPlans),
		"plans": pricingPlans,
	})
}

// handlePricingPlanDetail 单个方案详情。
// GET /api/v1/pricing/plans/{id}
func (s *Server) handlePricingPlanDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/pricing/plans/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少方案 ID"})
		return
	}
	for _, p := range pricingPlans {
		if p.ID == id {
			writeJSON(w, http.StatusOK, p)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "定价方案不存在"})
}

// handleLandingFeatures 功能亮点列表。
// GET /api/v1/landing/features
func (s *Server) handleLandingFeatures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	var list []FeatureShowcase
	for _, f := range featureShowcases {
		if category != "" && f.Category != category {
			continue
		}
		list = append(list, f)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":    len(list),
		"features": list,
	})
}

// handleLandingStats 平台统计数据。
// GET /api/v1/landing/stats
func (s *Server) handleLandingStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	// 展示数据（硬编码的营销数据，非真实统计）
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"brands_tracked":  "12,500+",
		"audits_run":      "1,200,000+",
		"users":           "3,800+",
		"ai_engines":      "6",
		"uptime":          "99.9%",
		"avg_bvs_improve": "+37%",
	})
}
