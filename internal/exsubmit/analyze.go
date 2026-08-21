// Package exsubmit 的子文件：回答结构化抽取（analyze.go）。
//
// 复用项目统一的适配器（adapter.Adapter）作为判定模型（judge），对大模型回答做：
//   - 情感倾向（positive / neutral / negative）
//   - 主题分类
//   - 中文摘要
//   - 被提及的实体（品牌 / 产品 / 机构 / 人物）
//   - 回答内引用的来源域名（正则抽取 + 域名分类，零 LLM 依赖，必做）
//
// 判定模型未配置或调用失败时，自动降级到词典/启发式，保证系统始终可用。
package exsubmit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"my-geo/internal/adapter"
	"my-geo/internal/brand/sourcedomain"
)

// Analysis 结构化分析结果。
type Analysis struct {
	Sentiment       string            `json:"sentiment"`        // positive / neutral / negative
	SentimentConf   float64           `json:"sentiment_conf"`   // 0-1
	Category        string            `json:"category"`         // 主题分类
	Summary         string            `json:"summary"`          // 摘要
	Mentions        []string          `json:"mentions"`         // 被提及实体
	SourceDomains   []string          `json:"source_domains"`   // 回答内引用域名
	SourceCategories map[string]string `json:"source_categories"` // domain -> category
}

// urlRe 粗略匹配 http(s) 链接（回答中常被引号/括号内包裹）。
var urlRe = regexp.MustCompile(`https?://[^\s"'<>()]+`)

// Analyzer 回答结构化抽取器。
//
// judge 为判定模型适配器（建议强推理模型）；为 nil 或 judge.Configured()==false 时，
// 情感/主题/实体自动降级，来源域名仍从文本正则抽取。
type Analyzer struct {
	judge   adapter.Adapter
	enabled bool
	timeout time.Duration
}

// NewAnalyzer 创建抽取器。judge 可为 nil（此时全部降级）。
func NewAnalyzer(judge adapter.Adapter) *Analyzer {
	a := &Analyzer{judge: judge, timeout: 30 * time.Second}
	a.enabled = judge != nil && judge.Configured()
	return a
}

// Enabled 是否启用了 LLM 判定（判定模型已配置）。
func (a *Analyzer) Enabled() bool { return a.enabled }

// Analyze 对一段回答做结构化抽取。无论 LLM 是否可用都会返回结果（降级兜底）。
func (a *Analyzer) Analyze(ctx context.Context, answer string) (*Analysis, error) {
	domains, cats := extractURLDomains(answer)
	res := &Analysis{
		Sentiment:        "neutral",
		SentimentConf:    0.5,
		Category:         "未分类",
		Summary:          truncate(answer, 200),
		Mentions:         []string{},
		SourceDomains:    domains,
		SourceCategories: cats,
	}
	if !a.enabled {
		return res, nil
	}
	prompt := buildAnalyzePrompt(answer)
	raw, err := a.ask(ctx, prompt)
	if err != nil {
		return res, nil // 降级：返回启发式结果
	}
	var r struct {
		Sentiment     string   `json:"sentiment"`
		Confidence    float64  `json:"confidence"`
		Category      string   `json:"category"`
		Summary       string   `json:"summary"`
		Mentions      []string `json:"mentions"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return res, nil
	}
	if normalizeSentiment(r.Sentiment) != "" {
		res.Sentiment = normalizeSentiment(r.Sentiment)
	}
	if r.Confidence > 0 && r.Confidence <= 1 {
		res.SentimentConf = r.Confidence
	}
	if strings.TrimSpace(r.Category) != "" {
		res.Category = strings.TrimSpace(r.Category)
	}
	if strings.TrimSpace(r.Summary) != "" {
		res.Summary = truncate(r.Summary, 500)
	}
	if len(r.Mentions) > 0 {
		res.Mentions = dedupe(r.Mentions)
	}
	return res, nil
}

// ask 调用判定模型，带超时与代码围栏剥离。
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
	if strings.HasPrefix(ans, "```") {
		if idx := strings.Index(ans, "\n"); idx >= 0 {
			ans = ans[idx+1:]
		}
		ans = strings.TrimSuffix(strings.TrimSpace(ans), "```")
	}
	return strings.TrimSpace(ans), nil
}

// extractURLDomains 从文本中抽取所有 URL 的规范化域名及分类（去重）。
func extractURLDomains(text string) ([]string, map[string]string) {
	matches := urlRe.FindAllString(text, -1)
	seen := map[string]string{}
	for _, m := range matches {
		d := sourcedomain.ExtractDomain(m)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; !ok {
			seen[d] = sourcedomain.CategorizeDomain(d)
		}
	}
	domains := make([]string, 0, len(seen))
	for d := range seen {
		domains = append(domains, d)
	}
	return domains, seen
}

func buildAnalyzePrompt(answer string) string {
	return fmt.Sprintf(`你是内容分析助手。请分析下面这段由大模型产出的「回答」，提取结构化信息。

回答内容：
"""
%s
"""

请只返回一个 JSON 对象（不要任何解释文字）：
{
  "sentiment": "positive|neutral|negative",
  "confidence": 0.0到1.0之间的数字,
  "category": "一句话主题分类，如 科技/医疗/金融/教育/编程/其他",
  "summary": "不超过 120 字的中文摘要",
  "mentions": ["被提及的品牌、产品、机构或人物实体（数组）"]
}`, truncate(answer, 6000))
}

func normalizeSentiment(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "positive", "pos", "正面", "positive.":
		return "positive"
	case "negative", "neg", "负面", "negative.":
		return "negative"
	case "neutral", "neu", "中性", "neutral.":
		return "neutral"
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
