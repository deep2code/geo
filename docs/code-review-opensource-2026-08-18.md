# 二次审核报告：对照开源生态的改进机会

> 审核日期：2026-08-18 ｜ 范围：全仓库（Go 后端 113 文件 / 38,110 行 + React 前端 + Docker/CI + 文档）
> 方法：代码扫描（3 路并行 agent）+ 关键发现人工验证 + 对标 Princeton GEO (KDD'24)、AutoGEO (ICLR'26)、主流 Go Web 工程实践
> 前提：8 月 18 日第一轮 32 个优化点已全部修复、Go 1.26 迁移已完成，本报告聚焦**新增/遗留**问题，不重复已修复项。

---

## 一、与开源项目的对比结论

| 维度 | Princeton GEO / AutoGEO | 本项目现状 | 差距 |
|---|---|---|---|
| GEO 策略覆盖 | 论文 9 法（实证） | **13 个策略文件全覆盖**（quotation/statistics/cite_sources/fluency/authoritative/technical_terms/easy_understand/unique_words/keyword + structure/faq/answer_first/schema） | ✅ 超配，无需补策略 |
| 双评分体系 | GEO 可见性 + GEU 效用 | GEO 6 维评分 ✅；GEU 校验仅存在于 `autorewriter` 改写路径（`autorewriter.go:72-85`） | ⚠️ 评分主链路未含效用维度，GEU 覆盖面不足 |
| 规则引擎 | AutoGEO 自动提取引擎偏好规则（rule extraction） | 全部专家规则**硬编码常量**（`scorer.go:23-39`），不可配置、不可版本化 | 🔴 战略级差距：无法随引擎偏好演化 |
| 评测能力 | AutoGEO 提供 `evaluate` 框架 + 3 数据集 + 可复现基准 | **无评测集、无回归基线**；113 Go 文件仅 6 个测试文件，前端 0 测试 | 🔴 战略级差距：无法证明"优化有效" |
| LLM 成本控制 | 强调 cost-efficient 模型 | 无重试/退避/并发上限/月度预算，熔断参数硬编码（`llm.go:26-31`） | 🔴 生产级差距 |
| 可观测性 | SaaS 标配 metrics/trace | 无 pprof、无 `/metrics`、无 Prometheus（grep 0 命中） | 🔴 生产级差距 |
| 部署 | — | Dockerfile 多阶段/非 root ✅；但 compose 库初始化断裂（见 P0-1） | ⚠️ 一键部署不可用 |
| 文档 | — | README 19K + getting-started + architecture（27+ Mermaid 图） | ✅ 明显优于同类开源项目 |

**战略结论**：本项目在「策略覆盖」「文档」「安全基线（第一轮修复后）」已属上游水平；真正的差距集中在四件事——**可观测性、评测体系、LLM 工程化、部署可用性**。以下按 P0/P1/P2 列出全部新发现。

---

## 〇、修复状态跟踪（2026-08-18 执行中）

