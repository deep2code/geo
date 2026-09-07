package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type roundTripRecorder struct {
	Request *http.Request
	Status  int
	Body    string
}

func newTransportProvider(status int, responseBody string) (*OpenAIProvider, *roundTripRecorder) {
	recorder := &roundTripRecorder{Status: status, Body: responseBody}
	provider := NewOpenAI("test-key",
		WithModel("gpt-4o-mini"),
		WithTimeout(time.Second),
		WithTemperature(0.3),
		WithMaxTokens(64),
	)
	provider.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder.Request = request
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Header:     make(http.Header),
		}, nil
	})
	return provider, recorder
}

func TestOpenAIProviderRewrite(t *testing.T) {
	var requestBody openAIChatRequest
	provider, recorder := newTransportProvider(http.StatusOK, `{"choices":[{"message":{"content":"优化内容"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	got, err := provider.Rewrite(context.Background(), "提示词", "原始内容")
	if err != nil {
		t.Fatalf("Rewrite 失败: %v", err)
	}
	if got != "优化内容" {
		t.Fatalf("返回内容错误: %q", got)
	}
	if err := json.NewDecoder(recorder.Request.Body).Decode(&requestBody); err != nil {
		t.Fatalf("解析请求失败: %v", err)
	}
	if recorder.Request.URL.Path != "/v1/chat/completions" {
		t.Fatalf("请求路径错误: %q", recorder.Request.URL.Path)
	}
	if recorder.Request.Header.Get("Authorization") != "Bearer test-key" {
		t.Fatalf("Authorization 头错误: %q", recorder.Request.Header.Get("Authorization"))
	}
	if requestBody.Model != "gpt-4o-mini" || len(requestBody.Messages) != 2 {
		t.Fatalf("请求参数错误: %+v", requestBody)
	}
	if requestBody.Temperature == nil || *requestBody.Temperature != 0.3 {
		t.Fatalf("temperature 错误: %+v", requestBody.Temperature)
	}
	if requestBody.MaxCompletionTokens == nil || *requestBody.MaxCompletionTokens != 64 {
		t.Fatalf("max_completion_tokens 错误: %+v", requestBody.MaxCompletionTokens)
	}
	usage := provider.LastUsage()
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Fatalf("token 用量错误: %+v", usage)
	}
}

func TestOpenAIProviderRewriteAPIError(t *testing.T) {
	provider, _ := newTransportProvider(http.StatusTooManyRequests, `{"error":{"message":"rate limit exceeded"}}`)
	_, err := provider.Rewrite(context.Background(), "提示词", "原始内容")
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("期望包含 API 错误信息, got %v", err)
	}
}
