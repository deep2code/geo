// Package analyzer 内容分析器，检测内容的 GEO 信号。
//
// 参考 geo-optimizer-skill 的 47 种研究背书检测方法，实现核心信号检测：
//   - 可引用性信号 (citability): 引用、统计、流畅、权威等
//   - 结构信号 (structure): 标题层级、列表、表格、结论前置等
//   - 负向信号 (negative): CTA过载、薄内容、关键词堆砌等
//   - 常青度评分 (evergreen): 内容时效衰减预测
package analyzer

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"my-geo/internal/models"
)

// Analyzer 内容分析器。
type Analyzer struct{}

// New 创建内容分析器。
func New() *Analyzer {
	return &Analyzer{}
}

// 预编译正则。
var (
	// 统计数据：数字 + 百分比/单位
	reStatistics = regexp.MustCompile(`\d+(\.\d+)?\s*[%％万千百万亿倍]|根据.*?研究|数据显示|统计表明`)
	// 引用来源：[1] / (来源:xxx) / 根据...报道
	reCiteSources = regexp.MustCompile(`\[\d+\]|来源[:：]|参考[:：]|根据.*?(报道|报告|研究|调查)|引自`)
	// 引用语：引号包裹的句子
	reQuotation = regexp.MustCompile(`["""''].{8,}["""'']`)
	// 标题层级
	reHeading = regexp.MustCompile(`(?m)^#{1,6}\s.+`)
	// 列表
	reList = regexp.MustCompile(`(?m)^[\-\*\+]\s|^\d+\.\s`)
	// 表格（Markdown）
	reTable = regexp.MustCompile(`(?m)^\|.+\|$`)
	// 英文单词（keyword_stuffing 检测用）
	reWord = regexp.MustCompile(`[A-Za-z]+`)
	// CTA 过载
	reCTA = regexp.MustCompile(`(?i)(立即购买|点击这里|马上注册|限时优惠|免费下载|subscribe now|buy now|click here|sign up)`)
	// URL
	reURL = regexp.MustCompile(`https?://[^\s）)]+`)
	// 技术术语（英文缩写/专业词）
	reTechnical = regexp.MustCompile(`\b[A-Z]{2,}\b|API|算法|架构|协议|框架`)
	// 汉字（countWords 中用于把中文替换为空格以统计英文单词）
	reHan = regexp.MustCompile(`[\p{Han}]`)
	// P2-4：常青度评分用的时效性/价格正则提升为包级变量，避免每次调用重新编译。
	reTimeSensitive = regexp.MustCompile(`20\d\d年|20\d\d-\d{1,2}|去年|本月|今日`)
	rePriceSensitive = regexp.MustCompile(`￥|¥|\$\d|价格|元/`)
)

// Analyze 分析内容，返回各类 GEO 信号检测结果。
func (a *Analyzer) Analyze(content string) *models.ContentAnalysis {
	analysis := &models.ContentAnalysis{
		RawText:           content,
		WordCount:         countWords(content),
		CitabilitySignals: make(map[string]bool),
		StructureSignals:  make(map[string]bool),
		AnalyzedAt:        time.Now(),
	}

	// 可引用性信号
	analysis.CitabilitySignals["statistics"] = reStatistics.MatchString(content)
	analysis.CitabilitySignals["cite_sources"] = reCiteSources.MatchString(content)
	analysis.CitabilitySignals["quotation"] = reQuotation.MatchString(content)
	analysis.CitabilitySignals["technical_terms"] = reTechnical.MatchString(content)
	analysis.CitabilitySignals["fluency"] = checkFluency(content)
	analysis.CitabilitySignals["authoritative"] = checkAuthoritative(content)
	analysis.CitabilitySignals["unique_words"] = checkUniqueWords(content)

	// 结构信号
	analysis.StructureSignals["heading_hierarchy"] = reHeading.MatchString(content)
	analysis.StructureSignals["lists"] = reList.MatchString(content)
	analysis.StructureSignals["tables"] = reTable.MatchString(content)
	analysis.StructureSignals["front_loading"] = checkFrontLoading(content)
	analysis.StructureSignals["definition_openings"] = checkDefinitionOpening(content)
	analysis.StructureSignals["faq"] = checkFAQ(content)

	// 负向信号
	analysis.NegativeSignals = detectNegativeSignals(content)

	// 常青度评分
	analysis.EvergreenScore = calcEvergreenScore(content, analysis)

	return analysis
}

// checkFluency 流畅度检测：句子平均长度适中、无明显重复。
func checkFluency(content string) bool {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return false
	}
	totalLen := 0
	for _, s := range sentences {
		totalLen += utf8.RuneCountInString(strings.TrimSpace(s))
	}
	avgLen := totalLen / len(sentences)
	// 句子平均长度 15-60 字符视为流畅
	return avgLen >= 15 && avgLen <= 60
}

