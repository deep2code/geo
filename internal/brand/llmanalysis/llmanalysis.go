// Package llmanalysis 提供基于 LLM 的品牌信号智能判定层。
//
// 它把 monitor/analyzer 中原本用"关键词词典法"完成的工作（情感倾向、竞品识别、
// 引用源识别、品牌准确性/幻觉检测）升级为一次 LLM 推理判定。相比词典法：
//   - 情感能理解上下文与反讽（如"贵得有道理"=正面）
//   - 竞品识别能理解语义等价（如"和某产品类似"）
//   - 引用源识别能理解回答到底"采信了谁"，而非仅正则抽 URL
//   - 准确性检测能把 AI 回答与已核验事实比对，标记矛盾/编造
//
// 设计原则：
//   - 复用 my-geo/internal/adapter 的统一适配器作为判定模型（judge），零额外依赖
//   - 判定模型未配置（或调用失败/解析失败）时，自动降级到词典法，保证系统可用
//   - 所有判定返回置信度，便于上层做加权与人工复核
//   - 仅依赖标准库 + adapter/models/knowledge 三个下游包，无导入环
package llmanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/models"
)

// Analyzer LLM 判定层。
//
// judge 为判定模型适配器（建议用强推理模型，如 deepseek / chatgpt）；
// 为 nil 或 judge.Configured()==false 时，所有方法自动降级到词典法。
type Analyzer struct {
	judge    adapter.Adapter
	enabled  bool
	timeout  time.Duration
}

// New 创建 LLM 判定层。judge 可为 nil（此时全部降级）。
func New(judge adapter.Adapter) *Analyzer {
	a := &Analyzer{judge: judge, timeout: 30 * time.Second}
	a.enabled = judge != nil && judge.Configured()
	return a
}

// Enabled 是否启用了 LLM 判定（判定模型已配置）。
func (a *Analyzer) Enabled() bool { return a.enabled }

// SetTimeout 设置判定调用超时（默认 30s）。
func (a *Analyzer) SetTimeout(d time.Duration) *Analyzer {
	if d > 0 {
		a.timeout = d
	}
	return a
}

// Fact 一条已核验的品牌事实，用于准确性/幻觉检测。
//
// 来源可以是知识库（knowledge 包）、官方资料、或用户手动录入的"品牌真相"。
// 判定层会把 AI 回答与这些事实比对，标记矛盾或编造。
type Fact struct {
	Statement string `json:"statement"`           // 事实陈述（如"GEO 成立于 2021 年"）
	Source    string `json:"source,omitempty"`    // 证据来源（可选，用于报告展示）
	Confidence float64 `json:"confidence,omitempty"` // 事实自身置信度 0-1（可选）
}

// AccuracyFlag 一条准确性标记（AI 回答与已核验事实的关系）。
type AccuracyFlag struct {
	Type     string `json:"type"`     // contradict / unsupported / hallucination / consistent
	Statement string `json:"statement"` // 相关事实陈述
	Excerpt  string `json:"excerpt,omitempty"` // AI 回答中相关的原文片段
	Detail   string `json:"detail"`   // 说明
	Severity string `json:"severity"` // critical / high / medium / low
}

// SourceClaim 一次 LLM 识别出的"回答采信的实体/来源"。
type SourceClaim struct {
	Name     string `json:"name"`     // 被采信的实体名（公司/媒体/个人/文档）
	URL      string `json:"url,omitempty"` // 相关 URL（若有）
	Kind     string `json:"kind,omitempty"` // review_site / media / docs / brand / other
	Cited    bool   `json:"cited"`    // 是否以引用形式出现
	Reason   string `json:"reason,omitempty"`
}

// Sentiment 判定品牌在给定文本中的情感倾向。
//
// 返回 label(positive/neutral/negative)、判定理由、置信度 0-1。
// 判定模型不可用或解析失败时降级到 fallbackSentiment（词典法）。
func (a *Analyzer) Sentiment(ctx context.Context, brandName string, aliases []string, answer string) (label, reason string, conf float64) {
	label, reason, conf = fallbackSentiment(brandName, aliases, answer)
	if !a.enabled {
		return label, reason, conf
	}
	prompt := buildSentimentPrompt(brandName, aliases, answer)
	raw, err := a.ask(ctx, prompt)
	if err != nil {
		return label, reason, conf // 降级
	}
	var r struct {
		Label   string  `json:"label"`
		Reason  string  `json:"reason"`
		Conf    float64 `json:"confidence"`
	}
	if err := parseJSON(raw, &r); err != nil || r.Label == "" {
		return label, reason, conf // 降级
	}
	l := normalizeLabel(r.Label)
	if l == "" {
		return label, reason, conf
	}
	c := r.Conf
	if c <= 0 || c > 1 {
		c = 0.6
	}
	return l, r.Reason, c
}

