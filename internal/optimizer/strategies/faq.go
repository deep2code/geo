package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// FAQStrategy FAQ 问答生成策略。
// 基于内容生成 3-5 个常见问题问答对，增强内容对问答型查询的覆盖。
type FAQStrategy struct{}

func (s *FAQStrategy) Name() string              { return "FAQ问答生成" }
func (s *FAQStrategy) Type() models.StrategyType { return models.StrategyFAQ }
func (s *FAQStrategy) Effectiveness() float64    { return 0.20 }

// PWCBoost 返回理论 PWC 增益百分比（工程扩展策略：FAQ 模式直接匹配问答查询）。
func (s *FAQStrategy) PWCBoost() float64 { return 18.0 }

func (s *FAQStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess FAQ 策略无需规则化预处理。
func (s *FAQStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

func (s *FAQStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in FAQ generation. " +
			"Based on the following content, generate 3 to 5 common question-answer pairs. " +
			"Format each as '## Q: <question>' followed by 'A: <answer>'. " +
			"Answers must be accurate and derived from the content. Append them at the end.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长 FAQ 生成的 GEO 专家。请基于以下内容生成 3-5 个常见问题问答对。\n" +
		"要求：\n" +
		"1. 提取内容中的核心信息，生成 3-5 个用户可能关心的常见问题\n" +
		"2. 每个问答对格式为：先一行「## Q：问题」，下一行「A：答案」\n" +
		"3. 答案须准确，且来源于原文内容，不得编造\n" +
		"4. 问答对附加在原文末尾，以「## 常见问题」作为小节标题\n" +
		"5. 问题表述自然，符合用户搜索习惯\n\n" +
		"待优化内容：\n" + safeContent(req)
}

var (
	// 规范 Q/A 前缀：Q: Q： 问: 等 -> Q：
	qPrefixRe = regexp.MustCompile(`(?im)^#{0,6}\s*(?:问|Q)[:：]\s*`)
	aPrefixRe = regexp.MustCompile(`(?im)^#{0,6}\s*(?:答|A)[:：]\s*`)
)

// Postprocess 规范 Q/A 格式：统一问/答前缀为「Q：」「A：」。
func (s *FAQStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	out = qPrefixRe.ReplaceAllString(out, "## Q：")
	out = aPrefixRe.ReplaceAllString(out, "A：")
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}
