# 续审报告：计费/支付/队列模块修复（2026-08-20）

> 审核日期：2026-08-20 ｜ 范围：8-19 新增模块 `internal/billing`、`internal/billing/payment`、`internal/queue`、`internal/adapter` 及 Webhook 入口
> 方法：读现有 review 文档（code-review-opensource-2026-08-18.md）+ Explore 子代理深度扫描 `internal/` + 关键 P1 人工读码验证
> 基线：8-18/8-19 的 P0/P1/P2 已基本落地；本轮聚焦**商业化新模块**的代码质量

---

## 一、发现与修复总览

| 编号 | 严重度 | 问题 | 位置 | 修复 |
|---|---|---|---|---|
| P1 | 🔴 | 支付宝 Webhook 必挂：handler 已 `io.ReadAll` 读空 `r.Body`，`VerifyWebhook` 又 `r.ParseForm()` 二次读已耗尽 body → `PostForm` 空 → 永远"缺少 sign" | `billing/handlers.go:287` → `payment/alipay.go:104` | 改用 `url.ParseQuery(string(body))`（微信/Stripe 已正确用 body 参数，仅支付宝踩坑） |
| P2-1 | 🟠 | （同 P1 根因）支付宝 body 二次读取 | 同上 | 同上 |
| P2-2 | 🟠 | Webhook 无幂等：支付宝/Stripe 重试投递时，`MarkOrderPaid` 的 `WHERE status IN('created','failed')` 二次命中返回错误 → handler 500 → 支付已成功却反复重试（重试风暴） | `billing/service.go:205` / `billing/store.go:264` | `store.MarkOrderPaid` 返回 `ErrOrderAlreadyPaid` 哨兵；`MarkOrderPaidAndActivate` 对"已 paid"订单幂等返回成功，仅确保套餐已激活 |
| P2-3 | 🟠 | 金额浮点格式化精度隐患 `fmt.Sprintf("%.2f", float64(cents)/100.0)` | `payment/alipay.go:66` | 整数运算 `fmt.Sprintf("%d.%02d", cents/100, cents%100)` |
| P2-4 | 🟠 | 微信/Stripe 用 `http.DefaultClient`（无超时），上游挂起永久占 goroutine | `payment/wechatpay.go:97`、`payment/stripe.go:90` | `payment` 包新增模块级 `paymentHTTPClient = &http.Client{Timeout: 30s}` 共用 |
| P2-5 | 🟠 | `io.ReadAll(resp.Body)` 错误被 `_` 吞掉 → 读失败时进 2xx 分支后 `json.Unmarshal(nil)` 报误导错 | `payment/wechatpay.go:102`、`payment/stripe.go:95` | 透出 `fmt.Errorf("...读取响应失败: %w", err)` |
| P2-6 | 🟠 | 微信平台证书验签为可选（`platCert==nil` 时跳过），未配证书无任何提示 | `payment/wechatpay.go:144` | 缺失/解析失败时在 `init()` 打印 `slog.Warn` 启动告警（满足微信官方安全要求的最小动作） |
| P2-7 | 🟠 | 队列序列化错误静默：`json.Marshal(report)` 失败写空结果；`payload` 损坏时 `_ = json.Unmarshal` 丢字段 | `internal/queue/queue.go:254`、`:155` | Marshal 失败 `return err`（致命）；Unmarshal 失败 `slog.Warn` |
| — | 🟡 | 验证级：支付宝 notify 网关要求响应体含字面量 `success` 才停止重试，原 handler 返回 JSON `{"ok":true}` → 不匹配会持续重试 | `billing/handlers.go:305` | 支付宝分支回 `text/plain; success`，其他渠道仍回 JSON |

> P2-7 原计划"显式 `asynq.Recover()`"，但 asynq v0.26 无此符号；asynq 默认已内置 panic 恢复（任务失败不拖垮 worker），故不额外加中间件。

---

## 二、已确认干净（未改动）

- `internal/billing/store.go`：全程 `ExecContext/QueryRowContext`，有 `defer rows.Close()`，无字符串拼接 SQL，无未关 rows
- `internal/adapter/*`：共享带池 `*http.Client` + 60s 超时，`urlRE` 包级编译，LRU 缓存用 `sync.RWMutex` + 上限 1000，无并发/泄漏
- `internal/queue/queue.go` 生命周期：`ShutdownTimeout`、context 取消、`sync.Once` Stop、30min 任务超时均正确，无 goroutine 泄漏
- `auth.BootstrapAdmin`：幂等 + 事务完整 + 密码强度强制

---

## 三、验证

- `go build ./...` ✅
- `go vet ./...` ✅（全仓无警告）
- `go test ./internal/billing/... ./internal/queue/...` ✅（payment + queue 单测通过）

---

## 四、待办（非本次范围，供后续）

- 真实商户号 + webhook 域名接入后，补一条端到端 Webhook 冒烟（微信/支付宝/Stripe 各一发，验证签名+幂等+`success` 响应）
- 发票直开、订阅续费/欠费降级、告警外推（钉钉/飞书）仍按 roadmap 排期
