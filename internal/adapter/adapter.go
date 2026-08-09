// Package adapter 实现各大生成式引擎（AI 搜索引擎）的统一适配器。
//
// GEO（生成式引擎优化）区别于传统 SEO 的核心能力在于：直接查询各大生成式引擎，
// 检测目标内容是否被引用、测量可见度。本包通过统一的 Adapter 接口封装不同引擎的
// API 差异（ChatGPT / Perplexity / Gemini / Claude），上层只需面向接口编程。
//
// 设计要点：
//   - 未配置 APIKey 时不报错，返回模拟响应，保证系统可无 key 运行
//   - 所有 HTTP 调用支持 context 超时取消
//   - 仅使用标准库 net/http，不引入第三方 HTTP 库
package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"my-geo/internal/models"
)

// Adapter 生成式引擎适配器接口。
//
// 各引擎适配器实现该接口，封装对应 AI 引擎的 API 调用，上层面向接口编程。
type Adapter interface {
	// Engine 返回适配器对应的引擎类型。
	Engine() models.EngineType
	// Query 向引擎发起查询，返回回答与引用列表。
	Query(ctx context.Context, query string) (*models.EngineResponse, error)
	// CheckCitation 查询引擎并返回引用了 targetURL 的引用列表。
	// 若目标 URL 未被引用则返回空切片。
	CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error)
	// Configured 是否已配置 API Key（未配置时返回模拟响应）。
	Configured() bool
}

// Config 适配器配置。
type Config struct {
	APIKey  string        // 引擎 API Key，为空时返回模拟响应
	BaseURL string        // 引擎 API 基地址，可配置，有合理默认值
	Model   string        // 使用的模型名称
	Timeout time.Duration // HTTP 调用超时时间，为 0 时使用默认值
}

// BaseAdapter 公共适配器基座，封装各引擎共享的 HTTP 调用与配置逻辑。
//
// 各具体适配器通过嵌入 BaseAdapter 复用 doPost/doGet 等方法。
type BaseAdapter struct {
	cfg Config
}

// Configured 是否已配置 APIKey。
func (b *BaseAdapter) Configured() bool {
	return b.cfg.APIKey != ""
}

// Config 返回当前配置（只读使用）。
func (b *BaseAdapter) Config() Config { return b.cfg }

// doRequest 核心请求方法，执行带 context 超时控制的 HTTP 调用。
//
// 自动附加 Content-Type: application/json，但不附加鉴权头，
// 由 doPost/doGet 或具体适配器按需附加。
func (b *BaseAdapter) doRequest(ctx context.Context, method, requestURL string, body []byte, headers map[string]string) ([]byte, error) {
	// 超时控制：以 Config.Timeout 与 ctx 自身截止时间中更早者为准
	if b.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.cfg.Timeout)
		defer cancel()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, fmt.Errorf("API 返回错误状态码 %d: %s", resp.StatusCode, truncate(data, 512))
	}
	return data, nil
}

// doPost 执行 POST 请求，自动附加 Authorization: Bearer {APIKey} 鉴权头。
//
// 适用于 OpenAI 兼容协议（ChatGPT / Perplexity）以及 Anthropic（Bearer 同样被接受）。
// 调用方可通过 headers 附加额外请求头。
func (b *BaseAdapter) doPost(ctx context.Context, requestURL string, body []byte, headers map[string]string) ([]byte, error) {
	h := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		h[k] = v
	}
	if b.cfg.APIKey != "" {
		h["Authorization"] = "Bearer " + b.cfg.APIKey
	}
	return b.doRequest(ctx, http.MethodPost, requestURL, body, h)
}

// doGet 执行 GET 请求，自动附加 Authorization: Bearer {APIKey} 鉴权头。
func (b *BaseAdapter) doGet(ctx context.Context, requestURL string, headers map[string]string) ([]byte, error) {
	h := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		h[k] = v
	}
	if b.cfg.APIKey != "" {
		h["Authorization"] = "Bearer " + b.cfg.APIKey
	}
	return b.doRequest(ctx, http.MethodGet, requestURL, nil, h)
}

// mockResponse 未配置 APIKey 时返回的模拟响应。
//
// answer 中提示需配置 key，Citations 为空，保证系统可无 key 运行。
func (b *BaseAdapter) mockResponse(engine models.EngineType) *models.EngineResponse {
	return &models.EngineResponse{
		Engine: engine,
		Answer: fmt.Sprintf("[模拟响应] 未配置 %s 引擎的 API Key，无法执行真实查询。"+
			"请在 Config.APIKey 中配置有效密钥后重试。", engine),
		Citations: nil,
	}
}

