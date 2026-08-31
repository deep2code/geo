// Package adversarial 对抗性防护：检测并防止恶意优化策略。
//
// 参考 ETH Zurich (ICLR 2025) 的对抗性 SEO 研究：
//   - 检测内容注入攻击
//   - 防止关键词堆砌
//   - 防止语义漂移
//   - 保护 AI 引擎输出质量
package adversarial

import (
	"math"
	"strings"
)

// ThreatLevel 威胁等级。
type ThreatLevel string

const (
	ThreatNone     ThreatLevel = "none"
	ThreatLow      ThreatLevel = "low"
	ThreatMedium   ThreatLevel = "medium"
	ThreatHigh     ThreatLevel = "high"
	ThreatCritical ThreatLevel = "critical"
)

// ThreatDetection 威胁检测结果。
type ThreatDetection struct {
	Level       ThreatLevel `json:"level"`
	Score       float64     `json:"score"`       // 0-100 威胁分数
	Signals     []ThreatSignal `json:"signals"`   // 检测到的信号
	Recommendation string    `json:"recommendation"` // 建议
}

// ThreatSignal 威胁信号。
type ThreatSignal struct {
	Type        string  `json:"type"`        // 信号类型
	Description string  `json:"description"` // 信号描述
	Severity    string  `json:"severity"`    // 严重程度
	Score       float64 `json:"score"`       // 信号分数 0-1
}

// DetectThreats 检测内容中的威胁信号。
func DetectThreats(content string) *ThreatDetection {
	signals := []ThreatSignal{}

	// 1. 关键词堆砌检测
	if score := detectKeywordStuffing(content); score > 0 {
		signals = append(signals, ThreatSignal{
			Type:        "keyword_stuffing",
			Description: "关键词密度过高，可能存在堆砌",
			Severity:    severityFromScore(score),
			Score:       score,
		})
	}

	// 2. 语义漂移检测
	if score := detectSemanticDrift(content); score > 0 {
		signals = append(signals, ThreatSignal{
			Type:        "semantic_drift",
			Description: "内容语义可能发生漂移",
			Severity:    severityFromScore(score),
			Score:       score,
		})
	}

	// 3. 内容注入检测
	if score := detectContentInjection(content); score > 0 {
		signals = append(signals, ThreatSignal{
			Type:        "content_injection",
			Description: "检测到可能的内容注入模式",
			Severity:    severityFromScore(score),
			Score:       score,
		})
	}

	// 4. 薄内容检测
	if score := detectThinContent(content); score > 0 {
		signals = append(signals, ThreatSignal{
			Type:        "thin_content",
			Description: "内容质量过低",
			Severity:    severityFromScore(score),
			Score:       score,
		})
	}

	// 5. 链接 spam 检测
	if score := detectLinkSpam(content); score > 0 {
		signals = append(signals, ThreatSignal{
			Type:        "link_spam",
			Description: "包含过多链接",
			Severity:    severityFromScore(score),
			Score:       score,
		})
	}

	// 计算总体威胁等级
	level, totalScore := calculateOverallThreat(signals)
	recommendation := generateRecommendation(level, signals)

	return &ThreatDetection{
		Level:          level,
		Score:          totalScore,
		Signals:        signals,
		Recommendation: recommendation,
	}
}

// detectKeywordStuffing 检测关键词堆砌。
func detectKeywordStuffing(content string) float64 {
	words := strings.Fields(content)
	if len(words) < 10 {
		return 0
	}

	// 计算词频
	freq := make(map[string]int)
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) > 2 { // 忽略短词
			freq[w]++
		}
	}

	// 检查是否有词重复过多
	maxRepeat := 0
	for _, count := range freq {
		if count > maxRepeat {
			maxRepeat = count
		}
	}

	// 重复超过 10% 视为堆砌
	ratio := float64(maxRepeat) / float64(len(words))
	if ratio > 0.1 {
		return min(100, ratio*500)
	}
	return 0
}

