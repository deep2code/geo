// Package topsource 提供 Top Source 归因分析：识别 LLM 在回答中引用的第三方
// 权威域名（如 G2、Capterra、行业博客等），帮品牌判断应在哪些站点投入
// 反向链接与 PR 资源。
//
// 与 kol 包（KOL/媒体引用情报）互补：kol 关注"谁被引用最多"，topsource 关注
// "品牌在哪些权威源上缺失曝光"，并给出可执行的入驻/外链建议。
// 仅依赖标准库，零外部依赖。
package topsource

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/sourcedomain"
	"my-geo/internal/models"
)

// SourceStat 单个引用源（按域名聚合）的统计信息。
type SourceStat struct {
	Domain       string  `json:"domain"`
	MentionCount int     `json:"mention_count"` // 引用该域名的 prompt 数（去重）
	CoverageRate float64 `json:"coverage_rate"` // 覆盖率：引用该域名的 prompt 占总 prompt 的百分比
	BrandPresent bool    `json:"brand_present"` // 品牌是否在该域名上出现（被提及或该域名即品牌官网）
	Category     string  `json:"category"`      // review_site / blog / docs / social / news / video / other
}

// AttributionReport Top Source 归因报告。
type AttributionReport struct {
	BrandName       string       `json:"brand_name"`
	TotalPrompts    int          `json:"total_prompts"`
	TotalCitations  int          `json:"total_citations"`
	TopSources      []SourceStat `json:"top_sources"`     // 所有被引用域名，按覆盖率降序
	MissingSources  []SourceStat `json:"missing_sources"` // 引用了竞品但品牌未曝光的域名
	Recommendations []string     `json:"recommendations"` // 针对 missing sources 的可执行建议
	AnalyzedAt      time.Time    `json:"analyzed_at"`
}

