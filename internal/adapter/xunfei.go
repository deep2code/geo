package adapter

import (
	"context"
	"time"

	"my-geo/internal/models"
)

// XunfeiAdapter 讯飞星火（科大讯飞 Spark）引擎适配器。
//
// 讯飞星火提供 OpenAI 兼容接口（spark-api-open），
// BaseURL: https://spark-api-open.xf-yun.com/v1
// 默认模型: generalv3.5（星火认知大模型 v3.5）
// API Key 格式: APIKey:APISecret（Bearer 认证）
type XunfeiAdapter struct {
	openAICompatibleAdapter
}

// NewXunfeiAdapter 创建讯飞星火适配器，对未设置的配置项填充合理默认值。
func NewXunfeiAdapter(cfg Config) *XunfeiAdapter {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	base := newOpenAICompatible(
		models.EngineXunfei, cfg,
		"https://spark-api-open.xf-yun.com/v1",
		"generalv3.5",
		"/chat/completions",
	)
	return &XunfeiAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *XunfeiAdapter) Engine() models.EngineType { return models.EngineXunfei }

// Query 向讯飞星火发起查询。
func (a *XunfeiAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询讯飞星火并返回引用了 targetURL 的引用列表。
func (a *XunfeiAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
