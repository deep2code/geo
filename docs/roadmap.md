# GEO 商业化落地路线图（待办清单）

> 说明：前端 SPA、PDF 报告、邮件系统、白标定制、限流/WAF/安全扫描、向量检索、
> 外部信号源扩展、品牌画像自动补全、竞品对比报告、管理员后台、帮助中心、工单系统、
> 官网落地页、CMS 插件、Chrome 扩展、GEO 排行榜、国内 LLM 适配器、
> 前端与交互（主题系统/移动端适配+PWA/组件库Token化）、法务计划（用户协议/隐私政策/DPA、
> 数据权利接口、爬虫合规 + AI 生成内容统一标识）、
> 账号体系（邮箱+密码注册登录/JWT会话/Workspace多租户/RBAC四角色/AdminAuditLog）
> 等 **27 项** 已落地，不再列入本清单。本文仅保留**待做事项**，作为下一阶段产品化迭代排期参考。
> 优先级：`P1` = 上线必备、`P2` = 增长驱动、`P3` = 体验优化。

---

## 一、身份、账号与权限

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 5 | SSO / SAML / OIDC | P2 | 对接企业飞书/钉钉/企业微信 IdP。 |

> 其余已落地（`internal/auth/*` + `internal/server/server.go` 中间件 + `internal/brand/history/*` workspace 改造）：
> - ✅ #1 邮箱 + 密码注册 / 登录（首用户开放，后续需 Owner/Admin 邀请）
> - ✅ #2 HMAC-SHA256 JWT（access 2h / refresh 14d）+ refresh 轮换 + 登出吊销 + 防暴
> - ✅ #3 Workspace 多租户 + `audit_history`/`BrandProfile` 按 workspace 自动迁移过滤
> - ✅ #4 Owner/Admin/Member/Viewer 四角色 × 9 Permissions RBAC 中间件
> - ✅ #6 Admin Audit Log（登录/改密/角色变更/删成员等关键操作 + IP/UA 留痕）

## 二、计费与商业化

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 10 | 套餐与配额模型 | P1 | Free / Pro / Enterprise；按品牌数、审计次数、邮件量、并发数限制。定价方案 API 已就绪，需接入配额执行。 |
| 11 | 支付渠道接入 | P1 | 支付宝 / 微信 / 银联；海外可考虑 Stripe。 |
| 12 | 发票与合同流 | P1 | 电子发票开票、订单管理、续费与到期提醒。 |
| 13 | 试用 / 邀请码 / 优惠券 | P2 | 7-14 天免费试用、渠道邀请码、促销折扣。 |
| 14 | 用量计量（Metering） | P1 | 审计次数、LLM Token、邮件数、PDF 导出、外部 API 调用计量入库。 |
| 15 | 订阅续费与欠费策略 | P1 | Grace period、自动续费开关、降级只读模式。 |

## 三、部署、运维与可靠性

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 20 | 生产部署脚本 / 一键 Helm Chart | P1 | 当前 `deploy.sh`/`docker-compose.yml` 偏开发用，需要 k8s 版本与水平扩容。 |
| 21 | 配置中心与密钥管理 | P1 | SMTP / LLM Key / DB 密码改用 Vault 或 KMS，不直接走 `.env`。 |
| 22 | 可观测性（Logging / Tracing / Metrics） | P1 | 接入 OpenTelemetry + Prometheus + Grafana；当前有 slog 结构化日志，需补 Tracing/Metrics。 |
| 23 | 健康检查与告警 | P1 | `/healthz`、`/readyz`；数据库 / Chromium / SMTP 连通性自检并钉钉/飞书告警。 |
| 24 | 数据备份与恢复 | P1 | SQLite/Postgres 每日定时备份，支持一键回滚；审计历史库定期冷备。 |
| 25 | 高可用与多副本 | P2 | API 无状态可多副本；历史库切换 Postgres + 只读副本。 |

## 四、数据层与 AI 能力升级

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 40 | 审计历史库从 SQLite 升级 Postgres | P1 | 多用户并发写入、索引、时序查询优化；当前 SQLite 适合单机。 |
| 43 | 引用与原文追溯缓存 | P1 | SERP 链接、新闻页正文需要带快照缓存与原文哈希，防"404 后评分无法复现"。 |
| 44 | LLM Provider 抽象 + Fallback 路由 | P1 | 当前有 adapter + 降级缓存，但缺主模型失败切备用模型的路由策略。 |
| 45 | 长内容缓存（Prompt Cache / Context Caching） | P2 | 对品牌画像、历史报告做缓存，降本提速。 |
| 46 | 多语言 / 多市场模型 | P2 | 中/英/日/东南亚市场差异化信号源与评分公式。前端 i18n 已就绪，需补市场差异化逻辑。 |
| 47 | BVS 版本管理与模型回归测试 | P1 | 评分算法改动要能 A/B 对比历史品牌分差，避免客户感知跳变。 |