// Analyze 分析品牌审计结果，生成 Top Source 归因报告。
//
// 流程：
//  1. 遍历所有 results 的 citations，提取每个引用的域名
//  2. 按域名聚合：引用条目数、覆盖的 prompt 数、引用该域名的引擎集合
//  3. 计算覆盖率 = 引用该域名的 prompt 数 / 总 prompt 数 × 100
//  4. 用 CategorizeDomain 归类每个域名
//  5. 判定 BrandPresent：该域名下的引用是否提及品牌名（或该域名即品牌官网）
//  6. MissingSources = BrandPresent=false 的第三方域名（LLM 引用了但品牌未曝光）
//  7. 针对 missing sources 按类别生成可执行建议
func Analyze(brandName string, results []brand.PromptResult, brandDomain string) *AttributionReport {
	report := &AttributionReport{
		BrandName:    brandName,
		TotalPrompts: len(results),
		AnalyzedAt:   time.Now(),
	}

	brandDomain = strings.ToLower(strings.TrimSpace(brandDomain))
	brandNameLower := strings.ToLower(strings.TrimSpace(brandName))

	// 域名聚合结构（内部使用，不导出）
	type domainAgg struct {
		domain        string
		citationCount int                        // 原始引用条目数（不去重）
		results       map[string]bool            // 引用该域名的 (prompt|engine) 去重集合 → 用于覆盖率
		engines       map[models.EngineType]bool // 引用该域名的引擎集合
		brandHit      bool                       // 该域名下的引用是否在标题/摘要中提及品牌名
	}
	agg := make(map[string]*domainAgg)
	totalCitations := 0

	for _, pr := range results {
		resultKey := pr.Prompt + "|" + string(pr.Engine)
		for _, c := range pr.Citations {
			domain := sourcedomain.ExtractDomain(c.URL)
			if domain == "" {
				continue
			}
			totalCitations++
			a, ok := agg[domain]
			if !ok {
				a = &domainAgg{
					domain:  domain,
					results: make(map[string]bool),
					engines: make(map[models.EngineType]bool),
				}
				agg[domain] = a
			}
			a.citationCount++
			a.results[resultKey] = true
			a.engines[pr.Engine] = true
			// 品牌是否在该引用的标题/摘要中出现
			if !a.brandHit && brandNameLower != "" {
				if strings.Contains(strings.ToLower(c.Title), brandNameLower) ||
					strings.Contains(strings.ToLower(c.Snippet), brandNameLower) {
					a.brandHit = true
				}
			}
		}
		// P1-a：优先采用 LLM 识别的"采信源"实体（比正则 URL 更语义化）。
		// 当某结果带了 ExtractedSources 时，把其 URL/Name 一并纳入域名聚合，
		// 并把 name 命中品牌名者标记为 brandHit（回答明确采信了品牌自身）。
		for _, s := range pr.ExtractedSources {
			domain := sourcedomain.ExtractDomain(s.URL)
			if domain == "" {
				domain = sourcedomain.ExtractDomain(s.Name)
			}
			if domain == "" {
				continue
			}
			totalCitations++
			a, ok := agg[domain]
			if !ok {
				a = &domainAgg{
					domain:  domain,
					results: make(map[string]bool),
					engines: make(map[models.EngineType]bool),
				}
				agg[domain] = a
			}
			a.citationCount++
			a.results[resultKey] = true
			a.engines[pr.Engine] = true
			if !a.brandHit && brandNameLower != "" {
				if strings.Contains(strings.ToLower(s.Name), brandNameLower) {
					a.brandHit = true
				}
			}
		}
	}
	report.TotalCitations = totalCitations

	// 避免除零：无 prompt 时覆盖率统一为 0
	totalPrompts := len(results)
	if totalPrompts == 0 {
		totalPrompts = 1
	}

	// 构建 SourceStat 列表
	stats := make([]SourceStat, 0, len(agg))
	for domain, a := range agg {
		mentionCount := len(a.results)
		coverage := float64(mentionCount) / float64(totalPrompts) * 100
		// 保留两位小数（避免浮点尾数）
		coverage = float64(int(coverage*100)) / 100
		brandPresent := a.brandHit || domain == brandDomain
		stats = append(stats, SourceStat{
			Domain:       domain,
			MentionCount: mentionCount,
			CoverageRate: coverage,
			BrandPresent: brandPresent,
			Category:     sourcedomain.CategorizeDomain(domain),
		})
	}

	// 排序：覆盖率降序 → 引擎数降序 → 域名升序（多级比较键构成全序，结果确定）
	slices.SortFunc(stats, func(a, b SourceStat) int {
		if c := cmp.Compare(b.CoverageRate, a.CoverageRate); c != 0 {
			return c
		}
		ei := len(agg[a.Domain].engines)
		ej := len(agg[b.Domain].engines)
		if c := cmp.Compare(ej, ei); c != 0 {
			return c
		}
		return cmp.Compare(a.Domain, b.Domain)
	})

	report.TopSources = stats

	// MissingSources：BrandPresent=false 的第三方域名（排除品牌官网本身）
	missing := make([]SourceStat, 0)
	for _, s := range stats {
		if !s.BrandPresent && s.Domain != brandDomain {
			missing = append(missing, s)
		}
	}
	report.MissingSources = missing

	// 生成建议（针对 missing sources，已按覆盖率降序保证优先级）
	report.Recommendations = generateRecommendations(missing)

	return report
}

// generateRecommendations 针对缺失源按类别生成可执行建议。
//
// 模板：
//   - review_site: 在 {domain} 上建立产品页面，该域名被 {coverage}% 的查询引用
//   - docs:        考虑在 {domain} 上创建技术文档或开源项目
//   - social:      在 {domain} 上加强品牌讨论与用户互动
//   - news/blog/video/other: 对应的曝光建议
func generateRecommendations(missing []SourceStat) []string {
	if len(missing) == 0 {
		return nil
	}
	recs := make([]string, 0, len(missing))
	for _, s := range missing {
		coverage := fmt.Sprintf("%.0f", s.CoverageRate)
		var rec string
		switch s.Category {
		case "review_site":
			rec = fmt.Sprintf("在 %s 上建立产品页面，该域名被 %s%% 的查询引用", s.Domain, coverage)
		case "docs":
			rec = fmt.Sprintf("考虑在 %s 上创建技术文档或开源项目", s.Domain)
		case "social":
			rec = fmt.Sprintf("在 %s 上加强品牌讨论与用户互动", s.Domain)
		case "news":
			rec = fmt.Sprintf("争取在 %s 上的媒体报道或新闻稿投放，该域名被 %s%% 的查询引用", s.Domain, coverage)
		case "blog":
			rec = fmt.Sprintf("考虑在 %s 上发布技术文章或品牌内容", s.Domain)
		case "video":
			rec = fmt.Sprintf("在 %s 上创建产品演示或教程视频", s.Domain)
		default:
			rec = fmt.Sprintf("评估在 %s 上的品牌曝光机会（该域名被 %s%% 的查询引用）", s.Domain, coverage)
		}
		recs = append(recs, rec)
	}
	return recs
}
