// Package llm 提供 LLM 抽象层，支持多 Provider 故障转移 + 轻量断路保护。
//
// 参考设计：
//   - https://github.com/gptscript-ai/go-openai (多 client 轮询 + fallback)
//   - https://github.com/sony/gobreaker  (熔断器状态机)
//
// 本实现采用"连续失败计数 + 冷却期"的简易断路器，无需额外依赖：
//
//	某 Provider 连续失败 >= 阈值 → 进入 CoolDown (默认 30s)，
//	期间跳过该 Provider；CoolDown 到期后再次允许 1 条探测请求，若成功则复位。
//
// 除熔断外还提供：
//   - 单 Provider 失败后的指数退避重试（默认 2 次、500ms 起、上限 4s）
//   - 全局限流：并发 Rewrite 调用上限（默认 8），超限时按 ctx 排队/取消
//
// 所有策略阈值均可通过 NewManagerWithOptions / Option 覆盖，便于按部署环境调优。
package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"my-geo/internal/models"
)

// 默认断路与冷却阈值（兼容旧代码的常量引用；实际生效值见 Options 默认值）。
const (
	// CircuitBreakFailures 连续失败 N 次 → 触发冷却。
	CircuitBreakFailures = 5
	// CircuitBreakCoolDown 冷却期：到期后尝试 1 次探测请求。
	CircuitBreakCoolDown = 30 * time.Second
)

// 默认重试与并发策略。
const (
	defaultRetryMax       = 2
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultRetryMaxDelay  = 4 * time.Second
	defaultMaxConcurrency = 8
)

// Options LLM 管理器的可配置策略参数。
//
// 零值字段在 NewManagerWithOptions 中会被默认值补齐，因此只设置关心的字段即可。
type Options struct {
	// RetryMax 单个 Provider 失败后的额外重试次数（指数退避）。0 表示不重试。
	RetryMax int
	// RetryBaseDelay 首次重试的等待时长，之后每次翻倍直至 RetryMaxDelay。
	RetryBaseDelay time.Duration
	// RetryMaxDelay 退避时长上限。
	RetryMaxDelay time.Duration
	// MaxConcurrency 并发 Rewrite 调用上限；<=0 表示不限制。
	MaxConcurrency int
	// CircuitFailures 连续失败阈值，达到后该 Provider 进入冷却。
	CircuitFailures int64
	// CircuitCoolDown 熔断冷却期。
	CircuitCoolDown time.Duration
}

// defaultOptions 返回与旧版写死常量一致的行为。
func defaultOptions() Options {
	return Options{
		RetryMax:         defaultRetryMax,
		RetryBaseDelay:   defaultRetryBaseDelay,
		RetryMaxDelay:    defaultRetryMaxDelay,
		MaxConcurrency:   defaultMaxConcurrency,
		CircuitFailures:  CircuitBreakFailures,
		CircuitCoolDown:  CircuitBreakCoolDown,
	}
}

// Option 修改 Options 的函数式配置项。
type Option func(*Options)

// WithRetry 设置单 Provider 的指数退避重试：max 次额外尝试、base 起步等待、
// maxDelay 封顶。max 传 0 表示禁用重试；base / maxDelay 传 0 保持默认值。
func WithRetry(max int, base, maxDelay time.Duration) Option {
	return func(o *Options) {
		o.RetryMax = max
		if base > 0 {
			o.RetryBaseDelay = base
		}
		if maxDelay > 0 {
			o.RetryMaxDelay = maxDelay
		}
	}
}

// WithMaxConcurrency 设置全局并发上限（同时进行的 Rewrite 调用数）。
func WithMaxConcurrency(n int) Option {
	return func(o *Options) {
		if n > 0 {
			o.MaxConcurrency = n
		}
	}
}

// WithCircuitBreak 设置熔断阈值：failures 次连续失败后冷却 coolDown 时长。
func WithCircuitBreak(failures int64, coolDown time.Duration) Option {
	return func(o *Options) {
		if failures > 0 {
			o.CircuitFailures = failures
		}
		if coolDown > 0 {
			o.CircuitCoolDown = coolDown
		}
	}
}

// Provider LLM 提供者接口。
type Provider interface {
	// Name 提供者名称（Status / 日志使用，建议稳定唯一）。
	Name() string
	// Rewrite 根据提示词改写内容。
	Rewrite(ctx context.Context, prompt, content string) (string, error)
	// Available 是否可用（已配置 API Key 等）。
	Available() bool
}

// providerState 每个 provider 的运行状态 + 熔断计数。
type providerState struct {
	consecutiveFails atomic.Int64
	totalFails       atomic.Int64
	totalCalls       atomic.Int64
	lastFailAt       atomic.Int64 // unix nano; 0 表示从未失败
	openUntil        atomic.Int64 // unix nano; 熔断到期时间戳，<= now 视为已恢复
}

