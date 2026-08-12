package adapter

import (
	"context"

	"my-geo/internal/models"
)

// GrokAdapter xAI Grok 引擎适配器。
//
// Grok 完全兼容 OpenAI Chat Completions 协议，
// BaseURL: https://api.x.ai
// 默认模型: grok-2-latest
type GrokAdapter struct {
	openAICompatibleAdapter
}

// NewGrokAdapter 创建 Grok 适配器。
func NewGrokAdapter(cfg Config) *GrokAdapter {
	base := newOpenAICompatible(
		models.EngineGrok, cfg,
		"https://api.x.ai",
		"grok-2-latest",
		"/v1/chat/completions",
	)
	return &GrokAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *GrokAdapter) Engine() models.EngineType { return models.EngineGrok }

// Query 向 Grok 发起查询。
func (a *GrokAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/v1/chat/completions")
}

// CheckCitation 查询 Grok 并返回引用了 targetURL 的引用列表。
func (a *GrokAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/v1/chat/completions")
}
