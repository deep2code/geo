package exsubmit

import (
	"context"
	"testing"
)

// TestAnalyzeFallback 无判定模型时仍能稳定降级（不 panic、来源域名必抽）。
func TestAnalyzeFallback(t *testing.T) {
	a := NewAnalyzer(nil) // 无 judge → 降级
	answer := `我们推荐使用 Acme 公司的产品，参见 https://www.github.com/acme/docs 的说明。
据知乎 https://www.zhihu.com/question/1 的讨论，该方案不错。`
	res, err := a.Analyze(context.Background(), answer)
	if err != nil {
		t.Fatalf("Analyze 不应返回错误: %v", err)
	}
	if res == nil {
		t.Fatal("Analyze 不应返回 nil")
	}
	if res.Sentiment == "" {
		t.Error("降级时 sentiment 应给出默认值")
	}
	if len(res.SourceDomains) < 2 {
		t.Errorf("应至少抽取到 2 个来源域名，实际: %v", res.SourceDomains)
	}
	// github.com 应为 docs 类，zhihu.com 应为 social 类
	if cat, ok := res.SourceCategories["github.com"]; !ok || cat != "docs" {
		t.Errorf("github.com 分类应为 docs，实际: %v", res.SourceCategories)
	}
	if cat, ok := res.SourceCategories["zhihu.com"]; !ok || cat != "social" {
		t.Errorf("zhihu.com 分类应为 social，实际: %v", res.SourceCategories)
	}
}

// TestExtractURLDomains 校验域名抽取 + 去重 + 已知域名分类。
func TestExtractURLDomains(t *testing.T) {
	domains, cats := extractURLDomains("see https://github.com/a and https://github.com/b")
	if len(domains) != 1 {
		t.Errorf("同域名应去重，实际: %v", domains)
	}
	if cats["github.com"] != "docs" {
		t.Errorf("github.com 分类应为 docs，实际: %v", cats)
	}
}
