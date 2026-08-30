package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider 可配置的测试 Provider。
type fakeProvider struct {
	name      string
	available bool
	// failN 前 N 次调用返回错误，之后成功（-1 表示永远失败）。
	failN int64
	// calls 累计调用次数。
	calls atomic.Int64
	// delay 每次调用的耗时（模拟慢 Provider）。
	delay time.Duration
	// err 返回的错误。
	err error
	// override 非空时优先调用（用于并发度统计等测试场景）。
	override func(ctx context.Context, prompt, content string) (string, error)
}

func newFake(name string, failN int64) *fakeProvider {
	return &fakeProvider{name: name, available: true, failN: failN}
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Available() bool {
	return f.available
}
func (f *fakeProvider) Rewrite(ctx context.Context, prompt, content string) (string, error) {
	f.calls.Add(1)
	if f.override != nil {
		return f.override(ctx, prompt, content)
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.failN < 0 || f.calls.Load() <= f.failN {
		return "", errors.New("fake provider error")
	}
	return "ok:" + f.name, nil
}

func TestRewriteRetrySucceedsAfterTransientFailure(t *testing.T) {
	p := newFake("p1", 1) // 第 1 次失败、第 2 次成功（重试 1 次）
	m := NewManager(p)

	out, err := m.Rewrite(context.Background(), "prompt", "content")
	if err != nil {
		t.Fatalf("Rewrite 应重试后成功, got err=%v", err)
	}
	if out != "ok:p1" {
		t.Fatalf("Rewrite 返回内容错误: %q", out)
	}
	if got := p.calls.Load(); got != 2 {
		t.Fatalf("应调用 2 次（1 失败 + 1 重试）, got %d", got)
	}
	// 成功应复位熔断计数
	st := m.stateByName["p1"]
	if st.consecutiveFails.Load() != 0 {
		t.Fatalf("成功后连续失败计数应复位, got %d", st.consecutiveFails.Load())
	}
}

func TestRewriteExhaustsRetriesThenFallsBack(t *testing.T) {
	alwaysFail := newFake("bad", -1)
	ok := newFake("good", 0)
	m := NewManagerWithOptions(
		[]Provider{alwaysFail, ok},
		WithRetry(2, time.Millisecond, time.Millisecond),
	)

	out, err := m.Rewrite(context.Background(), "prompt", "content")
	if err != nil {
		t.Fatalf("fallback 到 good 应成功, got err=%v", err)
	}
	if out != "ok:good" {
		t.Fatalf("返回内容错误: %q", out)
	}
	// bad: 1 次逻辑调用 + 2 次重试 = 3 次 attempt
	if got := alwaysFail.calls.Load(); got != 3 {
		t.Fatalf("bad provider 应被调用 3 次（含重试）, got %d", got)
	}
	// bad 连续失败计数为 1（一次逻辑调用失败）
	st := m.stateByName["bad"]
	if got := st.consecutiveFails.Load(); got != 1 {
		t.Fatalf("一次逻辑调用失败应计 1 次, got %d", got)
	}
}

func TestRewriteCircuitBreakSkipsProvider(t *testing.T) {
	p := newFake("unstable", -1)
	m := NewManagerWithOptions(
		[]Provider{p},
		WithRetry(0, 0, 0), // 不重试，快速累积失败
		WithCircuitBreak(3, time.Hour),
	)

	// 3 次调用触发熔断
	for i := 0; i < 3; i++ {
		if _, err := m.Rewrite(context.Background(), "p", "c"); err == nil {
			t.Fatalf("第 %d 次应失败", i+1)
		}
	}
	st := m.stateByName["unstable"]
	if !st.isOpen() {
		t.Fatal("3 次连续失败后应进入熔断")
	}

	// 熔断期间：冷却期未到期，不发起任何上游调用（保护下游），返回错误
	if _, err := m.Rewrite(context.Background(), "p", "c"); err == nil {
		t.Fatal("熔断期间应返回错误")
	}
	// 冷却期内不应有任何调用（第 4 次 Rewrite 不触发上游请求）
	if p.calls.Load() != 3 {
		t.Fatalf("冷却期内不应调用 provider, got %d", p.calls.Load())
	}

	// 熔断状态应保持
	if !st.isOpen() {
		t.Fatal("冷却期内熔断应保持")
	}
	// OpenUntil 已设置
	if st.openUntil.Load() <= 0 {
		t.Fatal("openUntil 应被设置")
	}

	// 冷却到期后：放行 1 次探测，探测仍失败则重新进入熔断
	st.openUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if _, err := m.Rewrite(context.Background(), "p", "c"); err == nil {
		t.Fatal("冷却到期后探测仍失败，应返回错误")
	}
	if p.calls.Load() != 4 {
		t.Fatalf("冷却到期后探测应触发第 4 次调用, got %d", p.calls.Load())
	}
	if !st.isOpen() {
		t.Fatal("探测失败后熔断应重新进入")
	}
}

func TestRewriteConcurrencyLimit(t *testing.T) {
	slow := newFake("slow", 0)
	slow.delay = 30 * time.Millisecond
	m := NewManagerWithOptions(
		[]Provider{slow},
		WithRetry(0, 0, 0),
		WithMaxConcurrency(2),
	)

	var inflight atomic.Int64
	var maxInflight atomic.Int64

	// 用 override 统计真实并发度（不改变调用计数/耗时语义）
	slow.override = func(ctx context.Context, prompt, content string) (string, error) {
		cur := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			max := maxInflight.Load()
			if cur <= max || maxInflight.CompareAndSwap(max, cur) {
				break
			}
		}
		time.Sleep(slow.delay)
		return "ok:" + slow.name, nil
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = m.Rewrite(context.Background(), "p", "c")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Fatalf("并发调用 %d 失败: %v", i, err)
		}
	}
	if got := maxInflight.Load(); got > 2 {
		t.Fatalf("并发上限应为 2, 实际峰值 %d", got)
	}
}

