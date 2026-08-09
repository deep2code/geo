package adapter

import (
	"context"

	"my-geo/internal/models"
)

// WenxinAdapter 文心一言（百度 ERNIE Bot，千帆兼容模式）引擎适配器。
//
// 通过百度千帆平台的 OpenAI 兼容模式接入，兼容 OpenAI Chat Completions 协议，
// BaseURL: https://qianfan.baidubce.com/v2
// 默认模型: ernie-speed-128k
type WenxinAdapter struct {
	openAICompatibleAdapter
}

// NewWenxinAdapter 创建文心一言适配器。
func NewWenxinAdapter(cfg Config) *WenxinAdapter {
	base := newOpenAICompatible(
		models.EngineWenxin, cfg,
		"https://qianfan.baidubce.com/v2",
		"ernie-speed-128k",
		"/chat/completions",
	)
	return &WenxinAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *WenxinAdapter) Engine() models.EngineType { return models.EngineWenxin }

// Query 向文心一言发起查询。
func (a *WenxinAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询文心一言并返回引用了 targetURL 的引用列表。
func (a *WenxinAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
