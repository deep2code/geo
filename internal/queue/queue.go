// Package queue 提供基于 Redis 的异步任务队列（asynq 背书）。
//
// 为什么用 Redis/asynq 而不是 MySQL：
// 用户明确选择 Redis 作为队列后端。asynq 在 Redis 之上提供了可靠的
// 任务分发、指数退避重试、死信（dead）、结果存储与状态巡检（Inspector），
// 远比手写「SELECT ... FOR UPDATE SKIP LOCKED」轮询更适合生产。
// 本项目此前把审计任务放在 MySQL（零额外依赖），现将队列独立到 Redis，
// 与计费库（MySQL）解耦——二者可各自独立启停。
//
// 连接通过环境变量注入：GEO_REDIS_ADDR（默认 127.0.0.1:6379）、
// GEO_REDIS_PASSWORD（可选，服务端开启 requirepass 时必填）。
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"my-geo/internal/brand"
)

const (
	// QueueAudit 审计任务队列名。
	QueueAudit = "audit"
	// TaskAudit 审计任务类型名。
	TaskAudit = "audit:brand"
)

// Status 任务状态（与前端轮询约定保持一致）。
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Job 异步审计任务（HTTP 层视图；底层由 asynq 任务承载）。
type Job struct {
	ID          string
	WorkspaceID string
	BrandName   string
	ProfileJSON string // brand.BrandProfile 的 JSON 快照（兼容旧调用方）
	Status      string
	ResultJSON  string
	ErrorMsg    string
	Attempts    int
	MaxAttempts int
	CreatedAt   int64
	FinishedAt  int64
}

// jobPayload 是写入 asynq 的任务负载，附带创建时间（TaskInfo 无 EnqueuedAt）。
type jobPayload struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	BrandName   string             `json:"brand_name"`
	CreatedAt   int64              `json:"created_at"`
	Profile     brand.BrandProfile `json:"profile"`
}

// Client 任务入队与状态查询（供 HTTP handler 调用）。
type Client struct {
	rdb  *asynq.Client
	insp *asynq.Inspector
	addr string
}

// NewClient 构建 Redis 背书的队列客户端，并做一次连通性校验。
// addr 为空时回退到 127.0.0.1:6379；password 可空。
func NewClient(addr, password string) (*Client, error) {
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	// 启动期连通性校验：连不上就直接禁用队列，避免运行期 503 抖动。
	rc := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := rc.Ping(pingCtx).Err()
	cancel()
	_ = rc.Close()
	if err != nil {
		return nil, fmt.Errorf("queue: 无法连接 Redis(%s): %w", addr, err)
	}
	opt := asynq.RedisClientOpt{Addr: addr, Password: password}
	return &Client{
		rdb:  asynq.NewClient(opt),
		insp: asynq.NewInspector(opt),
		addr: addr,
	}, nil
}

// Enqueue 入队一条审计任务，返回任务 ID。
func (c *Client) Enqueue(ctx context.Context, j *Job) (string, error) {
	if j.ID == "" {
		j.ID = newJobID()
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 3
	}
	payload := jobPayload{
		ID:          j.ID,
		WorkspaceID: j.WorkspaceID,
		BrandName:   j.BrandName,
		CreatedAt:   time.Now().Unix(),
	}
	if j.ProfileJSON != "" {
		var p brand.BrandProfile
		if err := json.Unmarshal([]byte(j.ProfileJSON), &p); err != nil {
			// 损坏的 ProfileJSON 不能静默入队：worker 会对零值 Profile
			// 跑完审计并写回"成功"结果，错误无人知晓
			return "", fmt.Errorf("queue: ProfileJSON 解析失败: %w", err)
		}
		payload.Profile = p
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("queue: 序列化任务失败: %w", err)
	}
	task := asynq.NewTask(TaskAudit, body,
		asynq.TaskID(j.ID),
		asynq.Queue(QueueAudit),
		asynq.MaxRetry(j.MaxAttempts),
		asynq.Timeout(30*time.Minute),
		asynq.Retention(24*time.Hour), // 完成后结果保留 24h，供轮询读取
	)
	info, err := c.rdb.EnqueueContext(ctx, task)
	if err != nil {
		return "", fmt.Errorf("queue: 入队失败: %w", err)
	}
	return info.ID, nil
}

