// Package market 提供多语言/多市场可见度审计支持。
//
// 同一品牌在不同语言/不同市场（英语市场 vs 日语市场 vs 中文市场）的可见度
// 差异巨大：用户向 AI 提问时使用的语言不同，引擎返回结果也不同。本包负责：
//   - 维护支持的市场列表（cn/us/jp/kr/de/fr/global）及各市场主流 AI 引擎
//   - 将中文查询词本地化（翻译/改写）为目标市场语言
//
// 翻译策略：
//  1. LLM 翻译（首选）：若 llmMgr 可用，发 prompt 让 LLM 批量翻译
//  2. 预设映射表（兜底）：内置常见查询词模板的翻译（zh→en/ja/ko/de/fr）
//
// 纯标准库实现，不引入任何翻译 SDK。
package market

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"my-geo/internal/llm"
)

// Market 市场定义：一个地理/语言市场及其主流 AI 引擎。
type Market struct {
	// 市场代码：cn/us/jp/kr/de/fr/global。
	Code string `json:"code"`
	// 市场中文名：中国大陆/美国/日本/韩国/德国/法国/全球。
	Name string `json:"name"`
	// 该市场主要语言代码列表，如 ["en"] / ["ja"] / ["zh"]。
	Languages []string `json:"languages"`
	// 该市场主流 AI 引擎（用于过滤），nil 表示不过滤。
	Engines []string `json:"engines,omitempty"`
}

// MarketConfig 市场+语言组合配置，作为审计请求的本地化参数。
type MarketConfig struct {
	// 市场代码（cn/us/jp/kr/de/fr/global）。
	Market string `json:"market"`
	// 语言代码（zh/en/ja/ko/de/fr）。
	Language string `json:"language"`
	// 是否翻译查询词（语言非中文时一般为 true）。
	Translate bool `json:"translate"`
}

// markets 所有支持的市场列表。
//
// Engines 字段用于"按市场过滤引擎"场景（前端可据此默认勾选该市场主流引擎）；
// nil 表示该市场不过滤（如 global）。
var markets = []Market{
	{
		Code:      "cn",
		Name:      "中国大陆",
		Languages: []string{"zh"},
		Engines:   []string{"qwen", "glm", "deepseek", "kimi", "wenxin", "doubao", "xiaomi", "xunfei", "yuanbao"},
	},
	{
		Code:      "us",
		Name:      "美国",
		Languages: []string{"en"},
		Engines:   []string{"chatgpt", "perplexity", "gemini", "claude"},
	},
	{
		Code:      "jp",
		Name:      "日本",
		Languages: []string{"ja"},
		Engines:   []string{"chatgpt", "gemini", "claude"},
	},
	{
		Code:      "kr",
		Name:      "韩国",
		Languages: []string{"ko"},
		Engines:   []string{"chatgpt", "gemini", "claude"},
	},
	{
		Code:      "de",
		Name:      "德国",
		Languages: []string{"de"},
		Engines:   []string{"chatgpt", "perplexity", "gemini", "claude"},
	},
	{
		Code:      "fr",
		Name:      "法国",
		Languages: []string{"fr"},
		Engines:   []string{"chatgpt", "perplexity", "gemini", "claude"},
	},
	{
		Code:      "global",
		Name:      "全球",
		Languages: []string{"en", "zh", "ja"},
		Engines:   nil, // 不过滤引擎
	},
}

// SupportedMarkets 返回所有支持的市场列表（深拷贝，调用方可安全修改——
// 浅拷贝的 slice 字段仍共享底层数组，调用方改 Engines 会污染包级全局配置）。
func SupportedMarkets() []Market {
	out := make([]Market, len(markets))
	for i, m := range markets {
		out[i] = m
		if m.Languages != nil {
			out[i].Languages = append([]string(nil), m.Languages...)
		}
		if m.Engines != nil {
			out[i].Engines = append([]string(nil), m.Engines...)
		}
	}
	return out
}

// FindMarket 按市场代码查找市场定义，未找到返回 nil。
func FindMarket(code string) *Market {
	for i := range markets {
		if markets[i].Code == code {
			return &markets[i]
		}
	}
	return nil
}

// langNames 语言代码 → 目标语言本地名（用于 LLM 提示词与前端展示）。
var langNames = map[string]string{
	"zh": "中文",
	"en": "English",
	"ja": "日本語",
	"ko": "한국어",
	"de": "Deutsch",
	"fr": "Français",
}

// LangName 返回语言代码对应的目标语言本地名，未知返回代码本身。
func LangName(code string) string {
	if name, ok := langNames[code]; ok {
		return name
	}
	return code
}