// ExtractSources 识别 AI 回答到底采信了哪些实体/来源。
//
// 相比 topsource 包的正则 URL 提取，这里用 LLM 语义理解"回答采信了谁"。
// 降级到 fallbackExtractSources（仅抽 URL）。
func (a *Analyzer) ExtractSources(ctx context.Context, answer string, brandDomain string) ([]SourceClaim, error) {
	if !a.enabled {
		return fallbackExtractSources(answer), nil
	}
	prompt := buildSourcePrompt(answer, brandDomain)
	raw, err := a.ask(ctx, prompt)
	if err != nil {
		return fallbackExtractSources(answer), nil
	}
	var r struct {
		Sources []SourceClaim `json:"sources"`
	}
	if err := parseJSON(raw, &r); err != nil {
		return fallbackExtractSources(answer), nil
	}
	if len(r.Sources) == 0 {
		return fallbackExtractSources(answer), nil
	}
	return r.Sources, nil
}

// Accuracy 把 AI 回答与已核验事实比对，检测矛盾/编造/无依据。
//
// 无 facts 时返回 nil。判定模型不可用时降级为基于关键词的比对（弱）。
func (a *Analyzer) Accuracy(ctx context.Context, brandName string, answer string, facts []Fact) ([]AccuracyFlag, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	if !a.enabled {
		return fallbackAccuracy(brandName, answer, facts), nil
	}
	prompt := buildAccuracyPrompt(brandName, answer, facts)
	raw, err := a.ask(ctx, prompt)
	if err != nil {
		return fallbackAccuracy(brandName, answer, facts), nil
	}
	var r struct {
		Flags []AccuracyFlag `json:"flags"`
	}
	if err := parseJSON(raw, &r); err != nil {
		return fallbackAccuracy(brandName, answer, facts), nil
	}
	for i := range r.Flags {
		if r.Flags[i].Severity == "" {
			r.Flags[i].Severity = "medium"
		}
	}
	return r.Flags, nil
}

// ask 调用判定模型，带超时控制，返回去空白后的原始回答。
func (a *Analyzer) ask(ctx context.Context, prompt string) (string, error) {
	if a.judge == nil {
		return "", fmt.Errorf("判定模型未配置")
	}
	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	resp, err := a.judge.Query(cctx, prompt)
	if err != nil {
		return "", err
	}
	ans := strings.TrimSpace(resp.Answer)
	// 去掉常见代码围栏（```json ... ```）
	if strings.HasPrefix(ans, "```") {
		if idx := strings.Index(ans, "\n"); idx >= 0 {
			ans = ans[idx+1:]
		}
		ans = strings.TrimSuffix(strings.TrimSpace(ans), "```")
	}
	return strings.TrimSpace(ans), nil
}

// ---------- Prompt 构造 ----------

func buildSentimentPrompt(brandName string, aliases []string, answer string) string {
	names := append([]string{brandName}, aliases...)
	return fmt.Sprintf(`你是品牌情感分析专家。判断下面这段 AI 回答中关于品牌「%s」的情感倾向。
品牌别名：%s

回答：
"""
%s
"""

请只返回一个 JSON 对象（不要任何解释文字）：
{"label":"positive|neutral|negative","reason":"一句话理由","confidence":0.0到1.0之间的数字}`,
		brandName, strings.Join(names, "、"), truncate(answer, 4000))
}

func buildSourcePrompt(answer string, brandDomain string) string {
	return fmt.Sprintf(`你是引用源分析专家。请分析下面这段 AI 回答到底"采信/引用了"哪些实体或来源（公司、媒体、评测站、文档、个人等）。
忽略品牌自身官网（%s）。

回答：
"""
%s
"""

请只返回一个 JSON 对象：
{"sources":[{"name":"实体名","url":"相关链接(可选)","kind":"review_site|media|docs|brand|other","cited":true或false,"reason":"为何认为回答采信了它"}]}`,
		brandDomain, truncate(answer, 4000))
}

func buildAccuracyPrompt(brandName string, answer string, facts []Fact) string {
	b, _ := json.Marshal(facts)
	return fmt.Sprintf(`你是事实核查专家。把下面这段关于品牌「%s」的 AI 回答与"已核验事实清单"比对，找出矛盾、无依据声称、或编造内容。

已核验事实清单：
%s

AI 回答：
"""
%s
"""

请只返回一个 JSON 对象：
{"flags":[{"type":"contradict|unsupported|hallucination|consistent","statement":"相关事实陈述","excerpt":"回答中相关原文(可选)","detail":"说明","severity":"critical|high|medium|low"}]}`,
		brandName, string(b), truncate(answer, 4000))
}

