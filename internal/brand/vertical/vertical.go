// Package vertical 实现行业类型自动检测与差异化审计建议。
//
// 借鉴 Claude SEO 的思路，自动识别 5 类业务形态（SaaS、本地服务、电商、
// 媒体出版、代理咨询），并针对每种行业上下文定制审计发现与运营建议，
// 使 BVS 评分维度权重与行动建议更贴合该行业的实际优化重点：
//   - SaaS：强调 Schema/JSON-LD、API 文档与定价页的可引用性
//   - 本地服务：强调 NAP 一致性、Google Business Profile 完备度、本地引用
//   - 电商：强调产品 Schema、Google Shopping、评价结构化数据
//   - 媒体出版：强调内容深度、E-E-A-T、Article 结构化数据
//   - 代理咨询：强调权威信号、案例研究、客户证言
//
// 仅依赖标准库 strings / fmt，无第三方依赖。
package vertical

import (
	"fmt"
	"strings"
)

// Vertical 业务垂直行业类型。
type Vertical string

const (
	VerticalSaaS         Vertical = "saas"
	VerticalLocalService Vertical = "local_service"
	VerticalEcommerce    Vertical = "ecommerce"
	VerticalPublisher    Vertical = "publisher"
	VerticalAgency       Vertical = "agency"
	VerticalUnknown      Vertical = "unknown"
)

// CheckFunc 评分前预检函数：基于品牌画像输出额外检查项。
type CheckFunc func(profile map[string]interface{}) []CheckResult

// RecFunc 评分后建议函数：基于品牌画像与最终 BVS 分数输出运营建议。
type RecFunc func(profile map[string]interface{}, score float64) []Recommendation

// AuditHooks 行业差异化审计钩子，允许各行业注入专属检查与建议逻辑。
type AuditHooks struct {
	// PreScoreChecks 在 BVS 评分前执行的检查项（如本地服务的 NAP 一致性）。
	PreScoreChecks []CheckFunc
	// PostScoreRecommendations 在 BVS 评分后基于分数生成的建议。
	PostScoreRecommendations []RecFunc
	// ReportTemplateOverrides 报告模板片段覆盖（key=片段名，value=模板文本）。
	ReportTemplateOverrides map[string]string
	// ScoreWeightOverrides 覆盖 BVS 维度权重（key=维度名，value=权重 0-1）。
	ScoreWeightOverrides map[string]float64
}

// CheckResult 单项检查结果。
type CheckResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`   // pass / fail / warn
	Severity string `json:"severity"` // critical / high / medium / low
	Detail   string `json:"detail"`
}

// Recommendation 运营建议。
type Recommendation struct {
	Priority string `json:"priority"` // high / medium / low
	Category string `json:"category"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// VerticalConfig 行业配置：标签、说明、评分权重与审计钩子。
type VerticalConfig struct {
	Vertical     Vertical           `json:"vertical"`
	Label        string             `json:"label"`
	Description  string             `json:"description"`
	ScoreWeights map[string]float64 `json:"score_weights"`
	Hooks        AuditHooks         `json:"-"`
}

// allVerticals 已知的 5 类业务垂直行业（不含 unknown）。
var allVerticals = []Vertical{
	VerticalSaaS,
	VerticalLocalService,
	VerticalEcommerce,
	VerticalPublisher,
	VerticalAgency,
}

// verticalKeywords 各行业检测关键词（小写匹配，兼顾中英文）。
var verticalKeywords = map[Vertical][]string{
	VerticalSaaS:         {"software", "platform", "saas", "cloud", "api", "订阅", "软件", "平台"},
	VerticalLocalService: {"餐厅", "美容", "维修", "诊所", "门店", "local", "service", "near me"},
	VerticalEcommerce:    {"shop", "store", "电商", "商城", "购物", "product", "sku"},
	VerticalPublisher:    {"media", "news", "blog", "出版", "媒体", "内容", "article"},
	VerticalAgency:       {"agency", "consulting", "代理", "咨询", "服务公司"},
}

// allConfigs 各行业配置缓存，init 时一次性构建。
var allConfigs = map[Vertical]*VerticalConfig{}

func init() {
	// 已知 5 类
	for _, v := range allVerticals {
		allConfigs[v] = buildConfig(v)
	}
	// unknown 兜底配置
	allConfigs[VerticalUnknown] = buildConfig(VerticalUnknown)
}

