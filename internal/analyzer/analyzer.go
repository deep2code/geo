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

// 检索友好度参数（与 scorer.go 保持一致）。
const (
	retrievalFullWordMin = 300  // 词数达标下限
	retrievalFullWordMax = 2000 // 词数达标上限
)

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

	// 检索友好度信号（SAGEO Arena 2026）
	analysis.RetrievalSignals = calcRetrievalSignals(content, analysis)

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

// tokenizeRetrieval 检索信号分词：英文按空白/整词，CJK 按单字切分。
// strings.Fields 对无空格分隔的中文内容只能得到极少"词块"，
// 导致长度/密度/多样性信号全部失真（中文文章被判"长度不达标"）。
func tokenizeRetrieval(content string) []string {
	var out []string
	for _, tok := range strings.Fields(content) {
		if !reHan.MatchString(tok) {
			out = append(out, tok)
			continue
		}
		var buf strings.Builder
		flush := func() {
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
		}
		for _, r := range tok {
			if r >= 0x4e00 && r <= 0x9fff {
				flush()
				out = append(out, string(r))
			} else {
				buf.WriteRune(r)
			}
		}
		flush()
	}
	return out
}

// calcRetrievalSignals 计算检索友好度信号。
//
// 基于 SAGEO Arena (2026) 研究发现：AutoGEO 的内容扩写策略导致检索排名下降 22.35，
// 原因是关键词密度稀释和语义漂移。此函数评估内容对 BM25/神经检索器的友好程度。
func calcRetrievalSignals(content string, a *models.ContentAnalysis) *models.RetrievalSignals {
	words := tokenizeRetrieval(content)
	wordCount := len(words)
	if wordCount == 0 {
		return &models.RetrievalSignals{RetrievalScore: 0}
	}
	cjkCount := 0
	for _, w := range words {
		if reHan.MatchString(w) {
			cjkCount++
		}
	}
	// CJK 占多数的内容：按字切分后无法做英文式"实体词保留"检测（该信号为英文设计）
	isCJKDominant := cjkCount*2 > wordCount

	// 1. 关键词密度：高频词占比（模拟 BM25 的词频信号）
	termFreq := make(map[string]int)
	for _, w := range words {
		lower := strings.ToLower(w)
		runes := utf8.RuneCountInString(lower)
		if runes > 1 || (runes == 1 && reHan.MatchString(lower)) { // 过滤单字符（CJK 单字除外）
			termFreq[lower]++
		}
	}
	maxFreq := 0
	for _, f := range termFreq {
		if f > maxFreq {
			maxFreq = f
		}
	}
	// 关键词密度 = 最高频词出现次数 / 总词数（健康区间 0.01-0.05）
	kwDensity := float64(maxFreq) / float64(wordCount)

	// 2. 内容长度是否在检索友好区间（口径与 a.WordCount 的中英文感知计数一致）
	contentLengthOK := wordCount >= retrievalFullWordMin && wordCount <= retrievalFullWordMax

	// 3. 无语义漂移：检查关键实体词是否保留（通过实体词占比估算）
	//    保留词 = 技术术语 + 数字 + 专有名词（首字母大写）
	preservedCount := 0
	for _, w := range words {
		runes := []rune(w)
		if len(runes) == 0 {
			continue
		}
		// 数字
		if runes[0] >= '0' && runes[0] <= '9' {
			preservedCount++
			continue
		}
		// 技术术语（全大写缩写）
		if reTechnical.MatchString(w) {
			preservedCount++
			continue
		}
		// 首字母大写（英文实体词）
		if runes[0] >= 'A' && runes[0] <= 'Z' && len(runes) > 1 {
			preservedCount++
		}
	}
	// 保留率 > 5% 视为无语义漂移；CJK 为主的内容该信号不适用，视为无漂移
	noSemanticDrift := float64(preservedCount)/float64(wordCount) > 0.05 || isCJKDominant

	// 4. 术语重叠得分：基于词汇多样性（Type-Token Ratio）
	uniqueWords := make(map[string]bool)
	for _, w := range words {
		uniqueWords[strings.ToLower(w)] = true
	}
	termOverlap := float64(len(uniqueWords)) / float64(wordCount)

	// 5. 综合检索友好度评分（0-100）
	score := 0.0
	// 关键词密度得分（0-25）：0.01-0.05 为最佳区间
	if kwDensity >= 0.01 && kwDensity <= 0.05 {
		score += 25
	} else if kwDensity > 0.05 {
		score += max(0, 25-200*(kwDensity-0.05)) // 过密扣分
	} else {
		score += kwDensity * 2500 // 过稀扣分
	}
	// 内容长度得分（0-25）：区间内满分；偏短线性；超长线性衰减
	//（原实现超长饱和为满分，与"过长稀释关键词密度"的建议文案相矛盾）
	if contentLengthOK {
		score += 25
	} else if wordCount < retrievalFullWordMin {
		score += max(0, 25*float64(wordCount)/float64(retrievalFullWordMin))
	} else {
		over := float64(wordCount - retrievalFullWordMax)
		score += max(0, 25*(1-over/float64(retrievalFullWordMax)))
	}
	// 语义漂移得分（0-25）
	if noSemanticDrift {
		score += 25
	}
	// 词汇多样性得分（0-25）
	score += min(25, termOverlap*50)

	return &models.RetrievalSignals{
		KeywordDensity:   kwDensity,
		ContentLengthOK:  contentLengthOK,
		NoSemanticDrift:  noSemanticDrift,
		TermOverlapScore: termOverlap,
		RetrievalScore:   min(100, max(0, score)),
	}
}