// LocalizePrompts 将查询词本地化（翻译/改写）为目标市场语言。
//
// 优先使用 LLM 翻译（若 llmMgr 可用且 HasAvailable）；
// LLM 不可用或失败时退化为预设映射表（覆盖最常见的中文查询词模板）。
// lang 为空或 "zh" 时直接返回原 prompts（无需翻译）。
//
// 返回的切片长度与输入一致并保持顺序；单条无法翻译时保留原文（best-effort）。
func LocalizePrompts(ctx context.Context, prompts []string, lang string, llmMgr *llm.Manager) ([]string, error) {
	if len(prompts) == 0 {
		return prompts, nil
	}
	// 中文/空语言无需翻译
	if lang == "" || lang == "zh" {
		return prompts, nil
	}

	// 1. 首选 LLM 翻译
	if llmMgr != nil && llmMgr.HasAvailable() {
		if out, err := localizeByLLM(ctx, prompts, lang, llmMgr); err == nil && len(out) == len(prompts) {
			return out, nil
		}
		// LLM 失败则继续走预设映射表兜底
	}

	// 2. 预设映射表兜底
	out := make([]string, len(prompts))
	for i, p := range prompts {
		if translated, ok := translateByPreset(p, lang); ok {
			out[i] = translated
		} else {
			// 预设表未覆盖，保留原文（避免空查询词）
			out[i] = p
		}
	}
	return out, nil
}

// localizeByLLM 用 LLM 批量翻译查询词。
//
// 让 LLM 输出 JSON 字符串数组，保持顺序与长度一致。
func localizeByLLM(ctx context.Context, prompts []string, lang string, llmMgr *llm.Manager) ([]string, error) {
	langName := LangName(lang)
	input, _ := json.Marshal(prompts)
	prompt := fmt.Sprintf(`你是专业的搜索查询词翻译专家。请将下方 JSON 数组中的每个中文查询词翻译为%s（语言代码 %s），
保持原有搜索意图，符合目标语言用户在 AI 搜索引擎中的自然提问习惯（不要逐字直译）。
品牌名/产品名/公司名等专有名词保留原文不翻译。
仅输出一个 JSON 字符串数组，长度和顺序与输入完全一致，不要任何解释或 markdown 标记。
输入数组：
%s`, langName, lang, string(input))
	raw, err := llmMgr.Rewrite(ctx, prompt, string(input))
	if err != nil {
		return nil, err
	}
	arr, err := parseStringArray(raw)
	if err != nil {
		return nil, err
	}
	return arr, nil
}

// parseStringArray 从可能含 markdown 代码块的文本中提取 JSON 字符串数组。
func parseStringArray(raw string) ([]string, error) {
	s := extractJSON(raw)
	if s == "" {
		// 退而求其次：直接找第一个 [ 到最后一个 ]
		start := strings.Index(raw, "[")
		end := strings.LastIndex(raw, "]")
		if start >= 0 && end > start {
			s = raw[start : end+1]
		}
	}
	if s == "" {
		return nil, fmt.Errorf("LLM 返回中未找到 JSON 数组")
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, fmt.Errorf("解析 JSON 数组失败: %w", err)
	}
	return arr, nil
}

