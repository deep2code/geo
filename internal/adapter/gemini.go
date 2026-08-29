package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"my-geo/internal/models"
)

// GeminiAdapter Google Gemini 引擎适配器。
//
// 调用 Google Gemini API：POST {BaseURL}/v1beta/models/{model}:generateContent?key={APIKey}
// 请求体含 contents/parts/text，响应解析 candidates[0].content.parts[0].text 作为 answer。
// 注意：Gemini 通过 URL 查询参数传递 API Key（非 Bearer 头），故此处直接调用底层 doRequest。
type GeminiAdapter struct {
	BaseAdapter
}

// NewGeminiAdapter 创建 Gemini 适配器，对未设置的配置项填充合理默认值。
func NewGeminiAdapter(cfg Config) *GeminiAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com"
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-1.5-flash"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &GeminiAdapter{BaseAdapter: BaseAdapter{cfg: cfg}}
}

// Engine 返回引擎类型。
func (a *GeminiAdapter) Engine() models.EngineType { return models.EngineGemini }

// geminiPart Gemini 内容片段。
type geminiPart struct {
	Text string `json:"text"`
}

// geminiContent Gemini 内容结构。
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

// geminiRequest Gemini generateContent 请求体。
type geminiRequest struct {
	Contents []geminiContent    `json:"contents"`
	Tools    []geminiSearchTool `json:"tools,omitempty"` // google_search 联网搜索
}

// geminiGroundingChunk Gemini grounding（google_search）引用块。
type geminiGroundingChunk struct {
	Web struct {
		URI string `json:"uri"`
	} `json:"web"`
}

// geminiGroundingMetadata Gemini 联网搜索的结构化引用元数据。
type geminiGroundingMetadata struct {
	GroundingChunks []geminiGroundingChunk `json:"groundingChunks"`
}

// geminiResponse Gemini generateContent 响应体。
type geminiResponse struct {
	Candidates []struct {
		Content           geminiContent            `json:"content"`
		GroundingMetadata *geminiGroundingMetadata `json:"groundingMetadata,omitempty"`
	} `json:"candidates"`
	UsageMetadata *geminiUsage `json:"usageMetadata,omitempty"`
	Error         *apiError    `json:"error,omitempty"`
}

// geminiUsage Gemini 用量字段（promptTokenCount / candidatesTokenCount / totalTokenCount）。
type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

func (u *geminiUsage) toTokenUsage() models.TokenUsage {
	if u == nil {
		return models.TokenUsage{}
	}
	return models.TokenUsage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
}

// Query 向 Gemini 发起查询。
func (a *GeminiAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	if !a.Configured() {
		return a.mockResponse(a.Engine()), nil
	}

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: query}}},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// API Key 通过 URL 查询参数传递，不使用 Authorization 头
	requestURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		a.cfg.BaseURL, a.cfg.Model, url.QueryEscape(a.cfg.APIKey))
	var data []byte
	if a.cfg.WebSearch {
		// 联网搜索：注入 google_search 工具（Gemini App 默认联网，弥补 API 无网测量差）。
		withTools := reqBody
		withTools.Tools = geminiSearchTools()
		rawTools, err := json.Marshal(withTools)
		if err != nil {
			return nil, fmt.Errorf("序列化带搜索工具请求体失败: %w", err)
		}
		data, err = a.doRequestWithSearchFallback(ctx, requestURL, rawTools, raw, nil)
		if err != nil {
			return nil, err
		}
	} else {
		data, err = a.doRequest(ctx, "POST", requestURL, raw, nil)
	}
	if err != nil {
		return nil, err
	}

	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 Gemini 响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("Gemini API 错误: %s", resp.Error.Message)
	}

	// 回答可能分多个 part（联网/多模态场景），需全部拼接，否则截断丢引用
	answer := ""
	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			answer += part.Text
		}
	}

	// 引用 = 正文 URL 提取 + grounding 结构化引用（联网回答正文通常不内嵌裸 URL），去重合并
	citations := ExtractCitations(answer, "")
	if len(resp.Candidates) > 0 && resp.Candidates[0].GroundingMetadata != nil {
		seen := make(map[string]bool, len(citations))
		for _, c := range citations {
			seen[c.URL] = true
		}
		for _, gc := range resp.Candidates[0].GroundingMetadata.GroundingChunks {
			u := cleanURL(gc.Web.URI)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			citations = append(citations, models.Citation{URL: u, Position: len(citations) + 1})
		}
	}

	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: citations,
		Usage:     resp.UsageMetadata.toTokenUsage(),
	}, nil
}

// CheckCitation 查询 Gemini 并返回引用了 targetURL 的引用列表。
func (a *GeminiAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}