// checkCitationDefault 默认的 CheckCitation 实现：调用 Query 后筛选匹配 targetURL 的引用。
//
// 各适配器可复用此函数，避免重复代码。未配置 APIKey 时 Query 返回模拟响应（引用为空），
// 筛选结果自然为空，满足“无 key 时 CheckCitation 返回空”的要求。
func checkCitationDefault(a Adapter, ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	resp, err := a.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return FilterCitationsByURL(resp.Citations, targetURL), nil
}

// urlRE 匹配回答文本中的 URL 引用。
var urlRE = regexp.MustCompile(`https?://[^\s\)\]\"'<>，。；：、]+`)

// ExtractCitations 从回答文本中提取 URL 引用，返回去重后的引用列表。
//
// 若 targetURL 非空，仅返回匹配 targetURL 的引用；若 targetURL 为空，返回全部提取到的引用。
// 提取出的引用 Position 按出现顺序从 1 开始编号。
func ExtractCitations(answer, targetURL string) []models.Citation {
	matches := urlRE.FindAllString(answer, -1)
	seen := make(map[string]bool, len(matches))
	var citations []models.Citation
	pos := 1
	for _, raw := range matches {
		u := cleanURL(raw)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		if targetURL != "" && !urlMatch(u, targetURL) {
			continue
		}
		citations = append(citations, models.Citation{URL: u, Position: pos})
		pos++
	}
	return citations
}

// FilterCitationsByURL 从已有引用列表中筛选匹配 targetURL 的引用。
func FilterCitationsByURL(citations []models.Citation, targetURL string) []models.Citation {
	if targetURL == "" {
		return citations
	}
	var matched []models.Citation
	for _, c := range citations {
		if urlMatch(c.URL, targetURL) {
			matched = append(matched, c)
		}
	}
	return matched
}

// urlMatch 判断 citationURL 是否指向 targetURL（host 必须一致，path 为前缀关系）。
func urlMatch(citationURL, targetURL string) bool {
	if citationURL == "" || targetURL == "" {
		return false
	}
	ch, cp := splitHostPath(citationURL)
	th, tp := splitHostPath(targetURL)
	// 解析失败时退化为标准化字符串比较
	if ch == "" || th == "" {
		return cleanURL(citationURL) == cleanURL(targetURL)
	}
	if ch != th {
		return false
	}
	// host 一致：任一 path 为空视为匹配整站
	if cp == "" || cp == "/" || tp == "" || tp == "/" {
		return true
	}
	if cp == tp {
		return true
	}
	return strings.HasPrefix(cp, tp+"/")
}

// splitHostPath 解析 URL 的 host（小写）与 path。
func splitHostPath(rawurl string) (host, path string) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Host), u.Path
}

// cleanURL 清理 URL 首尾的标点空白（正则可能带入句末标点）。
func cleanURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimRight(u, ".,;:!?)\"'】〉》」』")
	u = strings.TrimRight(u, "/")
	return u
}

// truncate 截断字节切片用于错误信息展示。
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// NewAdapter 工厂函数，根据引擎类型返回对应适配器。
//
// 未配置 APIKey 时返回的适配器仍可正常创建，调用 Query 时返回模拟响应。
func NewAdapter(engine models.EngineType, cfg Config) (Adapter, error) {
	switch engine {
	case models.EngineChatGPT:
		return NewChatGPTAdapter(cfg), nil
	case models.EnginePerplexity:
		return NewPerplexityAdapter(cfg), nil
	case models.EngineGemini:
		return NewGeminiAdapter(cfg), nil
	case models.EngineClaude:
		return NewClaudeAdapter(cfg), nil
	// 国内大模型
	case models.EngineQwen:
		return NewQwenAdapter(cfg), nil
	case models.EngineGLM:
		return NewGLMAdapter(cfg), nil
	case models.EngineDeepSeek:
		return NewDeepSeekAdapter(cfg), nil
	case models.EngineKimi:
		return NewKimiAdapter(cfg), nil
	case models.EngineWenxin:
		return NewWenxinAdapter(cfg), nil
	case models.EngineDoubao:
		return NewDoubaoAdapter(cfg), nil
	case models.EngineXiaomi:
		return NewXiaomiAdapter(cfg), nil
	case models.EngineXunfei:
		return NewXunfeiAdapter(cfg), nil
	case models.EngineYuanbao:
		return NewYuanbaoAdapter(cfg), nil
	default:
		return nil, fmt.Errorf("不支持的引擎类型: %s", engine)
	}
}
