# Skill: geo-triage-critical

## Description
关键问题分诊（triage）技能。运行 AI 可见度就绪度检查，识别所有 fail 状态项，并列出全部跟踪品牌以排查 BVS 显著下滑的品牌，最终输出一份按严重程度（Critical → High → Medium）排序的修复清单，帮助运营快速定位并优先处理影响 AI 可见度的阻断性问题。

## Trigger
当用户表达以下意图时触发：关键问题排查、问题分诊、triage、就绪度检查、readiness 检查、BVS 下滑排查、紧急修复、可见度阻断排查、检查我的站点对 AI 是否就绪、哪些品牌掉分了。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- `GET /api/v1/brand/readiness` 不依赖 AI 引擎 Key，零配置可调用（需目标站点可公网访问）。
- 审计历史库已启用（`history/brands`、`history/list` 可用），用于排查 BVS 下滑。
- 已知品牌官网 URL（用于 readiness 检查）或项目配置 `geo-project.json`。

## Workflow

### Step 1: 运行 AI 可见度就绪度检查
调用 `GET $GEO_BASE/api/v1/brand/readiness?url=<品牌官网>`（或 POST body `{"url":"..."}`）。
返回 `AuditResult`：
- `total_score`（0-100 综合就绪度）、`grade`（A-F）。
- `checks`：检查项列表，每项含 `name` / `status`（pass/fail/warn）/ `score` / `detail` / `evidence`。
  - 检查项覆盖：robots.txt、llms.txt、结构化数据（JSON-LD）、sitemap.xml、TTFB。

从 `checks` 中筛出所有 `status == "fail"` 的项，这些是阻断 AI 引擎抓取/理解的 Critical 问题。

### Step 2: 列出全部跟踪品牌
调用 `GET $GEO_BASE/api/v1/brand/history/brands` 获取所有有审计记录的品牌列表（返回 `count` 与 `brands` 数组）。

### Step 3: 逐品牌排查 BVS 下滑
对每个品牌调用 `GET $GEO_BASE/api/v1/brand/history/list?brand=<品牌名>&limit=10`，取最近若干条审计记录，比较：
- 最新记录 BVS（`score`）与上一条记录 BVS 的差值 ΔBVS。
- 若 ΔBVS ≤ 阈值（默认 -5 分，即下滑 ≥5 分），标记为“显著下滑”，需进一步排查。

> 对显著下滑的品牌，可进一步解析最新与上一条的 `report_json`，定位是哪个引擎或六维维度下滑（参考 geo-coach 的下钻方法）。

### Step 4: 汇总并按严重程度排序
将所有发现的问题按严重程度分级：
- **Critical（关键）**：readiness 检查中 status=fail 的项（直接阻断 AI 抓取，如 robots.txt 禁止、无结构化数据、TTFB 超时）。
- **High（高）**：BVS 显著下滑（ΔBVS ≤ -5）的品牌，或 readiness 中 status=warn 的项（潜在风险，如 llms.txt 缺失）。
- **Medium（中）**：BVS 轻微下滑（-5 < ΔBVS < 0）或 readiness warn 项中影响较小的。

每条问题标注：所属品牌/站点、问题描述、证据（`detail`/`evidence`）、建议修复方式、对应可调用的技能。

## Output
一份按 Critical → High → Medium 排序的修复清单，每条含：问题标题、严重程度、影响范围（哪个品牌/站点/引擎）、证据、修复建议、推荐后续技能。Critical 项需立即处理。

## Example
输入：用户“帮我排查一下关键问题，品牌字节跳动官网 bytedance.com，顺便看看所有跟踪品牌有没有掉分的”。

执行：
1. `GET $GEO_BASE/api/v1/brand/readiness?url=bytedance.com` → total_score 62 (D)，checks 中 robots.txt=pass、llms.txt=fail（缺失）、结构化数据=fail（无 JSON-LD）、sitemap=warn、TTFB=pass。
2. `GET $GEO_BASE/api/v1/brand/history/brands` → [字节跳动, 腾讯, 快手]。
3. 逐品牌 `GET $GEO_BASE/api/v1/brand/history/list?brand=<品牌>&limit=10`：
   - 字节跳动：最新 78 vs 上一条 72（+6，无下滑）。
   - 腾讯：最新 65 vs 上一条 74（-9，显著下滑）。
   - 快手：最新 60 vs 上一条 61（-1，轻微下滑）。

输出修复清单：
```
关键问题分诊报告
[Critical] 立即修复
1. bytedance.com 缺少 llms.txt（readiness fail）
   - 证据：HTTP 404
   - 修复：在站点根目录部署 llms.txt（可由 geo-content-rewrite 的 generated_assets 生成）
2. bytedance.com 无结构化数据 JSON-LD（readiness fail）
   - 证据：页面未检测到 JSON-LD
   - 修复：为重点页面注入 JSON-LD（schema 策略生成）

[High] 本周处理
3. 腾讯 BVS 显著下滑 74→65（ΔBVS -9）
   - 证据：perplexity 引用率从 70% 降至 48%
   - 修复：运行 geo-coach 下钻 + geo-content-rewrite 补强 perplexity 偏好内容
4. bytedance.com sitemap 仅 warn（潜在风险）
   - 修复：确认 sitemap.xml 可达且含最新 URL

[Medium] 关注
5. 快手 BVS 轻微下滑 61→60（ΔBVS -1）
   - 修复：持续观察，下周复盘再评估
```