| 编号 | 状态 | 修复内容 | 关键文件 |
|---|---|---|---|
| P0-1 | ✅ 已修复 | compose 启用 initdb 建 4 库并授权 | `docker-compose.yml`、`deploy/initdb/` |
| P0-2 | ✅ 已修复 | MCP Server 优雅关闭/超时/recovery/鉴权限流 | `internal/brand/mcpserver/mcpserver.go`、`cmd/geo/mcp_server.go` |
| P1-1 | ✅ 已修复 | `/metrics`（手写 Prometheus 文本）+ `/debug/pprof/` + LLM/HTTP 计数 | `internal/server/metrics.go`、`internal/llm/llm.go` |
| P1-2 | ✅ 已修复 | 删除零引用死代码 `internal/taskqueue/`（452 行） | `git rm internal/taskqueue/` |
| P1-3 | ✅ 已修复 | main 声明 version/commit/buildAt + `--version` + CI ldflags | `cmd/geo/main.go`、`.github/workflows/release.yml` |
| P1-4 | ✅ 已修复 | LLM 指数退避重试/并发信号量/可配置熔断 | `internal/llm/llm.go` |
| P1-5 | ✅ 已修复 | Prompt 注入防护（定界符+数据声明+截断+清洗） | `internal/brand/brand.go`、`internal/optimizer/autorewriter/` |
| P1-6 | ✅ 已修复（2026-08-19 改为移除） | 曾实现零依赖 SQL migrations（embed + 版本表 + checksum）；现按新架构**整体移除** `internal/migrate/`，DDL 全部沉淀 `deploy/initdb/02-schema.sql`，应用内不再内嵌建表 | `deploy/initdb/` |
| P1-7 | ✅ 已修复 | `.env` 加载 + 启动 fail-fast 校验 | `internal/config/env.go`、`cmd/geo/main.go` |
| P1-8 | ✅ 已修复 | 高价值单测（safeURL 协议白名单 / writeInternalError 脱敏防泄露 / requireDataAdmin legacy 放行 / util.SplitSentences·CountSentences / scorer OverrideWeights 配置化生效+鲁棒性）+ CI 增 quality-gates 作业（golangci-lint + govulncheck） | `internal/server/security_test.go`、`internal/util/util_test.go`、`internal/scorer/scorer_test.go`、`.github/workflows/ci.yml` |
| P1-9 | ✅ 已修复 | 弱密钥 fail-fast、API Key 恒时比较、JWT token_version 手动吊销、密码策略（8+ 字母数字） | `internal/config/env.go`、`internal/auth/auth.go`（token_version 列由 `deploy/initdb/02-schema.sql` auth 区块创建） |
| P1-10 | ✅ 已修复 | 抽 `internal/httputil`（JSON 读写/IP/分页统一，消除 10MB vs 1MB 漂移） | `internal/httputil/`、`internal/server/`、`internal/auth/handlers.go` |
| P1-11 | ✅ 已修复 | ticket 冒泡排序改 `slices.SortFunc` | `internal/server/ticket.go` |
| P2-3 | ✅ 已修复 | `globalKB` 无锁单例改 `atomic.Pointer`（失败可重试） | `internal/brand/knowledge/knowledge.go` |
| P2-4 | ✅ 已修复 | 常青度正则提升为包级变量（补全 212/216 两处内联） | `internal/analyzer/analyzer.go` |
| P2-5 | ✅ 已修复 | scorer 权重 const→var + `OverrideWeights` 配置化入口 | `internal/scorer/scorer.go` |
| P2-6 | ✅ 已修复 | jsonl_cache 降频 fsync（3 秒间隔，崩溃窗口可接受） | `internal/brand/chinacheck/jsonl_cache.go` |
| P2-7 | ✅ 已修复 | externalsignals cost 字段加 `sync.Mutex` 并发保护 | `internal/brand/externalsignals/externalsignals.go` |
| P2-8 | ✅ 已修复 | knowledge 精确全名匹配 O(1)（`nameExact` map）+ finalize 已有增量机制 | `internal/brand/knowledge/knowledge.go` |
| P2-10 | ✅ 已修复 | mail 模板解析缓存（`sync.Map`）+ 注释修正（SMTP 无连接池语义） | `internal/mail/mail.go` |
| P2-11 | ✅ 已修复 | MCP session 落内存 map 校验 + 统一 slog | `internal/brand/mcpserver/mcpserver.go` |
| P2-12 | ✅ 已修复 | ticket/admin/help 内存态数据标注"重启丢失/多副本需迁 MySQL" | `internal/server/ticket.go`、`admin.go`、`help.go` |
| P2-13 | ✅ 已修复 | compose 加 nginx 反代（profile proxy 可选）+ redis 改可选依赖 | `docker-compose.yml`、`deploy/nginx/nginx.conf` |
| P2-14 | ✅ 已修复 | 默认 DSN 移除 `multiStatements=true` | `internal/brand/offlinedb/offlinedb.go` |
| P2-15 | ✅ 已修复 | deploy.sh 弱口令检测生产模式 fail-fast | `scripts/deploy.sh` |
| P2-16 | ✅ 已修复 | legal.go 错误响应统一 `ErrorResponse` | `internal/server/legal.go` |
| P2-17 | ✅ 已修复 | 分页解析统一（随 P1-10 收敛） | `internal/httputil/httputil.go` |
| P2-1 | ❌ 不采用 | ServeMux 方法路由改造 —— **已实测验证不可行/会劣化**：`server.go` 末尾有 SPA 通配 `HandleFunc("/")`，Go 1.22 ServeMux 方法路由下，错误方法的请求会先匹配该通配而回落到 SPA（返回 404/HTML），而非干净的 405；现有每 handler 的 `if r.Method !=` 检查反而提供更正确的 API 语义，且无风险。详见文末"决策记录 P2-1"。 | `internal/server/server.go` |
| P2-2 | ✅ 已修复 | server.go 4056 行拆分（纯搬移，零逻辑变更，零行为变化）：核心 `Server`/构造/`registerRoutes`/`requireDataAdmin`/`newAutoRewriter` 留在 `server.go`（1483 行），handler 按域迁至 `web_handlers.go`/`pipeline_handlers.go`/`cms_handlers.go`/`brand_handlers.go`/`leaderboard_handlers.go`；按文件级作用域裁剪 import。build/vet/`go test ./...` 全绿。 | `internal/server/server.go` 及 5 个新文件 |
| P2-9 | ✅ 已修复（原） | `brand-audit/cache/db` 原归组到 `geo brand <sub>`（audit/cache/db）；本轮已按用户要求**彻底移除 CLI**，品牌相关能力迁移为「品牌审计」等前端页 + `GET /api/v1/brand/audit`、`POST /api/v1/brand/offlinedb/import*` 等端点；旧 `brand_*.go` 文件已删除 | `internal/server/brand_handlers.go`、`web-app/src/pages/*`、`README.md`、`docs/getting-started.md`、`docs/architecture.md` |

