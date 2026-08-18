package llm

import (
	"context"
	"errors"
	"testing"

	"my-geo/internal/models"
)

// fakeCostProvider 测试用假 Provider：实现 Provider + UsageReporter + ModelReporter。
type fakeCostProvider struct {
	name  string
	model string
	usage models.TokenUsage
	calls int
	err   error
}

func (f *fakeCostProvider) Name() string { return f.name }
func (f *fakeCostProvider) Available() bool { return true }
func (f *fakeCostProvider) Rewrite(_ context.Context, _, content string) (string, error) {
	f.calls++
	return content + "-opt", f.err
}
func (f *fakeCostProvider) LastUsage() models.TokenUsage { return f.usage }
func (f *fakeCostProvider) Model() string                { return f.model }

func TestManagerCostTracking(t *testing.T) {
	p := &fakeCostProvider{
		name:  "openai:gpt-4o-mini",
		model: "gpt-4o-mini",
		usage: models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}
	m := NewManager(p)
	if _, err := m.Rewrite(context.Background(), "prompt", "content"); err != nil {
		t.Fatalf("Rewrite 失败: %v", err)
	}
	report := m.Cost()
	if len(report.Rows) != 1 {
		t.Fatalf("期望 1 行成本，实际 %d", len(report.Rows))
	}
	row := report.Rows[0]
	if row.Model != "gpt-4o-mini" || row.Calls != 1 || row.PromptTokens != 100 || row.CompletionTokens != 50 {
		t.Fatalf("成本行异常: %+v", row)
	}
	// 预期成本 = 100/1000*0.00015 + 50/1000*0.0006 = 0.000015 + 0.000030 = 0.000045
	want := 0.000045
	if absf(row.CostUSD-want) > 1e-9 {
		t.Fatalf("成本计算错误: got %.9f want %.9f", row.CostUSD, want)
	}
}

func TestManagerBudgetBreaker(t *testing.T) {
	p := &fakeCostProvider{
		name:  "openai:gpt-4o-mini",
		model: "gpt-4o-mini",
		usage: models.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}
	// 预算设极小：单次调用成本 0.000045 > 0.00001，首次调用后即熔断。
	m := NewManagerWithOptions([]Provider{p}, WithMonthlyBudgetUSD(0.00001))
	if _, err := m.Rewrite(context.Background(), "p", "c"); err != nil {
		t.Fatalf("首次调用应成功: %v", err)
	}
	if !m.Cost().Breached {
		t.Fatal("首次调用后应已触发预算熔断")
	}
	// 第二次调用应被熔断拒绝。
	_, err := m.Rewrite(context.Background(), "p", "c")
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("期望 ErrBudgetExceeded，实际: %v", err)
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
