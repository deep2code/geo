package strategies

import (
	"strings"

	"my-geo/internal/models"
)

// EasyUnderstandStrategy 易懂性优化策略。
// 用通俗易懂的语言改写内容，面向普通读者，降低理解门槛。
type EasyUnderstandStrategy struct{}

func (s *EasyUnderstandStrategy) Name() string             { return "易懂性优化" }
func (s *EasyUnderstandStrategy) Type() models.StrategyType { return models.StrategyEasyUnderstand }
func (s *EasyUnderstandStrategy) Effectiveness() float64   { return 0.15 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *EasyUnderstandStrategy) PWCBoost() float64 { return 5.5 }

func (s *EasyUnderstandStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess 易懂性策略无需规则化预处理。
func (s *EasyUnderstandStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

func (s *EasyUnderstandStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in plain-language writing. " +
			"Rewrite the following content in clear, easy-to-understand language aimed at general readers. " +
			"Replace jargon with plain explanations; use short sentences and analogies. " +
			"Preserve all key facts and the original meaning.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长通俗易懂表达的 GEO 专家。请用平易近人的语言改写以下内容。\n" +
		"要求：\n" +
		"1. 面向普通读者，避免生僻词汇与晦涩表述\n" +
		"2. 对专业术语给出通俗解释或类比\n" +
		"3. 多用短句与口语化表达，降低阅读门槛\n" +
		"4. 保持原文所有关键事实与语义不变\n" +
		"5. 适当加入贴切的生活化比喻，帮助理解\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 易懂性策略无需后处理。
func (s *EasyUnderstandStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	return content
}
