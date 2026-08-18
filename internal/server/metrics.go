package server

// 进程级可观测性：GET /metrics 输出 Prometheus 文本格式指标（v0.0.4 文本协议，
// 零第三方依赖，手写实现）。供 Prometheus / Grafana 抓取，端点免鉴权（不含敏感数据）。
//
// 指标一览：
//
//	geo_build_info{version,commit}         构建信息（ldflags 注入，见 SetBuildInfo）
//	geo_uptime_seconds                     进程运行时长
//	go_goroutines                          当前 goroutine 数
//	geo_http_requests_total{code="2xx"}    已完成 HTTP 请求计数（按状态码分类）
//	geo_llm_total_calls                    全部 LLM 调用（含重试 attempt）
//	geo_llm_total_fails                    全部 LLM 失败
//	geo_llm_consecutive_fails              当前连续失败（熔断累计，全 provider 之和）
//	geo_llm_circuit_open                   1=存在熔断中的 provider，0=全部正常
//	geo_llm_provider_calls_total{provider} 单 provider 调用数
//	geo_llm_provider_fails_total{provider} 单 provider 失败数
//	geo_llm_provider_open{provider}        单 provider 熔断状态（1/0）

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// 构建信息（默认 dev；发布时由 CI 通过 SetBuildInfo 或 ldflags 注入）。
var (
	buildVersion = "dev"
	buildCommit  = "none"
)

// SetBuildInfo 由 main 包在启动时同步版本信息（与 geo --version 一致）。
func SetBuildInfo(version, commit string) {
	if version != "" {
		buildVersion = version
	}
	if commit != "" {
		buildCommit = commit
	}
}

var processStartAt = time.Now()

// HTTP 请求计数（按状态码分类）。/metrics 与 /debug/pprof 自身不计入，
// 避免抓取/调试产生自增噪声。
var (
	httpReq2xx atomic.Int64
	httpReq3xx atomic.Int64
	httpReq4xx atomic.Int64
	httpReq5xx atomic.Int64
)

// observeRequest 记录一次 HTTP 请求结果（requestLogger 调用）。
func observeRequest(path string, status int) {
	if path == "/metrics" || strings.HasPrefix(path, "/debug/pprof") {
		return
	}
	switch {
	case status >= 500:
		httpReq5xx.Add(1)
	case status >= 400:
		httpReq4xx.Add(1)
	case status >= 300:
		httpReq3xx.Add(1)
	default:
		httpReq2xx.Add(1)
	}
}

