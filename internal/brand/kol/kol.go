// Package kol 提供基于品牌审计结果的 KOL/媒体/信息源情报分析。
//
// 从品牌审计返回的 PromptResult 列表中挖掘"在 AI 搜索结果中被引用最多的
// KOL/媒体/信息源"，帮品牌决定内容投放和合作对象。
//
// 与 social 包（社媒情感监控）互补：social 衡量"被人讨论"，kol 衡量"被 AI 引用"。
// 仅依赖 net/url 等标准库，零外部依赖。
package kol

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"my-geo/internal/brand"
)

// SourceStat 单个引用源（按域名聚合）的统计信息。
type SourceStat struct {
	Domain        string   `json:"domain"`
	Title         string   `json:"title"`          // 最常见的标题
	MentionCount  int      `json:"mention_count"`  // 被引用次数（去重后的结果数）
	CitationCount int      `json:"citation_count"` // 被作为引用源的次数（原始引用条目数）
	Engines       []string `json:"engines"`        // 哪些引擎引用了它
	SOV           float64  `json:"sov"`            // 声量份额（引用数/总引用数 * 100）
	URL           string   `json:"url,omitempty"`  // 最常见的 URL
}

// KOLReport KOL/创作者情报报告。
type KOLReport struct {
	BrandName       string       `json:"brand_name"`
	TotalCitations  int          `json:"total_citations"`
	TotalSources    int          `json:"total_sources"`
	TopSources      []SourceStat `json:"top_sources"`      // 按引用次数排序（Top 20）
	ByDomain        []SourceStat `json:"by_domain"`        // 按域名聚合的完整列表
	Recommendations []string     `json:"recommendations"`  // 推荐操作（每条对应一个 Top 源）
}

// topN TopSources 返回的数量上限。
const topN = 20

// Analyze 从品牌审计的 PromptResult 列表中提取 KOL/媒体引用情报。
//
// 流程：
//  1. 遍历所有 results，收集所有 citations
//  2. 对每个 citation 提取域名（从 URL 中解析）
//  3. 按域名聚合统计：引用次数、涉及的引擎列表、声量份额
//  4. 排序取 Top 20
//  5. 生成推荐（多引擎覆盖/单引擎覆盖/竞品引用源）
func Analyze(brandName string, results []brand.PromptResult) *KOLReport {
	return AnalyzeWithCompetitors(brandName, results, nil)
}

// AnalyzeWithCompetitors 在 Analyze 基础上接受竞品列表，用于识别竞品引用源。
//
// competitors 为 nil 或空时跳过竞品域名检测，仅按引擎覆盖维度生成推荐。
// 竞品匹配基于域名（大小写不敏感），竞品 Domain 字段为空时跳过该项。
func AnalyzeWithCompetitors(brandName string, results []brand.PromptResult, competitors []brand.Competitor) *KOLReport {
	report := &KOLReport{BrandName: brandName}

	// 域名聚合结构（内部使用，不导出）
	type domainAgg struct {
		domain        string
		titleCount    map[string]int  // 标题 → 出现次数（用于取最常见的标题）
		engines       map[string]bool // 引用该域名的引擎集合（去重）
		results       map[string]bool // 引用该域名的 (prompt|engine) 去重集合
		citationCount int             // 总引用条目数（不去重）
		urlCount      map[string]int  // URL → 出现次数（用于取最常见的 URL）
	}

	agg := make(map[string]*domainAgg)
	totalCitations := 0

	for _, pr := range results {
		engine := string(pr.Engine)
		resultKey := pr.Prompt + "|" + engine
		for _, c := range pr.Citations {
			domain := extractDomain(c.URL)
			if domain == "" {
				continue
			}
			totalCitations++
			a, ok := agg[domain]
			if !ok {
				a = &domainAgg{
					domain:     domain,
					titleCount: make(map[string]int),
					engines:    make(map[string]bool),
					results:    make(map[string]bool),
					urlCount:   make(map[string]int),
				}
				agg[domain] = a
			}
			if c.Title != "" {
				a.titleCount[c.Title]++
			}
			if c.URL != "" {
				a.urlCount[c.URL]++
			}
			a.engines[engine] = true
			a.results[resultKey] = true
			a.citationCount++
		}
	}

	report.TotalCitations = totalCitations
	report.TotalSources = len(agg)

	// 构建 SourceStat 列表
	stats := make([]SourceStat, 0, len(agg))
	for domain, a := range agg {
		stat := SourceStat{
			Domain:        domain,
			CitationCount: a.citationCount,
			MentionCount:  len(a.results),
			Engines:       sortedKeys(a.engines),
		}
		if totalCitations > 0 {
			stat.SOV = float64(a.citationCount) / float64(totalCitations) * 100
			// 保留两位小数（避免浮点尾数）
			stat.SOV = float64(int(stat.SOV*100)) / 100
		}
		stat.Title = mostCommon(a.titleCount)
		stat.URL = mostCommon(a.urlCount)
		stats = append(stats, stat)
	}

	// 按 CitationCount 降序排序（相同时按 Domain 升序保证稳定）
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].CitationCount != stats[j].CitationCount {
			return stats[i].CitationCount > stats[j].CitationCount
		}
		return stats[i].Domain < stats[j].Domain
	})

	// ByDomain：完整域名聚合列表
	report.ByDomain = stats

	// TopSources：取前 topN（复制一份避免共享底层数组）
	top := stats
	if len(top) > topN {
		top = top[:topN]
	}
	report.TopSources = append([]SourceStat{}, top...)

	// 生成推荐
	report.Recommendations = generateRecommendations(report.TopSources, competitors)

	return report
}