// checkAuthoritative 权威语气检测：包含权威性措辞。
func checkAuthoritative(content string) bool {
	patterns := []string{"研究表明", "专家指出", "权威", "公认", "证实", "表明",
		"研究显示", "根据", "studies show", "experts", "proven", "demonstrated"}
	lower := strings.ToLower(content)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// checkUniqueWords 独特词汇检测：词汇多样性。
func checkUniqueWords(content string) bool {
	words := strings.Fields(content)
	if len(words) < 20 {
		return false
	}
	unique := make(map[string]bool)
	for _, w := range words {
		unique[strings.ToLower(w)] = true
	}
	ratio := float64(len(unique)) / float64(len(words))
	return ratio > 0.5
}

// checkFrontLoading 结论前置检测：首段即给出核心结论。
func checkFrontLoading(content string) bool {
	paragraphs := strings.Split(content, "\n\n")
	if len(paragraphs) == 0 {
		return false
	}
	first := strings.TrimSpace(paragraphs[0])
	// 首段超过 30 字符且包含结论性词语
	if utf8.RuneCountInString(first) < 30 {
		return false
	}
	conclusionWords := []string{"是", "为", "意味着", "总结", "综上", "核心", "关键",
		"is", "means", "conclusion", "summary", "key"}
	lower := strings.ToLower(first)
	for _, w := range conclusionWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// checkDefinitionOpening 定义式开头检测：以"X是..."开头。
func checkDefinitionOpening(content string) bool {
	trimmed := strings.TrimSpace(content)
	patterns := []string{"是指", "是一种", "是一个", "指的是", "is a", "is an", "refers to"}
	lower := strings.ToLower(trimmed)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// checkFAQ 问答结构检测。
func checkFAQ(content string) bool {
	patterns := []string{"FAQ", "常见问题", "问：", "答：", "Q:", "A:", "？\n"}
	for _, p := range patterns {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// detectNegativeSignals 负向信号检测。
func detectNegativeSignals(content string) []string {
	var negs []string

	// CTA 过载
	ctas := reCTA.FindAllString(content, -1)
	if len(ctas) >= 3 {
		negs = append(negs, "cta_overload")
	}
	// 薄内容
	if countWords(content) < 100 {
		negs = append(negs, "thin_content")
	}
	// 关键词堆砌：同一单词连续重复（Go RE2 不支持反向引用，用代码统计）
	if detectKeywordStuffing(content) {
		negs = append(negs, "keyword_stuffing")
	}
	// URL 过多（spam 信号）
	urls := reURL.FindAllString(content, -1)
	if len(urls) > 10 {
		negs = append(negs, "excessive_links")
	}
	// 无标题结构
	if !reHeading.MatchString(content) && !strings.Contains(content, "\n- ") && countWords(content) > 300 {
		negs = append(negs, "no_structure")
	}
	return negs
}

// detectKeywordStuffing 检测关键词堆砌：同一英文单词连续重复出现 5 次及以上。
//
// 此前的正则 (\w+[\s,，。]*){5,} 匹配的是"任意 5 个连续单词"而非"同词重复"，
// 任何正常英文正文都命中，导致英文内容评分系统性偏低（负向信号 +
// EvergreenScore 扣分 + 高优先级建议误触发）。
func detectKeywordStuffing(content string) bool {
	words := reWord.FindAllString(strings.ToLower(content), -1)
	run, maxRun := 1, 1
	for i := 1; i < len(words); i++ {
		if words[i] == words[i-1] {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 1
		}
	}
	return maxRun >= 5
}

// calcEvergreenScore 常青度评分（0-100）。
//
// 时效性内容（含日期/价格）得分低，知识性内容得分高。
func calcEvergreenScore(content string, a *models.ContentAnalysis) int {
	score := 70 // 基础分
	// 含具体日期 → 时效性内容，降低常青度
	if reTimeSensitive.MatchString(content) {
		score -= 20
	}
	// 含价格 → 易过期
	if rePriceSensitive.MatchString(content) {
		score -= 15
	}
	// 有结构化信号 → 提升
	for _, v := range a.StructureSignals {
		if v {
			score += 3
		}
	}
	// 有负向信号 → 降低
	score -= len(a.NegativeSignals) * 5
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// countWords 统计词数（中英文混合）。
func countWords(content string) int {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0
	}
	// 中文按字符计，英文按单词计
	cnCount := 0
	for _, r := range content {
		if r >= 0x4e00 && r <= 0x9fff {
			cnCount++
		}
	}
	enWords := len(strings.Fields(reHan.ReplaceAllString(content, " ")))
	if cnCount > enWords {
		return cnCount
	}
	return enWords
}

// splitSentences 分句。
func splitSentences(content string) []string {
	content = strings.ReplaceAll(content, "。", "。\n")
	content = strings.ReplaceAll(content, "！", "！\n")
	content = strings.ReplaceAll(content, "？", "？\n")
	content = strings.ReplaceAll(content, ". ", ".\n")
	content = strings.ReplaceAll(content, "! ", "!\n")
	content = strings.ReplaceAll(content, "? ", "?\n")
	parts := strings.Split(content, "\n")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