// Detect 基于品牌画像自动识别业务垂直行业。
//
// 分析 industry / category / domain / products / company.description 等字段，
// 通过关键词匹配统计各行业命中数，返回得分最高的行业；无命中返回 VerticalUnknown。
// 多个行业命中数相同时，按 allVerticals 的固定顺序取先者，保证结果稳定。
func Detect(profile map[string]interface{}) Vertical {
	if len(profile) == 0 {
		return VerticalUnknown
	}
	text := profileToText(profile)
	if text == "" {
		return VerticalUnknown
	}
	lower := strings.ToLower(text)

	scores := map[Vertical]int{}
	for _, v := range allVerticals {
		for _, kw := range verticalKeywords[v] {
			if strings.Contains(lower, strings.ToLower(kw)) {
				scores[v]++
			}
		}
	}

	best := VerticalUnknown
	bestScore := 0
	for _, v := range allVerticals {
		if scores[v] > bestScore {
			bestScore = scores[v]
			best = v
		}
	}
	return best
}

// profileToText 将品牌画像中可用于行业识别的文本字段拼接为一行。
//
// 提取 industry / category / domain / description / products / company 等字段，
// 兼容 string、[]string、[]interface{} 及嵌套 company map。
func profileToText(profile map[string]interface{}) string {
	var b strings.Builder
	for _, key := range []string{"industry", "category", "domain", "description", "company_description"} {
		if s := flagStr(profile, key); s != "" {
			b.WriteString(" ")
			b.WriteString(s)
		}
	}
	if v, ok := profile["products"]; ok {
		if s := joinAnySlice(v, " "); s != "" {
			b.WriteString(" ")
			b.WriteString(s)
		}
	}
	if v, ok := profile["company"]; ok {
		switch c := v.(type) {
		case map[string]interface{}:
			for _, key := range []string{"description", "industry", "name"} {
				if s := flagStr(c, key); s != "" {
					b.WriteString(" ")
					b.WriteString(s)
				}
			}
		case string:
			if c != "" {
				b.WriteString(" ")
				b.WriteString(c)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// GetConfig 返回指定行业的配置（含评分权重与审计钩子）。
//
// 未识别行业返回 unknown 兜底配置，永远返回非 nil。
func GetConfig(v Vertical) *VerticalConfig {
	if cfg, ok := allConfigs[v]; ok {
		return cfg
	}
	return allConfigs[VerticalUnknown]
}

// AllVerticals 返回全部 5 类已知业务垂直行业（不含 unknown）。
func AllVerticals() []Vertical {
	out := make([]Vertical, len(allVerticals))
	copy(out, allVerticals)
	return out
}

// Label 返回行业的中文标签。
func Label(v Vertical) string {
	if cfg, ok := allConfigs[v]; ok {
		return cfg.Label
	}
	return allConfigs[VerticalUnknown].Label
}

// RecommendationsFor 基于行业与 BVS 分数返回差异化的运营建议。
//
// 分数 < 60 视为低分，优先输出该行业最关键的补救动作；
// 分数 ≥ 60 则输出巩固型建议。每类行业均返回 2-3 条建议。
func RecommendationsFor(v Vertical, score float64) []Recommendation {
	low := score < 60
	var recs []Recommendation
	switch v {
	case VerticalSaaS:
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "content",
			Title:    "公开 API 文档并建立第三方软件评测页",
			Detail:   "创建 API 文档公开页，在 G2/Capterra 上建立产品页面，扩大 AI 引擎可引用的权威来源。",
		})
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "schema",
			Title:    "完善产品与定价页 Schema/JSON-LD",
			Detail:   "为产品页添加 SoftwareApplication Schema，定价页添加 Offer Schema，提升 AI 结构化可读性。",
		})
		if low {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "ai_readiness",
				Title:    "构建可被 AI 引用的集成与文档内容",
				Detail:   "补充集成指南、用例说明与对比页，使 AI 回答产品类查询时更易引用品牌官网。",
			})
		}
	case VerticalLocalService:
		if low {
			recs = append(recs, Recommendation{
				Priority: "high",
				Category: "local_seo",
				Title:    "完善商家资料与 NAP 一致性",
				Detail:   "优先完善 Google Business Profile / 高德商家资料，确保 NAP（名称-地址-电话）跨引用一致性。",
			})
		} else {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "local_seo",
				Title:    "持续维护本地引用与评价",
				Detail:   "定期更新商家资料、收集本地评价，并在主流本地目录保持 NAP 一致。",
			})
		}
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "schema",
			Title:    "部署 LocalBusiness Schema",
			Detail:   "为门店页添加 LocalBusiness + PostalAddress Schema，包含 openingHours / geo / telephone 字段。",
		})
		if low {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "content",
				Title:    "创建「near me」类查询的落地页",
				Detail:   "针对「XX near me」等本地意图查询，建立按服务区域组织的城市/区域落地页。",
			})
		}
	case VerticalEcommerce:
		if low {
			recs = append(recs, Recommendation{
				Priority: "high",
				Category: "schema",
				Title:    "为所有产品页部署 Product Schema",
				Detail:   "确保所有产品页面有 Product Schema（JSON-LD），包含 price/availability/rating 字段。",
			})
		} else {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "schema",
				Title:    "持续完善 Product Schema 字段",
				Detail:   "在 Product Schema 中补全 price/availability/rating/brand 字段，并接入 Merchant Center。",
			})
		}
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "content",
			Title:    "接入 Google Shopping 与评价结构化数据",
			Detail:   "提交商品 Feed 至 Google Shopping，并在产品页添加 Review / AggregateRating Schema。",
		})
		if low {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "technical_seo",
				Title:    "优化产品页加载性能与可抓取性",
				Detail:   "压缩产品图、预渲染关键 SKU 页面，确保 AI 爬虫可抓取到价格与库存字段。",
			})
		}
	case VerticalPublisher:
		if low {
			recs = append(recs, Recommendation{
				Priority: "high",
				Category: "schema",
				Title:    "为文章页部署 Article Schema",
				Detail:   "为每篇文章添加 Article Schema（JSON-LD），包含 author/datePublished/dateModified。",
			})
		} else {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "schema",
				Title:    "持续完善 Article Schema 与作者标记",
				Detail:   "确保每篇文章的 author/datePublished/dateModified 字段完整且与页面一致。",
			})
		}
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "content",
			Title:    "强化内容深度与 E-E-A-T 信号",
			Detail:   "为每个主题提供深度原创报道，补充作者履历、引用来源与事实核查标注。",
		})
		if low {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "ai_readiness",
				Title:    "提供可被 AI 引用的结构化摘要",
				Detail:   "在长文顶部增加要点摘要，便于 AI 引擎直接抽取作为答案引用。",
			})
		}
	case VerticalAgency:
		if low {
			recs = append(recs, Recommendation{
				Priority: "high",
				Category: "content",
				Title:    "发布案例研究页面",
				Detail:   "发布案例研究页面，包含客户名称、服务内容、可量化成果。",
			})
		} else {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "content",
				Title:    "持续补充案例研究",
				Detail:   "定期发布含客户名称、服务内容、可量化成果的案例研究，强化权威信号。",
			})
		}
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "ai_readiness",
			Title:    "建立客户证言与权威背书",
			Detail:   "补充可署名的客户证言、行业奖项与媒体报道，提升 AI 引擎对品牌权威性的判定。",
		})
		if low {
			recs = append(recs, Recommendation{
				Priority: "medium",
				Category: "schema",
				Title:    "部署 Organization / Service Schema",
				Detail:   "为官网添加 Organization + Service Schema，包含 serviceType / areaServed / founder 字段。",
			})
		}
	default:
		// Unknown：通用建议
		recs = append(recs, Recommendation{
			Priority: prio(low, "high", "medium"),
			Category: "content",
			Title:    "补全品牌实体信息与官网内容",
			Detail:   "完善品牌官网的公司介绍、产品页与结构化数据，提升 AI 引擎对品牌实体的识别度。",
		})
		recs = append(recs, Recommendation{
			Priority: prio(low, "medium", "low"),
			Category: "schema",
			Title:    "部署基础 Schema/JSON-LD",
			Detail:   "为官网添加 Organization Schema 与 BreadcrumbList，建立基础结构化可读性。",
		})
	}
	return recs
}