---

## 二、P0（严重，先修）

### P0-1 🔴 docker compose 一键启动必挂：数据库无法就绪
- `docker-compose.yml:14` 仅 `MYSQL_DATABASE: geo`，但 `:69-72` 四条 DSN 指向 `geo_offline` / `geo_history` / `geo_auth` / `geo_chinacheck` 四个库——**mysql 镜像不会创建这 4 个库**；`:21` 的 initdb 挂载还被注释掉了。
- 生产代码无 `CREATE DATABASE`（仅测试里有）。
- **建议**：启用 initdb（新增 `deploy/initdb/01-databases.sql` 建 4 库并授权），或在 app 启动时自动建库（`CREATE DATABASE IF NOT EXISTS` + `GRANT`）。修好后 `docker compose up` 应能做到开箱即用。
- **✅ 2026-08-19 落地**：启用 initdb（01 建库授权 + 02 建表）；且已从 5 库合并为单库 `geo`，DSN 全部指向 `/geo`。

### P0-2 🔴 MCP Server 裸跑：无优雅关闭、无超时、无 recovery、无鉴权限流
- `internal/brand/mcpserver/mcpserver.go:69`：`return http.ListenAndServe(s.addr, mux)`——无 signal 处理、无 ReadTimeout/WriteTimeout、无 panic recovery；对比 REST Server（`server.go:372-408`）已有完整的 `signal.NotifyContext` + `Shutdown(30s)`。
- 后果：任何 handler panic 直接拖垮整个进程；SIGTERM 时在途请求被硬断；**无 API Key 校验，任何能访问端口的人可调用 `geo_brand_audit` 触发昂贵 LLM 调用**。
- **建议**：抽取与 REST Server 相同的 `ListenAndServe` + 优雅关闭 + recovery 中间件；MCP 端点加 Key 校验或复用现有鉴权。

---

## 三、P1（重要，近期做）

### P1-1 🔴 可观测性空白：无 metrics / pprof / tracing
全仓库 `pprof`、`/metrics`、`prometheus` 均 0 命中。`slog` 已有且带 request_id（`middleware.go:126-136`），但：
- 无请求延迟/状态码直方图、无 LLM 调用量/成本/失败率指标（GEO 系统最该量化的就是"每次优化花了多少钱、成功率多高"）；
- `GEO_LOG_LEVEL` 解析失败被静默吞掉（`main.go:45`）。
- **建议**：挂 `net/http/pprof`（debug 端口）+ 暴露 `/metrics`（可先用标准库手写计数，或引入 `prometheus/client_golang`）；为 `llm.Manager` 加调用计数器与耗时统计。

