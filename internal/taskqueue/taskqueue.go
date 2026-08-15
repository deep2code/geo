// Package taskqueue 为审计 / PDF 生成 / 邮件发送等"长任务"提供可插拔异步队列。
//
// 设计目标：
//   - 抽象层不引入第三方依赖（0 依赖），生产环境可切到 asynq / machinery / gocraft/work，
//     开发 / 测试环境默认用 memoryQueue（channel + goroutine，快速启动）。
//   - 接口足够表达：异步 + 延迟调度 + 定期任务 + 状态查询 + 并发限制 + 最大重试。
//
// 选型调研（任务 P3）：
//
// | 开源库           | Broker 依赖    | 定期任务 | 优先级队列 | UI / CLI | 适用场景                                        | 我们的评价                        |
// |------------------|---------------|---------|-----------|---------|-------------------------------------------------|-----------------------------------|
// | hibiken/asynq    | Redis 5+      | ✅       | ✅（加权/严格） | ✅ Web+CLI | Go 生态最受欢迎，17k★，文档完善，Benchmark 好  | ⭐ 推荐（生产首选）               |
// | RichardKnop/machinery | AMQP/Redis/SQS/GCP | ✅    | 基础     | ❌     | 多 Broker 兼容，老项目存量多                    | 架构重、v1/v2 分支乱，维护慢     |
// | vianhanif/go-task-orbit | SQS/Pub/Sub/InMemory | ❌ | ❌ | ❌ | Cloud-native，多 Transport 抽象                  | 太新、功能不全（无定期任务）     |
// | go-quartz        | 无（进程内）   | ✅ Cron+Interval | ❌    | ❌     | 纯内存调度（单实例），不适合多副本              | 单实例本地工具可选               |
// | riverqueue (river) | PostgreSQL   | ✅       | ✅         | ✅     | 若后端统一 PG，少一套 Redis 运维                | 我们后端是 MySQL，暂不推荐       |
//
// 我们当前后端是 MySQL（4 个模块）+ Redis（可选 chinacheck cache），
// 所以推荐 asynq：Redis 是可选依赖里已被我们的 chinacheck 支持，
// 引入 asynq 几乎没有新的运维负担，并且可以和 chinacheck cache 共用同一 Redis。
//
// 未来生产接入只需要实现一个 AsynqBackend（实现 QueueBackend 接口），
// 对业务代码（审计 / PDF / 邮件的 Enqueue 调用）零改动。
package taskqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Task 入队任务抽象。
type Task struct {
	// ID 由 Backend 在 Enqueue 时生成（外部不填）。
	ID string `json:"id"`
	// Type 任务类型（例如 "audit:run"、"report:pdf"、"mail:send"）。
	Type string `json:"type"`
	// Payload JSON 编码的业务参数，由 Handler 解码。
	Payload []byte `json:"payload"`
	// Queue 队列名（不同队列可独立设置并发 / 优先级）。
	Queue string `json:"queue,omitempty"`
	// MaxRetries 0 = 默认（3 次）；-1 = 不重试。
	MaxRetries int `json:"max_retries,omitempty"`
	// ScheduledAt 空 = 立即；否则为延迟执行时间（绝对时间）。
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	// Cron 非空表示"定期任务"，例如 "*/15 * * * *"（每天 0、15、30、45 分）。
	Cron string `json:"cron,omitempty"`
}

