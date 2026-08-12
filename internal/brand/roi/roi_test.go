package roi

import (
	"testing"

	"my-geo/internal/models"
)

func TestTracker_RecordAndReport(t *testing.T) {
	tr := NewTracker()
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300})
	tr.Record(models.EngineClaude, "brand_audit", models.TokenUsage{PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700})

	report := tr.Report("all")
	if report.TotalCalls != 3 {
		t.Errorf("expected 3 calls, got %d", report.TotalCalls)
	}
	if report.TotalTokens != 1150 {
		t.Errorf("expected 1150 total tokens, got %d", report.TotalTokens)
	}
	if report.PromptTokens != 800 {
		t.Errorf("expected 800 prompt tokens, got %d", report.PromptTokens)
	}
	if report.CompletionTokens != 350 {
		t.Errorf("expected 350 completion tokens, got %d", report.CompletionTokens)
	}
	if report.TotalCost <= 0 {
		t.Errorf("expected positive cost, got %.4f", report.TotalCost)
	}
}

func TestTracker_ReportByEngine(t *testing.T) {
	tr := NewTracker()
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	tr.Record(models.EngineClaude, "query", models.TokenUsage{PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700})

	report := tr.Report("all")
	if len(report.ByEngine) != 2 {
		t.Fatalf("expected 2 engines, got %d", len(report.ByEngine))
	}
	// Claude has more tokens, should be first (descending)
	if report.ByEngine[0].Engine != models.EngineClaude {
		t.Errorf("expected claude first (more tokens), got %s", report.ByEngine[0].Engine)
	}
	if report.ByEngine[0].TotalTokens != 700 {
		t.Errorf("expected 700 tokens for claude, got %d", report.ByEngine[0].TotalTokens)
	}
}

func TestTracker_ReportByOperation(t *testing.T) {
	tr := NewTracker()
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	tr.Record(models.EngineClaude, "brand_audit", models.TokenUsage{PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700})

	report := tr.Report("all")
	if len(report.ByOperation) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(report.ByOperation))
	}
}

func TestTracker_EstimateCost(t *testing.T) {
	tr := NewTracker()
	// ChatGPT default: input $0.15/1K, output $0.60/1K
	// 1000 prompt + 500 completion = 0.15 + 0.30 = $0.45
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})
	report := tr.Report("all")
	expected := 0.15 + 0.30
	if abs(report.TotalCost-expected) > 0.001 {
		t.Errorf("expected cost %.4f, got %.4f", expected, report.TotalCost)
	}
}

func TestTracker_SetPrice(t *testing.T) {
	tr := NewTracker()
	tr.SetPrice(models.EngineChatGPT, PricePer1K{Input: 1.0, Output: 2.0})
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})
	report := tr.Report("all")
	// 1000 * 1.0/1000 + 500 * 2.0/1000 = 1.0 + 1.0 = 2.0
	if abs(report.TotalCost-2.0) > 0.001 {
		t.Errorf("expected cost 2.0, got %.4f", report.TotalCost)
	}
}

func TestTracker_RecordFromResponse(t *testing.T) {
	tr := NewTracker()
	resp := &models.EngineResponse{
		Engine: models.EngineChatGPT,
		Usage:  models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}
	tr.RecordFromResponse(models.EngineChatGPT, "query", resp)
	if tr.TotalCalls() != 1 {
		t.Errorf("expected 1 call, got %d", tr.TotalCalls())
	}
	report := tr.Report("all")
	if report.TotalTokens != 150 {
		t.Errorf("expected 150 tokens, got %d", report.TotalTokens)
	}
}

func TestTracker_RecordFromNilResponse(t *testing.T) {
	tr := NewTracker()
	tr.RecordFromResponse(models.EngineChatGPT, "query", nil)
	if tr.TotalCalls() != 1 {
		t.Errorf("expected 1 call, got %d", tr.TotalCalls())
	}
	report := tr.Report("all")
	if report.TotalTokens != 0 {
		t.Errorf("expected 0 tokens, got %d", report.TotalTokens)
	}
}

func TestTracker_ZeroUsage(t *testing.T) {
	tr := NewTracker()
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{})
	report := tr.Report("all")
	if report.TotalCalls != 1 {
		t.Errorf("expected 1 call even with zero usage, got %d", report.TotalCalls)
	}
	if report.TotalTokens != 0 {
		t.Errorf("expected 0 tokens, got %d", report.TotalTokens)
	}
	if report.TotalCost != 0 {
		t.Errorf("expected 0 cost, got %.4f", report.TotalCost)
	}
}

func TestTracker_MaxRecords(t *testing.T) {
	tr := NewTracker()
	tr.SetMaxRecords(5)
	for i := 0; i < 20; i++ {
		tr.Record(models.EngineChatGPT, "query", models.TokenUsage{TotalTokens: 1})
	}
	if tr.TotalCalls() > 10 {
		t.Errorf("expected at most ~10 records after trim, got %d", tr.TotalCalls())
	}
}

func TestTracker_ReportPeriodToday(t *testing.T) {
	tr := NewTracker()
	tr.Record(models.EngineChatGPT, "query", models.TokenUsage{TotalTokens: 100})
	report := tr.Report("today")
	if report.TotalCalls != 1 {
		t.Errorf("today should include just-recorded entry, got %d", report.TotalCalls)
	}
}

func TestFormatCost(t *testing.T) {
	if s := FormatCost(0.001); s != "$0.0010" {
		t.Errorf("expected $0.0010, got %s", s)
	}
	if s := FormatCost(1.5); s != "$1.50" {
		t.Errorf("expected $1.50, got %s", s)
	}
}

func TestFormatTokens(t *testing.T) {
	if s := FormatTokens(500); s != "500" {
		t.Errorf("expected 500, got %s", s)
	}
	if s := FormatTokens(1500); s != "1K" {
		t.Errorf("expected 1K, got %s", s)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