// ---------- 降级（词典法） ----------

func fallbackSentiment(brandName string, aliases []string, text string) (string, string, float64) {
	lower := strings.ToLower(text)
	names := append([]string{brandName}, aliases...)
	posScore, negScore := 0, 0
	posWords := []string{"best", "top", "leading", "recommend", "recommended", "excellent", "great", "popular", "trusted", "reliable", "powerful", "innovative", "优秀", "领先", "推荐", "最佳", "首选", "知名", "值得信赖", "强大", "创新", "权威", "出色"}
	negWords := []string{"worst", "avoid", "poor", "bad", "expensive", "limited", "outdated", "buggy", "slow", "complaint", "issue", "problem", "controversy", "差", "糟糕", "避免", "投诉", "过时", "昂贵", "局限", "负面"}
	for _, w := range posWords {
		posScore += strings.Count(lower, w)
	}
	for _, w := range negWords {
		negScore += strings.Count(lower, w)
	}
	mentioned := false
	for _, n := range names {
		if n != "" && strings.Contains(lower, strings.ToLower(n)) {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return "neutral", "未提及品牌", 0.5
	}
	if negScore > posScore {
		return "negative", "命中负面关键词较多", 0.6
	}
	if posScore > negScore && posScore > 0 {
		return "positive", "命中正面关键词较多", 0.6
	}
	return "neutral", "未命中明显情感关键词", 0.5
}

var urlFallbackRE = regexp.MustCompile(`https?://[^\s\)\]\"'<>，。；：、]+`)

func fallbackExtractSources(answer string) []SourceClaim {
	matches := urlFallbackRE.FindAllString(answer, -1)
	seen := map[string]bool{}
	var out []SourceClaim
	for _, raw := range matches {
		u := strings.TrimRight(raw, ".,;:!?)\"'】〉》」』")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, SourceClaim{Name: u, URL: u, Kind: "other", Cited: true})
	}
	return out
}

func fallbackAccuracy(brandName string, answer string, facts []Fact) []AccuracyFlag {
	lower := strings.ToLower(answer)
	var flags []AccuracyFlag
	for _, f := range facts {
		// 按注释意图实现弱信号判定：
		//   - 事实陈述中的数字出现在回答中 → consistent（弱）
		//   - 事实含数字但回答中找不到该数字 → unsupported（弱）
		//   - 事实不含数字 → 无法判定，跳过
		// 此前实现只看品牌名出现与否就给所有事实标 consistent，幻觉回答只要
		// 提到品牌名就会全部误报"一致"。
		nums := numberRE.FindAllString(f.Statement, -1)
		if len(nums) == 0 {
			continue
		}
		allFound := true
		for _, n := range nums {
			if !strings.Contains(lower, n) {
				allFound = false
				break
			}
		}
		if allFound {
			flags = append(flags, AccuracyFlag{
				Type: "consistent", Statement: f.Statement,
				Detail: "事实数字出现在回答中（弱信号，未做语义比对）", Severity: "low",
			})
		} else {
			flags = append(flags, AccuracyFlag{
				Type: "unsupported", Statement: f.Statement,
				Detail: "事实中的数字未出现在回答中（弱信号，未做语义比对）", Severity: "medium",
			})
		}
	}
	return flags
}

// numberRE 提取事实陈述中的数字（用于降级路径的弱比对）。
var numberRE = regexp.MustCompile(`\d+(?:\.\d+)?`)

// ---------- 工具 ----------

func normalizeLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "pos"):
		return "positive"
	case strings.HasPrefix(s, "neg"):
		return "negative"
	case strings.HasPrefix(s, "neu"):
		return "neutral"
	}
	return ""
}

var jsonObjRE = regexp.MustCompile(`\{.*\}`)

// parseJSON 从模型回答中提取第一个 JSON 对象并解析到 v。
func parseJSON(raw string, v any) error {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		m := jsonObjRE.FindString(raw)
		if m == "" {
			return fmt.Errorf("无 JSON 对象")
		}
		raw = m
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(v)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "...(截断)"
}

// JudgeInfo 返回判定模型状态（供前端/报告展示）。
func (a *Analyzer) JudgeInfo() (engine models.EngineType, configured bool) {
	if a.judge == nil {
		return "", false
	}
	return a.judge.Engine(), a.judge.Configured()
}
