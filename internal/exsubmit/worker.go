// Package exsubmit 的子文件：定时分析调度（worker.go）。
//
// Worker 后台定时扫描 external_submissions 中 status='pending' 的记录，逐条抽取结构
// 化结论并写回。判定失败时标记 failed + error_msg，不阻断后续记录。
package exsubmit

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// Worker 定时分析后台任务。
type Worker struct {
	store    Store
	analyzer *Analyzer
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// NewWorker 创建 Worker。interval<=0 时使用默认 5 分钟。
func NewWorker(store Store, analyzer *Analyzer, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Worker{
		store:    store,
		analyzer: analyzer,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 在独立 goroutine 中启动定时循环。进程退出时调用 Stop 释放。
func (w *Worker) Start() {
	go w.run()
}

// Stop 停止定时循环并等待结束。
func (w *Worker) Stop() {
	if w == nil || w.stop == nil {
		return
	}
	close(w.stop)
	<-w.done
}

// run 定时循环：首次立即跑一次，之后按 interval 周期处理。
func (w *Worker) run() {
	defer close(w.done)
	// 首次启动稍作延迟，避免与接口写入争抢连接。
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-timer.C:
		}
		w.runOnceSafe()
		timer.Reset(w.interval)
	}
}

// runOnceSafe 单批处理包 recover：panic 只丢失本批，worker 循环继续。
// recover 放在循环外会让 goroutine 永久退出，pending 记录从此无人分析。
func (w *Worker) runOnceSafe() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("external worker panic 已恢复", slog.Any("recover", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()
	n, err := w.ProcessOnce(context.Background())
	if err != nil {
		slog.Warn("external 分析批次失败", slog.Int("processed", n), slog.Any("error", err))
	} else if n > 0 {
		slog.Info("external 分析批次完成", slog.Int("processed", n))
	}
}

// ProcessOnce 处理一批（最多 batchSize 条）待分析记录，返回处理条数。
// 供定时循环与「手动触发」接口复用。
func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	const batchSize = 50
	pending, err := w.store.ListPending(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	now := time.Now().Unix()
	for _, sub := range pending {
		analysis, aErr := w.analyzer.Analyze(ctx, sub.Answer)
		if aErr != nil {
			_ = w.store.UpdateAnalysis(ctx, sub.ID, nil, "failed",
				truncate(aErr.Error(), 512), now)
			processed++
			continue
		}
		if analysis == nil {
			_ = w.store.UpdateAnalysis(ctx, sub.ID, nil, "failed",
				"analyzer 返回空分析结果", now)
			processed++
			continue
		}
		if err := w.store.UpdateAnalysis(ctx, sub.ID, analysis, "analyzed", "", now); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