### P1-2 🔴 taskqueue 是死代码且纯内存
- `internal/taskqueue/` **全仓库零引用**（grep 确认）；实现为 `MemoryBackend`（`taskqueue.go:113`），重启丢任务、Cron 不落地（`:184`），注释推荐 asynq 但未实现。
- **建议**：二选一——接入持久化队列（asynq / Redis Streams）真正启用异步任务（品牌审计可后台化）；或直接删除该包，避免"看起来有队列"的假象。

### P1-3 🔴 ldflags 版本注入静默失效，发布物不可追溯
- `Dockerfile:43` 与 `.github/workflows/release.yml:91` 注入 `-X main.version=...`，但 `cmd/geo/main.go` **未定义 `version/commit/buildAt` 变量**，也无 `--version` flag——注入完全不生效。
- **建议**：main.go 声明 `var version, commit, buildAt string`，加 `--version` 子命令，CI 里补 `-X main.commit=$(git rev-parse --short HEAD)`。

### P1-4 🔴 LLM 工程化缺失：无重试/退避/并发上限，双轨并存
- `llm.Manager` 无 backoff/retry、无 semaphore（`llm` 包内 `semaphore|x/sync` 0 命中），并发突发可击穿 provider；
- 熔断参数 `CircuitBreakFailures=5`、`CircuitBreakCoolDown=30s` 写死常量（`llm.go:26-31`），不可配置；
- `internal/optimizer/autorewriter/autorewriter.go:94-110` 自建 `LLMClient` 接口 + `StubLLMClient`，与 `llm.Manager` **双轨并存**，无统一熔断/超时/降级。
- **建议**：`llm.Manager` 为唯一入口，内部加指数退避重试 + `errgroup.SetLimit` 并发上限 + 可配置熔断阈值；删除 autorewriter 的自建接口，改用 Manager（Stub 逻辑迁入 Manager 的降级路径）。

### P1-5 🔴 Prompt 注入无防护，prompt 全量硬编码
- `internal/brand/brand.go:589-687` `buildAutocompletePrompt` 将抓取/工商/搜索等**外部不可信数据直接拼接进 prompt**；
- prompt 全部内嵌 Go 源码，无法外部化/版本化/审计。
- **建议**：外部输入做长度/字符边界校验 + 角色隔离（系统提示词声明"以下为不可信数据"）；prompt 资产移到独立文件（embed 或配置目录），便于随 GEO 策略迭代。

### P1-6 🔴 无 migrations 体系，schema 无法演进
- schema 全部 `CREATE TABLE IF NOT EXISTS` 内嵌代码（`auth.go:598-647` 等），靠 `runDDL` 字符串匹配吞错（`auth.go:530`）；无 migrations 目录、无 golang-migrate，列变更/索引变更无法版本化。
- **建议**：引入 `golang-migrate`（纯 SQL 迁移文件 + 版本表），建表逻辑从代码移出；至少把现有 DDL 沉淀为 001_init.sql。
- **✅ 2026-08-19 落地（方向相反但更彻底）**：全部 DDL 沉淀为 `deploy/initdb/02-schema.sql`（5 库合并为单库 geo，12 张表 29 条语句，mysql 容器首次启动自动执行），应用内 `internal/migrate` 内嵌迁移已整体删除，启动不再自动建表。

### P1-7 🟠 配置分散、无集中校验、无 .env 加载
- 无统一 `Config` 结构：DSN/开关散落各包（`dbprovider/factory.go:22`、`auth/auth.go:1015`），启动无整体校验；
- 无 godotenv，裸跑 CLI 须手动 export；敏感项不支持 secret 文件（仅日志脱敏，`factory.go:127`）；
- `cmd/geo/mcp_server.go:42` 端口 fallback 硬编码，未走配置。
- **建议**：收敛为 `config.Config` 集中定义 + 启动时 Validate（必填项缺失即 fail-fast）；支持 `--env-file` 或自动加载 `.env`。

