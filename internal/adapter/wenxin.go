package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/models"
)

// WenxinAdapter 文心一言（百度 ERNIE Bot）引擎适配器。
//
// 文心一言使用百度自有的 API 格式（非 OpenAI 兼容），
// BaseURL: https://aip.baidubce.com/rpc/2.0/ai_custom/v1
// API Key 通过 URL 参数 access_token 传递（非 Authorization 头）。
// 默认模型: ernie-speed-128k
type WenxinAdapter struct {
	BaseAdapter
}

// NewWenxinAdapter 创建文心一言适配器。
func NewWenxinAdapter(cfg Config) *WenxinAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://aip.baidubce.com/rpc/2.0/ai_custom/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "ernie-speed-128k"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &WenxinAdapter{BaseAdapter: BaseAdapter{cfg: cfg}}
}

// Engine 返回引擎类型。
func (a *WenxinAdapter) Engine() models.EngineType { return models.EngineWenxin }

// wenxinMessage 文心一言消息结构。
type wenxinMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// wenxinRequest 文心一言请求体。
type wenxinRequest struct {
	Messages []wenxinMessage `json:"messages"`
}

// wenxinResponse 文心一言响应体。
type wenxinResponse struct {
	Result   string    `json:"result"`             // 回答文本
	Error    *apiError `json:"error,omitempty"`    // 错误信息
	ErrorMsg string    `json:"error_msg,omitempty"` // 百度错误描述
	ID       string    `json:"id,omitempty"`
}

// Query 向文心一言发起查询。
//
// 文心一言的 API Key 作为 access_token 通过 URL 参数传递，
// 路径含模型名: /wenxin/llm/{model}
func (a *WenxinAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	if !a.Configured() {
		return a.mockResponse(a.Engine()), nil
	}

	reqBody := wenxinRequest{
		Messages: []wenxinMessage{
			{Role: "user", Content: query},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 文心一言 URL: {BaseURL}/wenxin/llm/{model}?access_token={APIKey}
	requestURL := fmt.Sprintf("%s/wenxin/llm/%s?access_token=%s",
		a.cfg.BaseURL, a.cfg.Model, a.cfg.APIKey)

	// 文心一言通过 URL 参数鉴权，不使用 Authorization 头
	data, err := a.doRequest(ctx, "POST", requestURL, raw, nil)
	if err != nil {
		return nil, err
	}

	var resp wenxinResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析文心一言响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("文心一言 API 错误: %s", resp.Error.Message)
	}
	if resp.ErrorMsg != "" {
		return nil, fmt.Errorf("文心一言 API 错误: %s", resp.ErrorMsg)
	}

	answer := resp.Result
	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: ExtractCitations(answer, ""),
	}, nil
}

// CheckCitation 查询文心一言并返回引用了 targetURL 的引用列表。
func (a *WenxinAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}