// handleMetrics 输出 Prometheus 文本格式指标。
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder
	b.WriteString("# HELP geo_build_info Build information.\n# TYPE geo_build_info gauge\n")
	fmt.Fprintf(&b, "geo_build_info{version=%q,commit=%q} 1\n", buildVersion, buildCommit)

	b.WriteString("# HELP geo_uptime_seconds Process uptime in seconds.\n# TYPE geo_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "geo_uptime_seconds %d\n", int64(time.Since(processStartAt).Seconds()))

	b.WriteString("# HELP go_goroutines Number of goroutines currently running.\n# TYPE go_goroutines gauge\n")
	fmt.Fprintf(&b, "go_goroutines %d\n", runtime.NumGoroutine())

	b.WriteString("# HELP geo_http_requests_total Total HTTP requests by status class.\n# TYPE geo_http_requests_total counter\n")
	fmt.Fprintf(&b, "geo_http_requests_total{code=\"2xx\"} %d\n", httpReq2xx.Load())
	fmt.Fprintf(&b, "geo_http_requests_total{code=\"3xx\"} %d\n", httpReq3xx.Load())
	fmt.Fprintf(&b, "geo_http_requests_total{code=\"4xx\"} %d\n", httpReq4xx.Load())
	fmt.Fprintf(&b, "geo_http_requests_total{code=\"5xx\"} %d\n", httpReq5xx.Load())

	// LLM 状态（汇总 + 单 provider）
	if s.llmMgr != nil {
		statuses := s.llmMgr.Status()
		var totalCalls, totalFails, consecFails, openCount int64
		for _, st := range statuses {
			totalCalls += st.TotalCalls
			totalFails += st.TotalFails
			consecFails += st.ConsecutiveFails
			if st.OpenUntil != "" {
				openCount++
			}
		}
		b.WriteString("# HELP geo_llm_total_calls Total LLM calls including retry attempts.\n# TYPE geo_llm_total_calls counter\n")
		fmt.Fprintf(&b, "geo_llm_total_calls %d\n", totalCalls)
		b.WriteString("# HELP geo_llm_total_fails Total LLM failures.\n# TYPE geo_llm_total_fails counter\n")
		fmt.Fprintf(&b, "geo_llm_total_fails %d\n", totalFails)
		b.WriteString("# HELP geo_llm_consecutive_fails Current consecutive failures across providers.\n# TYPE geo_llm_consecutive_fails gauge\n")
		fmt.Fprintf(&b, "geo_llm_consecutive_fails %d\n", consecFails)
		b.WriteString("# HELP geo_llm_circuit_open Whether any provider circuit breaker is open.\n# TYPE geo_llm_circuit_open gauge\n")
		circuitOpen := 0
		if openCount > 0 {
			circuitOpen = 1
		}
		fmt.Fprintf(&b, "geo_llm_circuit_open %d\n", circuitOpen)

		b.WriteString("# HELP geo_llm_provider_calls_total Total calls per provider.\n# TYPE geo_llm_provider_calls_total counter\n")
		for _, st := range statuses {
			fmt.Fprintf(&b, "geo_llm_provider_calls_total{provider=%q} %d\n", st.Name, st.TotalCalls)
		}
		b.WriteString("# HELP geo_llm_provider_fails_total Total failures per provider.\n# TYPE geo_llm_provider_fails_total counter\n")
		for _, st := range statuses {
			fmt.Fprintf(&b, "geo_llm_provider_fails_total{provider=%q} %d\n", st.Name, st.TotalFails)
		}
		b.WriteString("# HELP geo_llm_provider_open Circuit breaker open state per provider.\n# TYPE geo_llm_provider_open gauge\n")
		for _, st := range statuses {
			open := 0
			if st.OpenUntil != "" {
				open = 1
			}
			fmt.Fprintf(&b, "geo_llm_provider_open{provider=%q} %d\n", st.Name, open)
		}

		// LLM 成本仪表盘指标（按模型聚合 token 与美元成本）
		cost := s.llmMgr.Cost()
		b.WriteString("# HELP geo_llm_token_total LLM token consumption by model and kind.\n# TYPE geo_llm_token_total counter\n")
		for _, row := range cost.Rows {
			fmt.Fprintf(&b, "geo_llm_token_total{model=%q,kind=\"prompt\"} %d\n", row.Model, row.PromptTokens)
			fmt.Fprintf(&b, "geo_llm_token_total{model=%q,kind=\"completion\"} %d\n", row.Model, row.CompletionTokens)
		}
		b.WriteString("# HELP geo_llm_cost_usd_total Estimated LLM cost in USD by model.\n# TYPE geo_llm_cost_usd_total counter\n")
		for _, row := range cost.Rows {
			fmt.Fprintf(&b, "geo_llm_cost_usd_total{model=%q} %.6f\n", row.Model, row.CostUSD)
		}
		b.WriteString("# HELP geo_llm_cost_total_usd Total estimated LLM cost in USD.\n# TYPE geo_llm_cost_total_usd gauge\n")
		fmt.Fprintf(&b, "geo_llm_cost_total_usd %.6f\n", cost.TotalUSD)
		if cost.BudgetUSD > 0 {
			b.WriteString("# HELP geo_llm_cost_budget_usd Configured monthly LLM budget in USD.\n# TYPE geo_llm_cost_budget_usd gauge\n")
			fmt.Fprintf(&b, "geo_llm_cost_budget_usd %.6f\n", cost.BudgetUSD)
			b.WriteString("# HELP geo_llm_cost_breached Whether the LLM budget circuit breaker is tripped.\n# TYPE geo_llm_cost_breached gauge\n")
			breached := 0
			if cost.Breached {
				breached = 1
			}
			fmt.Fprintf(&b, "geo_llm_cost_breached %d\n", breached)
		}
	}

	io.WriteString(w, b.String())
}
