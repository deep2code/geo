# Skill: geo-competitive-landscape

## Description
竞争格局分析技能。基于项目配置的品牌画像（含竞品），执行一次完整的品牌可见度审计，跨引擎分析声量份额（Share of Voice），并调用 KOL/创作者情报接口做引用源归因，最终产出一份竞争格局报告：品牌与竞品在各 AI 引擎的可见度对比、声量份额、引用源分布与竞争差距。

## Trigger
当用户表达以下意图时触发：竞争格局分析、竞品分析、品牌审计、声量份额分析、Share of Voice、competitive landscape、竞品可见度对比、跑一次品牌审计、SOV 分析。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 已配置各目标引擎的 API Key 环境变量（否则 `POST /api/v1/brand/audit` 返回 503）。
- 已有项目配置文件 `geo-project.json`（`brand.BrandProfile` 结构，含 `competitors` 与 `prompts`）。
- 品牌审计引擎与历史库已初始化（KOL 分析可从历史库取最新审计结果）。

## Workflow

### Step 1: 读取品牌画像
读取项目配置 `geo-project.json`，得到 `brand.BrandProfile`（含 `name`、`competitors`、`prompts`、`target_engines`、`market`、`language`）。确认 `prompts` 非空、`competitors` 已列出主要竞品。

### Step 2: 执行品牌可见度审计
调用 `POST $GEO_BASE/api/v1/brand/audit`，请求体为完整的 `BrandProfile` JSON：
```json
{
  "name": "...",
  "aliases": [...],
  "domain": "...",
  "products": [...],
  "competitors": [{"name":"...","domain":"..."}],
  "prompts": ["..."],
  "target_engines": ["chatgpt","perplexity"],
  "market": "us",
  "language": "en"
}
```
返回 `VisibilityReport`，关键字段：
- `score`（BVS 0-100）、`grade`（A-F）、`tier`（household/midmarket/niche）
- `engine_stats`：各引擎的 mention_rate / citation_rate / share_of_voice / avg_position
- `competitor_sov`：各竞品声量份额
- `content_gaps`、`negative_mentions`、`actions`
- `results`：每条 prompt × 引擎的原始检测结果（供 Step 3 使用）

### Step 3: 跨引擎声量份额分析
基于 `engine_stats` 与 `competitor_sov` 汇总：
- 计算品牌在各引擎的 SOV，识别强势引擎与薄弱引擎。
- 将品牌 SOV 与各竞品 SOV 对比，计算竞争差距（gap = 竞品 SOV − 品牌 SOV）。
- 标记“竞品显著领先”的引擎与意图场景。

### Step 4: 调用 KOL 引用源归因
调用 `POST $GEO_BASE/api/v1/brand/kol/analyze`，请求体：
```json
{
  "brand_name": "...",
  "results": [...],
  "competitors": [...]
}
```
- `results` 直接传入 Step 2 返回的 `report.results`（每条 prompt×引擎检测结果）。
- `competitors` 传入品牌画像中的竞品列表，用于识别“竞品引用源，需关注”。
- 若不传 `results`，接口会从历史库取该品牌最新审计记录自动填充。

返回 `KOLReport`，含 `top_sources`（Top 20 引用域名按引用次数排序，含 SOV/engines/url）、`by_domain`（完整域名聚合）、`recommendations`。

### Step 5: 综合竞争格局报告
整合 Step 2-4 数据，产出竞争格局报告。

## Output
一份竞争格局报告，包含：
1. **总体可见度**：品牌 BVS / 等级 / 梯队，与竞品横向对比表。
2. **跨引擎 SOV 矩阵**：品牌与竞品在各引擎的声量份额及差距。
3. **引用源分布**：Top 引用域名、哪些域名偏向品牌、哪些偏向竞品。
4. **竞争差距清单**：按差距大小排序的竞品领先项，标注所在引擎与意图。
5. **行动建议**：结合报告 `actions` 与 SOV 差距，给出可执行优先级清单。

## Example
输入：项目配置品牌「字节跳动」，竞品 [腾讯, 快手]，目标引擎 [chatgpt, perplexity, gemini, claude]。

执行：
1. 读取 `geo-project.json`。
2. `POST $GEO_BASE/api/v1/brand/audit` → BVS=72（B，midmarket），perplexity 引用率最高 65%，claude 提及率最低 30%。
3. SOV 对比：字节跳动 38% / 腾讯 41% / 快手 21%；腾讯在 chatgpt 领先 12 个百分点。
4. `POST $GEO_BASE/api/v1/brand/kol/analyze`（results 来自审计）→ top_sources 含 wikipedia.org、36kr.com、zhihu.com，其中 zhihu.com 引用腾讯更多（竞品引用源，需关注）。
5. 报告摘要：
```
竞争格局（字节跳动 vs 腾讯/快手）
- 总体 BVS 72 (B) ｜梯队 midmarket
- 跨引擎 SOV：字节 38% / 腾讯 41% / 快手 21%
- 薄弱引擎：claude（提及率 30%）
- 竞品领先：腾讯在 chatgpt SOV 领先 12pt
- 关键引用源：wikipedia.org（中立）、zhihu.com（偏腾讯，需重点争取）
- 行动：① 补强 claude 引擎内容 ② 在 zhihu/36kr 增加品牌背书 ③ 针对腾讯领先意图产出对比内容
```
