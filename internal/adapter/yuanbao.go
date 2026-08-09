package adapter

import (
	"context"

	"my-geo/internal/models"
)

// YuanbaoAdapter 元宝/混元（腾讯）引擎适配器。
//
// 腾讯混元通过腾讯云提供，兼容 OpenAI Chat Completions 协议，
// BaseURL: https://api.hunyuan.cloud.tencent.com/v1
// 默认模型: hunyuan-lite
type YuanbaoAdapter struct {
	openAICompatibleAdapter
}

// NewYuanbaoAdapter 创建元宝/混元适配器。
func NewYuanbaoAdapter(cfg Config) *YuanbaoAdapter {
	base := newOpenAICompatible(
		models.EngineYuanbao, cfg,
		"https://api.hunyuan.cloud.tencent.com/v1",
		"hunyuan-lite",
		"/chat/completions",
	)
	return &YuanbaoAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *YuanbaoAdapter) Engine() models.EngineType { return models.EngineYuanbao }

// Query 向元宝/混元发起查询。
func (a *YuanbaoAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询元宝/混元并返回引用了 targetURL 的引用列表。
func (a *YuanbaoAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
