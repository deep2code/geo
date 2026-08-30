// Package promptversion 提供"被追踪 Prompt 的版本管理 + 实验归因"能力（P1-e）。
//
// 商业 GEO 需要回答"我改了内容 → 可见度涨没涨？"本包把每条被追踪的查询词版本化，
// 并在内容/策略变更前后自动做因果对比（实验），量化 lift。
//
// 不依赖 brand 包（避免导入环），仅依赖标准库。
package promptversion

import (
	"context"
	"sort"
	"sync"
	"time"
)

// TrackedPrompt 一条被追踪的查询词（高意图 prompt）。
type TrackedPrompt struct {
	ID        string `json:"id"`
	BrandID   string `json:"brand_id"`
	Text      string `json:"text"`
	Market    string `json:"market,omitempty"`
	Language  string `json:"language,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CurrentVersion int  `json:"current_version"` // 当前生效版本号
}

// PromptVersion 查询词的某个版本快照。
type PromptVersion struct {
	ID               string    `json:"id"`
	PromptID         string    `json:"prompt_id"`
	Version          int       `json:"version"`
	Content          string    `json:"content"` // 该版本实际下发给引擎的查询词
	CreatedAt        time.Time `json:"created_at"`
	BaselineVisibility float64 `json:"baseline_visibility"` // 发布前的可见度基线
	Note             string    `json:"note,omitempty"`
}

// Experiment 一次 A/B 对照实验：某 prompt 从 fromVer 切到 toVer 后的可见度对比。
type Experiment struct {
	ID         string    `json:"id"`
	PromptID   string    `json:"prompt_id"`
	FromVersion int      `json:"from_version"`
	ToVersion   int      `json:"to_version"`
	StartAt    time.Time `json:"start_at"`
	EndAt      *time.Time `json:"end_at,omitempty"`
	BeforeVisibility float64 `json:"before_visibility"` // 旧版本期均可见度
	AfterVisibility  float64 `json:"after_visibility"`  // 新版本期均可见度
	SampleSize  int      `json:"sample_size"` // 参与对比的引擎×查询组合数
	// 派生
	Lift        float64 `json:"lift"`         // After - Before（百分点）
	LiftPct     float64 `json:"lift_pct"`     // 相对提升 %
	Significant bool    `json:"significant"`  // 是否达到最小显著提升阈值
}

// MinSignificantLift 判定为"显著提升"的最小绝对 lift（百分点）。默认 5。
const MinSignificantLift = 5.0

// ComputeLift 计算实验的提升指标（就地填充派生字段）。
func (e *Experiment) ComputeLift() {
	e.Lift = e.AfterVisibility - e.BeforeVisibility
	if e.BeforeVisibility > 0 {
		e.LiftPct = e.Lift / e.BeforeVisibility * 100
	}
	e.Significant = e.Lift >= MinSignificantLift && e.SampleSize >= 3
}

// Store 持久化接口（注入式）。
type Store interface {
	CreatePrompt(ctx context.Context, p *TrackedPrompt) error
	AddVersion(ctx context.Context, v *PromptVersion) error
	ListVersions(ctx context.Context, promptID string) ([]PromptVersion, error)
	SaveExperiment(ctx context.Context, e *Experiment) error
	ListExperiments(ctx context.Context, promptID string) ([]Experiment, error)
}

// MemoryStore 内存实现。
//
// 被 HTTP handler 并发调用，所有方法需持锁（裸 map 并发写会直接 panic 崩进程）。
type MemoryStore struct {
	mu          sync.RWMutex
	prompts     map[string]*TrackedPrompt
	versions    map[string][]*PromptVersion
	experiments map[string][]*Experiment
}

// NewMemoryStore 创建内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		prompts:     map[string]*TrackedPrompt{},
		versions:    map[string][]*PromptVersion{},
		experiments: map[string][]*Experiment{},
	}
}

// CreatePrompt 创建被追踪 prompt。
func (m *MemoryStore) CreatePrompt(_ context.Context, p *TrackedPrompt) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	if p.CurrentVersion == 0 {
		p.CurrentVersion = 1
	}
	m.mu.Lock()
	m.prompts[p.ID] = p
	m.mu.Unlock()
	return nil
}

// AddVersion 追加版本。
func (m *MemoryStore) AddVersion(_ context.Context, v *PromptVersion) error {
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	m.mu.Lock()
	m.versions[v.PromptID] = append(m.versions[v.PromptID], v)
	// 更新当前版本
	if p, ok := m.prompts[v.PromptID]; ok && v.Version > p.CurrentVersion {
		p.CurrentVersion = v.Version
	}
	m.mu.Unlock()
	return nil
}

// ListVersions 列版本（按版本号升序）。
func (m *MemoryStore) ListVersions(_ context.Context, promptID string) ([]PromptVersion, error) {
	m.mu.RLock()
	vs := append([]*PromptVersion{}, m.versions[promptID]...)
	m.mu.RUnlock()
	sort.Slice(vs, func(i, j int) bool { return vs[i].Version < vs[j].Version })
	out := make([]PromptVersion, len(vs))
	for i, v := range vs {
		out[i] = *v
	}
	return out, nil
}

// SaveExperiment 保存实验。
func (m *MemoryStore) SaveExperiment(_ context.Context, e *Experiment) error {
	e.ComputeLift()
	m.mu.Lock()
	m.experiments[e.PromptID] = append(m.experiments[e.PromptID], e)
	m.mu.Unlock()
	return nil
}

// ListExperiments 列实验。
func (m *MemoryStore) ListExperiments(_ context.Context, promptID string) ([]Experiment, error) {
	m.mu.RLock()
	es := append([]*Experiment{}, m.experiments[promptID]...)
	m.mu.RUnlock()
	out := make([]Experiment, len(es))
	for i, e := range es {
		out[i] = *e
	}
	return out, nil
}