// ProviderStatus 对外可观测状态（/healthz 和 /admin/system 使用）。
type ProviderStatus struct {
	Name             string `json:"name"`
	Available        bool   `json:"available"`
	TotalCalls       int64  `json:"total_calls"`
	TotalFails       int64  `json:"total_fails"`
	ConsecutiveFails int64  `json:"consecutive_fails"`
	OpenUntil        string `json:"open_until,omitempty"` // 熔断到期时间（RFC3339，空=未熔断）
	LastFailAt       string `json:"last_fail_at,omitempty"`
}

// Manager LLM 提供者管理器：优先级列表 + 顺序尝试 + 断路保护 + 重试 + 并发上限。
//
// 策略：
//   - 按 providers 顺序尝试，首个成功返回；
//   - 遇到"已熔断（OpenUntil > now）"条目直接跳过；
//   - 单个 Provider 调用失败时按指数退避重试（RetryMax 次）；
//   - 成功后把该 provider 的 consecutiveFails 清零；重试仍失败时累加并检查阈值。
type Manager struct {
	mu        sync.RWMutex // 保护 providers 列表（不保护 state，state 本身原子）
	providers []Provider
	states    map[*providerState]struct{} // 仅用于遍历统计（不是必须）

	// 索引：Provider.Name -> state（Name 稳定不变，所以安全）
	stateByName map[string]*providerState

	sem  chan struct{} // 并发信号量；nil 表示不限制
	opts Options
}

// NewManager 创建管理器（默认策略：2 次重试、并发上限 8、连续 5 次失败冷却 30s）。
func NewManager(providers ...Provider) *Manager {
	return NewManagerWithOptions(providers)
}

// NewManagerWithOptions 创建管理器并应用自定义策略。
//
// 示例：
//
//	m := llm.NewManagerWithOptions(providers,
//	    llm.WithRetry(3, 300*time.Millisecond, 5*time.Second),
//	    llm.WithMaxConcurrency(16),
//	    llm.WithCircuitBreak(10, time.Minute),
//	)
func NewManagerWithOptions(providers []Provider, opts ...Option) *Manager {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	m := &Manager{
		providers:   append([]Provider(nil), providers...),
		stateByName: map[string]*providerState{},
		states:      map[*providerState]struct{}{},
		opts:        o,
	}
	if o.MaxConcurrency > 0 {
		m.sem = make(chan struct{}, o.MaxConcurrency)
	}
	for _, p := range providers {
		st := &providerState{}
		m.stateByName[p.Name()] = st
		m.states[st] = struct{}{}
	}
	return m
}

// isOpen 检查 provider 是否处于"熔断开"状态（冷却期未到）。
func (ps *providerState) isOpen() bool {
	until := ps.openUntil.Load()
	return until > 0 && until > time.Now().UnixNano()
}

// onSuccess 调用成功：复位计数 + 关断熔断。
func (ps *providerState) onSuccess() {
	ps.consecutiveFails.Store(0)
	ps.openUntil.Store(0)
}

// onFail 调用失败（重试全部耗尽）：计数++；超过阈值则进入冷却。
func (ps *providerState) onFail(failures int64, coolDown time.Duration) {
	ps.totalFails.Add(1)
	cf := ps.consecutiveFails.Add(1)
	ps.lastFailAt.Store(time.Now().UnixNano())
	if cf >= failures {
		ps.openUntil.Store(time.Now().Add(coolDown).UnixNano())
	}
}

// status 快照。
func (ps *providerState) status(name string, available bool) ProviderStatus {
	s := ProviderStatus{
		Name:             name,
		Available:        available,
		TotalCalls:       ps.totalCalls.Load(),
		TotalFails:       ps.totalFails.Load(),
		ConsecutiveFails: ps.consecutiveFails.Load(),
	}
	if t := ps.openUntil.Load(); t > 0 {
		s.OpenUntil = time.Unix(0, t).Format(time.RFC3339)
	}
	if t := ps.lastFailAt.Load(); t > 0 {
		s.LastFailAt = time.Unix(0, t).Format(time.RFC3339)
	}
	return s
}

