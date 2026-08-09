# Skill: geo-link-prospecting

## Description
链接（外链/引用）发掘技能。利用品牌审计中的 topsource 引用源归因，识别对 AI 引擎引用有高价值的外部域名，结合本地品牌知识库检索补全品牌实体信息，生成一份按优先级排序、按类型分类（评测站、行业博客、目录站、新闻媒体）的待争取外链/引用域名清单，指导品牌方主动获取高质量反向链接与 AI 引用。

## Trigger
当用户表达以下意图时触发：外链发掘、链接建设、引用源发掘、link prospecting、backlink 机会、找外链、争取引用域名、topsource 分析、AI 引用源建设。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 已执行过至少一次品牌审计（KOL 分析可从历史库取最新审计结果）；若无审计记录，需先运行 `geo-competitive-landscape` 技能。
- 本地品牌知识库（SinoFacts）已加载，`GET /api/v1/brand/knowledge/search` 可零配置调用。
- 已有项目配置 `geo-project.json`。

## Workflow

### Step 1: 获取引用源归因数据
调用 `POST $GEO_BASE/api/v1/brand/kol/analyze`，请求体：
```json
{"brand_name": "..."}
```
（不传 `results` 时接口自动从历史库取该品牌最新审计结果；可附带 `competitors` 用于识别竞品引用源。）

返回 `KOLReport`，重点关注：
- `top_sources`：Top 20 引用域名，含 `domain`、`mention_count`、`citation_count`、`engines`、`sov`、`url`。
- `by_domain`：完整域名聚合列表。
- `recommendations`：每条对应一个 Top 源的推荐操作。

### Step 2: 检索品牌知识库补充实体信息
对目标品牌调用 `GET $GEO_BASE/api/v1/brand/knowledge/search?q=<品牌名>&limit=5`（或 POST 同路径 body `{"q":"...","limit":5}`）。
返回品牌实体信息（brand_name/brand_domain/industry/category/products/company_name 等），用于：
- 识别品牌已有的官方可引用资产（官网、产品页、公司主体）。
- 判断哪些 top source 域名尚未收录品牌或收录不全，作为优先争取对象。

> 知识库覆盖 383 家中国出海软件公司（SinoFacts CC BY 4.0），离线零延迟。未命中不代表品牌不存在，仅说明本地知识库无收录。

### Step 3: 识别高价值待争取域名
从 `top_sources` + `by_domain` 中筛选“高价值且尚未充分引用品牌”的域名，筛选维度：
- **引用频次高但品牌缺席**：`citation_count` 高，但该域名主要引用竞品（来自 KOL `recommendations` 中“竞品引用源，需关注”标记）。
- **多引擎覆盖**：`engines` 字段含多个引擎，说明该源被多家 AI 引擎信任。
- **SOV 高**：`sov` 占比高，是行业权威源。
- **品牌未出现**：对比 Step 2 知识库与审计结果，品牌在该域名无提及。

### Step 4: 按类型分类与优先级排序
将筛选出的域名按类型分类：
- **评测站（review sites）**：如 G2、Capterra、Trustpilot、36kr 评测——适合产品对比/推荐意图。
- **行业博客（industry blogs）**：如技术博客、行业媒体——适合教程/深度内容意图。
- **目录站（directories）**：如 Wikipedia、维基百科、行业目录——适合品牌实体认知意图。
- **新闻媒体（news）**：如 36kr、TechCrunch、Reuters——适合品牌权威背书。

按“引用频次 × 多引擎覆盖 × 与品牌意图匹配度”评定优先级（high / medium / low）。

## Output
一份按类型分类、按优先级排序的待争取外链/引用域名清单，每个域名包含：域名、当前引用情况、覆盖引擎、建议获取方式（如提交产品评测、编辑维基词条、投稿行业博客）、预期收益（提升哪些引擎的引用率）。同时输出 JSON 汇总。

## Example
输入：品牌「字节跳动」，已有最新审计记录。

执行：
1. `POST $GEO_BASE/api/v1/brand/kol/analyze` body `{"brand_name":"字节跳动","competitors":[{"name":"腾讯"}]}` → top_sources 含 wikipedia.org（4 引擎，sov 18%）、zhihu.com（3 引擎，sov 12%，偏腾讯）、36kr.com（2 引擎，sov 9%）、g2.com（1 引擎，sov 5%，未提及字节）。
2. `GET $GEO_BASE/api/v1/brand/knowledge/search?q=字节跳动&limit=5` → 命中知识库，返回 brand_domain=bytedance.com、products=[抖音,TikTok,...]、company_name=字节跳动有限公司。
3-4. 筛选分类：
```json
{
  "prospects": [
    {"domain":"zhihu.com","type":"industry_blogs","priority":"high","engines":["chatgpt","perplexity","gemini"],"current":"偏腾讯引用","action":"在知乎建立品牌官方号+优质回答","expected":"提升 perplexity/chatgpt 引用率"},
    {"domain":"wikipedia.org","type":"directories","priority":"high","engines":["chatgpt","perplexity","gemini","claude"],"current":"中立收录","action":"完善维基词条+引用来源","expected":"提升全引擎实体认知"},
    {"domain":"g2.com","type":"review_sites","priority":"medium","engines":["perplexity"],"current":"未提及品牌","action":"提交产品评测页","expected":"提升推荐类意图引用"},
    {"domain":"36kr.com","type":"news","priority":"medium","engines":["chatgpt","perplexity"],"current":"少量提及","action":"投稿深度报道","expected":"提升权威背书"}
  ]
}
```
输出摘要：共发掘 4 个高价值域名，high 优先级 2 个（zhihu.com / wikipedia.org），建议优先完善维基词条与知乎品牌号建设。
