# Skill: geo-content-rewrite

## Description
内容改写技能。接收一段原始内容与目标查询，调用 GEO 优化接口应用 9 大 GEO 策略（Princeton 论文）进行改写，展示改写前后的 GEO 评分对比、已应用策略清单及各自提升幅度，并给出具体的逐句改写建议与预估可见度（PWC）提升，帮助内容创作者产出更易被 AI 搜索引擎引用的内容。

## Trigger
当用户表达以下意图时触发：内容改写、GEO 优化、内容优化、改写内容、content rewrite、optimize content、提升可引用性、内容改写建议、把这段内容改成 AI 友好。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 若需 LLM 自动改写（而非仅规则化预处理），需配置至少一个 LLM Provider（否则接口仅返回预处理后内容与评分，仍可用）。
- 已有待改写的原始内容文本，以及（可选）目标查询、目标引擎、领域类型、企业信息。

## Workflow

### Step 1: 收集改写输入
向用户收集：
- `content`（必填）：原始内容文本。
- `target_query`（可选）：该内容想优化的目标查询/提示词，用于上下文匹配。
- `target_engines`（可选）：目标 AI 引擎列表，如 `["chatgpt","perplexity"]`。
- `domain_type`（可选）：领域类型，影响策略推荐。
- `enterprise`（可选）：企业信息 `{company_name, product_name, description}`，用于实体一致性增强。
- `strategies`（可选）：指定策略子集；为空时引擎会按领域与引擎自动推荐全部 9 大策略。

### Step 2: 调用 GEO 优化接口
调用 `POST $GEO_BASE/api/v1/optimize`，请求体（`OptimizationRequest`）：
```json
{
  "content": "原始内容...",
  "url": "可选页面 URL",
  "title": "可选标题",
  "target_engines": ["chatgpt","perplexity"],
  "domain_type": "...",
  "strategies": [],
  "enterprise": {"company_name":"...","product_name":"...","description":"..."},
  "language": "zh"
}
```
> `strategies` 留空时，引擎默认应用全部 9 大 Princeton 策略（见下方策略表）。

返回 `OptimizationResponse`：
- `optimized_content`：改写后内容。
- `score_before` / `score_after`：改写前后 GEO 评分（0-100）。
- `geo_score`：可见度指标（含 mention_rate/citation_rate/share_of_voice 等）。
- `utility_score`：效用指标。
- `applied_strategies`：每个策略的执行结果（`strategy`/`applied`/`improvement`/`detail`）。
- `recommendations`：进一步优化建议（按 priority 排序）。
- `generated_assets`：生成的 `llms.txt` 与 JSON-LD 结构化资产。

### Step 3: 展示前后对比与策略效果
基于返回结果：
1. **评分对比**：展示 `score_before` → `score_after` 的提升，换算等级（A-F）。
2. **已应用策略清单**：列出 9 大策略中实际生效的策略及其预估提升幅度：

| 策略 | 标识 | 预估可见度提升 |
|------|------|----------------|
| 引用来源 | `cite_sources` | +27% |
| 统计数据 | `statistics` | +33% |
| 权威语气 | `authoritative` | — |
| 引用语 | `quotation` | +41% |
| 流畅度 | `fluency` | +29% |
| 易于理解 | `easy_understand` | — |
| 关键词 | `keyword` | — |
| 独特词汇 | `unique_words` | — |
| 技术术语 | `technical_terms` | — |

（另含扩展工程化策略：`structure`/`faq`/`schema`/`answer_first`，可叠加使用。）

3. **逐句改写建议**：对比 `optimized_content` 与原文，标出关键改动点（如补充了引用源、加入统计数据、结论前置等），并标注每处改动对应的策略与预估 PWC（Per-Web-Content）可见度提升。

### Step 4: 给出落地建议与结构化资产
- 输出 `recommendations` 中 high 优先级建议。
- 提供 `generated_assets.llms_txt` 与 `json_ld`，建议用户部署到站点根目录与页面 `<head>`。

## Output
一份内容改写报告，包含：① 改写前后全文对比 ② 评分提升（before→after + 等级）③ 已应用 9 大策略清单与各自提升 ④ 逐句改写建议与预估 PWC 提升 ⑤ 进一步优化建议 ⑥ 可部署的结构化资产（llms.txt / JSON-LD）。

## Example
输入：
```
content: "剪映是一款视频剪辑工具，功能很多，适合小白用户。"
target_query: "剪映怎么剪辑视频"
target_engines: ["perplexity","chatgpt"]
```

执行 `POST $GEO_BASE/api/v1/optimize`（strategies 留空，应用全部 9 策略）→
- score_before: 52 (F) → score_after: 81 (B)
- applied_strategies: cite_sources(已应用,+27%)、statistics(已应用,+33%)、quotation(未应用)、answer_first(已应用)、structure(已应用)...
- optimized_content: "剪映是字节跳动旗下的一款免费视频剪辑工具（来源：bytedance.com）。截至 2024 年，剪映月活超 1 亿，支持一键成片、关键帧、AI 字幕等功能，尤其适合零基础用户快速上手……"

输出报告摘要：
```
内容改写报告（剪映怎么剪辑视频）
- 评分：52 (F) → 81 (B)，提升 +29 分
- 生效策略：cite_sources(+27%) / statistics(+33%) / answer_first / structure
- 逐句建议：
  ① 首句补“字节跳动旗下”（technical_terms + entity 一致性）
  ② 加入“月活超 1 亿”统计数据（statistics，预估 PWC +33%）
  ③ 补充来源 bytedance.com（cite_sources，预估 PWC +27%）
  ④ 结论前置：直接回答“剪映是什么”（answer_first）
- 进一步建议：补充引用语（quotation，可再 +41%）
- 资产：已生成 llms.txt + JSON-LD，建议部署到站点根目录与页面 head
```
