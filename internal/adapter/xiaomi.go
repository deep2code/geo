package adapter

import (
	"context"

	"my-geo/internal/models"
)

// XiaomiAdapter 小米大模型（MiLM）引擎适配器。
//
// 小米大模型兼容 OpenAI Chat Completions 协议，
// BaseURL: https://api.xiaomi.com/v1
// 默认模型: milm-6b
type XiaomiAdapter struct {
	openAICompatibleAdapter
}

// NewXiaomiAdapter 创建小米大模型适配器。
func NewXiaomiAdapter(cfg Config) *XiaomiAdapter {
	base := newOpenAICompatible(
		models.EngineXiaomi, cfg,
		"https://api.xiaomi.com/v1",
		"milm-6b",
		"/chat/completions",
	)
	return &XiaomiAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *XiaomiAdapter) Engine() models.EngineType { return models.EngineXiaomi }

// Query 向小米大模型发起查询。
func (a *XiaomiAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询小米大模型并返回引用了 targetURL 的引用列表。
func (a *XiaomiAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
