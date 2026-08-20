package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"my-geo/internal/models"
)

// DeepSeekAdapter DeepSeek 引擎适配器。
//
// 接口演进（2026 更新）：
//   - 默认走 OpenAI 兼容 Chat Completions（POST /chat/completions），兼容旧部署。
//   - 开启联网搜索（WebSearch=true）时，改走 **Responses API**（POST /responses）：
//     DeepSeek 2026 新增服务端执行的 web_search 工具（tools:[{type:"web_search"}]），
//     一次请求内由服务端自动完成"搜索→开页→合成答案"闭环（目前 deepseek-v4-flash 支持）。
//     Chat Completions 端点无服务端搜索（仅 function calling），故联网必须走 Responses。
//   - Responses 失败（模型不支持等）自动降级回 Chat Completions 无网查询。
//
// BaseURL: https://api.deepseek.com
// 默认模型: deepseek-chat
type DeepSeekAdapter struct {
	openAICompatibleAdapter
}

// NewDeepSeekAdapter 创建 DeepSeek 适配器。
func NewDeepSeekAdapter(cfg Config) *DeepSeekAdapter {
	base := newOpenAICompatible(
		models.EngineDeepSeek, cfg,
		"https://api.deepseek.com",
		"deepseek-chat",
		"/v1/chat/completions",
	)
	return &DeepSeekAdapter{openAICompatibleAdapter: base}
}

// Engine 返回引擎类型。
func (a *DeepSeekAdapter) Engine() models.EngineType { return models.EngineDeepSeek }

// Query 向 DeepSeek 发起查询。
//
// 联网搜索开启时优先走 Responses API（服务端 web_search，2026 新增能力）；
// 任何失败自动降级回 Chat Completions 无网查询（与旧版行为一致）。
func (a *DeepSeekAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	if a.Configured() && a.cfg.WebSearch {
		if resp, err := a.queryResponses(ctx, query); err == nil {
			return resp, nil
		} else {
			_ = err // 降级：Chat Completions 无网查询
		}
	}
	return a.queryOpenAICompatible(ctx, query, "/v1/chat/completions")
}

// CheckCitation 查询 DeepSeek 并返回引用了 targetURL 的引用列表。
func (a *DeepSeekAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return a.checkCitationCompat(ctx, query, targetURL, "/v1/chat/completions")
}

// ---------- Responses API（2026：服务端 web_search）----------

// deepSeekResponsesRequest DeepSeek Responses API 请求体。
type deepSeekResponsesRequest struct {
	Model string        `json:"model"`
	Input []chatMessage `json:"input"`
	Tools []searchTool  `json:"tools,omitempty"`
	Store bool          `json:"store,omitempty"` // DeepSeek 无会话，固定 false
}

// deepSeekResponsesResponse DeepSeek Responses API 响应体。
//
// output 为执行记录数组：reasoning / web_search_call / message（final_answer）。
type deepSeekResponsesResponse struct {
	Output []struct {
		Type    string `json:"type"` // reasoning / web_search_call / message
		Status  string `json:"status"`
		Phase   string `json:"phase,omitempty"` // message 的 final_answer
		Role    string `json:"role,omitempty"`
		Action  struct {
			Type    string   `json:"type"` // search / open_page
			Queries []string `json:"queries,omitempty"`
			URL     string   `json:"url,omitempty"`
		} `json:"action,omitempty"`
		Content []struct {
			Type string `json:"type"` // output_text
			Text string `json:"text"`
		} `json:"content,omitempty"`
	} `json:"output"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

// queryResponses 走 DeepSeek Responses API，注入服务端 web_search 工具并收集引用。
func (a *DeepSeekAdapter) queryResponses(ctx context.Context, query string) (*models.EngineResponse, error) {
	reqBody := deepSeekResponsesRequest{
		Model: a.cfg.Model,
		Input: []chatMessage{{Role: "user", Content: query}},
		Tools: []searchTool{{Type: "web_search"}}, // 服务端 web_search（Responses 格式）
		Store: false,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化 DeepSeek Responses 请求体失败: %w", err)
	}

	requestURL := a.cfg.BaseURL + "/responses"
	data, err := a.doPost(ctx, requestURL, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek Responses 请求失败: %w", err)
	}

	var resp deepSeekResponsesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 DeepSeek Responses 响应失败: %w (响应: %s)", err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("DeepSeek Responses API 错误: %s", resp.Error.Message)
	}

	// 汇总最终答案（message + phase=final_answer 的 output_text）
	answer := ""
	for _, o := range resp.Output {
		if o.Type == "message" && (o.Phase == "final_answer" || o.Phase == "") {
			for _, c := range o.Content {
				if c.Type == "output_text" && c.Text != "" {
					answer += c.Text
				}
			}
		}
	}
	// 引用：web_search_call 的 open_page URL（服务端实际打开的页面）
	var citations []models.Citation
	pos := 1
	seen := map[string]bool{}
	for _, o := range resp.Output {
		if o.Type == "web_search_call" && o.Action.Type == "open_page" && o.Action.URL != "" {
			u := cleanURL(o.Action.URL)
			if !seen[u] {
				seen[u] = true
				citations = append(citations, models.Citation{URL: u, Position: pos})
				pos++
			}
		}
	}
	// 回答文本中若还有 URL 也补上（服务端有时直接把链接写进答案）
	citations = append(citations, ExtractCitations(answer, "")...)

	usage := models.TokenUsage{}
	if resp.Usage != nil {
		usage = models.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return &models.EngineResponse{
		Engine:    a.Engine(),
		Answer:    answer,
		Citations: dedupeCitations(citations),
		Usage:     usage,
	}, nil
}

// dedupeCitations 按 URL 去重引用（合并 Responses 结构化引用与文本提取）。
func dedupeCitations(in []models.Citation) []models.Citation {
	seen := map[string]bool{}
	out := make([]models.Citation, 0, len(in))
	for _, c := range in {
		if c.URL == "" || seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		c.Position = len(out) + 1
		out = append(out, c)
	}
	return out
}
