package brand

import "sort"

// BuildSeverityIssues 基于 BVS 7 维 + E-E-A-T 4 维得分生成健康问题清单。
//
// 每个维度根据得分映射为严重级别（SeverityOf），并附上影响描述与修复建议。
// 返回结果按严重级别排序：Critical → High → Medium → Low，同级按得分升序。
// 仅返回 Medium 及以上问题（Low 级别视为健康，不列入清单避免噪声）。
func BuildSeverityIssues(bd ScoreBreakdown) []HealthIssue {
	dims := []struct {
		name     string
		score    float64
		impact   string
		fix      string
	}{
		{"内容质量", bd.ContentQuality,
			"内容被 AI 引擎引用的频率与质量不足，直接影响品牌在 AI 回答中的曝光。",
			"创作引用来源丰富、统计数据扎实的高质量内容，提升被 AI 引用的概率。"},
		{"技术SEO", bd.TechnicalSEO,
			"品牌实体结构化基础薄弱，AI 难以建立品牌-公司-官网的实体关联。",
			"部署 Organization/Brand JSON-LD，补全公司实体信息，降低幽灵引用率。"},
		{"站内SEO", bd.OnPageSEO,
			"品牌提及率偏低，站内优化在 AI 回答中的曝光效果有限。",
			"优化官网标题、meta 描述与 H1，确保品牌名与核心品类强关联。"},
		{"Schema", bd.Schema,
			"结构化数据缺失或属性不足，AI 爬虫难以准确提取品牌实体信息。",
			"为官网添加 JSON-LD 结构化数据（Organization/WebSite/FAQPage），属性补至 5+。"},
		{"页面性能", bd.Performance,
			"引用位置靠后，间接反映页面加载与用户体验有待提升。",
			"优化页面加载速度，压缩图片，预渲染关键页面，提升 Core Web Vitals。"},
		{"AI就绪", bd.AIReadiness,
			"AI 搜索就绪度不足，情感正面率偏低或幽灵引用较多。",
			"管理品牌情感倾向，增加正面背书，降低幽灵引用率。"},
		{"图像优化", bd.ImageOptimization,
			"图像优化缺失，影响富摘要与 AI 可读性。",
			"为图片添加 alt 文本、结构化标记，压缩图片体积。"},
		// E-E-A-T 四维
		{"Experience", bd.Experience,
			"品牌运营经验信号不足，影响 AI 对品牌资历的判定。",
			"在官网与结构化数据中明确公司成立年份、发展历程与里程碑。"},
		{"Expertise", bd.Expertise,
			"专业性信号不足，行业匹配度与实体完备度有待提升。",
			"补全行业字段、产品线信息，发布专业技术内容强化专业形象。"},
		{"Authoritativeness", bd.Authoritativeness,
			"权威性不足，引用位置与声量份额落后于竞品。",
			"争取进入权威榜单、媒体报道，提升引用位置与声量份额。"},
		{"Trustworthiness", bd.Trustworthiness,
			"可信度信号不足，工商核验状态或幽灵引用率影响 AI 信任。",
			"完成工商核验、降低幽灵引用率、补全实体信息以增强可信度。"},
	}

	var issues []HealthIssue
	for _, d := range dims {
		sev := SeverityOf(d.score)
		// Low 级别视为健康，不列入清单
		if sev == SeverityLow {
			continue
		}
		issues = append(issues, HealthIssue{
			Dimension:    d.name,
			Score:        d.score,
			Severity:     sev,
			Impact:       d.impact,
			SuggestedFix: d.fix,
		})
	}

	// 按严重级别排序：Critical → High → Medium → Low，同级按得分升序
	sort.SliceStable(issues, func(i, j int) bool {
		si, sj := severityOrder(issues[i].Severity), severityOrder(issues[j].Severity)
		if si != sj {
			return si < sj
		}
		return issues[i].Score < issues[j].Score
	})
	return issues
}

// HasCriticalIssue 判断是否存在 Critical 级别问题（应阻断索引/发布）。
func HasCriticalIssue(issues []HealthIssue) bool {
	for _, iss := range issues {
		if iss.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// CriticalIssueCount 返回 Critical 级别问题数量。
func CriticalIssueCount(issues []HealthIssue) int {
	n := 0
	for _, iss := range issues {
		if iss.Severity == SeverityCritical {
			n++
		}
	}
	return n
}

// severityOrder 将严重级别映射为排序序号（数值越小越紧急）。
func severityOrder(s Severity) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 3
	default:
		return 4
	}
}