// GetJob 查询任务状态与结果（经 asynq Inspector）。
func (c *Client) GetJob(ctx context.Context, id string) (*Job, error) {
	info, err := c.insp.GetTaskInfo(QueueAudit, id)
	if err != nil {
		return nil, err
	}
	job := &Job{ID: id}
	switch info.State.String() {
	case "active", "retry", "aggregating":
		job.Status = StatusRunning
	case "pending", "scheduled":
		job.Status = StatusPending
	case "completed":
		job.Status = StatusSucceeded
	case "archived", "dead":
		// archived 是 asynq 重试耗尽/失败后的归档态，与 dead 同属失败。
		job.Status = StatusFailed
	default:
		job.Status = StatusPending
	}
	var p jobPayload
	if err := json.Unmarshal(info.Payload, &p); err != nil {
		slog.Warn("queue: 解析任务负载失败（数据可能损坏）",
			slog.String("job", id), slog.Any("error", err))
	}
	job.WorkspaceID = p.WorkspaceID
	job.BrandName = p.BrandName
	job.Attempts = info.Retried
	job.MaxAttempts = info.MaxRetry
	job.CreatedAt = p.CreatedAt
	if job.Status == StatusSucceeded {
		if info.Result != nil {
			job.ResultJSON = string(info.Result)
		}
		if !info.CompletedAt.IsZero() {
			job.FinishedAt = info.CompletedAt.Unix()
		}
	}
	if job.Status == StatusFailed {
		job.ErrorMsg = info.LastErr
		if !info.LastFailedAt.IsZero() {
			job.FinishedAt = info.LastFailedAt.Unix()
		}
	}
	return job, nil
}

// Close 释放底层连接。
func (c *Client) Close() error {
	if c.insp != nil {
		_ = c.insp.Close()
	}
	if c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}

// QueueStats 队列统计信息。
type QueueStats struct {
	Pending   int            `json:"pending"`
	Active    int            `json:"active"`
	Scheduled int            `json:"scheduled"`
	Completed int            `json:"completed"`
	Failed    int            `json:"failed"`
	Retry     int            `json:"retry"`
	Total     int            `json:"total"`
	Jobs      []QueueJobInfo `json:"jobs,omitempty"`
}

// QueueJobInfo 队列任务简要信息。
type QueueJobInfo struct {
	ID        string `json:"id"`
	BrandName string `json:"brand_name"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

// GetStats 获取队列各状态的任务数量。
func (c *Client) GetStats() (*QueueStats, error) {
	if c.insp == nil {
		return &QueueStats{}, nil
	}
	qi, err := c.insp.GetQueueInfo(QueueAudit)
	if err != nil {
		return nil, err
	}
	stats := &QueueStats{
		Pending:   qi.Pending,
		Active:    qi.Active,
		Scheduled: qi.Scheduled,
		Completed: qi.Completed,
		Failed:    qi.Archived,
		Retry:     qi.Retry,
	}
	stats.Total = stats.Pending + stats.Active + stats.Scheduled + stats.Completed + stats.Failed + stats.Retry
	return stats, nil
}

// Server 后台 worker（asynq Server），处理 audit 队列中的任务。
type Server struct {
	srv    *asynq.Server
	engine *brand.Engine
	stop   sync.Once
}

// NewServer 构建 worker。concurrency 为并发处理数（≤0 时默认 2）；password 可空。
func NewServer(addr, password string, engine *brand.Engine, concurrency int) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	if concurrency <= 0 {
		concurrency = 2
	}
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: addr, Password: password},
		asynq.Config{
			Concurrency:     concurrency,
			Queues:          map[string]int{QueueAudit: 1},
			StrictPriority:  false,
			ShutdownTimeout: 30 * time.Second,
			RetryDelayFunc:  asynq.DefaultRetryDelayFunc,
		},
	)
	return &Server{srv: srv, engine: engine}, nil
}

// Start 启动 worker（阻塞直到 ctx 取消或 Run 异常退出）。通常在独立 goroutine 中调用。
func (s *Server) Start(ctx context.Context) {
	mux := asynq.NewServeMux()
	mux.Handle(TaskAudit, asynq.HandlerFunc(s.processAudit))
	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Run(mux) }()
	select {
	case <-ctx.Done():
		s.Stop()
	case err := <-errCh:
		if err != nil {
			slog.Error("queue: worker 异常退出", slog.Any("error", err))
		}
	}
}

// Stop 优雅关闭 worker（幂等）。
func (s *Server) Stop() {
	s.stop.Do(func() {
		s.srv.Shutdown()
	})
}

// processAudit 执行审计：跑 brand.Engine.Audit，成功写回结果。
// 返回错误时 asynq 按 MaxRetry 指数退避重试，超限进入 dead 状态。
func (s *Server) processAudit(ctx context.Context, t *asynq.Task) error {
	var p jobPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("queue: 解析任务负载失败: %w", err) // 致命，不再重试
	}
	if s.engine == nil {
		return fmt.Errorf("queue: 品牌审计引擎未初始化")
	}
	report, err := s.engine.Audit(ctx, p.Profile)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("queue: 序列化审计结果失败: %w", err)
	}
	if rw := t.ResultWriter(); rw != nil {
		if _, werr := rw.Write(resultJSON); werr != nil {
			// 写回失败必须报错重试：否则任务被判 succeeded 但结果永久丢失，
			// 前端轮询到"成功"却拿不到报告（审计成本已真实消耗）
			return fmt.Errorf("queue: 写回结果失败（任务将重试）: %w", werr)
		}
	}
	slog.Info("queue: 异步审计完成", slog.String("job", p.ID), slog.String("brand", p.BrandName))
	return nil
}

// newJobID 生成任务 ID（带 job_ 前缀，16 字节熵）。
func newJobID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "job_" + time.Now().Format("20060102150405")
	}
	return "job_" + hex.EncodeToString(b)
}
