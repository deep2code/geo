package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// StructureStrategy 结构化优化策略。
// 为无标题的长文本添加基础 Markdown 标题分段，并将连续短行转为列表。
type StructureStrategy struct{}

func (s *StructureStrategy) Name() string              { return "结构化优化" }
func (s *StructureStrategy) Type() models.StrategyType { return models.StrategyStructure }
func (s *StructureStrategy) Effectiveness() float64    { return 0.22 }

// PWCBoost 返回理论 PWC 增益百分比（工程扩展策略：结构化内容便于 AI 解析抽取）。
func (s *StructureStrategy) PWCBoost() float64 { return 12.0 }

func (s *StructureStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

var (
	headingRe  = regexp.MustCompile(`^#{1,6}\s`)
	listItemRe = regexp.MustCompile(`^[-*]\s`)
)

// Preprocess 为无标题的长文本添加基础 Markdown 标题分段；将连续短行转为列表。
func (s *StructureStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	hasHeading := false
	for _, l := range lines {
		if headingRe.MatchString(l) {
			hasHeading = true
			break
		}
	}
	// 无标题且文本较长（>4 行）时按空行分段添加 ## 标题
	if !hasHeading && len(lines) > 4 {
		lines = addBasicHeadings(lines, req)
	}
	// 连续短行（<=20 字符、非空、非标题/列表）转为列表项
	lines = convertShortLinesToList(lines)
	return strings.Join(lines, "\n")
}

// addBasicHeadings 按空行分块，为每块添加基于序号的标题。
func addBasicHeadings(lines []string, req *models.OptimizationRequest) []string {
	var result []string
	sectionIdx := 0
	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		// 跳过仅空白块
		allBlank := true
		for _, b := range block {
			if strings.TrimSpace(b) != "" {
				allBlank = false
				break
			}
		}
		if allBlank {
			result = append(result, block...)
			block = block[:0]
			return
		}
		sectionIdx++
		result = append(result, "## 第"+itoa(sectionIdx)+"部分")
		result = append(result, block...)
		block = block[:0]
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			flush()
			result = append(result, l)
		} else {
			block = append(block, l)
		}
	}
	flush()
	return result
}

// convertShortLinesToList 将连续的短行转为 Markdown 列表项。
func convertShortLinesToList(lines []string) []string {
	var result []string
	i := 0
	for i < len(lines) {
		// 收集连续短行
		if strings.TrimSpace(lines[i]) != "" && !headingRe.MatchString(lines[i]) &&
			!listItemRe.MatchString(lines[i]) && lineRuneLen(lines[i]) <= 20 {
			j := i
			var group []string
			for j < len(lines) {
				l := lines[j]
				if strings.TrimSpace(l) == "" || headingRe.MatchString(l) || listItemRe.MatchString(l) {
					break
				}
				if lineRuneLen(l) > 20 {
					break
				}
				group = append(group, l)
				j++
			}
			if len(group) >= 2 {
				for _, g := range group {
					result = append(result, "- "+strings.TrimSpace(g))
				}
				i = j
				continue
			}
		}
		result = append(result, lines[i])
		i++
	}
	return result
}

func lineRuneLen(s string) int { return len([]rune(s)) }

// itoa 简单整数转字符串（避免引入 strconv 仅为此一处）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (s *StructureStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert in content structuring. " +
			"Reorganize the following content with clear heading hierarchy, lists, and tables where helpful. " +
			"Use Markdown. Improve logical flow and scannability. Preserve all key facts.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长内容结构化的 GEO 专家。请用清晰的结构重组以下内容。\n" +
		"要求：\n" +
		"1. 使用清晰的 Markdown 标题层级（# / ## / ###）划分内容\n" +
		"2. 将并列要点转为列表（- 或 1.），对比性数据可用表格呈现\n" +
		"3. 保证逻辑顺序与可扫读性，便于读者快速定位信息\n" +
		"4. 保持原文所有关键事实与语义不变\n" +
		"5. 标题层级不超过 ###，避免过深层级\n\n" +
		"待优化内容：\n" + safeContent(req)
}

var (
	// 匹配 4 个及以上 # 的标题，降级为 ###
	deepHeadingRe = regexp.MustCompile(`^(#{4,})\s`)
	// 多个连续空行
)

// Postprocess 规范 Markdown 标题层级（# 到 ###），合并多余空行。
func (s *StructureStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		// ### 以上层级降为 ###
		if deepHeadingRe.MatchString(l) {
			lines[i] = "### " + strings.TrimSpace(strings.TrimLeft(l, "#"))
		}
	}
	out := strings.Join(lines, "\n")
	out = multiNewlineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out) + "\n"
}