### P1-8 🟠 测试覆盖严重不足 + CI 无质量关卡
- 113 Go 文件仅 **6 个**测试文件（auth/roi/chinacheck×2/offlinedb/uniqueness），核心的 server、scorer、optimizer、llm、analyzer **零测试**；前端 0 测试（`@playwright/test` 已装但无一个 spec）；
- CI（`ci.yml`）只有 build+test，**无 golangci-lint、无 govulncheck、无 Docker 镜像构建冒烟**；`release.yml:88` 的 `BUILD_AT: $(date ...)` 在 env 块中不会展开（字面量）。
- **建议**：先补高价值单测（`writeInternalError`/`safeURL`/`requireDataAdmin`/`SplitSentences`/scorer 权重打分），再上 golangci-lint + govulncheck 关卡；前端补 3-5 条 Playwright 冒烟（登录→优化→审计主链路）。

### P1-9 🟠 鉴权细节留尾巴
- JWT 弱密钥仅告警不阻断（`auth.go:222`）；legacy API Key 用 `==` 比较非恒时（`auth.go:1299`）；
- 登录限流是单实例内存 map（`auth.go:999`），多副本部署即失效；密码策略仅 `len>=8`（`auth.go:684`）；
- JWT 内嵌 role，改角色后最长 2h 才生效（`auth.go:1146`）——建议支持手动吊销（token 版本号）。
- **建议**：弱密钥 fail-fast；API Key 比较改 `subtle.ConstantTimeCompare`；限流迁到 MySQL/Redis 共享存储；密码策略提到 10 位 + 复杂度。

### P1-10 🟠 请求样板重复：校验/工具函数跨包复制
- 40+ handler 手写"方法检查 + 解码 + 逐字段 if 校验"三段式（如 `server.go:770-773`），无 `validator` 结构体 tag 校验；
- `writeJSON/readJSON`（`server.go:1045-1077` vs `auth/handlers.go:69-87`，body 上限还不一致 10MB vs 1MB）、`clientIP/isTrustedProxy`（`middleware.go:809-841` vs `auth/handlers.go:95-179`）重复定义；`auth.go` 手写 `atoi`。
- **建议**：抽公共 `internal/httputil`（Bind/Validate/Respond/ClientIP/Paginate），handler 收敛为 `Respond(w, r, handlerFunc)` 辅助层；分页解析统一。

### P1-11 🟠 ticket.go 冒泡排序 O(n²)
- `internal/server/ticket.go:181-187` 双层循环冒泡排序，工单量大时退化；项目其他地方已用 `slices.SortFunc`。
- **建议**：改 `slices.SortFunc`（一行）。

---

## 四、P2（建议，排期做）