// detectSemanticDrift 检测语义漂移。
func detectSemanticDrift(content string) float64 {
	// 简化检测：检查内容是否包含过多生僻词
	words := strings.Fields(content)
	if len(words) < 20 {
		return 0
	}

	// 检查词汇多样性
	unique := make(map[string]bool)
	for _, w := range words {
		unique[strings.ToLower(w)] = true
	}

	// 词汇多样性过高可能表示语义漂移
	diversity := float64(len(unique)) / float64(len(words))
	if diversity > 0.95 {
		return (diversity - 0.95) * 1000
	}
	return 0
}

// detectContentInjection 检测内容注入。
func detectContentInjection(content string) float64 {
	score := 0.0

	// 检测隐藏文本
	if strings.Contains(content, "display:none") || strings.Contains(content, "visibility:hidden") {
		score += 50
	}

	// 检测异常标签
	suspicious := []string{"<script", "<iframe", "javascript:", "onclick", "onerror"}
	for _, s := range suspicious {
		if strings.Contains(strings.ToLower(content), s) {
			score += 30
		}
	}

	return min(100, score)
}

// detectThinContent 检测薄内容。
func detectThinContent(content string) float64 {
	words := strings.Fields(content)
	wordCount := len(words)

	if wordCount < 50 {
		return 100
	} else if wordCount < 100 {
		return 60
	} else if wordCount < 200 {
		return 30
	}
	return 0
}

// detectLinkSpam 检测链接 spam。
func detectLinkSpam(content string) float64 {
	// 计算链接数量
	linkCount := strings.Count(content, "http://") + strings.Count(content, "https://")
	words := strings.Fields(content)
	if len(words) == 0 {
		return 0
	}

	// 链接密度超过 5% 视为 spam
	ratio := float64(linkCount) / float64(len(words))
	if ratio > 0.05 {
		return min(100, ratio*1000)
	}
	return 0
}

// calculateOverallThreat 计算总体威胁等级。
func calculateOverallThreat(signals []ThreatSignal) (ThreatLevel, float64) {
	if len(signals) == 0 {
		return ThreatNone, 0
	}

	totalScore := 0.0
	for _, s := range signals {
		totalScore += s.Score
	}
	avgScore := totalScore / float64(len(signals))

	switch {
	case avgScore >= 80:
		return ThreatCritical, avgScore
	case avgScore >= 60:
		return ThreatHigh, avgScore
	case avgScore >= 40:
		return ThreatMedium, avgScore
	case avgScore > 0:
		return ThreatLow, avgScore
	default:
		return ThreatNone, 0
	}
}

// severityFromScore 从分数计算严重程度。
func severityFromScore(score float64) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 40:
		return "medium"
	default:
		return "low"
	}
}

// generateRecommendation 生成建议。
func generateRecommendation(level ThreatLevel, signals []ThreatSignal) string {
	switch level {
	case ThreatCritical:
		return "检测到严重威胁，建议立即停止当前优化策略并重新评估"
	case ThreatHigh:
		return "检测到高威胁，建议减少激进优化策略，优先保证内容质量"
	case ThreatMedium:
		return "检测到中等威胁，建议调整优化策略以避免被搜索引擎惩罚"
	case ThreatLow:
		return "检测到轻微威胁，建议持续监控并适当调整"
	default:
		return "未检测到明显威胁，可继续当前优化策略"
	}
}

// ValidateContent 验证内容安全性（供优化器调用）。
func ValidateContent(content string) (bool, string) {
	detection := DetectThreats(content)
	if detection.Level == ThreatHigh || detection.Level == ThreatCritical {
		return false, detection.Recommendation
	}
	return true, ""
}

func max(a, b float64) float64 {
	return math.Max(a, b)
}

func min(a, b float64) float64 {
	return math.Min(a, b)
}
