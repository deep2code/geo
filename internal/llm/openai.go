// openai.go OpenAI 兼容 LLM Provider 实现（官方 SDK：github.com/openai/openai-go/v3）。
//
// 支持所有兼容 OpenAI Chat Completions API 的服务（OpenAI / GLM / 本地模型）。
// 通过 BaseURL 可配置不同的兼容端点：
//   - 空          → SDK 默认（https://api.openai.com/v1/）
//   - 仅主机名    → 自动补 /v1（如 https://api.openai.com → …/v1，与旧版行为一致）
//   - 带版本路径  → 原样使用（如 https://open.bigmodel.cn/api/paas/v4）
package llm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"my-geo/internal/models"
)

// OpenAIProvider OpenAI 兼容的 LLM 提供者。
type OpenAIProvider struct {
	apiKey  string
	baseURL string // 空 = SDK 默认；见文件头注释的补 /v1 规则
	model   string // 默认 gpt-4o-mini
	timeout time.Duration
	sdk     openai.Client

	// P1-3：成本控制参数（可通过 With 选项覆盖，默认值见 NewOpenAI）。
	maxTokens   int     // 请求体 max_completion_tokens，<=0 表示不发送（用服务端默认）
	temperature float64 // 请求体 temperature

	// 最近一次调用的 token 用量（成本仪表盘用）。atomic.Value 存 models.TokenUsage，
	// 避免与并发调用竞争——成本统计为近似值，允许极小误差。
	lastUsage atomic.Value
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

// WithTimeout 设置单次请求超时。
func WithTimeout(d time.Duration) OpenAIOption {
	return func(p *OpenAIProvider) { p.timeout = d }
}

// WithMaxTokens 设置单次请求 max_completion_tokens 上限（P1-3 成本控制；<=0 不发送）。
// 注：Chat Completions 的 max_tokens 已废弃，推理模型（o 系/gpt-5 系）仅认本参数。
func WithMaxTokens(n int) OpenAIOption {
	return func(p *OpenAIProvider) { p.maxTokens = n }
}

// WithTemperature 设置采样温度（默认 0.4）。
func WithTemperature(t float64) OpenAIOption {
	return func(p *OpenAIProvider) { p.temperature = t }
}

// NewOpenAI 创建 OpenAI 兼容 Provider（官方 openai-go SDK v3）。
// 默认 maxTokens=2048（控制单次改写成本），temperature=0.4。
func NewOpenAI(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:      apiKey,
		baseURL:     "",
		model:       "gpt-4o-mini",
		timeout:     60 * time.Second,
		maxTokens:   2048,
		temperature: 0.4,
	}
	for _, o := range opts {
		o(p)
	}

	clientOpts := []option.RequestOption{
		option.WithAPIKey(p.apiKey),
		option.WithRequestTimeout(p.timeout),
	}
	if base := normalizeBaseURL(p.baseURL); base != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(base))
	}
	p.sdk = openai.NewClient(clientOpts...)
	return p
}

// normalizeBaseURL 归一化兼容端点：仅主机名时补 /v1（旧版行为），带版本路径原样。
func normalizeBaseURL(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" || u.Path == "/" {
		return raw + "/v1"
	}
	return raw
}

func (p *OpenAIProvider) Name() string    { return "openai:" + p.model }
func (p *OpenAIProvider) Available() bool { return p.apiKey != "" }

// isReasoningModel 推理模型不接受自定义 temperature（仅默认 1）。
// 按前缀识别：o1/o3/o4 系与 gpt-5 系。
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	for _, p := range []string{"o1", "o3", "o4", "gpt-5"} {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

// Rewrite 调用 Chat Completions API（SDK）改写内容。
func (p *OpenAIProvider) Rewrite(ctx context.Context, prompt, content string) (string, error) {
	if !p.Available() {
		return content, ErrNotConfigured
	}

	// P1-3：per-call 超时兜底——调用方 ctx 未设 deadline 时（如 Background），
	// 派生 p.timeout 作为单次调用上限，避免悬挂。
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	// 组合 system + user 消息
	systemMsg := "你是一位 GEO（生成式引擎优化）专家，擅长优化内容使其更容易被 AI 搜索引擎引用。"
	userMsg := prompt + "\n\n待优化内容：\n" + content

	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(p.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemMsg),
			openai.UserMessage(userMsg),
		},
	}
	// 推理模型（o1/o3/o4、gpt-5 系）不接受自定义 temperature（仅默认 1），发送会 400
	if !isReasoningModel(p.model) {
		params.Temperature = openai.Float(p.temperature)
	}
	// P1-3：显式发送 max_completion_tokens 上限，token 成本可控（旧参数 max_tokens 已废弃）
	if p.maxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(p.maxTokens))
	}

	completion, err := p.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) {
			return content, fmt.Errorf("调用 LLM 失败: %s", apiErr.Error())
		}
		return content, fmt.Errorf("调用 LLM 失败: %w", err)
	}
	if len(completion.Choices) == 0 {
		return content, fmt.Errorf("LLM 返回空结果")
	}

	rewritten := strings.TrimSpace(completion.Choices[0].Message.Content)
	if rewritten == "" {
		return content, fmt.Errorf("LLM 返回空内容")
	}

	// 记录 token 用量供成本仪表盘聚合（缺省 usage 时记为 0）。
	if usage := completion.Usage; usage.TotalTokens > 0 {
		p.lastUsage.Store(models.TokenUsage{
			PromptTokens:     int(usage.PromptTokens),
			CompletionTokens: int(usage.CompletionTokens),
			TotalTokens:      int(usage.TotalTokens),
		})
	}
	return rewritten, nil
}

// LastUsage 返回最近一次成功调用的 token 用量（成本仪表盘用）。
// 并发调用下为近似值——成本统计允许极小误差。
func (p *OpenAIProvider) LastUsage() models.TokenUsage {
	if v, ok := p.lastUsage.Load().(models.TokenUsage); ok {
		return v
	}
	return models.TokenUsage{}
}

// Model 返回模型名（成本归因用）。
func (p *OpenAIProvider) Model() string { return p.model }
