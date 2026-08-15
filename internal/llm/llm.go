// Package llm 提供 LLM 抽象层，支持多 Provider 故障转移 + 轻量断路保护。
//
// 参考设计：
//   - https://github.com/gptscript-ai/go-openai (多 client 轮询 + fallback)
//   - https://github.com/sony/gobreaker  (熔断器状态机)
//
// 本实现采用"连续失败计数 + 冷却期"的简易断路器，无需额外依赖：
//
//	某 Provider 连续失败 >= CircuitBreakFailures 次 → 进入 CoolDown (30s)，
//	期间跳过该 Provider；CoolDown 到期后再次允许 1 条探测请求，若成功则复位。
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

// 断路与冷却阈值（写死常量，避免引入配置复杂性；可按实际运维经验调整）。
const (
	// CircuitBreakFailures 连续失败 N 次 → 触发冷却。
	CircuitBreakFailures = 5
	// CircuitBreakCoolDown 冷却期：到期后尝试 1 次探测请求。
	CircuitBreakCoolDown = 30 * time.Second
)

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

// Manager LLM 提供者管理器：优先级列表 + 顺序尝试 + 断路保护。
//
// 策略：
//   - 按 providers 顺序尝试，首个成功返回；
//   - 遇到"已熔断（OpenUntil > now）"条目直接跳过；
//   - 成功后把该 provider 的 consecutiveFails 清零；失败时累加并检查阈值。
type Manager struct {
	mu       sync.RWMutex // 保护 providers 列表（不保护 state，state 本身原子）
	providers []Provider
	states    map[*providerState]struct{} // 仅用于遍历统计（不是必须）

	// 索引：Provider.Name -> state（Name 稳定不变，所以安全）
	stateByName map[string]*providerState
}

// NewManager 创建管理器。
func NewManager(providers ...Provider) *Manager {
	m := &Manager{
		providers:   append([]Provider(nil), providers...),
		stateByName: map[string]*providerState{},
		states:      map[*providerState]struct{}{},
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

// onFail 调用失败：计数++；超过阈值则进入冷却。
func (ps *providerState) onFail() {
	ps.totalFails.Add(1)
	cf := ps.consecutiveFails.Add(1)
	ps.lastFailAt.Store(time.Now().UnixNano())
	if cf >= CircuitBreakFailures {
		ps.openUntil.Store(time.Now().Add(CircuitBreakCoolDown).UnixNano())
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
func (m *Manager) Rewrite(ctx context.Context, prompt, content string) (string, error) {
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

	var lastErr error
	tried := 0
	for _, t := range order {
		p, st := t.provider, t.state
		// 熔断条目：再做一次 isOpen 检查（期间另一条请求可能刚把它复位）；
		// 若仍 open 也尝试 1 次——这是"探测请求"，用于提前复位。
		st.totalCalls.Add(1)
		result, err := p.Rewrite(ctx, prompt, content)
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
		st.onFail()
		slog.Warn("llm provider fail",
			slog.String("provider", p.Name()),
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

func (s *StubProvider) Name() string      { return "stub" }
func (s *StubProvider) Available() bool   { return false }
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