// Rewrite 按优先级尝试各 Provider；首个成功返回；失败时按顺序 fallback。
// Provider 若被熔断（连续失败触发冷却）则跳过；冷却期到期后允许 1 次探测。
//
// 并发安全：超过 MaxConcurrency 的调用会阻塞等待信号量；ctx 取消时立即返回。
func (m *Manager) Rewrite(ctx context.Context, prompt, content string) (string, error) {
	// 全局并发上限（在取 provider 列表之前获取，避免无谓的拷贝）。
	if m.sem != nil {
		select {
		case m.sem <- struct{}{}:
			defer func() { <-m.sem }()
		case <-ctx.Done():
			return content, ctx.Err()
		}
	}

	m.mu.RLock()
	providers := append([]Provider(nil), m.providers...)
	m.mu.RUnlock()

	if len(providers) == 0 {
		return content, nil
	}

	type try struct {
		provider Provider
		state    *providerState
	}
	var order []try
	var fallbackOrder []try // 熔断条目也放在这里，仅当所有正常条目失败后才探测一次
	for _, p := range providers {
		if !p.Available() {
			continue
		}
		m.mu.RLock()
		st := m.stateByName[p.Name()]
		m.mu.RUnlock()
		if st == nil {
			st = &providerState{}
			m.mu.Lock()
			m.stateByName[p.Name()] = st
			m.states[st] = struct{}{}
			m.mu.Unlock()
		}
		if st.isOpen() {
			fallbackOrder = append(fallbackOrder, try{p, st})
		} else {
			order = append(order, try{p, st})
		}
	}
	// 先用正常条目；正常条目全部失败，再从熔断条目里逐个"探测"1 次（帮助快速复位）
	order = append(order, fallbackOrder...)

	opts := m.opts
	var lastErr error
	tried := 0
	for _, t := range order {
		p, st := t.provider, t.state
		// 熔断条目：再做一次 isOpen 检查（期间另一条请求可能刚把它复位）；
		// 若仍 open 也尝试 1 次——这是"探测请求"，用于提前复位。
		result, err := m.callWithRetry(ctx, st, p, prompt, content, opts)
		tried++
		if err == nil {
			st.onSuccess()
			if tried > 1 {
				slog.Info("llm fallback success",
					slog.String("provider", p.Name()),
					slog.Int("attempt", tried))
			}
			return result, nil
		}
		if ctx.Err() != nil {
			// 调用方取消：不再尝试后续 provider，直接返回。
			return content, ctx.Err()
		}
		st.onFail(opts.CircuitFailures, opts.CircuitCoolDown)
		slog.Warn("llm provider fail",
			slog.String("provider", p.Name()),
			slog.Int("retries", opts.RetryMax),
			slog.Int64("consecutive_fails", st.consecutiveFails.Load()),
			slog.Any("error", err))
		lastErr = err
	}
	if lastErr != nil {
		return content, fmt.Errorf("所有 LLM 提供者均失败（已尝试 %d 个），最后错误: %w", tried, lastErr)
	}
	// 无可用 Provider，返回原文（兼容旧行为）
	return content, nil
}

// callWithRetry 带指数退避重试地调用单个 Provider。
//
// 每次 attempt 都计入 totalCalls（可观测性）；整个调用失败才累计 1 次
// consecutiveFail（熔断粒度 = 一次逻辑调用，避免网络抖动快速触发熔断）。
func (m *Manager) callWithRetry(ctx context.Context, st *providerState, p Provider, prompt, content string, opts Options) (string, error) {
	var result string
	var err error
	delay := opts.RetryBaseDelay
	for attempt := 0; ; attempt++ {
		st.totalCalls.Add(1)
		result, err = p.Rewrite(ctx, prompt, content)
		if err == nil || attempt >= opts.RetryMax {
			return result, err
		}
		// 指数退避等待（ctx 取消时中止）
		if delay <= 0 {
			delay = opts.RetryBaseDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
		delay *= 2
		if opts.RetryMaxDelay > 0 && delay > opts.RetryMaxDelay {
			delay = opts.RetryMaxDelay
		}
	}
}

// HasAvailable 是否有"已配置"的 Provider（不考虑熔断）。
func (m *Manager) HasAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.providers {
		if p.Available() {
			return true
		}
	}
	return false
}

// Status 返回每个 Provider 的运行状态快照（计数 + 熔断到期时间）。
// 健康检查 / 管理后台面板直接使用。
func (m *Manager) Status() []ProviderStatus {
	m.mu.RLock()
	providers := append([]Provider(nil), m.providers...)
	m.mu.RUnlock()

	out := make([]ProviderStatus, 0, len(providers))
	for _, p := range providers {
		m.mu.RLock()
		st := m.stateByName[p.Name()]
		m.mu.RUnlock()
		if st == nil {
			out = append(out, ProviderStatus{Name: p.Name(), Available: p.Available()})
			continue
		}
		out = append(out, st.status(p.Name(), p.Available()))
	}
	return out
}

// ErrNotConfigured LLM 未配置。
var ErrNotConfigured = errors.New("LLM 提供者未配置")

// StubProvider 默认桩实现，无 API Key 时使用。
//
// 不调用任何 LLM，直接返回原始内容，并在内容末尾追加优化提示注释。
type StubProvider struct{}

// NewStub 创建桩提供者。
func NewStub() *StubProvider { return &StubProvider{} }

func (s *StubProvider) Name() string    { return "stub" }
func (s *StubProvider) Available() bool { return false }
func (s *StubProvider) Rewrite(_ context.Context, _, content string) (string, error) {
	return content, nil
}

// MergeEngines 聚合目标引擎的预设偏好，生成统一改写约束。
func MergeEngines(engines []models.EngineType) string {
	if len(engines) == 0 {
		return ""
	}
	names := make([]string, 0, len(engines))
	for _, e := range engines {
		names = append(names, string(e))
	}
	return fmt.Sprintf("目标生成式引擎: %s。请确保内容符合这些引擎的内容偏好。", fmt.Sprintf("%v", names))
}
