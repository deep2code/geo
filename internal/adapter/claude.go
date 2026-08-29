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

// claudeSearchTool Anthropic 服务端联网搜索工具。
//
// GA 版要求带日期版本号的 type 与 name 字段：裸 {"type":"web_search"} 会被
// Messages API 拒绝（400），触发降级导致联网空转。
type claudeSearchTool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// claudeWebSearchTool 返回 Anthropic web_search 工具声明。
func claudeWebSearchTool() []claudeSearchTool {
	return []claudeSearchTool{{Type: "web_search_20250305", Name: "web_search"}}
}

// claudeRequest Anthropic Messages 请求体。
type claudeRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []chatMessage `json:"messages"`
	// Tools 联网搜索工具（Anthropic 2025 支持 web_search tool）。
	Tools []claudeSearchTool `json:"tools,omitempty"`
}

// claudeResponse Anthropic Messages 响应体。
type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *claudeUsage `json:"usage,omitempty"`
	Error *apiError    `json:"error,omitempty"`
}

// claudeUsage Anthropic 用量字段（input_tokens / output_tokens）。
type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (u *claudeUsage) toTokenUsage() models.TokenUsage {
	if u == nil {
		return models.TokenUsage{}
	}
	return models.TokenUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
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
	var data []byte
	if a.cfg.WebSearch {
		// 联网搜索：注入 Anthropic web_search 工具（Claude App 联网行为）。
		withTools := reqBody
		withTools.Tools = claudeWebSearchTool()
		rawTools, err := json.Marshal(withTools)
		if err != nil {
			return nil, fmt.Errorf("序列化带搜索工具请求体失败: %w", err)
		}
		data, err = a.doPostWithSearchFallback(ctx, requestURL, rawTools, raw, headers)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		data, err = a.doPost(ctx, requestURL, raw, headers)
		if err != nil {
			return nil, err
		}
	}
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

	// content 为块数组（text / server_tool_use / web_search_tool_result 混排），
	// 需拼接全部 text 块；只取 Content[0] 在工具块在前时返回空答案
	answer := ""
	for _, block := range resp.Content {
		if block.Type == "text" {
			answer += block.Text
		}
	}

	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: ExtractCitations(answer, ""),
		Usage:     resp.Usage.toTokenUsage(),
	}, nil
}

// CheckCitation 查询 Claude 并返回引用了 targetURL 的引用列表。
func (a *ClaudeAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}
