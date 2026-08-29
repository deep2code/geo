// Package discover 实现关键词→公司推断→品牌画像生成→自动 GEO 报告的一站式流程。
//
// 用户只需输入一个关键词（如 "短视频" / "在线教育" / "云计算"），
// 系统自动：
//  1. 从离线工商库 + SinoFacts 知识库中搜索匹配的公司
//  2. 多个结果时让用户选择
//  3. 基于公司信息自动生成品牌画像（名称、域名、行业、查询词等）
//  4. 执行品牌可见度审计 + AI 就绪度检查
//  5. 输出完整的 GEO 报告
package discover

import (
	"context"
	"fmt"
	"strings"

	"my-geo/internal/brand"
	"my-geo/internal/brand/knowledge"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/readiness"
	"my-geo/internal/brand/vertical"
)

// Candidate 关键词搜索匹配到的公司候选。
type Candidate struct {
	// 来源：offline_db / sinofacts
	Source string `json:"source"`
	// 公司名称
	Name string `json:"name"`
	// 统一社会信用代码（离线工商库才有）
	CreditCode string `json:"credit_code,omitempty"`
	// 法定代表人
	LegalRepresentative string `json:"legal_representative,omitempty"`
	// 注册资本
	Capital string `json:"capital,omitempty"`
	// 成立日期（离线工商库 RegistrationDay）
	EstablishedDate string `json:"established_date,omitempty"`
	// 经营范围
	BusinessScope string `json:"business_scope,omitempty"`
	// 省份
	Province string `json:"province,omitempty"`
	// 城市
	City string `json:"city,omitempty"`
	// 官网域名（知识库才有）
	Domain string `json:"domain,omitempty"`
	// 所属行业
	Industry string `json:"industry,omitempty"`
	// 品类
	Category string `json:"category,omitempty"`
	// 产品列表
	Products []string `json:"products,omitempty"`
	// 别名
	Aliases []string `json:"aliases,omitempty"`
	// 匹配分数 0-100
	Score float64 `json:"score,omitempty"`
	// 搜索摘要
	Summary string `json:"summary,omitempty"`
}

// DiscoverResult 关键词发现结果。
type DiscoverResult struct {
	Keyword    string      `json:"keyword"`
	Candidates []Candidate `json:"candidates"`
}

// GEOReport 完整 GEO 报告（品牌审计 + 就绪度 + 行业诊断）。
type GEOReport struct {
	// 输入关键词
	Keyword string `json:"keyword"`
	// 选中的公司名
	CompanyName string `json:"company_name"`
	// 自动生成的品牌画像
	Profile brand.BrandProfile `json:"profile"`
	// 行业类型
	Vertical vertical.Vertical `json:"vertical"`
	// 品牌可见度审计报告（可能为 nil，如未配置引擎 Key）
	BrandReport *brand.VisibilityReport `json:"brand_report,omitempty"`
	// AI 就绪度审计结果（可能为 nil，如无官网域名）
	ReadinessResult *readiness.AuditResult `json:"readiness_result,omitempty"`
	// 综合建议
	Suggestions []string `json:"suggestions"`
}

