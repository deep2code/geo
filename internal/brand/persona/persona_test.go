package persona

import (
	"testing"

	"my-geo/internal/models"
)

func TestAggregate(t *testing.T) {
	ps := []Persona{
		{ID: "cto", Name: "CTO", Prompts: []string{"最好的CRM工具"}},
		{ID: "marketer", Name: "市场经理", Keywords: []string{"营销", "推广"}},
	}
	results := []PersonaResult{
		{Prompt: "最好的CRM工具", Engine: models.EngineChatGPT, BrandMentioned: true, Sentiment: "positive", BrandPosition: 1},
		{Prompt: "最好的CRM工具", Engine: models.EnginePerplexity, BrandMentioned: false, Sentiment: "neutral"},
		{Prompt: "营销工具推荐", Engine: models.EngineChatGPT, BrandMentioned: true, Sentiment: "positive", BrandPosition: 2},
		{Prompt: "不相关查询", Engine: models.EngineChatGPT, BrandMentioned: true, Sentiment: "negative"},
	}
	segs := Aggregate(results, ps)
	if len(segs) != 2 {
		t.Fatalf("期望 2 个人设分群，得到 %d", len(segs))
	}
	// CTO：2 个查询，提及 1 次 → 50%
	var cto *Segment
	for i := range segs {
		if segs[i].PersonaID == "cto" {
			cto = &segs[i]
		}
	}
	if cto == nil {
		t.Fatal("缺少 cto 分群")
	}
	if cto.TotalPrompts != 2 || cto.MentionCount != 1 {
		t.Fatalf("cto 统计错误: %+v", cto)
	}
	if cto.MentionRate != 50 {
		t.Fatalf("cto 提及率应为 50%%，得到 %v", cto.MentionRate)
	}
	if len(cto.MissingPrompts) != 1 || cto.MissingPrompts[0] != "最好的CRM工具" {
		t.Fatalf("cto 缺失查询记录错误: %v", cto.MissingPrompts)
	}
	// marketer：1 个查询命中关键词
	var m *Segment
	for i := range segs {
		if segs[i].PersonaID == "marketer" {
			m = &segs[i]
		}
	}
	if m == nil || m.TotalPrompts != 1 {
		t.Fatalf("marketer 分群错误: %+v", m)
	}
}
