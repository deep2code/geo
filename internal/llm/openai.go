// openai.go OpenAI 兼容 LLM Provider 实现。
//
// 支持所有兼容 OpenAI Chat Completions API 的服务（OpenAI / Azure / GLM / 本地模型）。
// 通过 BaseURL 可配置不同的兼容端点。

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider OpenAI 兼容的 LLM 提供者。
type OpenAIProvider struct {
	apiKey  string
	baseURL string // 默认 https://api.openai.com
	model   string // 默认 gpt-4o-mini
	client  *http.Client
}

// OpenAIOption 配置选项。
type OpenAIOption func(*OpenAIProvider)

// WithBaseURL 设置 API 基础地址。
func WithBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) { p.baseURL = strings.TrimRight(url, "/") }
}

// WithModel 设置模型名称。
func WithModel(model string) OpenAIOption {
	return func(p *OpenAIProvider) { p.model = model }
}

// WithTimeout 设置 HTTP 超时。
func WithTimeout(d time.Duration) OpenAIOption {
	return func(p *OpenAIProvider) { p.client.Timeout = d }
}

// NewOpenAI 创建 OpenAI 兼容 Provider。
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com",
		model:   "gpt-4o-mini",
		client:  &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *OpenAIProvider) Name() string    { return "openai:" + p.model }
func (p *OpenAIProvider) Available() bool { return p.apiKey != "" }

// Rewrite 调用 Chat Completions API 改写内容。
func (p *OpenAIProvider) Rewrite(ctx context.Context, prompt, content string) (string, error) {
	if !p.Available() {
		return content, ErrNotConfigured
	}

	// 组合 system + user 消息
	systemMsg := "你是一位 GEO（生成式引擎优化）专家，擅长优化内容使其更容易被 AI 搜索引擎引用。"
	userMsg := prompt + "\n\n待优化内容：\n" + content

	body := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": userMsg},
		},
		"temperature": 0.4,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return content, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := p.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return content, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return content, fmt.Errorf("调用 LLM 失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return content, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return content, fmt.Errorf("LLM 返回错误 %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return content, fmt.Errorf("解析响应失败: %w", err)
	}
	if result.Error != nil {
		return content, fmt.Errorf("LLM 错误: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return content, fmt.Errorf("LLM 返回空结果")
	}

	rewritten := strings.TrimSpace(result.Choices[0].Message.Content)
	if rewritten == "" {
		return content, fmt.Errorf("LLM 返回空内容")
	}
	return rewritten, nil
}
