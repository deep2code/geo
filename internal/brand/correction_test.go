package brand

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"my-geo/internal/brand/history"
	"my-geo/internal/models"
)

// fakeHistory 内存版 history.Store（仅测试用）。
type fakeHistory struct {
	mu      sync.Mutex
	nextID  int64
	records map[int64]*history.Record
}

func newFakeHistory() *fakeHistory { return &fakeHistory{nextID: 1, records: map[int64]*history.Record{}} }

func (f *fakeHistory) Close() error { return nil }
func (f *fakeHistory) Path() string { return "fake" }
func (f *fakeHistory) Save(ctx context.Context, r history.Record) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	r.ID = id
	cp := r
	f.records[id] = &cp
	return id, nil
}
func (f *fakeHistory) List(ctx context.Context, brandName string, limit, offset int) ([]history.Record, error) {
	return nil, nil
}
func (f *fakeHistory) Latest(ctx context.Context, brandName string) (*history.Record, error) { return nil, nil }
func (f *fakeHistory) LatestForBrands(ctx context.Context, brandNames []string) ([]history.Record, error) {
	return nil, nil
}
func (f *fakeHistory) GetByID(ctx context.Context, id int64) (*history.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.records[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, nil
}
func (f *fakeHistory) UpdateReport(ctx context.Context, id int64, r history.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.records[id]; !ok {
		return nil
	}
	cp := r
	cp.ID = id
	f.records[id] = &cp
	return nil
}
func (f *fakeHistory) Brands(ctx context.Context) ([]string, error)          { return nil, nil }
func (f *fakeHistory) Stats(ctx context.Context) (history.Stats, error)      { return history.Stats{}, nil }
func (f *fakeHistory) DailyCounts(ctx context.Context, days int) ([]history.DailyBucket, error) {
	return nil, nil
}
func (f *fakeHistory) Clear(ctx context.Context) error             { return nil }
func (f *fakeHistory) DeleteOlderThan(ctx context.Context, days int) (int64, error) { return 0, nil }

// seedCorrectionRecord 构造一条含 2 条结果的完整审计记录（chatgpt 未提及 / kimi 提及），
// 存入 fake history，返回 recordID。
func seedCorrectionRecord(t *testing.T, e *Engine, fh *fakeHistory) int64 {
	t.Helper()
	profile := BrandProfile{
		Name:     "Acme",
		Domain:   "acme.com",
		Prompts:  []string{"best crm tool"},
		Products: []string{"AcmeCRM"},
	}
	results := []PromptResult{
		{Prompt: "best crm tool", Engine: models.EngineChatGPT, BrandMentioned: false, BrandPosition: 0, Sentiment: "neutral"},
		{Prompt: "best crm tool", Engine: models.EngineKimi, BrandMentioned: true, BrandPosition: 2, Sentiment: "positive"},
	}
	stats := e.scorer.Aggregate(results, profile, e.configuredEngines)
	ent := EntityCompleteness(profile)
	score, grade, tier, breakdown := e.scorer.ScoreWithProfile(stats, &profile, ent)
	report := e.reporter.Build(profile, results, stats, score, grade, tier, breakdown)
	report.ProfileSnapshot = &profile // 模拟新格式审计记录（含快照）

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	id, err := fh.Save(context.Background(), history.Record{
		BrandName: "Acme",
		Generated: time.Now().Unix(),
		Score:     report.Score,
		Grade:     report.Grade,
		MentionRate: report.ScoreBreakdown.MentionRate,
		CitationRate: report.ScoreBreakdown.CitationRate,
		ReportJSON: string(b),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestCorrectResultRecompute 验证：修正提及 false→true 后，
// 留痕（原值+修正人+原因）写入、报告级元信息更新、聚合统计与 BVS 重算、落库更新。
func TestCorrectResultRecompute(t *testing.T) {
	fh := newFakeHistory()
	e := New(WithHistoryDB(fh))
	id := seedCorrectionRecord(t, e, fh)

	mentioned := true
	report, err := e.CorrectResult(context.Background(), CorrectResultInput{
		RecordID:    id,
		BrandName:   "Acme",
		Index:       0, // chatgpt 结果：未提及 → 修正为提及
		Mentioned:   &mentioned,
		Sentiment:   strPtr("negative"), // 顺带修正情感
		CorrectedBy: "ops@acme.com",
		Reason:      "人工核对回答原文，AI 在第一段明确推荐了 Acme",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. 单条留痕：原值/修正值/修正人/原因。
	pr := report.Results[0]
	if pr.Correction == nil {
		t.Fatal("修正后结果应带 Correction 留痕")
	}
	if pr.Correction.CorrectedBy != "ops@acme.com" {
		t.Fatalf("修正人应为 ops@acme.com: %q", pr.Correction.CorrectedBy)
	}
	if pr.Correction.Reason == "" {
		t.Fatal("修正原因不能为空")
	}
	if pr.Correction.PrevMentioned || !pr.Correction.Mentioned || !pr.BrandMentioned {
		t.Fatalf("提及修正留痕错误: prev=%v now=%v", pr.Correction.PrevMentioned, pr.BrandMentioned)
	}
	if pr.Correction.PrevSentiment != "neutral" || pr.Sentiment != "negative" {
		t.Fatalf("情感修正留痕错误: prev=%q now=%q", pr.Correction.PrevSentiment, pr.Sentiment)
	}
	// 提及=true 且位置为 0 → 语义自洽置 1。
	if pr.BrandPosition != 1 {
		t.Fatalf("提及修正后位置应自洽置 1: %d", pr.BrandPosition)
	}

	// 2. 报告级元信息。
	if report.Corrected == nil || report.Corrected.CorrectedCount != 1 ||
		report.Corrected.LastCorrectedBy != "ops@acme.com" {
		t.Fatalf("报告级修正元信息错误: %+v", report.Corrected)
	}

	// 3. 聚合重算：chatgpt 引擎提及数 0→1，提及率 0→100。
	var chatGPTStats *EngineStats
	for i := range report.EngineStats {
		if report.EngineStats[i].Engine == models.EngineChatGPT {
			chatGPTStats = &report.EngineStats[i]
		}
	}
	if chatGPTStats == nil {
		t.Fatal("重算后缺少 chatgpt 引擎统计")
	}
	if chatGPTStats.MentionCount != 1 || chatGPTStats.MentionRate != 100 {
		t.Fatalf("chatgpt 提及率应重算为 100%%: count=%d rate=%.1f",
			chatGPTStats.MentionCount, chatGPTStats.MentionRate)
	}

	// 4. BVS 重算：提及率上升，分数不应下降。
	oldRec, _ := fh.GetByID(context.Background(), id)
	var oldReport VisibilityReport
	if err := json.Unmarshal([]byte(oldRec.ReportJSON), &oldReport); err != nil {
		t.Fatal(err)
	}
	// oldRec 已被 UpdateReport 覆盖，从种子值对比：修正前 score 应 <= 修正后。
	if report.Score < oldReport.Score-1e-9 {
		t.Fatalf("提及修正后 BVS 应不降: before=%.2f after=%.2f", oldReport.Score, report.Score)
	}

	// 5. 落库更新：记录中的 report_json 已是最新（含留痕与重算结果）。
	rec, _ := fh.GetByID(context.Background(), id)
	var stored VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Results) != 2 || stored.Results[0].Correction == nil {
		t.Fatal("落库报告应包含修正后的留痕")
	}
	// 标量同步更新。
	if rec.MentionRate != report.ScoreBreakdown.MentionRate {
		t.Fatalf("标量 mention_rate 未同步: rec=%.2f report=%.2f",
			rec.MentionRate, report.ScoreBreakdown.MentionRate)
	}
}

// TestCorrectResultValidation 参数校验。
func TestCorrectResultValidation(t *testing.T) {
	fh := newFakeHistory()
	e := New(WithHistoryDB(fh))
	id := seedCorrectionRecord(t, e, fh)
	mention := true
	ctx := context.Background()

	cases := []struct {
		name string
		in   CorrectResultInput
		want string // 期望错误包含的片段
	}{
		{"无 historyDB", CorrectResultInput{RecordID: 1, BrandName: "Acme", Index: 0, Mentioned: &mention, Reason: "x"}, "未配置审计历史存储"},
		{"record_id 非法", CorrectResultInput{RecordID: 0, BrandName: "Acme", Index: 0, Mentioned: &mention, Reason: "x"}, "record_id"},
		{"原因必填", CorrectResultInput{RecordID: id, BrandName: "Acme", Index: 0, Mentioned: &mention}, "reason"},
		{"无修正字段", CorrectResultInput{RecordID: id, BrandName: "Acme", Index: 0, Reason: "x"}, "至少提供一个"},
		{"记录不存在", CorrectResultInput{RecordID: 9999, BrandName: "Acme", Index: 0, Mentioned: &mention, Reason: "x"}, "记录不存在"},
		{"品牌不匹配", CorrectResultInput{RecordID: id, BrandName: "Other", Index: 0, Mentioned: &mention, Reason: "x"}, "品牌不匹配"},
		{"下标越界", CorrectResultInput{RecordID: id, BrandName: "Acme", Index: 5, Mentioned: &mention, Reason: "x"}, "下标越界"},
		{"情感非法", CorrectResultInput{RecordID: id, BrandName: "Acme", Index: 0, Sentiment: strPtr("awesome"), Reason: "x"}, "sentiment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := e
			if tc.name == "无 historyDB" {
				eng = New() // 不带 historyDB
			}
			_, err := eng.CorrectResult(ctx, tc.in)
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误应包含 %q: %v", tc.want, err)
			}
		})
	}
}

// TestCorrectResultNoSnapshotFallback 旧记录（无 ProfileSnapshot）反推画像仍可重算。
func TestCorrectResultNoSnapshotFallback(t *testing.T) {
	fh := newFakeHistory()
	e := New(WithHistoryDB(fh))

	// 构造无快照的旧格式记录。
	oldReport := VisibilityReport{
		BrandName: "Legacy",
		Industry:  "企业软件",
		Category:  "CRM",
		Results: []PromptResult{
			{Prompt: "best crm", Engine: models.EngineChatGPT, BrandMentioned: false, BrandPosition: 0, Sentiment: "neutral"},
			{Prompt: "best crm", Engine: models.EngineKimi, BrandMentioned: true, BrandPosition: 1, Sentiment: "positive"},
			{Prompt: "best pm tool", Engine: models.EngineChatGPT, BrandMentioned: true, BrandPosition: 1, Sentiment: "neutral"},
		},
	}
	b, _ := json.Marshal(oldReport)
	id, err := fh.Save(context.Background(), history.Record{BrandName: "Legacy", Score: 50, Grade: "C", ReportJSON: string(b)})
	if err != nil {
		t.Fatal(err)
	}

	cite := true
	report, err := e.CorrectResult(context.Background(), CorrectResultInput{
		RecordID:  id,
		BrandName: "Legacy",
		Index:     2,
		Cited:     &cite,
		Reason:    "回答末尾附带了官网链接",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[2].Correction == nil || !report.Results[2].BrandCited {
		t.Fatal("旧记录反推画像后修正引用应生效")
	}
	if len(report.Results) != 3 {
		t.Fatalf("结果条数应保持 3: %d", len(report.Results))
	}
}

func strPtr(s string) *string { return &s }
