package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleMetrics(t *testing.T) {
	s := &Server{}
	// 造一些请求计数（2xx / 4xx / 5xx；/metrics 自身不应计数）
	observeRequest("/api/v1/health", http.StatusOK)
	observeRequest("/api/v1/health", http.StatusOK)
	observeRequest("/api/v1/brand/audit", http.StatusNotFound)
	observeRequest("/api/v1/brand/report/pdf", http.StatusInternalServerError)
	observeRequest("/metrics", http.StatusOK)               // 应被过滤
	observeRequest("/debug/pprof/goroutine", http.StatusOK) // 应被过滤

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.handleMetrics(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()

	want := []string{
		`geo_build_info{version="dev",commit="none"} 1`,
		`geo_http_requests_total{code="2xx"} 2`,
		`geo_http_requests_total{code="3xx"} 0`,
		`geo_http_requests_total{code="4xx"} 1`,
		`geo_http_requests_total{code="5xx"} 1`,
		"geo_uptime_seconds",
		"go_goroutines",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("metrics 输出缺少 %q\n--- 实际输出 ---\n%s", w, body)
		}
	}
}

func TestObserveRequestFiltersSelf(t *testing.T) {
	// 清空计数后验证过滤逻辑（/metrics、/debug/pprof 不计入）
	before2xx := httpReq2xx.Load()
	observeRequest("/metrics", 200)
	observeRequest("/debug/pprof/profile", 200)
	if httpReq2xx.Load() != before2xx {
		t.Errorf("自身端点不应计入计数: before=%d after=%d", before2xx, httpReq2xx.Load())
	}
}

func TestMetricsWithLLMManager(t *testing.T) {
	// 有 LLM 管理器时输出 llm 指标段
	s := &Server{llmMgr: newLLMManagerFromEnv()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.handleMetrics(rec, req)
	body := rec.Body.String()

	want := []string{
		"# HELP geo_llm_total_calls",
		"geo_llm_total_calls 0",
		"# HELP geo_llm_circuit_open",
		"geo_llm_circuit_open 0",
		"geo_llm_provider_calls_total",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("metrics 缺少 %q\n--- 实际输出 ---\n%s", w, body)
		}
	}
}

func TestSetBuildInfo(t *testing.T) {
	SetBuildInfo("v9.9.9", "abc123")
	defer SetBuildInfo("dev", "none")
	if buildVersion != "v9.9.9" || buildCommit != "abc123" {
		t.Errorf("SetBuildInfo 未生效: version=%q commit=%q", buildVersion, buildCommit)
	}
}