// ---------- 各行业配置构建 ----------

// buildConfig 构建指定行业的配置（标签、说明、评分权重、审计钩子）。
func buildConfig(v Vertical) *VerticalConfig {
	switch v {
	case VerticalSaaS:
		w := map[string]float64{
			"content_quality": 0.25,
			"technical_seo":   0.20,
			"schema":          0.15,
			"ai_readiness":    0.15,
			"onpage":          0.15,
			"performance":     0.05,
			"image":           0.05,
		}
		return &VerticalConfig{
			Vertical:     v,
			Label:        "SaaS/软件",
			Description:  "软件即服务/云平台，强调 Schema/JSON-LD、API 文档与定价页的可引用性。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PreScoreChecks: []CheckFunc{saasPreCheck},
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(v, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	case VerticalLocalService:
		w := map[string]float64{
			"content_quality": 0.15,
			"technical_seo":   0.15,
			"schema":          0.10,
			"ai_readiness":    0.10,
			"onpage":          0.15,
			"performance":     0.05,
			"local_seo":       0.30,
		}
		return &VerticalConfig{
			Vertical:     v,
			Label:        "本地服务",
			Description:  "线下门店/本地服务商，强调 NAP 一致性、GMB 完备度与本地引用。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PreScoreChecks: []CheckFunc{localPreCheck},
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(v, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	case VerticalEcommerce:
		w := map[string]float64{
			"content_quality": 0.20,
			"technical_seo":   0.20,
			"schema":          0.20,
			"ai_readiness":    0.10,
			"onpage":          0.15,
			"performance":     0.10,
			"image":           0.05,
		}
		return &VerticalConfig{
			Vertical:     v,
			Label:        "电商",
			Description:  "在线零售/电商，强调产品 Schema、Google Shopping 与评价结构化数据。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PreScoreChecks: []CheckFunc{ecommercePreCheck},
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(v, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	case VerticalPublisher:
		w := map[string]float64{
			"content_quality": 0.35,
			"technical_seo":   0.20,
			"schema":          0.10,
			"ai_readiness":    0.10,
			"onpage":          0.15,
			"performance":     0.05,
			"image":           0.05,
		}
		return &VerticalConfig{
			Vertical:     v,
			Label:        "媒体/出版",
			Description:  "媒体/出版/内容站，强调内容深度、E-E-A-T 与 Article 结构化数据。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PreScoreChecks: []CheckFunc{publisherPreCheck},
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(v, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	case VerticalAgency:
		w := map[string]float64{
			"content_quality": 0.25,
			"technical_seo":   0.20,
			"schema":          0.10,
			"ai_readiness":    0.15,
			"onpage":          0.15,
			"performance":     0.10,
			"image":           0.05,
		}
		return &VerticalConfig{
			Vertical:     v,
			Label:        "代理/咨询",
			Description:  "代理/咨询/服务公司，强调权威信号、案例研究与客户证言。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PreScoreChecks: []CheckFunc{agencyPreCheck},
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(v, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	default:
		// Unknown：默认权重
		w := map[string]float64{
			"content_quality": 0.23,
			"technical_seo":   0.22,
			"onpage":          0.20,
			"schema":          0.10,
			"performance":     0.10,
			"ai_readiness":    0.10,
			"image":           0.05,
		}
		return &VerticalConfig{
			Vertical:     VerticalUnknown,
			Label:        "未识别",
			Description:  "未识别行业的默认配置，采用通用 BVS 维度权重。",
			ScoreWeights: w,
			Hooks: AuditHooks{
				PostScoreRecommendations: []RecFunc{
					func(profile map[string]interface{}, score float64) []Recommendation {
						return RecommendationsFor(VerticalUnknown, score)
					},
				},
				ScoreWeightOverrides: w,
			},
		}
	}
}

// ---------- 各行业评分前预检函数 ----------

// saasPreCheck 检查 SaaS 品牌的 API 文档与定价页公开情况。
func saasPreCheck(profile map[string]interface{}) []CheckResult {
	var out []CheckResult
	hasAPI := flagBool(profile, "api_docs") || flagStr(profile, "api_docs_url") != ""
	hasPricing := flagBool(profile, "pricing_page") || flagStr(profile, "pricing_url") != ""
	if !hasAPI {
		out = append(out, CheckResult{
			Name: "api_docs_public", Status: "warn", Severity: "medium",
			Detail: "未检测到公开 API 文档，SaaS 品牌应提供可被 AI 引擎引用的 API 文档页。",
		})
	}
	if !hasPricing {
		out = append(out, CheckResult{
			Name: "pricing_page", Status: "warn", Severity: "medium",
			Detail: "未检测到公开定价页，建议提供透明定价以提升可引用性与转化。",
		})
	}
	return out
}

// localPreCheck 检查本地服务品牌的 GMB 完备度与 NAP 一致性。
func localPreCheck(profile map[string]interface{}) []CheckResult {
	var out []CheckResult
	hasNAP := flagStr(profile, "address") != "" && flagStr(profile, "phone") != ""
	gmb := flagBool(profile, "gmb_claimed") || flagBool(profile, "google_business_profile")
	if !gmb {
		out = append(out, CheckResult{
			Name: "gmb_completeness", Status: "fail", Severity: "critical",
			Detail: "未检测到已认领的 Google Business Profile / 高德商家资料，本地服务品牌应优先完善商家资料。",
		})
	}
	if !hasNAP {
		out = append(out, CheckResult{
			Name: "nap_consistency", Status: "fail", Severity: "high",
			Detail: "NAP（名称-地址-电话）信息不完整，需确保跨所有本地目录与官网一致。",
		})
	}
	return out
}

// ecommercePreCheck 检查电商品牌的 Product Schema 与评价结构化数据。
func ecommercePreCheck(profile map[string]interface{}) []CheckResult {
	var out []CheckResult
	if !flagBool(profile, "product_schema") {
		out = append(out, CheckResult{
			Name: "product_schema", Status: "fail", Severity: "high",
			Detail: "未检测到 Product Schema（JSON-LD），电商页面应包含 price/availability/rating 字段。",
		})
	}
	if !flagBool(profile, "has_reviews") {
		out = append(out, CheckResult{
			Name: "product_reviews", Status: "warn", Severity: "medium",
			Detail: "未检测到产品评价结构化数据，建议接入 Review Schema 以增强富摘要与 AI 可读性。",
		})
	}
	return out
}

// publisherPreCheck 检查媒体/出版品牌的 Article Schema 与作者权威性。
func publisherPreCheck(profile map[string]interface{}) []CheckResult {
	var out []CheckResult
	if !flagBool(profile, "article_schema") {
		out = append(out, CheckResult{
			Name: "article_schema", Status: "fail", Severity: "high",
			Detail: "未检测到 Article Schema（JSON-LD），应包含 author/datePublished/dateModified 字段。",
		})
	}
	if !flagBool(profile, "author_profiles") {
		out = append(out, CheckResult{
			Name: "eeat_author", Status: "warn", Severity: "medium",
			Detail: "未检测到作者权威性页面，建议为每位作者建立含履历的专属页以强化 E-E-A-T 信号。",
		})
	}
	return out
}

// agencyPreCheck 检查代理/咨询品牌的案例研究与客户证言。
func agencyPreCheck(profile map[string]interface{}) []CheckResult {
	var out []CheckResult
	if !flagBool(profile, "case_studies") {
		out = append(out, CheckResult{
			Name: "case_studies", Status: "warn", Severity: "high",
			Detail: "未检测到案例研究页面，建议发布含客户名称、服务内容、可量化成果的案例。",
		})
	}
	if !flagBool(profile, "client_testimonials") {
		out = append(out, CheckResult{
			Name: "client_testimonials", Status: "warn", Severity: "medium",
			Detail: "未检测到客户证言，建议补充可署名的客户评价以增强权威信号。",
		})
	}
	return out
}

// ---------- 通用辅助函数 ----------

// prio 低分返回 high 优先级，否则返回 normal。
func prio(low bool, high, normal string) string {
	if low {
		return high
	}
	return normal
}

// flagStr 从 map 中读取字符串字段并 TrimSpace。
func flagStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		return strings.TrimSpace(toStr(v))
	}
	return ""
}

// flagBool 从 map 中读取布尔标志，兼容 bool / string / 数值类型。
func flagBool(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		s := strings.ToLower(strings.TrimSpace(b))
		return s == "true" || s == "yes" || s == "1" || s == "on"
	case int:
		return b != 0
	case int64:
		return b != 0
	case float64:
		return b != 0
	}
	return false
}

// joinAnySlice 将可能是 []string / []interface{} 的值用 sep 拼接为字符串。
func joinAnySlice(v interface{}, sep string) string {
	if v == nil {
		return ""
	}
	switch vals := v.(type) {
	case []string:
		return strings.Join(vals, sep)
	case []interface{}:
		parts := make([]string, 0, len(vals))
		for _, item := range vals {
			if s := strings.TrimSpace(toStr(item)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, sep)
	default:
		return toStr(v)
	}
}

// toStr 将任意值转换为字符串，nil 返回空串。
func toStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