| # | 发现 | 位置 | 建议 |
|---|---|---|---|
| P2-1 | 路由仍为 Go 1.22 前风格：60+ 条扁平 `HandleFunc` + 每 handler 手写 `if r.Method !=`，路径参数靠 `strings.TrimPrefix`+`SplitN` 手动解析（`ticket.go:214`、`admin.go:205`） | `server.go:429-556` | 升级到 `http.ServeMux` 方法路由（`"POST /api/v1/analyze"`）或引入 chi；第一轮已权衡跳过，此轮仍建议**先只做方法路由**（低风险机械替换），手动路径解析逐步替换 |
| P2-2 | `server.go` 4056 行巨型文件（已修复） | `internal/server/server.go` | 已按 handler 域拆分：核心留在 `server.go`（1483 行），handler 迁至 `web_handlers.go`/`pipeline_handlers.go`/`cms_handlers.go`/`brand_handlers.go`/`leaderboard_handlers.go`；纯搬移零逻辑变更 |
| P2-3 | `globalKB` 无锁单例初始化竞态（先判空后赋值非原子） | `knowledge.go:95-159` | `sync.Once` 或 `atomic.Pointer[Knowledge]` |
| P2-4 | `countWords` 每次调用 `regexp.MustCompile` | `analyzer.go:247` | 提升为包级 `var reHan`（同文件 30-34 行已是此模式） |
| P2-5 | scorer 权重全为包级常量（`weightQuotation=8.0` 等），不可配置 | `scorer.go:23-39` | 提升为 `var` + 接入 config，支持按行业/引擎覆盖权重（对标 AutoGEO 规则集） |
| P2-6 | chinacheck JSONL 缓存每次 `set` 都 Open+Write+`f.Sync()` | `jsonl_cache.go:237` | 批量累积 + 降频 fsync（或直接换 MySQL/Redis 缓存） |
| P2-7 | `externalsignals` 的 `cost` 字段注释声明"顺序聚合"，并发审计即数据竞争 | `externalsignals.go:88` | `atomic` 或加锁 |
| P2-8 | knowledge 向量检索 O(N) 双重循环全量匹配 + TF-IDF 每次 `Add` 全量 `finalize()` 重建 | `knowledge.go:342-352`、`vector.go` | 倒排索引 + 增量更新（当前 383 家规模无碍，品牌数上去后再做） |
| P2-9 | CLI 20 个子命令平铺，brand 系未归组，无 `--version` | `main.go:76-95` | cobra 分组 `geo brand <sub>` + `--version`（与 P1-3 联动） |
| P2-10 | mail 包声称"复用 SMTP 连接"实际每次新建连接；模板每次 `template.Parse` | `mail/mail.go:191,595` | 注释修正或连接池化 + 启动时解析模板缓存 |
| P2-11 | MCP Server 用 `fmt.Printf` 而非 slog；sessionID 生成后不存储不校验（形同虚设） | `cmd/geo/mcp_server.go:58-64`、`mcpserver.go:122` | 统一 slog；session 落内存 map 并校验 `Mcp-Session-Id` |
| P2-12 | 工单/公告/新手引导状态存进程内存，重启丢失 | `ticket.go:38`、`admin.go:49`、`help.go:36` | 标注为易失态或迁移 MySQL |
| P2-13 | compose 无 nginx/TLS 终止层，app 直接暴露 8080；`geo` 服务强制 `depends_on` 可选的 redis | `docker-compose.yml:82` | 加 caddy/nginx 反代 + TLS；redis 改为可选或明确必需 |
| P2-14 | 默认 DSN 仍含 `multiStatements=true` + `tls=false` | `offlinedb.go:29` | 默认值收敛为最小权限；multiStatements 仅建表/导入路径启用 |
| P2-15 | deploy.sh 检测到弱口令 `geoPass` 仅告警继续部署 | `scripts/deploy.sh:66` | 生产模式 fail-fast |
| P2-16 | 错误响应仍有零星不一致：`legal.go:197` 用 `map[string]string`；`server.go:1567` 返回 200 但 body 含 `"error"` | — | 统一 `ErrorResponse`，业务错误语义移到 4xx 状态码 |
| P2-17 | 内存态分页/状态码无统一封装（三处手写 page/limit 解析） | `admin.go:149`、`ticket.go:156`、`auth/handlers.go:627` | 随 P1-10 的 httputil 一并收敛 |

---

## 五、战略级改进方向（对标 AutoGEO 的差异化竞争点）

1. **评测体系（最值得投入）**：AutoGEO 有 GEO-Bench 基准 + `evaluate` 命令。本项目已建一个**中文 GEO 评测集**（10-20 个代表性 query × 待优化页面 × 主流中文引擎），通过 Web「评测」页批量跑改前/改后引用率对比，产出可复现报告。这是证明产品价值的核心资产。
2. **规则集外部化**：把 scorer 权重 + 13 个策略的触发条件从常量改为**可配置规则集**（JSON/YAML + 版本号），支持按行业/引擎偏好组合——这正是 AutoGEO"rule extraction"想解决的，而本项目可以先用配置化实现 80% 价值。
3. **LLM 成本仪表盘**：在 metrics 基础上按品牌/任务/引擎聚合 token 消耗与美元成本，设置月度预算熔断（对齐用户已有的 DeepSeek 涨价对冲思路）。
4. **CI 质量门禁**：golangci-lint + govulncheck + `go test -race` + 前端 typecheck 全进 CI，release 加版本注入（P1-3）。
5. **部署开箱即用**：修 P0-1 后补一份 `docker compose up` 冒烟脚本进 CI，确保每次提交都能一键起。

