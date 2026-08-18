// Package roi 提供 API 调用的 token 用量与成本追踪能力。
//
// 在 GEO 场景中，多引擎并行查询会产生可观的 API 成本。本包提供：
//   - Tracker：线程安全的用量累积器，按引擎/操作类型分类统计
//   - 定价表：各引擎每 1K token 的单价（美元），可动态更新
//   - Report：生成用量与成本报告，支持按引擎、按操作类型、按时间维度汇总
//
// 设计要点：
//   - 仅使用标准库，零外部依赖
//   - 线程安全，支持并发记录
//   - 定价表可运行时动态更新（如汇率变化、引擎调价）
//   - 从 models.EngineResponse.Usage 自动提取用量
package roi

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"time"

	"my-geo/internal/models"
)

// ============================================================
// 1. 定价表
// ============================================================

// PricePer1K 每 1K token 的单价（美元）。
type PricePer1K struct {
	Input  float64 // 输入 token 单价（$/1K tokens）
	Output float64 // 输出 token 单价（$/1K tokens）
}

// defaultPrices 各引擎默认定价（美元/1K tokens）。
// 数据来源：各厂商公开定价页（截至 2025 年），仅供参考。
// 生产环境可通过 SetPrice 运行时更新。
var defaultPrices = map[models.EngineType]PricePer1K{
	models.EngineChatGPT:    {Input: 0.15, Output: 0.60},  // gpt-4o-mini
	models.EnginePerplexity: {Input: 0.15, Output: 0.60},  // sonar
	models.EngineGemini:     {Input: 0.075, Output: 0.30}, // gemini-1.5-flash
	models.EngineClaude:     {Input: 0.80, Output: 4.00},  // claude-3-5-sonnet
	models.EngineGrok:       {Input: 0.50, Output: 1.50},  // grok-2
	models.EngineQwen:       {Input: 0.04, Output: 0.12},  // qwen-turbo
	models.EngineGLM:        {Input: 0.05, Output: 0.15},  // glm-4-flash
	models.EngineDeepSeek:   {Input: 0.03, Output: 0.08},  // deepseek-chat
	models.EngineKimi:       {Input: 0.12, Output: 0.12},  // moonshot-v1-8k
	models.EngineWenxin:     {Input: 0.12, Output: 0.12},  // ernie-speed
	models.EngineDoubao:     {Input: 0.03, Output: 0.06},  // doubao-pro
	models.EngineXiaomi:     {Input: 0.05, Output: 0.10},  // MiLM
	models.EngineXunfei:     {Input: 0.05, Output: 0.10},  // spark-lite
	models.EngineYuanbao:    {Input: 0.10, Output: 0.20},  // hunyuan-standard
}

// ============================================================
// 2. 用量记录
// ============================================================

// Record 单次 API 调用的用量记录。
type Record struct {
	Engine    models.EngineType
	Operation string // 操作类型：query / check_citation / optimize / brand_audit
	Timestamp time.Time
	Usage     models.TokenUsage
	Cost      float64 // 估算成本（美元）
}

