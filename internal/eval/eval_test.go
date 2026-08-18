package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCitationRate(t *testing.T) {
	// 0 → 0
	if got := citationRate(0); got != 0 {
		t.Fatalf("citationRate(0) = %v, want 0", got)
	}
	// 负输入安全夹到 0
	if got := citationRate(-5); got != 0 {
		t.Fatalf("citationRate(-5) = %v, want 0", got)
	}
	// 单调性 + 有界
	prev := -1.0
	for _, rel := range []float64{0.1, 0.5, 1.0, 1.752, 5.0, 50.0} {
		got := citationRate(rel)
		if got < 0 || got > 1 {
			t.Fatalf("citationRate(%v)=%v 超出 [0,1]", rel, got)
		}
		if got < prev {
			t.Fatalf("citationRate 非单调：rel=%v got=%v < prev=%v", rel, got, prev)
		}
		prev = got
	}
	// 小值近似线性：citationRate(0.179) ≈ 0.164
	if got := citationRate(0.179); got < 0.16 || got > 0.17 {
		t.Fatalf("citationRate(0.179)=%v 期望≈0.164", got)
	}
}

// fakeChecker 内存实现 LiveCitationChecker，用于验证 Evaluate 的 live 接入分支。
type fakeChecker struct {
	cited  bool
	detail string
}

func (f fakeChecker) CheckCitation(_ context.Context, _, _, _ string) (bool, string, error) {
	return f.cited, f.detail, nil
}

func TestHTTPLiveChecker(t *testing.T) {
	cases := []struct {
		name     string
		answer   string
		target   string
		wantCite bool
	}{
		{"命中主机", "参考来源 https://example.com/page 显示……", "https://example.com/page", true},
		{"未命中", "根据公开资料，大模型推理成本主要取决于……", "https://example.com/page", false},
		{"大小写不敏感", "见 EXAMPLE.COM 的报道", "https://example.com/x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer k" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				resp := chatResponse{Choices: []struct {
					Message message `json:"message"`
				}{{Message: message{Content: tc.answer}}}}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			c := NewHTTPLiveChecker(srv.URL, "gpt-4o-mini", "k")
			cited, detail, err := c.CheckCitation(context.Background(), "q", tc.target, "content")
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if cited != tc.wantCite {
				t.Fatalf("cited=%v want %v (detail=%s)", cited, tc.wantCite, detail)
			}
			if !strings.Contains(detail, "example.com") {
				t.Fatalf("detail 未含主机名: %s", detail)
			}
		})
	}
}

func TestHTTPLiveChecker_NoKey(t *testing.T) {
	c := NewHTTPLiveChecker("https://api.openai.com/v1", "gpt-4o-mini", "")
	if _, _, err := c.CheckCitation(context.Background(), "q", "https://x.com", "c"); err == nil {
		t.Fatal("缺 API Key 应返回错误")
	}
}
