package adapter

import (
	"context"

	"my-geo/internal/models"
)

// QwenAdapter 通义千问（阿里云 DashScope）引擎适配器。
//
// 通义千问兼容 OpenAI Chat Completions 协议，
// BaseURL: https://dashscope.aliyuncs.com/compatible-mode
// 默认模型: qwen-plus
type QwenAdapter struct {
	openAICompatibleAdapter
}

// NewQwenAdapter 创建通义千问适配器。
func NewQwenAdapter(cfg Config) *QwenAdapter {
	base := newOpenAICompatible(
		models.EngineQwen, cfg,
		"https://dashscope.aliyuncs.com/compatible-mode",
		"qwen-plus",
		"/v1/chat/completions",
	)
	return &QwenAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *QwenAdapter) Engine() models.EngineType { return models.EngineQwen }

// Query 向通义千问发起查询。
func (a *QwenAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/v1/chat/completions")
}

// CheckCitation 查询通义千问并返回引用了 targetURL 的引用列表。
func (a *QwenAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/v1/chat/completions")
}
