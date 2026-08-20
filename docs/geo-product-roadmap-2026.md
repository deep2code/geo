# GEO 商业化能力路线图与落地记录（2026-08-20）

> 依据《GEO 系统能力成熟度矩阵》评审结论（2026-08-19），将全部 P0/P1/P2 项落地为真实代码。
> 本文档为**实现状态**记录：每项含目标、落地位置（file:line/包/API）、验证结果。
> 评审背景见 `docs/code-review-opensource-2026-08-18.md` 与 `docs/code-review-2026-08-20.md`。

## 实现原则

- **零外部依赖**：全部新代码仅用标准库 + 复用既有 `internal/adapter` / `knowledge` / `history` 等包
- **优雅降级**：LLM 判定不可用时自动回退词典法，系统行为与旧版一致
- **无导入环**：新增包保持 leaf 化（`llmanalysis` / `attribution` / `persona` / `promptversion` 均不反向依赖 brand）
- **DDL 单一事实来源**：新表全部写入 `deploy/initdb/02-schema.sql`，应用内零建表
- 验证：`go build ./...` ✅ / `go vet ./...` ✅ / 新包单测全绿 ✅

---

## 🔴 P0 — 商业价值核心

### P0-1 ✅ LLM 判定层替代关键词词典法

**目标**：情感/位置/引用识别的"词典法地基"升级为 LLM 推理。

- 新包 `internal/brand/llmanalysis/`：
  - `Analyzer.Sentiment()` — LLM 情感判定（上下文/反讽理解），返回 label+理由+置信度
  - `Analyzer.ExtractSources()` — LLM 识别"回答采信了谁"（语义级，非正则）
  - `Analyzer.Accuracy()` — 回答与已核验事实比对（P0-3 共用）
  - 全部带词典法降级（`fallbackSentiment` / `fallbackExtractSources` / `fallbackAccuracy`）
- 接入点：
  - `Monitor.WithJudge(adapter)` + `brand.WithJudge(adapter)`；`monitor.go queryOne` 情感判定优先走 LLM
  - `internal/server/server.go pickJudgeAdapter()` — 自动选判定模型（deepseek > chatgpt > glm > 首个已配置）
- 验证：`llmanalysis_test.go` 覆盖降级路径与 JSON 围栏解析

### P0-2 ✅ AI 引荐流量 / ROI 归因

**目标**：把"被 AI 引用"归因到"会话 → 转化 → 收入"，回答客户"为 ROI 续费"。

- 新包 `internal/brand/attribution/`：
  - `Source` 接口（GA4 / 站点日志 / UTM 可插拔），`IsAIReferrer()` 已知 AI 域名判定，`UTMMatcher` token 级匹配（防 "email" 误中 "ai"）
  - `Tracker.Compute()` — 日粒度聚合 + 可见度弹性归因（AI 引荐 × 权重 × 可见度/100）
  - `AttributionReport` / `DailyAttribution` / `Store` 接口 + `MemoryStore`
- API：`POST /api/v1/brand/attribution`（传 traffic + visibility，返回归因报告）
- 表：`ai_traffic` / `ai_conversion`
- 验证：`attribution_test.go` 覆盖 referrer/UTM/归因计算

### P0-3 ✅ 品牌准确性 / 幻觉检测

**目标**：AI 关于品牌的回答是**准确**还是**编造**——企业客户（金融/医疗）硬需求。

- `llmanalysis.Accuracy(ctx, brand, answer, facts)` — LLM 标记 `contradict/unsupported/hallucination/consistent` + severity
- 事实来源：`Reporter.buildBrandFacts()` — 品牌画像（公司成立年/简介/产品/行业）+ `knowledge` 包最佳匹配条目
- 接入：`Reporter.SetAnalyzer()` → `detectAccuracy()`（仅对"品牌被提及"的回答调用，控制成本）
- 报告字段：`VisibilityReport.AccuracyFlags`
- 验证：`llmanalysis_test.go` 降级路径

---

## 🟠 P1 — 差异化

### P1-a ✅ Source 情报 LLM 深化 + 执行闭环

**目标**：topsource 从正则 URL 升级为"理解采信实体"，且 MissingSources → 可执行行动项。

- `topsource.Analyze()` 聚合循环新增 `pr.ExtractedSources` 语义源（LLM 识别）→ 纳入域名聚合与 brandHit 判定
- 执行闭环：`handleBrandAudit` 中 `topsource.Analyze` 的 Recommendations 自动追加为 `ActionItem{category:"source"}`（入驻/外链建议落进报告）
- 既有端点 `POST /api/v1/brand/topsource/analyze` 保留

### P1-b ✅ SOV 加权 + 历史排名精度

**目标**：竞品声量从裸提及计数升级为带权重的可信指标。

