package brand

import (
	"strings"
)

// 负面分类常量。
const (
	NegativeCategoryProductIssue            = "product_issue"             // 产品缺陷/bug/质量问题
	NegativeCategoryServiceIssue            = "service_issue"             // 客服/售后/服务态度
	NegativeCategoryPricingIssue            = "pricing_issue"             // 价格/性价比/收费
	NegativeCategoryCompetitiveDisadvantage = "competitive_disadvantage" // 竞争劣势/不如竞品
	NegativeCategoryFalseInfo               = "false_info"               // 虚假/欺骗/诈骗
	NegativeCategorySecurityPrivacy         = "security_privacy"         // 隐私/数据安全
	NegativeCategoryOther                   = "other"                    // 其他负面
)

// negativeRules 负面分类规则（关键词 → 分类 + 严重级别）。
// 顺序敏感：先匹配的规则生效，因此将严重级别高的规则放前面。
var negativeRules = []struct {
	keywords []string
	category string
	severity Severity
}{
	// 虚假/欺骗类（critical）
	{
		keywords: []string{"虚假", "骗", "骗局", "套路", "坑人", "诈骗", "欺诈", "假的", "不可信", "造假",
			"scam", "fraud", "fake", "deceptive", "misleading", "cheat"},
		category: NegativeCategoryFalseInfo,
		severity: SeverityCritical,
	},
	// 安全/隐私类（critical）
	{
		keywords: []string{"隐私", "泄露", "数据泄露", "安全漏洞", "黑客", "后门", "窃取", "监控", "追踪",
			"privacy", "leak", "breach", "vulnerability", "hack", "backdoor", "spy"},
		category: NegativeCategorySecurityPrivacy,
		severity: SeverityCritical,
	},
	// 产品问题类（high）
	{
		keywords: []string{"bug", "崩溃", "卡顿", "不好用", "质量问题", "缺陷", "故障", "无法使用", "不兼容",
			"难用", "垃圾", "差劲", "buggy", "crash", "broken", "defect", "glitch", "unusable"},
		category: NegativeCategoryProductIssue,
		severity: SeverityHigh,
	},
	// 服务问题类（high）
	{
		keywords: []string{"客服", "售后", "服务态度", "不回应", "推诿", "投诉无门", "没人理", "联系不上",
			"customer service", "support", "unresponsive", "rude", "ignored"},
		category: NegativeCategoryServiceIssue,
		severity: SeverityHigh,
	},
	// 价格问题类（medium）
	{
		keywords: []string{"贵", "价格高", "性价比低", "收费", "乱收费", "隐藏费用", "涨价", "不值",
			"expensive", "overpriced", "pricey", "hidden fee", "costly", "rip-off"},
		category: NegativeCategoryPricingIssue,
		severity: SeverityMedium,
	},
	// 竞争劣势类（medium）
	{
		keywords: []string{"不如", "比不上", "竞品更好", "替代", "更差", "落后", "跟不上", "过时",
			"better alternative", "inferior", "outdated", "lag behind", "worse than"},
		category: NegativeCategoryCompetitiveDisadvantage,
		severity: SeverityMedium,
	},
}

// ClassifyNegative 对负面文本进行分类，返回分类与严重级别。
//
// 基于关键词匹配，按规则顺序判定（先匹配的生效，critical 优先）。
// 无匹配时返回 other / medium。
func ClassifyNegative(text string) (string, Severity) {
	if strings.TrimSpace(text) == "" {
		return NegativeCategoryOther, SeverityMedium
	}
	lower := strings.ToLower(text)
	for _, rule := range negativeRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return rule.category, rule.severity
			}
		}
	}
	return NegativeCategoryOther, SeverityMedium
}

// NegativeCategoryLabel 返回负面分类的中文标签。
func NegativeCategoryLabel(category string) string {
	switch category {
	case NegativeCategoryFalseInfo:
		return "虚假信息"
	case NegativeCategorySecurityPrivacy:
		return "安全隐私"
	case NegativeCategoryProductIssue:
		return "产品问题"
	case NegativeCategoryServiceIssue:
		return "服务问题"
	case NegativeCategoryPricingIssue:
		return "价格问题"
	case NegativeCategoryCompetitiveDisadvantage:
		return "竞争劣势"
	default:
		return "其他负面"
	}
}

// NegativeSummary 负面查询监控摘要。
type NegativeSummary struct {
	TotalCount      int            `json:"total_count"`
	CriticalCount   int            `json:"critical_count"`
	ByCategory      map[string]int `json:"by_category"`
	TopCategories   []string       `json:"top_categories"` // 按数量降序的 Top3 分类
	RequiresAlert   bool           `json:"requires_alert"` // 是否需要告警（critical > 0 或总数 ≥ 5）
	AlertMessage    string         `json:"alert_message,omitempty"`
}

// SummarizeNegatives 聚合负面提及，生成监控摘要与告警。
func SummarizeNegatives(negs []NegativeMention) NegativeSummary {
	s := NegativeSummary{ByCategory: map[string]int{}}
	s.TotalCount = len(negs)
	for _, n := range negs {
		cat := n.Category
		if cat == "" {
			cat = NegativeCategoryOther
		}
		s.ByCategory[cat]++
		if n.Severity == SeverityCritical {
			s.CriticalCount++
		}
	}
	// Top3 分类
	type catCount struct {
		cat   string
		count int
	}
	var ccs []catCount
	for c, n := range s.ByCategory {
		ccs = append(ccs, catCount{c, n})
	}
	// 简单排序（冒泡，数量小）
	for i := 0; i < len(ccs); i++ {
		for j := i + 1; j < len(ccs); j++ {
			if ccs[j].count > ccs[i].count {
				ccs[i], ccs[j] = ccs[j], ccs[i]
			}
		}
	}
	top := 3
	if len(ccs) < top {
		top = len(ccs)
	}
	for i := 0; i < top; i++ {
		s.TopCategories = append(s.TopCategories, ccs[i].cat)
	}
	// 告警判定
	if s.CriticalCount > 0 {
		s.RequiresAlert = true
		s.AlertMessage = "检测到 critical 级别负面提及（虚假信息/安全隐私），需立即处理"
	} else if s.TotalCount >= 5 {
		s.RequiresAlert = true
		s.AlertMessage = "负面提及数量较多（≥5 条），建议排查信息源并优化品牌内容"
	}
	return s
}
