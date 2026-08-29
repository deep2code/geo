package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/models"
)

// ChatGPTAdapter ChatGPT (OpenAI) 引擎适配器。
//
// 调用 OpenAI Chat Completions API：POST {BaseURL}/v1/chat/completions
// 请求体含 model 与 messages，响应解析 choices[0].message.content 作为 answer，
// 并从 answer 中正则提取 URL 作为 citations。
type ChatGPTAdapter struct {
	BaseAdapter
}

// NewChatGPTAdapter 创建 ChatGPT 适配器，对未设置的配置项填充合理默认值。
func NewChatGPTAdapter(cfg Config) *ChatGPTAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &ChatGPTAdapter{BaseAdapter: BaseAdapter{cfg: cfg}}
}

// Engine 返回引擎类型。
func (a *ChatGPTAdapter) Engine() models.EngineType { return models.EngineChatGPT }

// chatMessage OpenAI 消息结构。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionRequest OpenAI Chat Completions 请求体。
type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// Tools 联网搜索工具（web_search）。未配置 WebSearch 时为空（不注入）。
	Tools []searchTool `json:"tools,omitempty"`
	// EnableSearch 通义/部分 OpenAI 兼容端点的联网开关参数（非 tool 方式）。
	EnableSearch *bool `json:"enable_search,omitempty"`
	// WebSearch 文心（百度千帆）内置联网搜索配置对象（非 tool 方式）。
	WebSearch *wenxinWebSearchConfig `json:"web_search,omitempty"`
}

// chatCompletionResponse OpenAI Chat Completions 响应体。
type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage *usagePayload `json:"usage,omitempty"`
	Error *apiError     `json:"error,omitempty"`
}

// usagePayload OpenAI 兼容协议的 token 用量字段。
type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// toTokenUsage 将 usagePayload 转为 models.TokenUsage。
func (u *usagePayload) toTokenUsage() models.TokenUsage {
	if u == nil {
		return models.TokenUsage{}
	}
	return models.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

// apiError 各引擎通用的错误结构。
type apiError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Query 向 ChatGPT 发起查询。
func (a *ChatGPTAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
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

	requestURL := a.cfg.BaseURL + "/v1/chat/completions"
	var data []byte
	if a.cfg.WebSearch {
		// 联网搜索：模拟 ChatGPT App 的默认 Browse 行为（测量真实用户看到的引用）。
		// 端点不支持 web_search 工具时自动回退无搜索查询。
		withTools := reqBody
		withTools.Tools = webSearchTool()
		rawTools, err := json.Marshal(withTools)
		if err != nil {
			return nil, fmt.Errorf("序列化带搜索工具请求体失败: %w", err)
		}
		data, err = a.doPostWithSearchFallback(ctx, requestURL, rawTools, raw, nil)
		if err != nil {
			return nil, err
		}
	} else {
		data, err = a.doPost(ctx, requestURL, raw, nil)
	}
	if err != nil {
		return nil, err
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 ChatGPT 响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("ChatGPT API 错误: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return &models.EngineResponse{Engine: a.Engine()}, nil
	}

	answer := resp.Choices[0].Message.Content
	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: ExtractCitations(answer, ""),
		Usage:     resp.Usage.toTokenUsage(),
	}, nil
}

// CheckCitation 查询 ChatGPT 并返回引用了 targetURL 的引用列表。
func (a *ChatGPTAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}