// Discover 从离线工商库 + SinoFacts 知识库中搜索匹配关键词的公司。
//
// 参数：
//   - ctx: 上下文
//   - keyword: 搜索关键词（如 "短视频" / "在线教育" / "腾讯"）
//   - offlineDB: 离线工商库（可为 nil，跳过工商库搜索）
//   - kb: SinoFacts 知识库（可为 nil，跳过知识库搜索）
//
// 返回去重后的候选列表，按匹配分数降序排列。
func Discover(ctx context.Context, keyword string, offlineDB offlinedb.DB, kb *knowledge.Knowledge) (*DiscoverResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}

	result := &DiscoverResult{Keyword: keyword}
	seen := map[string]bool{} // 按公司名去重

	// 1. 离线工商库搜索（Meilisearch 中文全文检索，匹配公司名/经营范围/法人等）
	if offlineDB != nil {
		companies, err := offlineDB.Search(ctx, offlinedb.SearchOptions{
			Query: keyword,
			TopN:  10,
		})
		if err == nil {
			for _, c := range companies {
				name := strings.TrimSpace(c.Name)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				result.Candidates = append(result.Candidates, Candidate{
					Source:              "offline_db",
					Name:                name,
					CreditCode:          c.Code,
					LegalRepresentative: c.LegalRepresentative,
					Capital:             c.Capital,
					EstablishedDate:     c.RegistrationDay,
					BusinessScope:       c.BusinessScope,
					Province:            c.Province,
					City:                c.City,
					Score:               c.Score,
				})
			}
		}
	}

	// 2. SinoFacts 知识库搜索（品牌名/域名/行业匹配）
	if kb != nil {
		results := kb.Search(keyword, 5)
		for _, r := range results {
			name := strings.TrimSpace(r.Entry.BrandName)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			cand := Candidate{
				Source:   "sinofacts",
				Name:     name,
				Domain:   r.Entry.BrandDomain,
				Industry: r.Entry.Industry,
				Category: r.Entry.Category,
				Products: append([]string{}, r.Entry.Products...),
				Aliases:  append([]string{}, r.Entry.BrandAliases...),
				Score:    r.Score,
				Summary:  r.Entry.DescriptionZh,
			}
			result.Candidates = append(result.Candidates, cand)
		}
	}

	return result, nil
}

// BuildProfile 根据选中的候选公司 + 关键词自动生成品牌画像。
//
// 自动生成：
//   - 品牌名 = 公司简称（从全称中提取）
//   - 域名 = 候选域名（如有）
//   - 行业 = 从经营范围/知识库推断
//   - 查询词 = 基于关键词 + 行业生成 5-8 个高意图 prompt
//   - 竞品 = 留空（由审计过程中自动发现）
func BuildProfile(cand *Candidate, keyword string) brand.BrandProfile {
	profile := brand.BrandProfile{
		Name:     cand.Name,
		Aliases:  cand.Aliases,
		Domain:   cand.Domain,
		Industry: cand.Industry,
		Category: cand.Category,
		Products: cand.Products,
		Prompts:  GeneratePrompts(keyword, cand.Industry, cand.Category),
	}

	// 如果有工商数据，填充公司信息
	if cand.CreditCode != "" || cand.LegalRepresentative != "" {
		profile.Company = &brand.Company{
			Name:                cand.Name,
			CreditCode:          cand.CreditCode,
			LegalRepresentative: cand.LegalRepresentative,
			RegisteredCapital:   cand.Capital,
			BusinessScope:       cand.BusinessScope,
			Province:            cand.Province,
			EstablishedDate:     cand.EstablishedDate,
		}
	}

	// 从公司名提取简称作为别名
	short := extractShortName(cand.Name)
	if short != "" && short != cand.Name {
		profile.Aliases = append(profile.Aliases, short)
	}

	return profile
}

// GeneratePrompts 基于关键词 + 行业自动生成高意图查询词。
//
// 生成的 prompt 模拟用户在 AI 搜索引擎中的真实提问方式，
// 覆盖推荐/对比/评测/怎么样/排名 等常见意图。
func GeneratePrompts(keyword, industry, category string) []string {
	var prompts []string

	// 通用推荐类
	prompts = append(prompts,
		fmt.Sprintf("最好的%s", keyword),
		fmt.Sprintf("%s推荐", keyword),
		fmt.Sprintf("%s排行榜", keyword),
		fmt.Sprintf("%s哪个好", keyword),
	)

	// 对比评测类
	prompts = append(prompts,
		fmt.Sprintf("%s对比", keyword),
		fmt.Sprintf("%s怎么样", keyword),
	)

	// 行业特定类
	indLower := strings.ToLower(industry + " " + category)
	switch {
	case strings.Contains(indLower, "saas") || strings.Contains(indLower, "软件") || strings.Contains(indLower, "云"):
		prompts = append(prompts,
			fmt.Sprintf("%s免费替代方案", keyword),
			fmt.Sprintf("%s开源方案", keyword),
		)
	case strings.Contains(indLower, "电商") || strings.Contains(indLower, "零售"):
		prompts = append(prompts,
			fmt.Sprintf("%s平台哪个靠谱", keyword),
		)
	case strings.Contains(indLower, "教育") || strings.Contains(indLower, "培训"):
		prompts = append(prompts,
			fmt.Sprintf("%s平台口碑", keyword),
		)
	default:
		prompts = append(prompts,
			fmt.Sprintf("%s品牌有哪些", keyword),
		)
	}

	return prompts
}

