package brand

import (
	"testing"
	"time"

	"my-geo/internal/models"
)

// TestCollectSourceCitations 验证采集逻辑：域名提取/分类/错误结果跳过/时间戳。
func TestCollectSourceCitations(t *testing.T) {
	ts := time.Date(2026, 8, 21, 10, 0, 0, 0, time.Local)
	report := &VisibilityReport{
		BrandName:   "Acme",
		GeneratedAt: ts,
		ProfileSnapshot: &BrandProfile{WorkspaceID: "ws-1"},
		Results: []PromptResult{
			{
				Prompt: "best crm",
				Engine: models.EngineChatGPT,
				Citations: []models.Citation{
					{URL: "https://www.g2.com/products/acme/reviews", Position: 1},
					{URL: "https://zhihu.com/question/123", Position: 2},
					{URL: "https://bad url.com/x", Position: 3}, // 无法解析域名，应跳过
				},
			},
			{
				Prompt: "best crm",
				Engine: models.EngineKimi,
				Error:  "timeout", // 错误结果整体跳过
				Citations: []models.Citation{
					{URL: "https://medium.com/@x/acme-review", Position: 1},
				},
			},
		},
	}
	recs := collectSourceCitations(7, report)
	if len(recs) != 2 {
		t.Fatalf("应采集 2 条来源（错误结果与坏 URL 跳过），实际 %d: %+v", len(recs), recs)
	}
	// 第一条：g2.com → review_site
	r0 := recs[0]
	if r0.Engine != "chatgpt" || r0.SourceDomain != "g2.com" || r0.SourceCategory != "review_site" {
		t.Fatalf("g2 来源解析错误: %+v", r0)
	}
	if r0.RecordID != 7 || r0.BrandName != "Acme" || r0.Prompt != "best crm" || r0.WorkspaceID != "ws-1" {
		t.Fatalf("来源元信息错误: %+v", r0)
	}
	if r0.CitedAt != ts.Unix() {
		t.Fatalf("cited_at 应为审计时间: %d", r0.CitedAt)
	}
	// 第二条：zhihu.com → social
	if recs[1].SourceDomain != "zhihu.com" || recs[1].SourceCategory != "social" {
		t.Fatalf("zhihu 来源解析错误: %+v", recs[1])
	}
}
