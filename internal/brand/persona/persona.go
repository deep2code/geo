// Package persona 提供"买家人设分群测量"能力（P1-c）。
//
// 成熟 GEO 产品（如 Gumshoe）的差异化卖点：不仅给"品牌级平均分"，还回答
// "哪类买家在 AI 里看不见你"。本包按买家人设聚合可见度/情感/位置，定位
// 特定人群的内容盲点。
//
// 为避免与 brand 包形成导入环，本包只依赖标准库，使用轻量自有结果类型；
// brand 包负责把 PromptResult 转换为 PersonaResult 后调用本包。
package persona

import (
	"sort"
	"strings"

	"my-geo/internal/models"
)

// Persona 一个买家人设（目标受众切片）。
type Persona struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`        // 人设名，如"技术决策者 CTO"
	Description string   `json:"description,omitempty"`
	// 该人设典型会问的查询词（与 BrandProfile.Prompts 同源，按人设细分）。
	Prompts     []string `json:"prompts"`
	// 人设画像关键词（用于从全量 prompt 中自动归类未显式归属的查询）。
	Keywords    []string `json:"keywords,omitempty"`
}

// PersonaResult 单条查询的检测结果（由 brand.PromptResult 转换而来，去环）。
type PersonaResult struct {
	Prompt         string
	Engine         models.EngineType
	BrandMentioned bool
	Sentiment      string // positive/neutral/negative
	BrandPosition  int
}

// Segment 单个人设的聚合结果。
type Segment struct {
	PersonaID      string  `json:"persona_id"`
	Name           string  `json:"name"`
	TotalPrompts   int     `json:"total_prompts"`
	MentionCount   int     `json:"mention_count"`
	MentionRate    float64 `json:"mention_rate"`
	AvgPosition    float64 `json:"avg_position"`
	PositiveRate   float64 `json:"positive_rate"`
	SentimentScore float64 `json:"sentiment_score"` // 正面1/中性0/负面-1 的均值
	// 该人设下品牌缺席的查询（内容机会）。
	MissingPrompts []string `json:"missing_prompts,omitempty"`
}

// Aggregate 按人设聚合可见度。
//
// results 为全量查询结果；personas 为人设定义。每条 result 按其 Prompt 命中某个人设
// 的 Prompts 或 Keywords 归类；未命中任何人的归为 "general"（若提供）。
func Aggregate(results []PersonaResult, personas []Persona) []Segment {
	type acc struct {
		seg      Segment
		positions []float64
		pos, neu, neg int
	}
	accs := map[string]*acc{}
	getAcc := func(p Persona) *acc {
		a, ok := accs[p.ID]
		if !ok {
			a = &acc{seg: Segment{PersonaID: p.ID, Name: p.Name}}
			accs[p.ID] = a
		}
		return a
	}

	// 构建 (prompt -> personaID) 归属表
	owner := map[string]string{}
	for _, p := range personas {
		for _, pr := range p.Prompts {
			owner[strings.ToLower(strings.TrimSpace(pr))] = p.ID
		}
	}
	keywordOwner := func(prompt string) string {
		pl := strings.ToLower(prompt)
		for _, p := range personas {
			for _, k := range p.Keywords {
				if k != "" && strings.Contains(pl, strings.ToLower(k)) {
					return p.ID
				}
			}
		}
		return ""
	}

	for _, r := range results {
		id := owner[strings.ToLower(strings.TrimSpace(r.Prompt))]
		if id == "" {
			id = keywordOwner(r.Prompt)
		}
		if id == "" {
			continue // 无人设归属则跳过（仅统计显式人设）
		}
		a := getAcc(findPersona(personas, id))
		a.seg.TotalPrompts++
		if r.BrandMentioned {
			a.seg.MentionCount++
			if r.BrandPosition > 0 {
				a.positions = append(a.positions, float64(r.BrandPosition))
			}
		} else {
			a.seg.MissingPrompts = append(a.seg.MissingPrompts, r.Prompt)
		}
		switch r.Sentiment {
		case "positive":
			a.pos++
		case "negative":
			a.neg++
		default:
			a.neu++
		}
	}

	out := make([]Segment, 0, len(accs))
	for _, a := range accs {
		s := a.seg
		if s.TotalPrompts > 0 {
			s.MentionRate = float64(s.MentionCount) / float64(s.TotalPrompts) * 100
			s.PositiveRate = float64(a.pos) / float64(s.TotalPrompts) * 100
			// 情感得分：-1..1
			s.SentimentScore = float64(a.pos-a.neg) / float64(s.TotalPrompts)
		}
		if len(a.positions) > 0 {
			sum := 0.0
			for _, p := range a.positions {
				sum += p
			}
			s.AvgPosition = sum / float64(len(a.positions))
		}
		s.MissingPrompts = dedupe(s.MissingPrompts)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MentionRate != out[j].MentionRate {
			return out[i].MentionRate > out[j].MentionRate
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func findPersona(ps []Persona, id string) Persona {
	for _, p := range ps {
		if p.ID == id {
			return p
		}
	}
	return Persona{ID: id, Name: id}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