### 战略项落地状态（2026-08-18，本会话已实施 #1/#2/#3）

| # | 方向 | 状态 | 交付物 |
|---|------|------|--------|
| 1 | 评测体系 | ✅ 已实施 | `internal/eval` 包 + Web「评测」页（`POST /api/v1/evaluate`）；`config/benchmarks/zh-geo-sample.json` 12 个中文跨领域用例；离线代理指标（有界引用率 `1−e^(−相对可见度得分)`）避免提升% 失真 |
| 2 | 规则集外部化 | ✅ 已实施 | `internal/config/ruleset.go` + Web「规则集」页（`GET/POST /api/v1/rules*`）；`config/rules/*.json` 示例；`scorer.ApplyRuleSet` 注入权重与策略系数；优化/评分/分析均支持规则集参数 |
| 3 | LLM 成本仪表盘 | ✅ 已实施 | `internal/llm` 按模型聚合 token/USD（`modelPricing` 表 + 月度预算熔断 `ErrBudgetExceeded`）；`/api/v1/admin/cost` 端点 + Web 成本仪表盘页；Prometheus `geo_llm_cost_*` 指标 |
| 4 | CI 质量门禁 | ✅ 前轮已完成（P1-8） | golangci-lint + govulncheck + `go test -race` |
| 5 | 部署开箱即用 | ✅ 已实施 | compose 对 `.env` 设 `required:false`（缺失不报错，改用以 `environment:` 默认值一键启动）+ `GEO_ADMIN_KEY` 透传；`scripts/smoke-compose.sh` + CI `deploy-smoke` 作业（构建→探活→`/metrics`→成本端点鉴权 403/200） |

> 说明：#1 评测集的相对引用得分为**离线代理指标**（无需联网/API Key，可复现）；在 Web「评测」页开启 live 模式并填入 LLM Key（`sk-xxx`，OpenAI 兼容 Chat Completions）接入真实引擎实测引用，覆盖 Actual 指标并报告 `live_cited`。指标修正点：原始 `RelativeCitationScore` 为加性指标（可 >1），直接算提升% 会得出 +886% 这类失真值；改为先经 `1−e^(−rel)` 映射到 0–1 再算提升%，结果有界、可解释（实测平均预期提升约 +408%）。

### 补充：系统诊断三模块（2026-08-18 新增，非原战略项）

为可运维性补充三类诊断能力（包 `internal/diagnostics`）：

1. **关键业务健康检查**（`BusinessHealth`）：评分 / 分析 / 优化管线端到端探针；LLM 改写业务在已配置 Provider 时做真实端到端调用验证；三个 MySQL 模块（离线工商库 / 审计历史 / China-Check 缓存）TCP 探活。
2. **属性/参数/配置校验**（`ConfigCheck`）：日志级别与格式、服务端口、LLM 预算、鉴权与弱密钥（`config.Validate` 复用）、管理员密钥、LLM/引擎 Key、各 DSN 格式、白标主题色、定时审计配置、外部规则集合法性。
3. **系统自检**（`SelfCheck`）：运行时快照（Go 版本 / OS / CPU / goroutine / 内存）+ 上述两类聚合 + 整体健康等级，渲染为 JSON（供前端消费）。

入口（**前端为主，新手友好**）：Web UI 左侧导航「🩺 系统自检」一键运行，按健康/隐患/问题分组展示并给出修复建议。管理后台 `GET /api/v1/admin/selfcheck` 在**服务端未配置 `GEO_ADMIN_KEY` 时开放**（开箱即用），一旦配置则要求 `X-Admin-Key`（防配置/密钥存在性泄露）。`pkg/geo.Engine` 新增 `LLMAvailable()` / `LLMStatus()` 供业务探针使用。

**CLI 已全部移除**：用户要求所有独立命令（`geo optimize/score/analyze/serve/brand*/mcp-server/readiness/discover/drift/rules/evaluate/cost` 等）一律改为前端界面操作。现 `cmd/geo/main.go` 仅启动 Web 服务（serve 成为默认行为），并随同进程启动 MCP Server（`:9090` `/mcp`）；原命令文件 `brand_*.go`、`cost.go`、`rules.go`、`evaluate.go`、`discover.go`、`mcp_server.go` 已删除，其能力迁移为 `GET /api/v1/rules*`、`POST /api/v1/evaluate`、`POST /api/v1/brand/offlinedb/import*` 等端点 + 对应前端页（规则集 / 评测 / 工商库导入 / 集成）。

