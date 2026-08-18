package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LiveCitationChecker 用真实生成式引擎查询 query，判断 target 是否被引用/提及。
//
// 这是把评测从"离线代理"升级为"实测"的可插拔接口：实现可对接 ChatGPT / Perplexity /
// 通义千问 / DeepSeek 等任意引擎。本包内置一个 OpenAI 兼容 Chat Completions 的 HTTP 实现
// （见 HTTPLiveChecker），也可自行实现该接口对接其他引擎或缓存层。
type LiveCitationChecker interface {
	// CheckCitation 返回：是否被引用（cited）、诊断信息（detail）、错误。
	// targetURL 为目标页面地址，targetContent 为其内容（供实现方按需构造查询/比对）。
	CheckCitation(ctx context.Context, query, targetURL, targetContent string) (cited bool, detail string, err error)
}

// HTTPLiveChecker 基于 OpenAI 兼容 Chat Completions API 的实测引用检查器。
//
// 判定逻辑：向引擎提交 query，取回答文本，检查 targetURL 的主机名是否出现在回答中。
// 这是 GEO 可见度的合理代理：被引用 ≈ 回答中出现了该来源。如需更精确（如语义匹配、
// 引用块识别），可实现自己的 LiveCitationChecker。
type HTTPLiveChecker struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
	// HostOf 从 targetURL 提取用于匹配的主机名（可覆盖，默认取 URL 主机）。
	HostOf func(targetURL string) string
}

// NewHTTPLiveChecker 构造 OpenAI 兼容检查器（base 形如 https://api.openai.com/v1）。
func NewHTTPLiveChecker(baseURL, model, apiKey string) *HTTPLiveChecker {
	return &HTTPLiveChecker{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// CheckCitation 实现 LiveCitationChecker。
func (c *HTTPLiveChecker) CheckCitation(ctx context.Context, query, targetURL, _ string) (bool, string, error) {
	if c.APIKey == "" {
		return false, "", fmt.Errorf("live 检查器缺少 API Key（通过 --llm-key 或 GEO_LLM_KEY 提供）")
	}
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []message{
			{Role: "system", Content: "你是中立的检索摘要助手，只根据已知信息简要回答用户问题。"},
			{Role: "user", Content: query},
		},
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return false, "", err
	}
	endpoint := c.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return false, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("引擎返回 %d: %s", resp.StatusCode, string(data))
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return false, "", err
	}
	if len(cr.Choices) == 0 {
		return false, "引擎未返回 choices", nil
	}
	answer := cr.Choices[0].Message.Content

	// 提取并匹配来源主机名
	host := ""
	if c.HostOf != nil {
		host = c.HostOf(targetURL)
	} else if targetURL != "" {
		if h, e := extractHost(targetURL); e == nil {
			host = h
		}
	}
	if host == "" {
		return false, "无可用来源主机（target_url 为空或无法解析）", nil
	}
	if strings.Contains(strings.ToLower(answer), strings.ToLower(host)) {
		return true, fmt.Sprintf("回答中包含来源主机 %q", host), nil
	}
	return false, fmt.Sprintf("回答中未出现来源主机 %q", host), nil
}

// extractHost 从 URL 提取主机名（含端口则去除）。
func extractHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}