// GenerateReport 基于选中的候选公司生成完整 GEO 报告。
//
// 流程：
//  1. BuildProfile 生成品牌画像
//  2. vertical.Detect 识别行业类型
//  3. brand.Engine.Audit 执行品牌可见度审计（如有引擎配置）
//  4. readiness.Audit 执行 AI 就绪度检查（如有域名）
//  5. 汇总建议
func GenerateReport(ctx context.Context, cand *Candidate, keyword string, brandEngine *brand.Engine) (*GEOReport, error) {
	profile := BuildProfile(cand, keyword)

	report := &GEOReport{
		Keyword:     keyword,
		CompanyName: cand.Name,
		Profile:     profile,
	}

	// 行业类型检测
	profileMap := map[string]interface{}{
		"name":     profile.Name,
		"industry": profile.Industry,
		"category": profile.Category,
		"products": profile.Products,
	}
	report.Vertical = vertical.Detect(profileMap)

	// 品牌可见度审计
	if brandEngine != nil {
		if br, err := brandEngine.Audit(ctx, profile); err == nil {
			report.BrandReport = br
		}
	}

	// AI 就绪度检查（需要域名）
	if profile.Domain != "" {
		url := "https://" + profile.Domain
		if result, err := readiness.Audit(ctx, url); err == nil {
			report.ReadinessResult = result
		}
	}

	// 生成综合建议
	report.Suggestions = generateSuggestions(report)

	return report, nil
}

// generateSuggestions 基于报告结果生成综合优化建议。
func generateSuggestions(report *GEOReport) []string {
	var suggestions []string

	// 品牌审计建议
	if report.BrandReport != nil {
		if report.BrandReport.Score < 60 {
			suggestions = append(suggestions, "品牌可见度评分较低，建议优先优化内容质量和结构化数据")
		}
		if report.BrandReport.Score < 80 {
			suggestions = append(suggestions, "品牌在 AI 引擎中的引用率有提升空间，建议补充权威引用和统计数据")
		}
	}

	// 就绪度建议
	if report.ReadinessResult != nil {
		for _, check := range report.ReadinessResult.Checks {
			if check.Status == "fail" && check.Severity == "critical" {
				suggestions = append(suggestions, fmt.Sprintf("【紧急】%s：%s", check.Name, check.Detail))
			}
		}
		if report.ReadinessResult.TotalScore < 60 {
			suggestions = append(suggestions, "网站 AI 就绪度不足，建议优先修复 robots.txt 和 llms.txt")
		}
	}

	// 行业建议
	switch report.Vertical {
	case vertical.VerticalSaaS:
		suggestions = append(suggestions, "SaaS 行业：建议完善 API 文档、定价页的 Schema 结构化数据")
	case vertical.VerticalLocalService:
		suggestions = append(suggestions, "本地服务：建议确保 NAP（名称/地址/电话）一致性，完善 Google 商家资料")
	case vertical.VerticalEcommerce:
		suggestions = append(suggestions, "电商行业：建议添加 Product Schema 和用户评价结构化数据")
	case vertical.VerticalPublisher:
		suggestions = append(suggestions, "媒体出版：建议加强 E-E-A-T 信号和 Article 结构化数据")
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "品牌 GEO 状态良好，建议持续监测并定期审计")
	}

	return suggestions
}

// extractShortName 从公司全称中提取简称。
//
// 例如："腾讯科技（深圳）有限公司" → "腾讯"
func extractShortName(fullName string) string {
	// 去除常见公司后缀。注意更长的后缀必须排在前面：
	// "股份有限公司" 以 "有限公司" 结尾，若先裁短后缀会把长后缀裁成 "股份" 残留
	suffixes := []string{
		"股份有限公司", "有限责任公司", "集团有限公司",
		"科技有限公司", "技术有限公司",
		"有限公司",
	}
	name := fullName
	for _, s := range suffixes {
		name = strings.TrimSuffix(name, s)
	}
	// 去除括号及内容
	if idx := strings.IndexAny(name, "（("); idx > 0 {
		name = strings.TrimSpace(name[:idx])
	}
	return strings.TrimSpace(name)
}