// extractJSON 从可能包含 markdown 代码块的文本中提取 JSON 片段。
func extractJSON(s string) string {
	if start := strings.Index(s, "```json"); start >= 0 {
		start += len("```json")
		if end := strings.Index(s[start:], "```"); end > 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if start := strings.Index(s, "```"); start >= 0 {
		start += len("```")
		// 跳过可能的语言标记行
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(s[start:], "```"); end > 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	return ""
}

// ---------- 预设映射表兜底 ----------
//
// presetRule 一条查询词模板翻译规则。
//
//	Pattern  含一个通配符 "*"，匹配中文模板中的可变部分（行业/品类/品牌名等）
//	Template 翻译后模板，用 {{X}} 占位符替换通配符内容
//
// 匹配时按列表顺序，命中第一条即返回；因此更具体的规则需排在前面。
type presetRule struct {
	Pattern  string
	Template string
}

// presetTable 预设翻译映射表：lang → 规则列表。
//
// 仅覆盖最常见的中文查询词模板（不要求全覆盖）；未命中则保留原文。
var presetTable = map[string][]presetRule{
	"en": {
		// 行业类
		{Pattern: "推荐几个*行业的知名厂商", Template: "top {{X}} companies"},
		{Pattern: "推荐*行业的知名厂商", Template: "top {{X}} companies"},
		{Pattern: "最好的*公司", Template: "best {{X}} companies"},
		{Pattern: "最好的*软件", Template: "best {{X}} software"},
		{Pattern: "最好的*工具", Template: "best {{X}} tools"},
		{Pattern: "最好的*平台", Template: "best {{X}} platforms"},
		// 品牌类
		{Pattern: "*怎么样？", Template: "{{X}} review"},
		{Pattern: "*怎么样", Template: "{{X}} review"},
		{Pattern: "*产品介绍", Template: "{{X}} product overview"},
		{Pattern: "*和竞品对比", Template: "{{X}} vs competitors"},
		{Pattern: "*和其他同类产品有什么区别", Template: "{{X}} vs alternatives"},
		{Pattern: "*和类似的公司有哪些", Template: "companies like {{X}}"},
		{Pattern: "和*类似的公司", Template: "companies like {{X}}"},
		// 品类类
		{Pattern: "*软件哪个好？", Template: "which {{X}} software is the best"},
		{Pattern: "*软件哪个好", Template: "which {{X}} software is the best"},
		{Pattern: "推荐几款好用的*工具", Template: "recommended {{X}} tools"},
		{Pattern: "推荐几款*工具", Template: "recommended {{X}} tools"},
	},
	"ja": {
		{Pattern: "推荐几个*行业的知名厂商", Template: "{{X}}業界の有名企業を教えて"},
		{Pattern: "推荐*行业的知名厂商", Template: "{{X}}業界の有名企業を教えて"},
		{Pattern: "最好的*公司", Template: "最高の{{X}}企業"},
		{Pattern: "最好的*软件", Template: "最高の{{X}}ソフトウェア"},
		{Pattern: "最好的*工具", Template: "最高の{{X}}ツール"},
		{Pattern: "*怎么样？", Template: "{{X}}の評価は？"},
		{Pattern: "*怎么样", Template: "{{X}}の評価は？"},
		{Pattern: "*产品介绍", Template: "{{X}}製品紹介"},
		{Pattern: "*和竞品对比", Template: "{{X}}と競合の比較"},
		{Pattern: "*软件哪个好？", Template: "{{X}}ソフトウェアどれがいい？"},
		{Pattern: "推荐几款好用的*工具", Template: "おすすめの{{X}}ツール"},
	},
	"ko": {
		{Pattern: "推荐几个*行业的知名厂商", Template: "{{X}} 업계 유명 기업 추천"},
		{Pattern: "最好的*公司", Template: "최고의 {{X}} 기업"},
		{Pattern: "最好的*软件", Template: "최고의 {{X}} 소프트웨어"},
		{Pattern: "*怎么样？", Template: "{{X}} 후기"},
		{Pattern: "*怎么样", Template: "{{X}} 후기"},
		{Pattern: "*产品介绍", Template: "{{X}} 제품 소개"},
	},
	"de": {
		{Pattern: "推荐几个*行业的知名厂商", Template: "top {{X}} unternehmen"},
		{Pattern: "最好的*软件", Template: "beste {{X}} software"},
		{Pattern: "*怎么样？", Template: "{{X}} erfahrungen"},
		{Pattern: "*产品介绍", Template: "{{X}} produktübersicht"},
	},
	"fr": {
		{Pattern: "推荐几个*行业的知名厂商", Template: "meilleures entreprises {{X}}"},
		{Pattern: "最好的*软件", Template: "meilleur logiciel {{X}}"},
		{Pattern: "*怎么样？", Template: "avis {{X}}"},
		{Pattern: "*产品介绍", Template: "présentation produit {{X}}"},
	},
}

// translateByPreset 用预设映射表翻译单条查询词。
// 返回 (翻译结果, 是否命中预设表)；未命中返回 (原, false)。
func translateByPreset(prompt, lang string) (string, bool) {
	rules, ok := presetTable[lang]
	if !ok {
		return prompt, false
	}
	trimmed := strings.TrimSpace(prompt)
	for _, r := range rules {
		if placeholder, ok := matchWildcard(r.Pattern, trimmed); ok {
			return strings.ReplaceAll(r.Template, "{{X}}", placeholder), true
		}
	}
	return prompt, false
}

// matchWildcard 单通配符模式匹配。
//
// pattern 含且仅含一个 "*"；将其拆为 prefix+suffix，
// s 须以 prefix 开头、以 suffix 结尾，且中间部分非空（len(s) >= len(prefix)+len(suffix)）。
// 命中返回 (中间内容, true)。
//
// 例：matchWildcard("最好的*软件", "最好的CRM软件") → ("CRM", true)
func matchWildcard(pattern, s string) (string, bool) {
	idx := strings.Index(pattern, "*")
	if idx < 0 || strings.Index(pattern[idx+1:], "*") >= 0 {
		// 不含通配符或含多个通配符：要求完全相等
		if pattern == s {
			return "", true
		}
		return "", false
	}
	prefix := pattern[:idx]
	suffix := pattern[idx+1:]
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	if !strings.HasSuffix(s, suffix) {
		return "", false
	}
	// 边界检查必须在切片之前：prefix/suffix 在 s 中字节级重叠时（如
	// pattern="a*b"、s="ab"）先切片会直接 slice bounds panic。
	// 且要求中间部分非空（严格大于），空匹配（"最好的软件" 命中 "最好的*软件"）
	// 不视为命中——否则产出双空格且品牌名丢失的残缺查询词。
	if len(s) <= len(prefix)+len(suffix) {
		return "", false
	}
	middle := s[len(prefix) : len(s)-len(suffix)]
	return middle, true
}