## 五、产品功能增强（商业价值强相关）

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 60 | 品牌告警规则引擎 | P1 | 当前 SPA 里规则在 localStorage（仅前端），需要服务端持久化 + 周期评估 + 自动发邮件。 |
| 61 | 周报自动分发订阅 | P1 | 定时任务把周维度 BVS 汇总、排名变化、告警摘要，邮件推送给订阅人。 |
| 64 | 任务队列（审计 / PDF / 邮件异步） | P1 | 当前接口同步执行，长审计会超时；改用 `asynq`/`machinery` + worker。 |
| 65 | 任务进度与实时通知 | P2 | WebSocket/SSE 推送审计进度、完成通知、失败原因。 |
| 66 | 可导出 CSV / Excel 明细 | P2 | 引擎得分明细、关键词、Top 引用列表结构化导出。 |
| 67 | 品牌协作评论与 @ 提及 | P2 | 报告上直接批注，@某人并发邮件 / 飞书。 |
| 68 | 版本对比（两次审计 Diff） | P1 | BVS 各分项变化、Top 引用新增/消失、引擎级变化表格。 |
| 69 | 目标追踪（OKR 式目标 + 达成进度） | P2 | 给品牌设定季度 BVS 目标并展示进度条。 |
| 70 | 内容审计与品牌得分联动 | P2 | 用户用"内容优化"改写后，可一键预测对目标品牌 BVS 的提升方向。 |

## 六、法务计划（合规与风险控制）✅ 已落地

> 两项 P1 已在 `internal/server/legal.go`、`internal/util/util.go`（合规 UA/robots/限频）、
> `internal/brand/crawler`、`internal/brand/crawlability`（统一合规请求）、
> `internal/brand/report/report.go`（报告页脚 AI 声明）、`internal/mail/mail.go`（邮件页脚 AI 声明）、
> `web-app/src/pages/{Terms,Privacy,DPA}` 与 `web-app/src/pages/Settings`（数据权利 UI）实现。

| # | 事项 | 优先级 | 状态 | 说明 |
|---|------|--------|------|------|
| 80 | 用户协议 / 隐私政策 + 数据处理合规 | P1 | ✅ 已落地 | ToS/Privacy/DPA SPA 页面 + Footer/Settings 链接 + 后端 3 个数据权利接口（access/export/delete 含 request_id + SLA + 审计占位）；前端 Settings "数据权利" tab 对接完成。 |
| 81 | 爬虫合规 + AI 生成内容标识 | P1 | ✅ 已落地 | 所有对外请求统一 `MyGEOBot/1.0 (+/legal/bot; compliance@mygeo.ai)` UA；请求前 robots.txt 检查；每主机 600ms 礼貌限频；服务端 `/legal/bot` 避风港信息页 + `/robots.txt`。所有 AI 产出（报告 HTML/PDF、邮件、SPA 页面、改写/优化 API）统一 `X-AI-Generated` / `X-AI-Disclaimer` 响应头 + 页脚声明 + 引用追溯。 |

## 七、商业交付与支持

| # | 事项 | 优先级 | 说明 |
|---|------|--------|------|
| 104 | Demo 环境 / 沙箱数据 | P1 | 不用注册就能体验 5 个预置品牌的历史审计与报告。 |

---

## 建议的下一阶段迭代顺序

### 阶段 1：生产可用（P1 必做）

> 账号体系（注册登录/JWT/Workspace/RBAC/AdminAuditLog）已在 `internal/auth` + `internal/brand/history` 落地，不再列入。

1. 配置中心与密钥管理（Vault/KMS）
2. 生产部署脚本（Helm Chart）+ 可观测性
3. 健康检查与告警 + 数据备份
4. 套餐与配额模型 + 用量计量
5. 审计历史库从 SQLite 升级 Postgres

### 阶段 2：商业化闭环（P1 必做）

1. 支付渠道接入 + 发票与合同流
2. 订阅续费与欠费策略
3. 任务队列（审计/PDF/邮件异步）
4. 服务端告警规则引擎 + 周报自动分发
5. 版本对比（两次审计 Diff）
6. Demo 环境 / 沙箱数据

### 阶段 3：增长驱动（P2）

1. SSO / SAML / OIDC + SLA 文档
2. 外部信号源真实 API 接入（指数平台、社媒平台、CRM）
3. 支付渠道扩展 + 试用/邀请码/优惠券
4. 多语言/多市场差异化评分
5. 任务进度实时通知 + 协作评论

### 阶段 4：体验优化（P2 + P3）

1. 长内容缓存 + 高可用多副本
2. 目标追踪（OKR 式）+ 内容审计联动
3. BVS 版本管理与模型回归测试 A/B 平台
4. 引用与原文快照缓存
5. LLM Provider 抽象 + Fallback 路由
