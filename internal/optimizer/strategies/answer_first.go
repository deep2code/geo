package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// AnswerFirstStrategy 结论前置策略。
// 将核心结论放在开头首段（TL;DR 风格），提升关键信息的位置得分。
type AnswerFirstStrategy struct{}

func (s *AnswerFirstStrategy) Name() string              { return "结论前置" }
func (s *AnswerFirstStrategy) Type() models.StrategyType { return models.StrategyAnswerFirst }
func (s *AnswerFirstStrategy) Effectiveness() float64    { return 0.24 }

// PWCBoost 返回理论 PWC 增益百分比（工程扩展策略：答案前置提升位置权重，PWC = position-adjusted）。
func (s *AnswerFirstStrategy) PWCBoost() float64 { return 25.0 }

func (s *AnswerFirstStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

var (
	// 结论性引导词，用于判断首段是否已是结论
	conclusionLeadRe = regexp.MustCompile(`(总之|综上|结论是|由此可见|可以得出|总而言之|核心结论|TL;DR|概而言之|由此可见)`)
	// 段落总结标记
	summaryMarkerRe = regexp.MustCompile(`(综上|总结|结论|总而言之|由此可见|概括来说)`)
)

// Preprocess 检测首段是否已是结论；若否则提取末尾总结段移至开头。
func (s *AnswerFirstStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	paragraphs := splitParagraphs(content)
	if len(paragraphs) == 0 {
		return content
	}
	// 首段已是结论，无需调整
	if conclusionLeadRe.MatchString(paragraphs[0]) {
		return content
	}
	// 从末尾寻找总结段
	summaryIdx := -1
	for i := len(paragraphs) - 1; i >= 1; i-- {
		if summaryMarkerRe.MatchString(paragraphs[i]) || conclusionLeadRe.MatchString(paragraphs[i]) {
			summaryIdx = i
			break
		}
	}
	if summaryIdx == -1 {
		return content
	}
	// 将总结段移至开头，原位置删除
	summary := paragraphs[summaryIdx]
	rest := append([]string{}, paragraphs[:summaryIdx]...)
	rest = append(rest, paragraphs[summaryIdx+1:]...)
	result := append([]string{summary}, rest...)
	return strings.Join(result, "\n\n") + "\n"
}

// splitParagraphs 按空行切分段落，忽略空段落。
func splitParagraphs(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "\n\n")
	var paras []string
	for _, p := range raw {
		if strings.TrimSpace(p) != "" {
			paras = append(paras, strings.TrimSpace(p))
		}
	}
	return paras
}

func (s *AnswerFirstStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in answer-first structuring. " +
			"Rewrite the following content so the core conclusion appears in the opening paragraph (TL;DR style). " +
			"Keep supporting details below. Preserve all key facts and the original meaning.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长结论前置的 GEO 专家。请将以下内容改写为「结论先行」的结构。\n" +
		"要求：\n" +
		"1. 将核心结论提炼并放在开头首段，采用 TL;DR 风格的直接概述\n" +
		"2. 首段应能独立回答读者最核心的问题\n" +
		"3. 支撑性细节与论据保留在后续段落，保持逻辑递进\n" +
		"4. 保持原文所有关键事实与语义不变\n" +
		"5. 首段简洁有力，避免冗长铺垫\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 确保首段为结论性概述：若首段无结论引导词，则补充「综上所述，」前缀。
func (s *AnswerFirstStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	paragraphs := splitParagraphs(content)
	if len(paragraphs) == 0 {
		return content
	}
	first := paragraphs[0]
	if !conclusionLeadRe.MatchString(first) {
		paragraphs[0] = "综上所述，" + first
	}
	out := strings.Join(paragraphs, "\n\n")
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}
