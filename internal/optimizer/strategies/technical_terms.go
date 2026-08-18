package strategies

import (
	"strings"

	"my-geo/internal/models"
)

// TechnicalTermsStrategy 技术术语增强策略。
// 适当补充专业术语并给出简明解释，提升内容的专业度与可引用性。
type TechnicalTermsStrategy struct{}

func (s *TechnicalTermsStrategy) Name() string              { return "技术术语增强" }
func (s *TechnicalTermsStrategy) Type() models.StrategyType { return models.StrategyTechnicalTerms }
func (s *TechnicalTermsStrategy) Effectiveness() float64    { return 0.20 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *TechnicalTermsStrategy) PWCBoost() float64 { return 10.0 }

func (s *TechnicalTermsStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess 技术术语策略无需规则化预处理。
func (s *TechnicalTermsStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

func (s *TechnicalTermsStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in technical writing. " +
			"Enhance the following content by adding appropriate domain-specific technical terms, " +
			"each accompanied by a concise explanation. " +
			"Improve precision without sacrificing readability. Preserve the original meaning.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长技术写作的 GEO 专家。请为以下内容适当补充专业术语并给出简明解释。\n" +
		"要求：\n" +
		"1. 在合适位置补充领域相关的专业术语，提升专业度\n" +
		"2. 对每个补充的术语给出简明易懂的解释（可用括号或「即…」说明）\n" +
		"3. 术语使用须准确，符合领域规范\n" +
		"4. 保持原文语义与关键事实不变\n" +
		"5. 术语密度适中，避免影响可读性\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 技术术语策略无需后处理。
func (s *TechnicalTermsStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	return content
}