- `Reporter.calcCompetitorSOVWeighted()` — 引擎覆盖权重（未配置 key 的模拟引擎权重 0.3）+ 位置权重（首段 1.0，逐段 ×0.85，下限 0.2）
- 报告字段：`VisibilityReport.WeightedCompetitorSOV`（与裸版并存，前端可对照）

### P1-c ✅ 买家人设分群测量

**目标**：回答"哪类买家在 AI 里看不见你"（Gumshoe 差异化卖点）。

- 新包 `internal/brand/persona/`：`Persona` 定义 + `Aggregate()` 按人设聚合提及率/情感/位置/缺失查询
- 引擎方法 `Engine.PersonaBreakdown(ctx, profile, personas)`（独立于 Audit，不落库）
- API：`POST /api/v1/brand/persona`
- 表：`personas`
- 验证：`persona_test.go`

### P1-d ✅ 开放测量 API

**目标**：测量能力对外暴露，供 agency / BI 接入。

- `POST /api/v1/brand/measure` — 只读测量快照（score/grade/tier/加权 SOV/人设/准确性/源缺口），剥离原始逐条回答
- 鉴权：`X-GEO-API-Key` 头匹配 `GEO_OPENAPI_KEY`（未配置返回 503，默认关闭）

### P1-e ✅ Prompt 版本管理 + 实验归因

**目标**：内容改了 → 可见度涨没涨？给"因果归因"。

- 新包 `internal/brand/promptversion/`：`TrackedPrompt` / `PromptVersion` / `Experiment`（`ComputeLift()` 量化 lift + 显著性）+ `Store` 接口 + `MemoryStore`
- API：`POST /api/v1/brand/prompt`、`/version`、`/experiment`、`GET /versions`、`/experiments`
- 表：`tracked_prompts` / `prompt_versions` / `prompt_experiments`
- 验证：`promptversion_test.go`

---

## 🟡 P2 — 规模化

### P2-a ✅ 多语言/多市场地理维度

既有能力确认（无需新增）：`BrandProfile.Market/Language`、`market.LocalizePrompts()`、`GET /api/v1/brand/markets`。

### P2-b ✅ 白标 agency 多客户隔离

- `Whitelabel` 结构体与 `/api/v1/meta/whitelabel` 新增租户字段：`agency_name` / `tenant_id` / `support_email`
- 环境变量：`GEO_WL_AGENCY_NAME` / `GEO_WL_TENANT_ID` / `GEO_WL_SUPPORT_EMAIL`

### P2-c ✅ 预测性策略生成

- `GET /api/v1/brand/predict?brand=xxx&horizon=N` — 读历史审计分数做最小二乘线性拟合，外推未来 N 期（默认 4，≤24），返回斜率/预测序列/策略建议（上升加码 / 下降预警 / 维持）

---

## 新增 API 一览

| 方法 | 路径 | 能力 | 鉴权 |
|---|---|---|---|
| POST | `/api/v1/brand/attribution` | ROI 归因 | 现有 API Key |
| POST | `/api/v1/brand/persona` | 人设分群 | 现有 API Key |
| POST | `/api/v1/brand/measure` | 开放测量快照 | `X-GEO-API-Key`（`GEO_OPENAPI_KEY`） |
| POST | `/api/v1/brand/prompt` | 创建被追踪 prompt | 现有 API Key |
| POST | `/api/v1/brand/prompt/version` | 追加版本 | 现有 API Key |
| GET | `/api/v1/brand/prompt/versions?prompt_id=` | 列版本 | 现有 API Key |
| POST | `/api/v1/brand/prompt/experiment` | 保存实验 | 现有 API Key |
| GET | `/api/v1/brand/prompt/experiments?prompt_id=` | 列实验 | 现有 API Key |
| GET | `/api/v1/brand/predict?brand=&horizon=` | 趋势预测 | 现有 API Key |

## 新增表（deploy/initdb/02-schema.sql，9~11 节）

`ai_traffic` / `ai_conversion` / `tracked_prompts` / `prompt_versions` / `prompt_experiments` / `personas`

## 验证结果

- `go build ./...` ✅
- `go vet ./...` ✅（全仓无警告）
- 新包单测：`llmanalysis` / `attribution` / `persona` / `promptversion` 全绿 ✅
- 唯一失败：`TestChinaCheckConnectivity`（外部 China-Check API 401，网络依赖，与本次改动无关）

## 待办（后续）

1. `attribution` / `promptversion` 的 MySQL Store 实现（当前为内存，接入 GA4 后落库）
2. 前端页面：ROI 归因看板 / 人设分群图 / 预测曲线（后端就绪，等前端排期）
3. 真实商户号 + webhook 端到端冒烟（沿用 code-review-2026-08-20 待办）
