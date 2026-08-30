package strategies

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"my-geo/internal/models"
	"my-geo/internal/util"
)

// FluencyStrategy 流畅度优化策略。
// 合并过短句子、拆分过长句子，提升文本流畅度与可读性。
type FluencyStrategy struct{}

func (s *FluencyStrategy) Name() string              { return "流畅度优化" }
func (s *FluencyStrategy) Type() models.StrategyType { return models.StrategyFluency }
func (s *FluencyStrategy) Effectiveness() float64    { return 0.29 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *FluencyStrategy) PWCBoost() float64 { return 20.0 }

func (s *FluencyStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

var (
	// 连续多个中文/英文逗号间过短片段（少于 6 个字符）的简单识别：逗号密度
	shortSentRe = regexp.MustCompile(`[，,]\s*`)
)

// Preprocess 合并过短句子，拆分过长句子（按句号统计，过短则与下一句合并，过长按逗号拆分）。
func (s *FluencyStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = balanceLine(line)
	}
	return strings.Join(lines, "\n")
}

// balanceLine 对单行做长短句平衡：合并过短句、拆分过长句。
func balanceLine(line string) string {
	sentences := util.SplitSentences(line)
	// 合并过短句（去空白后 rune 数 <= 8 视为过短；此前按字节数判断，
	// 中文 8 字节仅约 2 个字）。合并时把前一句句号换成逗号真正连成一句
	// ——此前直接拼接与原串等价，是恒等变换。
	merged := make([]string, 0, len(sentences))
	for _, sen := range sentences {
		if utf8.RuneCountInString(strings.TrimSpace(sen)) <= 8 && len(merged) > 0 {
			last := merged[len(merged)-1]
			if strings.HasSuffix(last, "。") {
				last = last[:len(last)-len("。")] + "，"
			}
			merged[len(merged)-1] = last + sen
		} else {
			merged = append(merged, sen)
		}
	}
	// 拆分过长句（> 80 rune 且含逗号，按首个逗号拆为两句）。
	// 拆分点逗号替换为句号——此前保留原逗号再拼回，两句等于原句，恒等。
	final := make([]string, 0, len(merged))
	for _, sen := range merged {
		if utf8.RuneCountInString(sen) > 80 && shortSentRe.MatchString(sen) {
			parts := shortSentRe.Split(sen, 2)
			if len(parts) == 2 {
				final = append(final, parts[0]+"。", strings.TrimSpace(parts[1]))
				continue
			}
		}
		final = append(final, sen)
	}
	return strings.Join(final, "")
}

func (s *FluencyStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in readability optimization. " +
			"Improve the fluency and readability of the following content. " +
			"Merge overly short sentences and split overly long ones; ensure smooth transitions. " +
			"Preserve the original meaning and all key facts.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长流畅度优化的 GEO 专家。请提升以下内容的流畅度与可读性。\n" +
		"要求：\n" +
		"1. 合并过于零碎的短句，使表达更连贯\n" +
		"2. 拆分过长的复合句，避免阅读负担\n" +
		"3. 增加恰当的过渡词，保证句与句、段与段衔接自然\n" +
		"4. 保持原文语义与所有关键事实不变\n" +
		"5. 不要增删实质性信息，仅做语言层面的流畅化\n\n" +
		"待优化内容：\n" + safeContent(req)
}

// Postprocess 去除多余空行与行首尾多余空格。
func (s *FluencyStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	out := strings.Join(lines, "\n")
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}
