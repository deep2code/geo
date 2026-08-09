# Skill: geo-coach

## Description
周度 AI 教练技能。回顾品牌可见度评分（BVS）随时间的变化趋势，调用历史趋势与聚合统计接口获取数据，生成一份周度总结：本周哪些维度改善、哪些下降、下周应聚焦什么，并产出按影响力排序的可执行下一步行动清单，作为品牌方的每周 GEO 运营复盘。

## Trigger
当用户表达以下意图时触发：周度复盘、每周总结、AI 教练、BVS 趋势、周报、weekly coach、本周 GEO 表现、趋势分析、下周做什么、GEO 运营建议。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 审计历史库已启用（`brand/history/*` 接口可用），且品牌已有 ≥2 条历史审计记录（否则无法对比趋势）。
- 已知目标品牌名（或通过 `history/brands` 列出全部跟踪品牌后选择）。
- 建议配合定时调度器（`scheduler`）按周自动审计，保证历史数据连续。

## Workflow

### Step 1: 确定复盘品牌与时间窗口
- 调用 `GET $GEO_BASE/api/v1/brand/history/brands` 获取所有有审计记录的品牌列表。
- 由用户选择目标品牌（或对全部品牌逐一复盘）。
- 设定时间窗口：默认“最近 7 天 vs 上一周”，可调整。需取足够多的历史记录覆盖窗口（建议 `limit=50`）。

### Step 2: 获取历史趋势数据
调用 `GET $GEO_BASE/api/v1/brand/history/list?brand=<品牌名>&limit=50`（或 POST body `{"brand":"...","limit":50}`）。
返回该品牌最近 50 条审计记录（按时间排序），每条含 `score`（BVS）、`grade`、`generated_at`、`report_json` 等。

按时间窗口切分：
- 本周记录集合 `R_this`（最近 7 天）。
- 上周记录集合 `R_last`（上一个 7 天）。
- 计算本周 BVS 均值 / 最新值，与上周对比，得出 ΔBVS。

### Step 3: 获取聚合统计
调用 `GET $GEO_BASE/api/v1/brand/history/stats` 获取历史库整体统计（审计总次数、品牌数、时间跨度、各品牌审计次数等），用于在周报中提供横向基准（如该品牌审计频次是否达标、与其它品牌对比）。

### Step 4: 下钻维度变化
对本周与上周的最新审计记录，分别解析 `report_json` 中的 `VisibilityReport`，对比六维评分明细（`score_breakdown`：mention_rate / citation_rate / share_of_voice / citation_position / sentiment / entity_recognition）与各引擎统计（`engine_stats`），识别：
- **改善项**：本周较上周提升的维度 / 引擎。
- **下降项**：本周较上周下滑的维度 / 引擎。
- **持平项**：无明显变化的维度。

同时关注 `content_gaps`、`negative_mentions`、`actions` 的变化。

### Step 5: 生成周度总结与下一步行动
综合以上数据，产出：
1. **本周摘要**：BVS 变化（ΔBVS）、等级变化、梯队变化。
2. **改善 / 下降清单**：按维度与引擎列出。
3. **下周聚焦**：基于下降项与高优先级 `actions`，选定 2-3 个聚焦方向。
4. **可执行下一步**：按预估影响力排序的行动清单，每条标注对应技能（如内容改写、外链发掘、关键问题修复）。

## Output
一份周度 GEO 教练报告，包含：① 本周 BVS 摘要与环比变化 ② 六维与各引擎的改善/下降明细 ③ 下周聚焦方向 ④ 按影响力排序的可执行行动清单（含建议调用的技能与预估收益）。

## Example
输入：用户“给我本周的 GEO 周报，品牌字节跳动”。

执行：
1. `GET $GEO_BASE/api/v1/brand/history/brands` → [字节跳动, 腾讯, ...]，选定字节跳动。
2. `GET $GEO_BASE/api/v1/brand/history/list?brand=字节跳动&limit=50` → 取得 8 条记录，本周 2 条、上周 2 条。
3. `GET $GEO_BASE/api/v1/brand/history/stats` → 字节跳动累计审计 8 次，频次达标。
4. 解析 report_json 对比：
   - 本周最新 BVS 78（C+） vs 上周 72（B）→ ΔBVS +6，但 perplexity 引用率从 65% 降到 52%。
   - mention_rate 提升、citation_rate 下降。

输出报告摘要：
```
字节跳动 · 周度 GEO 教练报告
- 本周 BVS 78 (C+)，环比 +6 分；等级 B→C+（注：分数升但等级阈值跨越，实际改善）
- 改善项：mention_rate +8pt（claude 引擎提及率 30%→45%）、entity_recognition +5pt
- 下降项：citation_rate -13pt（perplexity 引用率 65%→52%，需重点关注）
- 下周聚焦：① 恢复 perplexity 引用率 ② 巩固 claude 增长
- 可执行下一步（按影响力排序）：
  1. [高] 针对 perplexity 下降的查询，运行 geo-content-rewrite 补强 cite_sources/quotation（预估 PWC +27%~41%）
  2. [高] 运行 geo-link-prospecting，在 perplexity 偏好的引用源（如 zhihu/wikipedia）争取外链
  3. [中] 运行 geo-triage-critical 检查站点 readiness，排除 robots/llms.txt 阻断
```
