package promptversion

import (
	"context"
	"testing"
)

func TestExperimentLift(t *testing.T) {
	e := &Experiment{
		PromptID:        "p1",
		FromVersion:     1,
		ToVersion:       2,
		BeforeVisibility: 40,
		AfterVisibility:  52,
		SampleSize:       6,
	}
	e.ComputeLift()
	if e.Lift != 12 {
		t.Fatalf("Lift 应为 12，得到 %v", e.Lift)
	}
	if !e.Significant {
		t.Error("lift=12 且 sample=6 应判定显著")
	}
	e2 := &Experiment{BeforeVisibility: 40, AfterVisibility: 41, SampleSize: 2}
	e2.ComputeLift()
	if e2.Significant {
		t.Error("lift=1 不应判定显著")
	}
}

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	p := &TrackedPrompt{ID: "p1", BrandID: "b1", Text: "最好的CRM工具"}
	if err := s.CreatePrompt(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.CurrentVersion != 1 {
		t.Fatalf("默认版本应为 1，得到 %d", p.CurrentVersion)
	}
	if err := s.AddVersion(ctx, &PromptVersion{PromptID: "p1", Version: 2, Content: "best CRM 2026"}); err != nil {
		t.Fatal(err)
	}
	vs, _ := s.ListVersions(ctx, "p1")
	if len(vs) != 1 || vs[0].Version != 2 {
		t.Fatalf("版本列表错误: %+v", vs)
	}
	if s.prompts["p1"].CurrentVersion != 2 {
		t.Fatalf("current_version 应更新为 2")
	}
}
