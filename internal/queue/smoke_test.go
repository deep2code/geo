package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

// TestQueueRedisSmoke 验证 Redis/asynq 关键路径：入队 -> 派发 -> 结果回写 -> Inspector 读取。
// 需要可用 Redis；通过 GEO_SMOKE_REDIS 指定地址（默认 127.0.0.1:6390）。
// 无 Redis 时整体跳过，不污染日常 `go test`。
func TestQueueRedisSmoke(t *testing.T) {
	addr := os.Getenv("GEO_SMOKE_REDIS")
	if addr == "" {
		addr = "127.0.0.1:6390"
	}
	cli, err := NewClient(addr)
	if err != nil {
		t.Skipf("跳过：无法连接 Redis(%s): %v", addr, err)
	}
	defer cli.Close()

	// 起一个本地 asynq worker（stub handler），模拟 processAudit 的结果写回行为。
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{QueueAudit: 1},
	})
	mux := asynq.NewServeMux()
	mux.Handle(TaskAudit, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var p jobPayload
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			return err
		}
		result, _ := json.Marshal(map[string]string{"brand": p.BrandName, "ok": "1"})
		if _, werr := task.ResultWriter().Write(result); werr != nil {
			return werr
		}
		return nil
	}))
	go srv.Run(mux)
	defer srv.Shutdown()

	// 入队
	jobID, err := cli.Enqueue(context.Background(), &Job{
		WorkspaceID: "ws_smoke",
		BrandName:   "测试品牌",
		ProfileJSON: `{"name":"测试品牌","prompts":["x"]}`,
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	// 轮询直到成功（最多 10s）
	var job *Job
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err = cli.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("查询任务失败: %v", err)
		}
		if job.Status == StatusSucceeded || job.Status == StatusFailed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("任务未成功，状态=%s err=%s", job.Status, job.ErrorMsg)
	}
	if job.ResultJSON == "" {
		t.Fatalf("成功但无结果回写")
	}
	var r map[string]string
	if err := json.Unmarshal([]byte(job.ResultJSON), &r); err != nil || r["ok"] != "1" {
		t.Fatalf("结果内容异常: %s", job.ResultJSON)
	}
	t.Logf("冒烟通过: job=%s status=%s result=%s", job.ID, job.Status, job.ResultJSON)
}