func TestRewriteContextCancellation(t *testing.T) {
	p := newFake("slow", -1)
	p.delay = 50 * time.Millisecond
	m := NewManager(p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := m.Rewrite(ctx, "p", "c")
	if err == nil {
		t.Fatal("ctx 超时应返回错误")
	}
}

func TestRewriteNoProviders(t *testing.T) {
	m := NewManager()
	out, err := m.Rewrite(context.Background(), "p", "原文")
	if err != nil {
		t.Fatalf("无 Provider 时应返回原文且无错误, got %v", err)
	}
	if out != "原文" {
		t.Fatalf("应返回原文, got %q", out)
	}
}

func TestOptionsOverride(t *testing.T) {
	p := newFake("p", -1)
	m := NewManagerWithOptions(
		[]Provider{p},
		WithRetry(4, time.Millisecond, time.Millisecond),
		WithMaxConcurrency(3),
		WithCircuitBreak(2, time.Minute),
	)

	if m.opts.RetryMax != 4 {
		t.Fatalf("RetryMax 应为 4, got %d", m.opts.RetryMax)
	}
	if m.opts.MaxConcurrency != 3 {
		t.Fatalf("MaxConcurrency 应为 3, got %d", m.opts.MaxConcurrency)
	}
	if m.opts.CircuitFailures != 2 {
		t.Fatalf("CircuitFailures 应为 2, got %d", m.opts.CircuitFailures)
	}
	if m.opts.CircuitCoolDown != time.Minute {
		t.Fatalf("CircuitCoolDown 应为 1m, got %v", m.opts.CircuitCoolDown)
	}
	if cap(m.sem) != 3 {
		t.Fatalf("信号量容量应为 3, got %d", cap(m.sem))
	}
}

func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.RetryMax != defaultRetryMax || o.RetryBaseDelay != defaultRetryBaseDelay ||
		o.RetryMaxDelay != defaultRetryMaxDelay || o.MaxConcurrency != defaultMaxConcurrency ||
		o.CircuitFailures != CircuitBreakFailures || o.CircuitCoolDown != CircuitBreakCoolDown {
		t.Fatalf("默认值与常量不一致: %+v", o)
	}
}
