package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"my-geo/internal/models"
)

func TestIsToolUnsupportedError(t *testing.T) {
	cases := map[error]bool{
		errors.New("API 返回错误状态码 400: unrecognized request argument supplied: tools"): true,
		errors.New("unknown parameter: 'enable_search'"):                    true,
		errors.New("model does not support tools"):                          true,
		errors.New("HTTP 请求失败: connection refused"):                     false,
		errors.New("API 返回错误状态码 401: invalid api key"):               false,
		nil: false,
	}
	for err, want := range cases {
		if got := isToolUnsupportedError(err); got != want {
			t.Errorf("isToolUnsupportedError(%v) = %v, want %v", err, got, want)
		}
	}
}

// TestQueryInjectsWebSearchTool 验证 WebSearch 开启时请求体携带 web_search 工具。
func TestQueryInjectsWebSearchTool(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"答案"}}]}`))
	}))
	defer srv.Close()

	a := NewChatGPTAdapter(Config{APIKey: "k", BaseURL: srv.URL, Model: "gpt-4o-mini", WebSearch: true})
	resp, err := a.Query(context.Background(), "最好的CRM工具")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "答案" {
		t.Fatalf("answer 错误: %q", resp.Answer)
	}
	if !strings.Contains(gotBody, `"tools"`) || !strings.Contains(gotBody, `"web_search"`) {
		t.Fatalf("请求体应包含 web_search 工具: %s", gotBody)
	}
	// 校验可解析
	var req chatCompletionRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "web_search" {
		t.Fatalf("tools 注入错误: %+v", req.Tools)
	}
}

// TestQueryNoWebSearch 未开启时请求体不含 tools（向后兼容）。
func TestQueryNoWebSearch(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	a := NewChatGPTAdapter(Config{APIKey: "k", BaseURL: srv.URL, Model: "gpt-4o-mini"})
	if _, err := a.Query(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotBody, `"tools"`) {
		t.Fatalf("未开启 WebSearch 不应注入 tools: %s", gotBody)
	}
}

// TestFallbackWhenToolUnsupported 端点返回"不支持工具"错误时，自动回退无工具重试。
func TestFallbackWhenToolUnsupported(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		if calls == 1 && strings.Contains(string(buf), "web_search") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unrecognized request argument supplied: tools"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"回退答案"}}]}`))
	}))
	defer srv.Close()

	a := NewChatGPTAdapter(Config{APIKey: "k", BaseURL: srv.URL, Model: "gpt-4o-mini", WebSearch: true})
	resp, err := a.Query(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "回退答案" {
		t.Fatalf("应回退无工具重试: %q", resp.Answer)
	}
	if calls != 2 {
		t.Fatalf("应请求 2 次（带工具失败→无工具成功），实际 %d", calls)
	}
}

// TestGeminiGoogleSearch 验证 Gemini 注入 google_search 工具。
func TestGeminiGoogleSearch(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gemini 答案"}]}}]}`))
	}))
	defer srv.Close()

	a := NewGeminiAdapter(Config{APIKey: "k", BaseURL: srv.URL, Model: "gemini-2.0-flash", WebSearch: true, Timeout: 5 * time.Second})
	resp, err := a.Query(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "gemini 答案" {
		t.Fatalf("answer: %q", resp.Answer)
	}
	if !strings.Contains(gotBody, "google_search") {
		t.Fatalf("Gemini 请求应包含 google_search 工具: %s", gotBody)
	}
}

// TestOpenAICompatibleQwenEnableSearch 验证通义用 enable_search 参数而非 tools。
func TestOpenAICompatibleQwenEnableSearch(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"通义答案"}}]}`))
	}))
	defer srv.Close()

	a := newOpenAICompatible(models.EngineQwen, Config{APIKey: "k", BaseURL: srv.URL, Model: "qwen-plus", WebSearch: true}, "", "", "/chat/completions")
	resp, err := a.queryOpenAICompatible(context.Background(), "q", "/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "通义答案" {
		t.Fatalf("answer: %q", resp.Answer)
	}
	if !strings.Contains(gotBody, `"enable_search":true`) {
		t.Fatalf("通义应使用 enable_search 参数: %s", gotBody)
	}
	if strings.Contains(gotBody, `"tools"`) {
		t.Fatalf("通义不应注入 tools: %s", gotBody)
	}
}

// TestDeepSeekToolType 验证 DeepSeek 联网走 Responses API 的 web_search 工具。
func TestDeepSeekToolType(t *testing.T) {
	tools := searchToolsFor(models.EngineDeepSeek)
	if len(tools) != 1 || tools[0].Type != "web_search" {
		t.Fatalf("DeepSeek 工具类型应为 web_search: %+v", tools)
	}
	if got := searchToolsFor(models.EngineKimi); got[0].Type != "web_search" {
		t.Fatalf("Kimi 应使用 web_search: %+v", got)
	}
}

// TestDeepSeekResponsesWebSearch 验证 DeepSeek 联网时走 /v1/responses 且解析 web_search_call 引用。
func TestDeepSeekResponsesWebSearch(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("应请求 /v1/responses，实际 %s", r.URL.Path)
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{
			"id":"resp_1","object":"response","model":"deepseek-v4-flash","status":"completed",
			"output":[
				{"type":"web_search_call","id":"c1","status":"completed","action":{"type":"search","queries":["q"]}},
				{"type":"web_search_call","id":"c2","status":"completed","action":{"type":"open_page","url":"https://news.cn/article/1"}},
				{"type":"message","id":"m1","status":"completed","phase":"final_answer","role":"assistant",
				 "content":[{"type":"output_text","text":"根据搜索，DeepSeek-V4 已发布，来源 https://news.cn/article/1"}]}
			],
			"usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150}
		}`))
	}))
	defer srv.Close()

	a := NewDeepSeekAdapter(Config{APIKey: "k", BaseURL: srv.URL, Model: "deepseek-v4-flash", WebSearch: true})
	resp, err := a.Query(context.Background(), "DeepSeek 最新版本？")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"type":"web_search"`) {
		t.Fatalf("Responses 请求应含 web_search 工具: %s", gotBody)
	}
	if !strings.Contains(resp.Answer, "DeepSeek-V4") {
		t.Fatalf("应解析出最终答案: %q", resp.Answer)
	}
	if len(resp.Citations) == 0 {
		t.Fatal("应从 web_search_call 提取引用")
	}
	if resp.Usage.TotalTokens != 150 {
		t.Fatalf("usage 解析错误: %+v", resp.Usage)
	}
}

// TestOpenAICompatibleWenxinWebSearch 验证文心注入 web_search 配置对象且清除引用角标。
func TestOpenAICompatibleWenxinWebSearch(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"文心答案^[1]^，来源 https://baike.baidu.com/item/x ^[2]^"}}]}`))
	}))
	defer srv.Close()

	a := newOpenAICompatible(models.EngineWenxin, Config{APIKey: "k", BaseURL: srv.URL, Model: "ernie-5.1", WebSearch: true}, "", "", "/chat/completions")
	resp, err := a.queryOpenAICompatible(context.Background(), "q", "/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"web_search":{"enable":true`) {
		t.Fatalf("文心应注入 web_search 配置对象: %s", gotBody)
	}
	if strings.Contains(resp.Answer, "^[") {
		t.Fatalf("引用角标应被清除: %q", resp.Answer)
	}
	if !strings.Contains(resp.Answer, "baike.baidu.com") {
		t.Fatalf("正文 URL 应保留: %q", resp.Answer)
	}
}
