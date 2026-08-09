package strategies

import (
	"strings"

	"my-geo/internal/models"
)

// UniqueWordsStrategy 独特词汇增强策略。
// 使用丰富多样的词汇，增加独特表述，提升内容区分度。
type UniqueWordsStrategy struct{}

func (s *UniqueWordsStrategy) Name() string             { return "独特词汇增强" }
func (s *UniqueWordsStrategy) Type() models.StrategyType { return models.StrategyUniqueWords }
func (s *UniqueWordsStrategy) Effectiveness() float64   { return 0.18 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准：词汇多样性对 PWC 影响不显著）。
func (s *UniqueWordsStrategy) PWCBoost() float64 { return 0.0 }

func (s *UniqueWordsStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess 独特词汇策略无需规则化预处理。
func (s *UniqueWordsStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

func (s *UniqueWordsStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in lexical richness. " +
			"Rewrite the following content to use a richer, more diverse vocabulary. " +
			"Replace repetitive words with varied, distinctive expressions. " +
			"Preserve the original meaning and key facts.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长词汇丰富的 GEO 专家。请为以下内容增加独特多样的词汇表述。\n" +
		"要求：\n" +
		"1. 使用丰富多样的词汇，避免重复用词\n" +
		"2. 以同义词、近义词替换高频重复词，增加表述的独特性\n" +
		"3. 适当引入精准且不常见的专业表达，提升内容区分度\n" +
		"4. 保持原文语义与关键事实不变\n" +
		"5. 确保用词准确自然，不生造词汇\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 独特词汇策略无需后处理。
func (s *UniqueWordsStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	return content
}
