package adapter

import (
	"context"

	"my-geo/internal/models"
)

// DoubaoAdapter 豆包（字节跳动火山引擎 Ark）引擎适配器。
//
// 豆包通过火山引擎 Ark 平台提供，兼容 OpenAI Chat Completions 协议，
// BaseURL: https://ark.cn-beijing.volces.com/api/v3
// 默认模型: doubao-pro-32k（接入点 ID 或模型名）
type DoubaoAdapter struct {
	openAICompatibleAdapter
}

// NewDoubaoAdapter 创建豆包适配器。
func NewDoubaoAdapter(cfg Config) *DoubaoAdapter {
	base := newOpenAICompatible(
		models.EngineDoubao, cfg,
		"https://ark.cn-beijing.volces.com/api/v3",
		"doubao-pro-32k",
		"/chat/completions",
	)
	return &DoubaoAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *DoubaoAdapter) Engine() models.EngineType { return models.EngineDoubao }

// Query 向豆包发起查询。
func (a *DoubaoAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询豆包并返回引用了 targetURL 的引用列表。
func (a *DoubaoAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
