package strategies

import (
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// StatisticsStrategy 统计数据增强策略。
// 为论断补充具体统计数据、百分比与数值，显著提升内容可信度与引用率。
type StatisticsStrategy struct{}

func (s *StatisticsStrategy) Name() string              { return "统计数据增强" }
func (s *StatisticsStrategy) Type() models.StrategyType { return models.StrategyStatistics }
func (s *StatisticsStrategy) Effectiveness() float64    { return 0.33 }

// PWCBoost 返回理论 PWC 增益百分比（Princeton GEO 论文基准）。
func (s *StatisticsStrategy) PWCBoost() float64 { return 32.8 }

func (s *StatisticsStrategy) Validate(req *models.OptimizationRequest) bool {
	return req != nil && strings.TrimSpace(req.Content) != ""
}

// Preprocess 识别不含任何数字的论断段落，标记[数据待补充]便于 LLM 定位。
func (s *StatisticsStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过空行、标题、列表项
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		// 段落中无任何数字，视为缺少数据支撑的论断
		if !digitRe.MatchString(line) {
			lines[i] = line + " [数据待补充]"
		}
	}
	return strings.Join(lines, "\n")
}

func (s *StatisticsStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	if req != nil && req.Language != "" && req.Language != "zh" {
		return "You are a GEO expert specializing in data-driven content. " +
			"Augment the following content with concrete statistics, percentages, and numerical figures for each claim. " +
			"Add specific data points where claims lack quantitative support. " +
			"Preserve the original meaning; do not fabricate implausible numbers.\n\nContent:\n" + safeContent(req)
	}
	return "你是一位擅长数据驱动内容的 GEO 专家。请为以下内容中的论断补充具体统计数据。\n" +
		"要求：\n" +
		"1. 为每个缺乏数据支撑的论断补充具体的统计数据、百分比或数值\n" +
		"2. 数据应真实可信，标注来源年份或机构（如：根据 2023 年 XX 报告）\n" +
		"3. 优先使用权威公开数据，避免编造不合理的数字\n" +
		"4. 保持原文语义与结构，仅增补数据以增强说服力\n" +
		"5. 对于已标记「[数据待补充]」的位置，请替换为真实数据后移除标记\n\n" +
		"待优化内容：\n" + safeContent(req)
}

var (
	// 规范百分比写法：百分之 50 -> 50%
	percentRe = regexp.MustCompile(`百分之\s*(\d+(?:\.\d+)?)`)
	// 规范千分位：纯数字带逗号保留，去除数字与单位间多余空格
	numUnitSpaceRe = regexp.MustCompile(`(\d)\s+(%|万|亿|个|人|次|元)`)
)

// Postprocess 规范数字格式：统一百分比写法、单位前多余空格。
func (s *StatisticsStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	out := content
	out = percentRe.ReplaceAllString(out, "$1%")
	out = numUnitSpaceRe.ReplaceAllString(out, "$1$2")
	// 清理残留标记
	out = strings.ReplaceAll(out, " [数据待补充]", "")
	out = strings.ReplaceAll(out, "[数据待补充]", "")
	return out
}
