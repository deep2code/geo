// Package llm 提供 LLM 抽象层，支持多 Provider 与故障转移。
//
// 参考 geo-optimizer (Go) 的 LLM 抽象层设计：策略引擎通过 Provider 接口
// 调用 LLM 完成内容改写，具体实现可替换（OpenAI 兼容 / 自建模型）。
// 未配置 API Key 时使用 StubProvider，仅返回规则化预处理结果。
package llm

import (
	"context"
	"errors"
	"fmt"

	"my-geo/internal/models"
)

// Provider LLM 提供者接口。
type Provider interface {
	// Name 提供者名称。
	Name() string
	// Rewrite 根据提示词改写内容。
	Rewrite(ctx context.Context, prompt, content string) (string, error)
	// Available 是否可用（已配置 API Key 等）。
	Available() bool
}

// Manager LLM 提供者管理器，支持多 Provider 故障转移。
type Manager struct {
	providers []Provider
}

// NewManager 创建管理器。
func NewManager(providers ...Provider) *Manager {
	return &Manager{providers: providers}
}

// Rewrite 按优先级尝试各 Provider，首个成功的返回结果。
func (m *Manager) Rewrite(ctx context.Context, prompt, content string) (string, error) {
	if len(m.providers) == 0 {
		return content, nil
	}
	var lastErr error
	for _, p := range m.providers {
		if !p.Available() {
			continue
		}
		result, err := p.Rewrite(ctx, prompt, content)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return content, fmt.Errorf("所有 LLM 提供者均失败，最后错误: %w", lastErr)
	}
	// 无可用 Provider，返回原文
	return content, nil
}

// HasAvailable 是否有可用 Provider。
func (m *Manager) HasAvailable() bool {
	for _, p := range m.providers {
		if p.Available() {
			return true
		}
	}
	return false
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
