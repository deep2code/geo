package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/models"
)

// ClaudeAdapter Anthropic Claude 引擎适配器。
//
// 调用 Anthropic Messages API：POST {BaseURL}/v1/messages
// 请求头需附加 x-api-key 与 anthropic-version，请求体含 model/max_tokens/messages，
// 响应解析 content[0].text 作为 answer。
type ClaudeAdapter struct {
	BaseAdapter
}

// NewClaudeAdapter 创建 Claude 适配器，对未设置的配置项填充合理默认值。
func NewClaudeAdapter(cfg Config) *ClaudeAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Model == "" {
		cfg.Model = "claude-3-5-sonnet-20241022"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &ClaudeAdapter{BaseAdapter: BaseAdapter{cfg: cfg}}
}

// Engine 返回引擎类型。
func (a *ClaudeAdapter) Engine() models.EngineType { return models.EngineClaude }

// anthropicVersion Anthropic API 版本号。
const anthropicVersion = "2023-06-01"

// claudeRequest Anthropic Messages 请求体。
type claudeRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []chatMessage `json:"messages"`
}

// claudeResponse Anthropic Messages 响应体。
type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *apiError `json:"error,omitempty"`
}

// Query 向 Claude 发起查询。
func (a *ClaudeAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	if !a.Configured() {
		return a.mockResponse(a.Engine()), nil
	}

	reqBody := claudeRequest{
		Model:     a.cfg.Model,
		MaxTokens: 1024,
		Messages: []chatMessage{
			{Role: "user", Content: query},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// Anthropic 鉴权通过 x-api-key 头（doPost 同时附加的 Bearer 头同样被接受）
	headers := map[string]string{
		"x-api-key":         a.cfg.APIKey,
		"anthropic-version": anthropicVersion,
	}
	requestURL := a.cfg.BaseURL + "/v1/messages"
	data, err := a.doPost(ctx, requestURL, raw, headers)
	if err != nil {
		return nil, err
	}

	var resp claudeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 Claude 响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("Claude API 错误: %s", resp.Error.Message)
	}

	answer := ""
	if len(resp.Content) > 0 {
		answer = resp.Content[0].Text
	}

	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: ExtractCitations(answer, ""),
	}, nil
}

// CheckCitation 查询 Claude 并返回引用了 targetURL 的引用列表。
func (a *ClaudeAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}
