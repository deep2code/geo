package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
	"my-geo/internal/util"
)

// QuotationStrategy 引用语增强策略。
// 为关键观点补充权威引用语（引号包裹的直接引述），效果系数最高 (+41%)。
type QuotationStrategy struct{}

func (s *QuotationStrategy) Name() string              { return "引用语增强" }
func (s *QuotationStrategy) Type() models.StrategyType { return models.StrategyQuotation }
func (s *QuotationStrategy) Effectiveness() float64    { return 0.41 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *QuotationStrategy) PWCBoost() float64 { return 37.1 }

func (s *QuotationStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// conclusionRe 匹配关键结论句的常见引导词。
var conclusionRe = regexp.MustCompile(`(总之|综上|因此|由此可见|可以得出|结论是|总而言之|可见|综上所述|这意味着)`)

// Preprocess 识别关键结论句，标记[关键结论]便于 LLM 优先补充引用语。
func (s *QuotationStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		sentences := util.SplitSentences(line)
		for j, sen := range sentences {
			if conclusionRe.MatchString(sen) {
				sentences[j] = sen + "[关键结论]"
			}
		}
		lines[i] = strings.Join(sentences, "")
	}
	return strings.Join(lines, "\n")
}

func (s *QuotationStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in quotation enhancement. " +
			"Add authoritative direct quotations (wrapped in quotes) to the key viewpoints in the following content. " +
			"Quote recognized experts, scholars, or official statements. " +
			"Keep the original meaning; do not invent fake attributions.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长引用语增强的 GEO 专家。请为以下内容的关键观点补充权威引用语。\n" +
		"要求：\n" +
		"1. 为关键观点补充权威直接引述，用引号「」或英文双引号包裹引用内容\n" +
		"2. 引用语应来自领域专家、学者或权威机构的公开表述\n" +
		"3. 标注引述来源（如「XX 教授指出：『...』」）\n" +
		"4. 优先为已标记「[关键结论]」的句子补充引用语，处理完成后移除标记\n" +
		"5. 不要编造虚假引述，无法确定来源时可标注「据相关专家」\n" +
		"6. 保持原文语义与结构不变\n\n" +
		"待优化内容：\n" + safeContent(req)
}

var (
	// 将中文弯引号 “” 统一为「」（此前两个正则都是 ASCII 直引号 "…"，
	// 第二条纯冗余，中文弯引号统一逻辑从未生效）
	dquotePairRe = regexp.MustCompile(`“([^”]*)”`)
	// 英文直引号统一
	equotePairRe = regexp.MustCompile(`"([^"]*)"`)
)

// Postprocess 规范引号格式：统一引号配对，清理残留标记。
func (s *QuotationStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	// 统一中文双引号 ""/“” 为 「」
	out = dquotePairRe.ReplaceAllString(out, "「$1」")
	out = equotePairRe.ReplaceAllString(out, "「$1」")
	// 清理残留标记
	out = strings.ReplaceAll(out, "[关键结论]", "")
	// 修正「」内侧多余空格
	out = strings.ReplaceAll(out, "「 ", "「")
	out = strings.ReplaceAll(out, " 」", "」")
	return strings.TrimSpace(out) + "\n"
}
