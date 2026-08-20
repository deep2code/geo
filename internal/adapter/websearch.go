package adapter

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"my-geo/internal/models"
)

// searchTool OpenAI 兼容协议（Chat Completions）的联网搜索工具。
//
// OpenAI（ChatGPT）、Grok/xAI、Kimi、智谱 GLM、豆包（方舟）等厂商
// 支持 tools: [{type: "web_search"}] 注入；部分端点不支持时会报错，
// 由 doPostWithSearchFallback 自动降级。
type searchTool struct {
	Type string `json:"type"`
}

// webSearchTool 返回 OpenAI 兼容协议的 web_search 工具声明。
func webSearchTool() []searchTool {
	return []searchTool{{Type: "web_search"}}
}

// searchToolsFor 按引擎返回其官方联网搜索工具类型：
//   - DeepSeek：web_search（走 Responses API 的服务端搜索，2026 新增；
//     DeepSeekAdapter.Query 在 WebSearch 开启时直接走 POST /responses）
//   - 其余 OpenAI 兼容引擎：web_search
func searchToolsFor(engine models.EngineType) []searchTool {
	return webSearchTool()
}

// wenxinWebSearchConfig 文心（百度千帆）内置联网搜索参数对象。
//
// 官方文档（cloud.baidu.com/doc/qianfan-docs/s/Wm8r4sw29）：ERNIE 系列模型在请求体
// 追加 web_search 对象即启用联网，非 tools 方式。支持 ERNIE 5.1/5.0/X1.1/4.5-turbo。
type wenxinWebSearchConfig struct {
	Enable          bool   `json:"enable"`            // 是否启用联网搜索
	EnableCitation  bool   `json:"enable_citation"`   // 回答中返回 ^[1]^ 引用角标
	EnableTrace     bool   `json:"enable_trace"`      // 是否返回溯源信息
	EnableStatus    bool   `json:"enable_status"`     // 是否返回搜索触发信号
	SearchMode      string `json:"search_mode"`       // auto（按意图）/ required（强制）
	SearchNumber    int    `json:"search_number"`     // 检索文献数 1-28
	ReferenceNumber int    `json:"reference_number"`  // 总结文献数（≤ search_number）
}

// wenxinWebSearch 返回默认的文心联网搜索配置（开启引用角标与溯源）。
func wenxinWebSearch() *wenxinWebSearchConfig {
	return &wenxinWebSearchConfig{
		Enable:          true,
		EnableCitation:  true,
		EnableTrace:     true,
		EnableStatus:    true,
		SearchMode:      "auto",
		SearchNumber:    10,
		ReferenceNumber: 5,
	}
}

// citationMarkRE 匹配文心引用角标 ^[1]^ / ^[1][2]^。
var citationMarkRE = mustCompileCitationMark()

func mustCompileCitationMark() *regexp.Regexp {
	return regexp.MustCompile(`\^\[\d+\](?:\[\d+\])*\^`)
}

// stripWenxinCitations 去除回答中的文心引用角标（^[n]^），保留正文。
func stripWenxinCitations(answer string) string {
	return citationMarkRE.ReplaceAllString(answer, "")
}

// geminiSearchTool Gemini google_search 工具（generateContent 的 tools 字段）。
type geminiSearchTool struct {
	GoogleSearch struct{} `json:"google_search"`
}

// geminiSearchTools 返回 Gemini 的 google_search 工具。
func geminiSearchTools() []geminiSearchTool {
	return []geminiSearchTool{{}}
}

// doPostWithSearchFallback 先携带搜索工具发送；若端点不支持该工具（400 /
// unrecognized / unknown field / not supported 等），回退到无搜索版本重试一次。
//
// 这是"给所有引擎开联网"的安全网：不支持工具的端点不会崩，行为回到旧版。
func (b *BaseAdapter) doPostWithSearchFallback(ctx context.Context, url string, withTools, withoutTools []byte, headers map[string]string) ([]byte, error) {
	if len(withTools) == 0 {
		return b.doPost(ctx, url, withoutTools, headers)
	}
	data, err := b.doPost(ctx, url, withTools, headers)
	if err == nil {
		return data, nil
	}
	if !isToolUnsupportedError(err) {
		return nil, err
	}
	slog.Warn("引擎不支持联网搜索工具，回退无搜索查询（引用率可能偏低）", slog.String("url", url))
	return b.doPost(ctx, url, withoutTools, headers)
}

// isToolUnsupportedError 判断错误是否源于"端点不支持搜索工具/未知参数"。
func isToolUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, k := range []string{
		"web_search", "enable_search", "google_search", "search tool",
		"unrecognized", "unsupported", "unknown field", "unknown parameter",
		"not supported", "does not support", "unknown argument",
		"invalid parameter", "tools",
	} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

// doRequestWithSearchFallback 与 doPostWithSearchFallback 同逻辑，基于通用 doRequest
// （用于不走 doPost 的引擎，如 Gemini 的 generateContent）。
func (b *BaseAdapter) doRequestWithSearchFallback(ctx context.Context, url string, withTools, withoutTools []byte, headers map[string]string) ([]byte, error) {
	if len(withTools) == 0 {
		return b.doRequest(ctx, http.MethodPost, url, withoutTools, headers)
	}
	data, err := b.doRequest(ctx, http.MethodPost, url, withTools, headers)
	if err == nil {
		return data, nil
	}
	if !isToolUnsupportedError(err) {
		return nil, err
	}
	slog.Warn("引擎不支持联网搜索工具，回退无搜索查询（引用率可能偏低）", slog.String("url", url))
	return b.doRequest(ctx, http.MethodPost, url, withoutTools, headers)
}
