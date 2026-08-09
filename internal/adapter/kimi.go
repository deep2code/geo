package adapter

import (
	"context"

	"my-geo/internal/models"
)

// KimiAdapter Kimi（月之暗面 Moonshot）引擎适配器。
//
// Kimi 兼容 OpenAI Chat Completions 协议，
// BaseURL: https://api.moonshot.cn
// 默认模型: moonshot-v1-8k
type KimiAdapter struct {
	openAICompatibleAdapter
}

// NewKimiAdapter 创建 Kimi 适配器。
func NewKimiAdapter(cfg Config) *KimiAdapter {
	base := newOpenAICompatible(
		models.EngineKimi, cfg,
		"https://api.moonshot.cn",
		"moonshot-v1-8k",
		"/v1/chat/completions",
	)
	return &KimiAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *KimiAdapter) Engine() models.EngineType { return models.EngineKimi }

// Query 向 Kimi 发起查询。
func (a *KimiAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/v1/chat/completions")
}

// CheckCitation 查询 Kimi 并返回引用了 targetURL 的引用列表。
func (a *KimiAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/v1/chat/completions")
}
