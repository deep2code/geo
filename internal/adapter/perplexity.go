package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/models"
)

// PerplexityAdapter Perplexity 引擎适配器。
//
// Perplexity API 兼容 OpenAI 格式，默认 BaseURL 为 https://api.perplexity.ai，
// 调用 POST /chat/completions。响应除 choices[0].message.content 外，
// 额外返回 citations 字段（URL 字符串数组），适配器将其转换为结构化 Citation。
type PerplexityAdapter struct {
	BaseAdapter
}

// NewPerplexityAdapter 创建 Perplexity 适配器，对未设置的配置项填充合理默认值。
func NewPerplexityAdapter(cfg Config) *PerplexityAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.perplexity.ai"
	}
	if cfg.Model == "" {
		cfg.Model = "sonar"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &PerplexityAdapter{BaseAdapter: BaseAdapter{cfg: cfg}}
}

// Engine 返回引擎类型。
func (a *PerplexityAdapter) Engine() models.EngineType { return models.EnginePerplexity }

// perplexityResponse Perplexity 响应体，兼容 OpenAI 格式并包含 citations 字段。
type perplexityResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Citations []string      `json:"citations,omitempty"`
	Usage     *usagePayload `json:"usage,omitempty"`
	Error     *apiError     `json:"error,omitempty"`
}

// Query 向 Perplexity 发起查询。
func (a *PerplexityAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	if !a.Configured() {
		return a.mockResponse(a.Engine()), nil
	}

	reqBody := chatCompletionRequest{
		Model: a.cfg.Model,
		Messages: []chatMessage{
			{Role: "user", Content: query},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// Perplexity BaseURL 默认不带 /v1 前缀，路径为 /chat/completions
	requestURL := a.cfg.BaseURL + "/chat/completions"
	data, err := a.doPost(ctx, requestURL, raw, nil)
	if err != nil {
		return nil, err
	}

	var resp perplexityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 Perplexity 响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("Perplexity API 错误: %s", resp.Error.Message)
	}

	answer := ""
	if len(resp.Choices) > 0 {
		answer = resp.Choices[0].Message.Content
	}

	// 优先使用 Perplexity 返回的结构化 citations；若为空则从 answer 中正则提取
	citations := citationsFromStrings(resp.Citations)
	if len(citations) == 0 && answer != "" {
		citations = ExtractCitations(answer, "")
	}

	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: citations,
		Usage:     resp.Usage.toTokenUsage(),
	}, nil
}

// CheckCitation 查询 Perplexity 并返回引用了 targetURL 的引用列表。
func (a *PerplexityAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}

// citationsFromStrings 将 URL 字符串数组转换为结构化 Citation，按顺序编号。
func citationsFromStrings(urls []string) []models.Citation {
	if len(urls) == 0 {
		return nil
	}
	citations := make([]models.Citation, 0, len(urls))
	for i, u := range urls {
		if u == "" {
			continue
		}
		citations = append(citations, models.Citation{URL: u, Position: i + 1})
	}
	return citations
}
