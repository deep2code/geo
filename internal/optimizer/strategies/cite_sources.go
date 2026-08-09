// Package strategies 定义 GEO 优化策略接口与实现。
//
// 采用策略模式 (Strategy Pattern)，每个策略实现统一接口，
// 可插拔注册到优化引擎。策略实现基于 Princeton 大学 GEO 论文的
// 9 种优化方法及工程化扩展策略。
package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// CiteSourcesStrategy 引用来源增强策略。
// 为关键论断补充可信来源引用，提升生成式引擎的引用率。
type CiteSourcesStrategy struct{}

// Name 返回策略名称。
func (s *CiteSourcesStrategy) Name() string { return "引用来源增强" }

// Type 返回策略类型标识。
func (s *CiteSourcesStrategy) Type() models.StrategyType { return models.StrategyCiteSources }

// Effectiveness 返回预期效果系数。
func (s *CiteSourcesStrategy) Effectiveness() float64 { return 0.27 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *CiteSourcesStrategy) PWCBoost() float64 { return 42.6 }

// Validate 判断请求是否适用：内容非空即可。
func (s *CiteSourcesStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

var hasCitationRe = regexp.MustCompile(`\[\d+\]|\[来源`)

// Preprocess 检测已有引用；若内容中无引用标记，则在含数字的句子后追加[来源：待补充]。
func (s *CiteSourcesStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	// 已存在引用标记则不再追加占位
	if hasCitationRe.MatchString(content) {
		return content
	}
	var b strings.Builder
	// 按中文句号/英文句点/换行切分并逐句处理
	lines := strings.Split(content, "\n")
	for li, line := range lines {
		sentences := splitSentences(line)
		for i, sen := range sentences {
			if digitRe.MatchString(sen) && !hasCitationRe.MatchString(sen) {
				sentences[i] = sen + "[来源：待补充]"
			}
		}
		b.WriteString(strings.Join(sentences, ""))
		if li < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// BuildPrompt 构建引用来源增强的 LLM 提示词。
func (s *CiteSourcesStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO (Generative Engine Optimization) expert. " +
			"Enhance the following content by adding credible source citations to key claims. " +
			"Format citations as footnote-style markers like [1][2] and append a 'Sources:' list at the end. " +
			"Ensure every factual assertion is backed by a citation. Keep the original meaning intact.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位 GEO（生成式引擎优化）专家。请为以下内容中的关键论断补充可信来源引用。\n" +
		"要求：\n" +
		"1. 为每个事实性论断添加脚注式引用标记，格式如 [1][2]\n" +
		"2. 在内容末尾附上「参考资料」列表，列出对应的来源\n" +
		"3. 引用来源应来自权威机构、学术期刊、官方数据等可信渠道\n" +
		"4. 保持原文语义不变，仅增补引用，不要删减核心信息\n" +
		"5. 不要编造虚假来源，若无法确定具体来源可标注为「待核实」\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 规范化引用格式：统一脚注标记、合并空白。
func (s *CiteSourcesStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	// 规范连续的引用标记间多余空格：[1] [2] -> [1][2]
	out = citeBracketSpaceRe.ReplaceAllString(out, "][")
	// 将「参考资料」「来源」等列表标题统一为「参考资料」
	out = strings.ReplaceAll(out, "参考来源：", "参考资料：")
	out = strings.ReplaceAll(out, "来源列表：", "参考资料：")
	// 合并多余空行
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}

// --- 包级共享辅助（供本包各策略复用，避免重复定义）---

var (
	citeBracketSpaceRe = regexp.MustCompile(`\]\s+\[`)
	multiNewlineRe     = regexp.MustCompile(`\n{3,}`)
	sentenceSplitRe    = regexp.MustCompile(`([。！？!?])`)
	digitRe            = regexp.MustCompile(`\d`)
)

// safeContent 安全返回请求内容，req 为 nil 时返回空串。
func safeContent(req *models.OptimizationRequest) string {
	if req == nil {
		return ""
	}
	return req.Content
}

// splitSentences 按中英文句末标点（。！？!?）切分单行为句子，
// 切分后保留标点在句尾。返回的句子拼接后与原行等价。
func splitSentences(line string) []string {
	if line == "" {
		return []string{""}
	}
	indices := sentenceSplitRe.FindAllStringSubmatchIndex(line, -1)
	if len(indices) == 0 {
		return []string{line}
	}
	var sentences []string
	prev := 0
	for _, idx := range indices {
		// idx[2]:idx[3] 为捕获组（标点）的范围
		end := idx[3]
		sentences = append(sentences, line[prev:end])
		prev = end
	}
	if prev < len(line) {
		sentences = append(sentences, line[prev:])
	}
	return sentences
}