---

## 六、建议落地顺序

| 阶段 | 内容 | 预估改动面 |
|---|---|---|
| 第一波（1-2 天） | P0-1 建库、P0-2 MCP 加固、P1-11 冒泡排序、P1-3 版本注入、P2-4 正则提升 | 小 |
| 第二波（2-3 天） | P1-2 taskqueue 取舍、P1-4 LLM 重试/并发/单入口、P1-5 prompt 防护、P1-6 migrations、P2-5 权重配置化 | 中 |
| 第三波（3-5 天） | P1-1 可观测性、P1-8 测试补课 + CI 关卡、P1-10 httputil 收敛、P2-1 方法路由、P2-2 server.go 拆分 | 中-大 |
| 战略项（按需） | 评测集 + 评测页、规则集版本化、成本仪表盘 | 新模块 |

> 备注：第一轮有意跳过的 P2-1（方法路由，本轮实测验证不可行/会劣化，见第七节决策记录）、P2-2（拆分 server.go，本轮已执行：拆为 6 个文件、零逻辑变更、build/vet/test 全绿）。

---

## 七、决策记录 P2-1（ServeMux 方法路由改造）—— 经实测验证，不采用

**背景**：原审查建议把 `server.go` 中 60+ 条扁平 `HandleFunc` + 每 handler 内 `if r.Method !=` 的检查，改为 Go 1.22+ `http.ServeMux` 方法路由（`"POST /api/v1/analyze"`），让 mux 直接返回 405。

**实测结论**：**会导致行为劣化，故不采用。**

根因——`server.go` 路由表末尾有 SPA 通配 `s.mux.HandleFunc("/", s.handleWebSPA)`，必须放在最后兜底非 API 路径。Go 1.22 ServeMux 的方法路由语义是：**模式匹配优先于方法匹配**。一旦为某路由声明了方法（如 `"POST /api/v1/brand/audit"`），用错误方法（GET）请求时，mux 不会返回 405，而是继续向下匹配到通配 `"/"`，最终落到 SPA handler 返回 404/HTML。

- 改造前：handler 内 `if r.Method != http.MethodPost` 返回干净的 `405 Method Not Allowed`——**API 语义更正确**。
- 改造后：错误方法请求回落到 SPA，返回 404/HTML——**劣化**。

**代价/收益**：收益仅为"少写几行方法判断 + 由框架强制 405"；代价是几乎所有 API 在错误方法下语义变差，且需逐一核对每条路由的真实方法（含 GET+POST 双方法、免方法约束的路由），改动面大、回归风险高。

**决定**：保持现状（每 handler 内方法检查），不引入方法路由。若未来要彻底解决，正确做法是把 SPA 兜底从方法路由 mux 中剥离（如用独立 `http.ServeMux` 或在 handler 内显式 405 后再 fallback），而非简单改路由表。

**附带产出**：为此写了 `TestMethodRouting` 等用例验证上述行为，确认劣化后已删除该测试并完整回滚 `registerRoutes`（build/vet/test 均通过）。

---

## 八、续审 2026-08-20（新增计费/支付/队列模块）

8-19 落地的商业化新模块（billing / payment / queue / adapter）经二次审核，发现并修复 **1 个 P1 + 7 个 P2**，详见 **`docs/code-review-2026-08-20.md`**。要点：

- **P1（实锤）**：支付宝 Webhook 因 `r.Body` 被二次读取，所有支付宝回调失败 → 改用 `url.ParseQuery(body)`
- **P2**：Webhook 幂等（重试风暴）、金额浮点格式化、微信/Stripe HTTP 无超时、吞 `io.ReadAll` 错误、微信验签可选告警、队列序列化错误静默；并让支付宝响应体回 `success` 防重试
- 验证：`go build ./...` / `go vet ./...` / `go test ./internal/billing/... ./internal/queue/...` 全绿
