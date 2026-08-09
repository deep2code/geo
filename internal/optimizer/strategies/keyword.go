package strategies

import (
	"strings"

	"my-geo/internal/models"
)

// KeywordStrategy 关键词优化策略。
// 自然融入主题关键词，避免堆砌，提升语义相关性。
type KeywordStrategy struct{}

func (s *KeywordStrategy) Name() string             { return "关键词优化" }
func (s *KeywordStrategy) Type() models.StrategyType { return models.StrategyKeyword }
func (s *KeywordStrategy) Effectiveness() float64   { return 0.10 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准：关键词堆砌会降权，为负值）。
func (s *KeywordStrategy) PWCBoost() float64 { return -8.7 }

func (s *KeywordStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess 关键词策略无需规则化预处理。
func (s *KeywordStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

func (s *KeywordStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in keyword optimization. " +
			"Identify the core topic keywords of the following content and weave them in naturally. " +
			"Avoid keyword stuffing; ensure natural reading flow. " +
			"Preserve the original meaning and key facts.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长关键词优化的 GEO 专家。请为以下内容自然融入主题关键词。\n" +
		"要求：\n" +
		"1. 识别内容的核心主题关键词，并在行文中自然融入\n" +
		"2. 关键词出现需符合语境，避免生硬堆砌\n" +
		"3. 适当使用关键词的同义/近义变体，丰富表达\n" +
		"4. 保持原文语义与关键事实不变\n" +
		"5. 关键词密度合理，确保可读性优先\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 检测并消除关键词堆砌：同一词连续出现 3 次及以上时收敛为一次。
//
// 注：Go 的 regexp（RE2）不支持反向引用，故用分词方式检测。
func (s *KeywordStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	return collapseRepeats(content)
}

// collapseRepeats 收敛连续重复出现的相同词（2-8 字符），重复 3 次及以上收敛为 1 次。
// 无堆砌时返回原文以保留原始格式。
func collapseRepeats(content string) string {
	tokens := strings.Fields(content)
	if len(tokens) < 3 {
		return content
	}
	var result []string
	modified := false
	i := 0
	for i < len(tokens) {
		token := tokens[i]
		r := []rune(token)
		if len(r) >= 2 && len(r) <= 8 {
			j := i + 1
			for j < len(tokens) && tokens[j] == token {
				j++
			}
			if j-i >= 3 {
				result = append(result, token)
				i = j
				modified = true
				continue
			}
		}
		result = append(result, token)
		i++
	}
	if !modified {
		return content
	}
	return strings.Join(result, " ")
}
