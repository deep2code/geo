package strategies

import (
	"strings"

	"my-geo/internal/models"
)

// AuthoritativeStrategy 权威语气增强策略。
// 将弱化措辞替换为权威表述，并增加权威机构与专家背书。
type AuthoritativeStrategy struct{}

func (s *AuthoritativeStrategy) Name() string              { return "权威语气增强" }
func (s *AuthoritativeStrategy) Type() models.StrategyType { return models.StrategyAuthoritative }
func (s *AuthoritativeStrategy) Effectiveness() float64    { return 0.25 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *AuthoritativeStrategy) PWCBoost() float64 { return 15.0 }

func (s *AuthoritativeStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// 弱化词 -> 权威表述 的替换表。
var weakWordReplacements = []struct{ from, to string }{
	{"我觉得", "研究表明"},
	{"我认为", "研究证实"},
	{"可能", "数据显示"},
	{"也许", "据观测"},
	{"大概", "统计表明"},
	{"应该是", "已有证据表明"},
	{"好像是", "已有证据表明"},
	{"说不定", "据观测"},
	{"估计", "测算显示"},
	{"我觉得吧", "研究表明"},
	{"我猜", "据测算"},
	{"I think", "Research shows"},
	{"I believe", "Studies confirm"},
	{"maybe", "data indicates"},
	{"perhaps", "evidence suggests"},
}

// Preprocess 将弱化措辞替换为权威表述。
func (s *AuthoritativeStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	for _, r := range weakWordReplacements {
		out = strings.ReplaceAll(out, r.from, r.to)
	}
	return out
}

func (s *AuthoritativeStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in authoritative content. " +
			"Rewrite the following content with a confident, authoritative tone. " +
			"Add backing from authoritative institutions or domain experts where appropriate. " +
			"Remove hedging language. Keep the original meaning and key facts intact.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长权威表达的 GEO 专家。请用权威语气改写以下内容。\n" +
		"要求：\n" +
		"1. 使用确定、自信的表述，去除「可能」「也许」「我觉得」等模糊措辞\n" +
		"2. 在关键论断处补充权威机构或领域专家的背书（如「据 WHO 数据」「XX 研究指出」）\n" +
		"3. 保持客观专业，避免夸张与绝对化表述\n" +
		"4. 保持原文核心事实与语义不变\n" +
		"5. 适当增强论断的严谨性与可信度\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 清理多余修饰：去除连续感叹号、空格规整。
func (s *AuthoritativeStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	// 多个连续感叹号收敛为单个
	for strings.Contains(out, "！！") {
		out = strings.ReplaceAll(out, "！！", "！")
	}
	for strings.Contains(out, "!!") {
		out = strings.ReplaceAll(out, "!!", "!")
	}
	// 中文标点间多余空格
	out = strings.ReplaceAll(out, " 。", "。")
	out = strings.ReplaceAll(out, " ，", "，")
	// 合并多余空行
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}
