package brand

import (
	"context"
	"testing"

	"my-geo/internal/adapter"
	"my-geo/internal/models"
)

// fakeAdapter 可控返回结果的假适配器（用于采样多数票测试）。
type fakeAdapter struct {
	engine models.EngineType
	// 每次 Query 依次返回 answers 中的一条（循环），模拟 LLM 采样方差
	answers []string
	calls   int
	cfg     bool
}

func (f *fakeAdapter) Engine() models.EngineType { return f.engine }
func (f *fakeAdapter) Configured() bool          { return f.cfg }

func (f *fakeAdapter) Query(_ context.Context, query string) (*models.EngineResponse, error) {
	a := f.answers[f.calls%len(f.answers)]
	f.calls++
	return &models.EngineResponse{Engine: f.engine, Answer: a}, nil
}

func (f *fakeAdapter) CheckCitation(_ context.Context, query, targetURL string) ([]models.Citation, error) {
	resp, _ := f.Query(context.Background(), query)
	return adapter.FilterCitationsByURL(resp.Citations, targetURL), nil
}

// TestSamplingMajorityVote 多次采样多数票：3 次采样 2 次提及 → 判定提及 + consistency 2/3。
func TestSamplingMajorityVote(t *testing.T) {
	f := &fakeAdapter{
		engine: models.EngineChatGPT,
		cfg:    true,
		answers: []string{
			"Acme 是最好的 CRM 工具。",   // 提及
			"可以考虑 Salesforce 和 HubSpot。", // 未提及
			"Acme 值得推荐。",           // 提及
		},
	}
	m := NewMonitor(map[models.EngineType]adapter.Adapter{models.EngineChatGPT: f}).WithSamples(3)
	profile := BrandProfile{Name: "Acme", Prompts: []string{"最好的CRM工具"}, TargetEngines: []models.EngineType{models.EngineChatGPT}}
	results, err := m.Run(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果，得到 %d", len(results))
	}
	r := results[0]
	if !r.BrandMentioned {
		t.Fatal("多数票（2/3 提及）应判定为提及")
	}
	if r.Samples != 3 {
		t.Fatalf("Samples 应为 3，得到 %d", r.Samples)
	}
	if r.MentionVotes != 2 {
		t.Fatalf("MentionVotes 应为 2，得到 %d", r.MentionVotes)
	}
	if r.Consistency < 0.66 || r.Consistency > 0.67 {
		t.Fatalf("Consistency 应为 2/3≈0.67，得到 %v", r.Consistency)
	}
}

// TestSamplingMinorityNotMentioned 多数票否定：3 次采样 1 次提及 → 判定未提及。
func TestSamplingMinorityNotMentioned(t *testing.T) {
	f := &fakeAdapter{
		engine: models.EngineChatGPT,
		cfg:    true,
		answers: []string{
			"Acme 是最好的 CRM 工具。",
			"考虑 Salesforce 和 HubSpot。",
			"也看看 HubSpot。",
		},
	}
	m := NewMonitor(map[models.EngineType]adapter.Adapter{models.EngineChatGPT: f}).WithSamples(3)
	profile := BrandProfile{Name: "Acme", Prompts: []string{"最好的CRM工具"}, TargetEngines: []models.EngineType{models.EngineChatGPT}}
	results, _ := m.Run(context.Background(), profile)
	if results[0].BrandMentioned {
		t.Fatal("1/3 提及不应判定为提及（多数票）")
	}
	if results[0].Consistency < 0.33 || results[0].Consistency > 0.34 {
		t.Fatalf("Consistency 应为 1/3≈0.33，得到 %v", results[0].Consistency)
	}
}

// TestSamplingDefaultSingle 默认单次采样（行为与旧版一致），profile.Samples 覆盖生效。
func TestSamplingDefaultSingle(t *testing.T) {
	f := &fakeAdapter{engine: models.EngineChatGPT, cfg: true, answers: []string{"Acme 很好。"}}
	m := NewMonitor(map[models.EngineType]adapter.Adapter{models.EngineChatGPT: f}) // 默认 samples=1
	profile := BrandProfile{Name: "Acme", Prompts: []string{"q"}, TargetEngines: []models.EngineType{models.EngineChatGPT}}
	results, _ := m.Run(context.Background(), profile)
	if results[0].Samples != 1 {
		t.Fatalf("默认应为单次采样，得到 %d", results[0].Samples)
	}
	if f.calls != 1 {
		t.Fatalf("默认单次应只查 1 次，实际 %d", f.calls)
	}

	// profile.Samples=2 覆盖
	f2 := &fakeAdapter{engine: models.EngineChatGPT, cfg: true, answers: []string{"Acme 很好。", "Acme 也好。"}}
	m2 := NewMonitor(map[models.EngineType]adapter.Adapter{models.EngineChatGPT: f2})
	profile2 := BrandProfile{Name: "Acme", Prompts: []string{"q"}, TargetEngines: []models.EngineType{models.EngineChatGPT}, Samples: 2}
	results2, _ := m2.Run(context.Background(), profile2)
	if results2[0].Samples != 2 || f2.calls != 2 {
		t.Fatalf("profile.Samples=2 应覆盖（samples=%d calls=%d）", results2[0].Samples, f2.calls)
	}
}

// TestSamplingConsolidatesCompetitors 采样后竞品并集去重。
func TestSamplingConsolidatesCompetitors(t *testing.T) {
	f := &fakeAdapter{
		engine: models.EngineChatGPT,
		cfg:    true,
		answers: []string{
			"Acme 和 Salesforce 都不错。",
			"Salesforce 和 HubSpot 领先。",
		},
	}
	m := NewMonitor(map[models.EngineType]adapter.Adapter{models.EngineChatGPT: f}).WithSamples(2)
	profile := BrandProfile{
		Name: "Acme", Prompts: []string{"q"},
		TargetEngines: []models.EngineType{models.EngineChatGPT},
		Competitors:   []Competitor{{Name: "Salesforce"}, {Name: "HubSpot"}},
	}
	results, _ := m.Run(context.Background(), profile)
	r := results[0]
	names := map[string]bool{}
	for _, cm := range r.CompetitorMentions {
		names[cm.Name] = true
	}
	if !names["Salesforce"] || !names["HubSpot"] {
		t.Fatalf("竞品并集应含 Salesforce 与 HubSpot: %+v", r.CompetitorMentions)
	}
}