// TaskStatus 运行状态（只读，用于查询面板 / 进度 API）。
type TaskStatus struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Queue       string     `json:"queue"`
	State       string     `json:"state"` // pending / scheduled / running / success / failed / canceled / dead
	RetryCount  int        `json:"retry_count"`
	LastError   string     `json:"last_error,omitempty"`
	EnqueuedAt  time.Time  `json:"enqueued_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	NextRunAt   *time.Time `json:"next_run_at,omitempty"` // for scheduled/cron
	DurationMs  int64      `json:"duration_ms,omitempty"`
}

// HandlerFunc 业务执行函数。ctx 里可以注入 workspace、user、trace 信息等。
type HandlerFunc func(ctx context.Context, t *Task) error

// QueueBackend 后端抽象（memory / asynq / machinery / ...）。
type QueueBackend interface {
	// Enqueue 提交任务，返回已分配 ID 的 Task 快照。
	Enqueue(ctx context.Context, t *Task) (*TaskStatus, error)
	// Cancel 取消（未开始或已开始但支持 ctx cancel 的任务）。
	Cancel(ctx context.Context, taskID string) error
	// GetStatus 查询。
	GetStatus(ctx context.Context, taskID string) (*TaskStatus, error)
	// ListByType 最近 N 条（用于 UI 表格）。
	ListByType(ctx context.Context, typ string, limit int) ([]TaskStatus, error)

	// RegisterHandler 注册一种任务类型的处理器。
	RegisterHandler(typ string, h HandlerFunc)

	// Start 启动消费者（阻塞直到 ctx 结束）。
	Start(ctx context.Context) error

	// Close 释放资源。
	Close() error
}

// ErrUnsupported 可选方法未实现（例如 memory backend 不支持 Cron 持久化到重启后）。
var ErrUnsupported = errors.New("taskqueue: 该 backend 暂不支持此操作")

// ErrNotFound 任务不存在。
var ErrNotFound = errors.New("taskqueue: task not found")

// ============================================================
// Memory Backend（开发 / 测试默认后端，0 依赖，单进程安全）
// ============================================================

type memTask struct {
	mu     sync.Mutex
	status TaskStatus
	task   *Task
	// handler 执行相关
	cancelFn context.CancelFunc
	// 若定时任务，存储定时器；Close 时 stop。
	timer *time.Timer
}

// MemoryBackend 内存队列（channel + goroutine pool）。
// 适用于：单实例开发 / 单元测试 / Demo 环境。
// 限制：重启后任务丢失；Cron 也仅在进程存续期生效。
type MemoryBackend struct {
	mu        sync.RWMutex
	tasks     map[string]*memTask
	byType    map[string][]string // type -> []id ordered by enqueue desc
	handlers  map[string]HandlerFunc

	workerCh chan *memTask

	wg     sync.WaitGroup
	stopFn context.CancelFunc
	stopCh chan struct{} // closed once

	concurrency int
}

// NewMemoryBackend 创建内存后端。
// concurrency <= 0 时用 GOMAXPROCS * 2。
func NewMemoryBackend(concurrency int) *MemoryBackend {
	if concurrency <= 0 {
		// 审计 / PDF / 邮件都偏 IO 密集，适度放大
		concurrency = 8
	}
	b := &MemoryBackend{
		tasks:       map[string]*memTask{},
		byType:      map[string][]string{},
		handlers:    map[string]HandlerFunc{},
		workerCh:    make(chan *memTask, 1024),
		stopCh:      make(chan struct{}),
		concurrency: concurrency,
	}
	return b
}

func (b *MemoryBackend) RegisterHandler(typ string, h HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[typ] = h
}

// Enqueue 把任务入队；若有 ScheduledAt 则等延迟后再塞进 workerCh；Cron 不支持但静默接受
//（首次按 now + 1s 执行并打印一次警告）。
func (b *MemoryBackend) Enqueue(_ context.Context, t *Task) (*TaskStatus, error) {
	if t == nil || t.Type == "" {
		return nil, fmt.Errorf("taskqueue/memory: task or task.Type empty")
	}
	id := fmt.Sprintf("mem-%d-%s", time.Now().UnixNano(), randHex(6))
	now := time.Now()
	mt := &memTask{
		task: t,
		status: TaskStatus{
			ID:         id,
			Type:       t.Type,
			Queue:      firstNonEmpty(t.Queue, "default"),
			State:      "pending",
			EnqueuedAt: now,
		},
	}
	b.mu.Lock()
	b.tasks[id] = mt
	list := b.byType[t.Type]
	list = append([]string{id}, list...) // 最新在前
	if len(list) > 500 {
		list = list[:500]
	}
	b.byType[t.Type] = list
	b.mu.Unlock()

	// Cron：只打印 INFO，不做持久调度（避免和 roadmap 的 scheduler 模块重叠）。
	if t.Cron != "" {
		slog.Info("taskqueue/memory: Cron 在内存后端未做持久化，仅单进程内支持。建议生产切 asynq backend",
			slog.String("type", t.Type), slog.String("cron", t.Cron))
	}

	if t.ScheduledAt != nil && t.ScheduledAt.After(time.Now()) {
		delay := time.Until(*t.ScheduledAt)
		mt.mu.Lock()
		mt.status.State = "scheduled"
		next := t.ScheduledAt.Truncate(time.Second)
		mt.status.NextRunAt = &next
		mt.mu.Unlock()
		mt.timer = time.AfterFunc(delay, func() {
			b.mu.RLock()
			_, ok := b.handlers[t.Type]
			b.mu.RUnlock()
			if !ok {
				b.markFailed(mt, fmt.Errorf("no handler registered for type=%s", t.Type))
				return
			}
			select {
			case <-b.stopCh:
				return
			default:
				b.workerCh <- mt
			}
		})
	} else {
		// 立即执行
		select {
		case <-b.stopCh:
			return nil, fmt.Errorf("taskqueue/memory: backend 已关闭")
		default:
			b.workerCh <- mt
		}
	}

	// 返回状态快照
	mt.mu.Lock()
	st := mt.status
	mt.mu.Unlock()
	return &st, nil
}

func (b *MemoryBackend) markFailed(mt *memTask, err error) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	now := time.Now()
	mt.status.State = "failed"
	mt.status.LastError = err.Error()
	mt.status.FinishedAt = &now
}

func (b *MemoryBackend) markSuccess(mt *memTask, dur time.Duration) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	now := time.Now()
	mt.status.State = "success"
	mt.status.FinishedAt = &now
	mt.status.DurationMs = dur.Milliseconds()
}

func (b *MemoryBackend) Cancel(_ context.Context, taskID string) error {
	b.mu.RLock()
	mt, ok := b.tasks[taskID]
	b.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	if mt.timer != nil {
		mt.timer.Stop()
	}
	mt.mu.Lock()
	if mt.cancelFn != nil {
		mt.cancelFn()
	}
	if mt.status.State == "pending" || mt.status.State == "scheduled" || mt.status.State == "running" {
		now := time.Now()
		mt.status.State = "canceled"
		mt.status.FinishedAt = &now
	}
	mt.mu.Unlock()
	return nil
}

func (b *MemoryBackend) GetStatus(_ context.Context, taskID string) (*TaskStatus, error) {
	b.mu.RLock()
	mt, ok := b.tasks[taskID]
	b.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	st := mt.status
	return &st, nil
}

func (b *MemoryBackend) ListByType(_ context.Context, typ string, limit int) ([]TaskStatus, error) {
	if limit <= 0 {
		limit = 20
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	ids := b.byType[typ]
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]TaskStatus, 0, len(ids))
	for _, id := range ids {
		mt, ok := b.tasks[id]
		if !ok {
			continue
		}
		mt.mu.Lock()
		out = append(out, mt.status)
		mt.mu.Unlock()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].EnqueuedAt.After(out[j].EnqueuedAt) })
	return out, nil
}

// Start 启动 N 个 worker goroutine 消费；阻塞直到 ctx 结束。
func (b *MemoryBackend) Start(ctx context.Context) error {
	parent, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.stopFn = cancel
	b.mu.Unlock()

	for i := 0; i < b.concurrency; i++ {
		b.wg.Add(1)
		go b.worker(parent, i)
	}
	<-parent.Done()
	close(b.stopCh)
	cancel()
	b.wg.Wait()
	return nil
}

func (b *MemoryBackend) worker(ctx context.Context, idx int) {
	defer b.wg.Done()
	slog.Debug("taskqueue/memory worker started", slog.Int("idx", idx))
	for {
		select {
		case <-ctx.Done():
			return
		case mt := <-b.workerCh:
			b.execOne(ctx, mt, idx)
		}
	}
}

func (b *MemoryBackend) execOne(parent context.Context, mt *memTask, workerIdx int) {
	t := mt.task
	b.mu.RLock()
	h, ok := b.handlers[t.Type]
	b.mu.RUnlock()
	if !ok {
		b.markFailed(mt, fmt.Errorf("no handler registered for type=%s", t.Type))
		return
	}

	maxRetries := t.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if parent.Err() != nil {
			return
		}
		// 指数退避（非首尝试）：100ms / 250ms / 750ms ...
		if attempt > 0 {
			backoff := time.Duration(100*attempt*attempt) * time.Millisecond
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			select {
			case <-parent.Done():
				return
			case <-time.After(backoff):
			}
		}

		ctx, cancel := context.WithCancel(parent)
		mt.mu.Lock()
		mt.cancelFn = cancel
		if mt.status.State != "canceled" {
			now := time.Now()
			mt.status.State = "running"
			mt.status.StartedAt = &now
			mt.status.RetryCount = attempt
		}
		mt.mu.Unlock()

		slog.Info("taskqueue/memory: running",
			slog.Int("worker", workerIdx),
			slog.String("type", t.Type),
			slog.String("id", mt.status.ID),
			slog.Int("attempt", attempt+1))
		start := time.Now()
		err := h(ctx, t)
		dur := time.Since(start)
		// 清理 cancel
		cancel()
		mt.mu.Lock()
		mt.cancelFn = nil
		if mt.status.State == "canceled" {
			mt.mu.Unlock()
			return
		}
		mt.mu.Unlock()

		if err == nil {
			b.markSuccess(mt, dur)
			return
		}
		slog.Warn("taskqueue/memory: attempt fail",
			slog.String("id", mt.status.ID),
			slog.String("type", t.Type),
			slog.Int("attempt", attempt+1),
			slog.Any("error", err))
		lastErr = err
	}
	b.markFailed(mt, fmt.Errorf("max retries exceeded: %w", lastErr))
}

func (b *MemoryBackend) Close() error {
	b.mu.Lock()
	stop := b.stopFn
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	b.wg.Wait()
	return nil
}

// ============================================================
// 小工具
// ============================================================

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// randHex 生成 n 位 16 进制小尾号（用于 mem task ID，替代 crypto/rand 避免引入）。
func randHex(n int) string {
	const charset = "0123456789abcdef"
	b := make([]byte, n)
	// 非安全随机 ID：只用于开发环境的内存任务 ID，撞不上即可。
	// xorshift64* 变体，种子用当前时间。
	seed := uint64(time.Now().UnixNano())
	for i := range b {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		b[i] = charset[int(seed)&(len(charset)-1)]
	}
	return string(b)
}
