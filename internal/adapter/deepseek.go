package adapter

import (
	"context"

	"my-geo/internal/models"
)

// DeepSeekAdapter DeepSeek 引擎适配器。
//
// DeepSeek 完全兼容 OpenAI Chat Completions 协议，
// BaseURL: https://api.deepseek.com
// 默认模型: deepseek-chat
type DeepSeekAdapter struct {
	openAICompatibleAdapter
}

// NewDeepSeekAdapter 创建 DeepSeek 适配器。
func NewDeepSeekAdapter(cfg Config) *DeepSeekAdapter {
	base := newOpenAICompatible(
		models.EngineDeepSeek, cfg,
		"https://api.deepseek.com",
		"deepseek-chat",
		"/v1/chat/completions",
	)
	return &DeepSeekAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *DeepSeekAdapter) Engine() models.EngineType { return models.EngineDeepSeek }

// Query 向 DeepSeek 发起查询。
func (a *DeepSeekAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/v1/chat/completions")
}

// CheckCitation 查询 DeepSeek 并返回引用了 targetURL 的引用列表。
func (a *DeepSeekAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/v1/chat/completions")
}