// generateRecommendations 针对 Top 源逐个生成推荐操作文案。
//
// 推荐规则：
//   - 竞品官网域名 → "竞品引用源，需关注"
//   - 被多个引擎引用（≥2）→ "高影响力媒体，建议合作"
//   - 仅被单个引擎引用 → "单引擎覆盖，可扩大投放"
func generateRecommendations(topSources []SourceStat, competitors []brand.Competitor) []string {
	if len(topSources) == 0 {
		return nil
	}
	// 构建竞品域名集合（domain(小写) → 竞品名）
	competitorDomains := make(map[string]string)
	for _, c := range competitors {
		d := strings.ToLower(strings.TrimSpace(c.Domain))
		if d != "" {
			competitorDomains[d] = c.Name
		}
	}

	recs := make([]string, 0, len(topSources))
	for _, s := range topSources {
		var rec string
		switch {
		case competitorDomains[s.Domain] != "":
			rec = fmt.Sprintf("【%s】竞品引用源（%s 官网），被引用 %d 次（SOV %.1f%%，%d 个引擎），需关注竞品内容策略",
				s.Domain, competitorDomains[s.Domain], s.CitationCount, s.SOV, len(s.Engines))
		case len(s.Engines) >= 2:
			rec = fmt.Sprintf("【%s】高影响力媒体，被 %d 个引擎引用 %d 次（SOV %.1f%%），建议合作",
				s.Domain, len(s.Engines), s.CitationCount, s.SOV)
		default:
			rec = fmt.Sprintf("【%s】单引擎覆盖（%s），引用 %d 次（SOV %.1f%%），可扩大投放",
				s.Domain, strings.Join(s.Engines, ","), s.CitationCount, s.SOV)
		}
		recs = append(recs, rec)
	}
	return recs
}

// extractDomain 从 URL 中提取规范化的域名。
//
// 处理逻辑：
//   - 空字符串或解析失败返回 ""
//   - 无 scheme 时补 https:// 让 url.Parse 正确解析 host
//   - 去掉端口、转为小写、去掉 "www." 前缀
func extractDomain(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	// 无 scheme 时补一个，让 url.Parse 能正确解析出 Host
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	// 去掉端口
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	// 去掉 www. 前缀
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return ""
	}
	return host
}

// mostCommon 从 string→count 映射中返回出现次数最多的键。
// 次数相同时取字典序最小的键（保证稳定）。
func mostCommon(counts map[string]int) string {
	best := ""
	bestN := 0
	for k, n := range counts {
		if n > bestN || (n == bestN && (best == "" || k < best)) {
			best = k
			bestN = n
		}
	}
	return best
}

// sortedKeys 返回 map 中所有键的升序切片（用于 Engines 去重排序）。
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
