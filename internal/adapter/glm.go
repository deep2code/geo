package adapter

import (
	"context"

	"my-geo/internal/models"
)

// GLMAdapter 智谱 GLM（智谱AI BigModel）引擎适配器。
//
// 智谱 GLM-4 兼容 OpenAI Chat Completions 协议，
// BaseURL: https://open.bigmodel.cn/api/paas/v4
// 默认模型: glm-4-flash
type GLMAdapter struct {
	openAICompatibleAdapter
}

// NewGLMAdapter 创建智谱 GLM 适配器。
func NewGLMAdapter(cfg Config) *GLMAdapter {
	base := newOpenAICompatible(
		models.EngineGLM, cfg,
		"https://open.bigmodel.cn/api/paas/v4",
		"glm-4-flash",
		"/chat/completions",
	)
	return &GLMAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *GLMAdapter) Engine() models.EngineType { return models.EngineGLM }

// Query 向智谱 GLM 发起查询。
func (a *GLMAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	return a.queryOpenAICompatible(ctx, query, "/chat/completions")
}

// CheckCitation 查询智谱 GLM 并返回引用了 targetURL 的引用列表。
func (a *GLMAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/chat/completions")
}