// EngineStats 单引擎汇总统计。
type EngineStats struct {
	Engine           models.EngineType `json:"engine"`
	Calls            int               `json:"calls"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	TotalTokens      int               `json:"total_tokens"`
	TotalCost        float64           `json:"total_cost"`
}

// OperationStats 单操作类型汇总统计。
type OperationStats struct {
	Operation   string  `json:"operation"`
	Calls       int     `json:"calls"`
	TotalTokens int     `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// Report 用量与成本报告。
type Report struct {
	GeneratedAt      time.Time        `json:"generated_at"`
	Period           string           `json:"period"` // "all" / "today" / "7d" / "30d"
	TotalCalls       int              `json:"total_calls"`
	TotalTokens      int              `json:"total_tokens"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	TotalCost        float64          `json:"total_cost"`
	ByEngine         []EngineStats    `json:"by_engine"`
	ByOperation      []OperationStats `json:"by_operation"`
}

// ============================================================
// 3. Tracker 用量追踪器
// ============================================================

// Tracker 线程安全的 token 用量与成本追踪器。
//
// 记录每次 API 调用的 token 用量并估算成本，支持按引擎、操作类型、时间维度汇总。
type Tracker struct {
	mu         sync.RWMutex
	records    []Record
	prices     map[models.EngineType]PricePer1K
	maxRecords int // 最大记录数（防止内存无限增长），0 为不限
}

// NewTracker 创建追踪器。
func NewTracker() *Tracker {
	prices := make(map[models.EngineType]PricePer1K, len(defaultPrices))
	for k, v := range defaultPrices {
		prices[k] = v
	}
	return &Tracker{
		records:    make([]Record, 0, 256),
		prices:     prices,
		maxRecords: 10000, // 默认保留最近 1 万条
	}
}

// SetPrice 设置或更新某引擎的定价。
func (t *Tracker) SetPrice(engine models.EngineType, price PricePer1K) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prices[engine] = price
}

// GetPrice 获取某引擎的当前定价。
func (t *Tracker) GetPrice(engine models.EngineType) PricePer1K {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getPriceLocked(engine)
}

// getPriceLocked 在已持有锁的前提下获取定价（无锁版本）。
func (t *Tracker) getPriceLocked(engine models.EngineType) PricePer1K {
	if p, ok := t.prices[engine]; ok {
		return p
	}
	return PricePer1K{}
}

// estimateCostLocked 在已持有锁的前提下估算成本（无锁版本）。
func (t *Tracker) estimateCostLocked(engine models.EngineType, usage models.TokenUsage) float64 {
	price := t.getPriceLocked(engine)
	return float64(usage.PromptTokens)/1000*price.Input +
		float64(usage.CompletionTokens)/1000*price.Output
}

// Record 记录一次 API 调用的用量。
//
// 通常从 models.EngineResponse.Usage 提取。若用量为零（如模拟响应），仍记录调用次数但不计 token。
func (t *Tracker) Record(engine models.EngineType, operation string, usage models.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cost := t.estimateCostLocked(engine, usage)
	rec := Record{
		Engine:    engine,
		Operation: operation,
		Timestamp: time.Now(),
		Usage:     usage,
		Cost:      cost,
	}

	t.records = append(t.records, rec)
	// 超过上限时丢弃最旧记录（环形缓冲思路，简单实现）
	if t.maxRecords > 0 && len(t.records) > t.maxRecords {
		// 丢弃前 10% 避免频繁拷贝
		drop := len(t.records) - t.maxRecords
		if drop < len(t.records)/10 {
			drop = len(t.records) / 10
		}
		t.records = t.records[drop:]
	}
}

// RecordFromResponse 从 EngineResponse 中提取用量并记录。
func (t *Tracker) RecordFromResponse(engine models.EngineType, operation string, resp *models.EngineResponse) {
	if resp == nil {
		t.Record(engine, operation, models.TokenUsage{})
		return
	}
	t.Record(engine, operation, resp.Usage)
}

// SetMaxRecords 设置最大记录数。
func (t *Tracker) SetMaxRecords(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maxRecords = n
}

// TotalCalls 返回总调用次数。
func (t *Tracker) TotalCalls() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.records)
}

// Report 生成用量报告。
//
// period 支持："all"（全部）、"today"（今日）、"7d"（近7天）、"30d"（近30天）。
func (t *Tracker) Report(period string) Report {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var since time.Time
	now := time.Now()
	switch period {
	case "today":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "7d":
		since = now.AddDate(0, 0, -7)
	case "30d":
		since = now.AddDate(0, 0, -30)
	default:
		period = "all"
	}

	engineMap := make(map[models.EngineType]*EngineStats)
	opMap := make(map[string]*OperationStats)
	report := Report{GeneratedAt: now, Period: period}

	for i := range t.records {
		rec := &t.records[i]
		if !since.IsZero() && rec.Timestamp.Before(since) {
			continue
		}

		report.TotalCalls++
		report.TotalTokens += rec.Usage.TotalTokens
		report.PromptTokens += rec.Usage.PromptTokens
		report.CompletionTokens += rec.Usage.CompletionTokens
		report.TotalCost += rec.Cost

		es, ok := engineMap[rec.Engine]
		if !ok {
			es = &EngineStats{Engine: rec.Engine}
			engineMap[rec.Engine] = es
		}
		es.Calls++
		es.PromptTokens += rec.Usage.PromptTokens
		es.CompletionTokens += rec.Usage.CompletionTokens
		es.TotalTokens += rec.Usage.TotalTokens
		es.TotalCost += rec.Cost

		os, ok := opMap[rec.Operation]
		if !ok {
			os = &OperationStats{Operation: rec.Operation}
			opMap[rec.Operation] = os
		}
		os.Calls++
		os.TotalTokens += rec.Usage.TotalTokens
		os.TotalCost += rec.Cost
	}

	// 按 token 用量降序排列
	for _, es := range engineMap {
		report.ByEngine = append(report.ByEngine, *es)
	}
	slices.SortFunc(report.ByEngine, func(a, b EngineStats) int { return cmp.Compare(b.TotalTokens, a.TotalTokens) })

	for _, os := range opMap {
		report.ByOperation = append(report.ByOperation, *os)
	}
	slices.SortFunc(report.ByOperation, func(a, b OperationStats) int { return cmp.Compare(b.TotalCost, a.TotalCost) })

	return report
}

// FormatCost 格式化成本为美元字符串。
func FormatCost(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// FormatTokens 格式化 token 数量（带千分位）。
func FormatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dK", n/1000)
}
