// openai_compatible.go 提供 OpenAI 兼容协议的通用查询逻辑。
//
// 通义千问、智谱 GLM、DeepSeek、Kimi 等国内大模型均兼容 OpenAI Chat Completions
// API 格式，仅 BaseURL 与默认模型不同。本文件抽取公共查询逻辑，各适配器嵌入
// openAICompatibleAdapter 后只需设置 engine/BaseURL/Model 即可复用。
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"my-geo/internal/models"
)

// openAICompatibleAdapter OpenAI 兼容协议公共适配器基座。
//
// 各国内大模型适配器嵌入此结构，通过 engine 字段区分身份，
// 通过 BaseURL/Model 字段适配不同服务商。
type openAICompatibleAdapter struct {
	BaseAdapter
	engine models.EngineType
}

// queryOpenAICompatible 执行 OpenAI 兼容的 Chat Completions 查询。
//
// 调用 POST {BaseURL}/v1/chat/completions（或 /chat/completions，取决于 BaseURL 配置），
// 解析 choices[0].message.content 作为 answer，从 answer 中正则提取 URL 作为 citations。
func (a *openAICompatibleAdapter) queryOpenAICompatible(ctx context.Context, query, path string) (*models.EngineResponse, error) {
	if !a.Configured() {
		return a.mockResponse(a.engine), nil
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

	requestURL := a.cfg.BaseURL + path
	var data []byte
	if a.cfg.WebSearch {
		// 联网搜索（按厂商格式）：
		//   - 通义 Qwen：enable_search 参数（非 tool）
		//   - 其余 OpenAI 兼容引擎（Kimi/GLM/豆包/Grok 等）：web_search tool
		// 端点不支持时自动回退无搜索查询。
		withTools := reqBody
		switch {
		case a.engine == models.EngineQwen:
			// 通义：enable_search 参数（非 tool）
			enabled := true
			withTools.EnableSearch = &enabled
		case a.engine == models.EngineWenxin:
			// 文心（百度千帆）：web_search 配置对象（非 tool），enable_citation 出引用角标
			withTools.WebSearch = wenxinWebSearch()
		default:
			// 其余 OpenAI 兼容引擎：web_search tool
			withTools.Tools = searchToolsFor(a.engine)
		}
		rawTools, err := json.Marshal(withTools)
		if err != nil {
			return nil, fmt.Errorf("序列化带搜索工具请求体失败: %w", err)
		}
		data, err = a.doPostWithSearchFallback(ctx, requestURL, rawTools, raw, nil)
	} else {
		data, err = a.doPost(ctx, requestURL, raw, nil)
	}
	if err != nil {
		return nil, err
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析 %s 响应失败: %w (响应: %s)", a.engine, err, truncate(data, 512))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("%s API 错误: %s", a.engine, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return &models.EngineResponse{Engine: a.engine}, nil
	}

	answer := resp.Choices[0].Message.Content
	// 文心联网回答带 ^[1]^ 引用角标，展示前清除（引用仍可从正文 URL 提取）
	if a.engine == models.EngineWenxin && a.cfg.WebSearch {
		answer = stripWenxinCitations(answer)
	}
	return &models.EngineResponse{
		Engine:    a.engine,
		Answer:    answer,
		Citations: ExtractCitations(answer, ""),
		Usage:     resp.Usage.toTokenUsage(),
	}, nil
}

// checkCitationCompat 兼容适配器通用的 CheckCitation 实现。
//
// 直接调用 queryOpenAICompatible 后筛选匹配 targetURL 的引用，
// 避免依赖 Adapter 接口（openAICompatibleAdapter 本身不实现完整接口）。
func (a *openAICompatibleAdapter) checkCitationCompat(ctx context.Context, query, targetURL, path string) ([]models.Citation, error) {
	resp, err := a.queryOpenAICompatible(ctx, query, path)
	if err != nil {
		return nil, err
	}
	return FilterCitationsByURL(resp.Citations, targetURL), nil
}

// newOpenAICompatible 创建 OpenAI 兼容适配器实例，填充默认值。
func newOpenAICompatible(engine models.EngineType, cfg Config, defaultBaseURL, defaultModel, path string) openAICompatibleAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return openAICompatibleAdapter{
		BaseAdapter: BaseAdapter{cfg: cfg},
		engine:      engine,
	}
}
