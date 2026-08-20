package attribution

import (
	"context"
	"testing"
	"time"
)

func TestIsAIReferrer(t *testing.T) {
	cases := map[string]bool{
		"https://chat.openai.com/":   true,
		"https://www.perplexity.ai/": true,
		"https://example.com/blog":   false,
		"":                           false,
	}
	for in, want := range cases {
		if got := IsAIReferrer(in); got != want {
			t.Errorf("IsAIReferrer(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestUTMMatcher(t *testing.T) {
	m := UTMMatcher{ExtraKeywords: []string{"gemini"}}
	if !m.Match("chatgpt", "organic") {
		t.Error("utm_source=chatgpt 应命中")
	}
	if !m.Match("newsletter", "ai") {
		t.Error("utm_medium=ai 应命中")
	}
	if !m.Match("gemini_ads", "cpc") {
		t.Error("ExtraKeywords=gemini 应命中")
	}
	if m.Match("newsletter", "email") {
		t.Error("普通邮件流量不应命中")
	}
}

func TestCompute(t *testing.T) {
	from, _ := time.Parse("2006-01-02", "2026-08-01")
	to, _ := time.Parse("2006-01-02", "2026-08-02")
	tr := NewTracker(nil)
	tr.AIAttributionWeight = 1.0
	vis := map[string]float64{"2026-08-01": 50}
	rep, err := tr.Compute(context.Background(), "brand1", from, to, vis)
	if err != nil {
		t.Fatal(err)
	}
	// 无流量源 → 全 0 且不报错
	if rep.TotalSessions != 0 || rep.AttributedRevenue != 0 {
		t.Fatalf("空流量应全 0: %+v", rep)
	}

	// 有流量源
	src := &fakeSource{pts: []TrafficPoint{
		{Date: "2026-08-01", Source: "ga4", Sessions: 100, Conversions: 10, Revenue: 1000, AISourced: true},
	}}
	tr2 := NewTracker([]Source{src})
	tr2.AIAttributionWeight = 0.7
	rep2, err := tr2.Compute(context.Background(), "brand1", from, to, vis)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.AISessions != 100 {
		t.Fatalf("AI 会话应为 100: %+v", rep2)
	}
	// 归因 = 100 * 0.7 * (50/100) = 35
	if rep2.AttributedSessions != 35 {
		t.Fatalf("归因会话应为 35，得到 %d", rep2.AttributedSessions)
	}
	if rep2.AIShare != 100 {
		t.Fatalf("AI 占比应为 100%%，得到 %v", rep2.AIShare)
	}
}

type fakeSource struct{ pts []TrafficPoint }

func (f *fakeSource) Name() string                          { return "fake" }
func (f *fakeSource) Configured() bool                      { return true }
func (f *fakeSource) Fetch(context.Context, time.Time, time.Time) ([]TrafficPoint, error) {
	return f.pts, nil
}
