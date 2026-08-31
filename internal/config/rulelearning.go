// Package config 自动化规则学习：从引擎交互中自动发现优化规则。
//
// 参考 AutoGEO (ICLR 2026) 的 Rule Extraction 方法：
//   - 分析引擎对不同内容的偏好模式
//   - 自动提取规则（如"含统计数据的内容被引用率更高"）
//   - 生成可配置的规则集供 scorer 使用
package config

import (
	"sort"
	"strings"
)

// Rule 自动学习到的规则。
type Rule struct {
	ID          string             `json:"id"`
	Pattern     string             `json:"pattern"`      // 规则模式（如 "statistics"）
	Description string             `json:"description"`  // 规则描述
	Confidence  float64            `json:"confidence"`   // 置信度 0-1
	Impact      float64            `json:"impact"`       // 预期影响 0-1
	Domain      string             `json:"domain,omitempty"` // 适用领域
	Engine      string             `json:"engine,omitempty"` // 适用引擎
	Evidence    []RuleEvidence     `json:"evidence"`     // 证据列表
}

// RuleEvidence 规则证据。
type RuleEvidence struct {
	ContentHash string  `json:"content_hash"` // 内容指纹
	Score       float64 `json:"score"`        // 评分
	Cited       bool    `json:"cited"`        // 是否被引用
	Position    int     `json:"position"`     // 引用位置
}

// LearnedRuleSet 学习到的规则集。
type LearnedRuleSet struct {
	Rules     []Rule  `json:"rules"`
	Confidence float64 `json:"confidence"` // 整体置信度
	Domain    string  `json:"domain,omitempty"`
	Engine    string  `json:"engine,omitempty"`
}

// ExtractRules 从历史数据中自动提取规则。
func ExtractRules(data []InteractionData) *LearnedRuleSet {
	if len(data) == 0 {
		return &LearnedRuleSet{}
	}

	rules := make(map[string]*Rule)

	// 分析每个交互数据点
	for _, d := range data {
		// 检测各种模式
		detectAndAccumulate(rules, d)
	}

	// 计算置信度和影响
	result := &LearnedRuleSet{
		Rules: make([]Rule, 0, len(rules)),
	}
	for _, r := range rules {
		if r.Confidence > 0.3 { // 过滤低置信度规则
			result.Rules = append(result.Rules, *r)
		}
	}

	// 按影响力排序
	sort.Slice(result.Rules, func(i, j int) bool {
		return result.Rules[i].Impact > result.Rules[j].Impact
	})

	// 计算整体置信度
	if len(result.Rules) > 0 {
		totalConf := 0.0
		for _, r := range result.Rules {
			totalConf += r.Confidence
		}
		result.Confidence = totalConf / float64(len(result.Rules))
	}

	return result
}

// InteractionData 引擎交互数据。
type InteractionData struct {
	Content      string  `json:"content"`
	EngineScore  float64 `json:"engine_score"`
	Cited        bool    `json:"cited"`
	Position     int     `json:"position"`
	Sentiment    string  `json:"sentiment"`
	Domain       string  `json:"domain,omitempty"`
	Engine       string  `json:"engine,omitempty"`
}

// detectAndAccumulate 检测模式并累积统计。
func detectAndAccumulate(rules map[string]*Rule, d InteractionData) {
	content := strings.ToLower(d.Content)

	// 检测各种模式
	patterns := []struct {
		id      string
		check   func(string) bool
		desc    string
	}{
		{"statistics", func(s string) bool {
			return strings.Contains(s, "%") || strings.Contains(s, "数据") || strings.Contains(s, "研究")
		}, "包含统计数据"},
		{"citations", func(s string) bool {
			return strings.Contains(s, "来源") || strings.Contains(s, "参考") || strings.Contains(s, "[")
		}, "包含引用来源"},
		{"authoritative", func(s string) bool {
			return strings.Contains(s, "研究表明") || strings.Contains(s, "专家") || strings.Contains(s, "权威")
		}, "使用权威语气"},
		{"faq", func(s string) bool {
			return strings.Contains(s, "faq") || strings.Contains(s, "常见问题") || strings.Contains(s, "？")
		}, "包含FAQ结构"},
		{"structured", func(s string) bool {
			return strings.Contains(s, "##") || strings.Contains(s, "- ") || strings.Contains(s, "|")
		}, "结构化内容"},
	}

	for _, p := range patterns {
		if p.check(content) {
			ruleID := p.id
			if _, ok := rules[ruleID]; !ok {
				rules[ruleID] = &Rule{
					ID:          ruleID,
					Pattern:     ruleID,
					Description: p.desc,
					Evidence:    []RuleEvidence{},
				}
			}
			rule := rules[ruleID]
			rule.Evidence = append(rule.Evidence, RuleEvidence{
				Score:  d.EngineScore,
				Cited:  d.Cited,
				Position: d.Position,
			})
			// 更新置信度和影响
			updateRuleStats(rule)
		}
	}
}

// updateRuleStats 更新规则统计。
func updateRuleStats(rule *Rule) {
	evidence := rule.Evidence
	if len(evidence) == 0 {
		return
	}

	citedCount := 0
	totalScore := 0.0
	for _, e := range evidence {
		if e.Cited {
			citedCount++
		}
		totalScore += e.Score
	}

	rule.Confidence = float64(citedCount) / float64(len(evidence))
	rule.Impact = totalScore / float64(len(evidence)) / 100.0 // 归一化到 0-1
}

// ApplyRulesToRuleSet 将学习到的规则转换为配置规则集。
func ApplyRulesToRuleSet(learned *LearnedRuleSet, existing map[string]float64) map[string]float64 {
	result := make(map[string]float64)
	for k, v := range existing {
		result[k] = v
	}

	for _, rule := range learned.Rules {
		if weight, ok := result[rule.Pattern]; ok {
			// 增强高置信度规则的权重
			boost := rule.Confidence * rule.Impact * 0.5
			result[rule.Pattern] = weight * (1 + boost)
		}
	}

	return result
}
