# Skill: geo-keyword-clustering

## Description
关键词（查询/提示词）聚类技能。接收一组目标查询或提示词列表，对每条查询调用内容信号分析接口提取语义特征，再按搜索意图（对比类、推荐类、教程类、本地类、交易类等）进行语义相似度分组，输出带优先级的聚类分组，帮助运营明确内容建设的优先顺序。

## Trigger
当用户表达以下意图时触发：关键词聚类、查询分组、提示词分类、keyword clustering、按意图分组查询、把目标 query 分一下类、聚类分析、整理目标查询。

## Prerequisites
- GEO 服务正在运行，设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 已有一组目标查询 / 提示词（来源：项目配置 `geo-project.json` 的 `prompts` 字段，或用户直接提供）。
- 分析接口 `POST /api/v1/analyze` 不依赖外部 AI 引擎 Key，零配置可调用。

## Workflow

### Step 1: 收集目标查询列表
从项目配置文件读取 `prompts`，或由用户直接提供一组查询。去重、去除空白项，得到待聚类的查询集合 `Q = [q1, q2, ..., qn]`。

### Step 2: 逐条调用内容信号分析
对每条查询 `q` 调用 `POST $GEO_BASE/api/v1/analyze`，请求体：
```json
{"content": "q"}
```
返回该查询的内容信号分析结果（含可引用性信号、结构信号、负向信号、关键词、统计信号等）。将每条查询的分析特征作为聚类输入向量。

> 说明：`/analyze` 接收的是“内容”，对查询而言会返回其语义特征（关键词、术语、结构倾向等），用于推断意图。若查询较短，可拼接品牌上下文（如「品牌名 + 查询」）以增强特征。

### Step 3: 按搜索意图聚类
基于 Step 2 的特征，结合查询文本本身，按以下意图类别分组（可按需扩展）：
- **对比类（comparison）**：含“vs、和…哪个好、区别、对比”等，用户在权衡选项。
- **推荐类（recommendation）**：含“推荐、最好、排行、Top、哪家好”等，寻求建议。
- **教程类（how-to）**：含“怎么、如何、教程、步骤、方法”等，寻求操作指引。
- **本地类（local）**：含地点、附近、哪里有、城市名等，带地理位置意图。
- **交易类（transactional）**：含“价格、多少钱、购买、下载、免费、优惠”等，带转化意图。
- **品牌/实体类（entity）**：直接询问品牌是什么、介绍、背景等实体认知意图。

### Step 4: 评估各组优先级
为每个聚类组评定优先级（high / medium / low），评定依据：
- 查询数量（覆盖面）
- 商业价值（交易类、推荐类通常高价值）
- 内容缺口（结合审计报告 `content_gaps`，若该意图组在审计中提及率低则优先级提升）
- 可控性（教程类、品牌类内容品牌方可主动建设，优先级偏高）

## Output
带优先级的聚类分组报告，每组包含：意图类别、组内查询列表、查询数量、优先级、建议内容形式（如教程→操作指南、对比→对比表、推荐→榜单页）。同时输出一份 JSON 汇总便于后续技能消费。

## Example
输入查询（来自项目配置）：
```
["什么是字节跳动","抖音和快手哪个好","怎么用剪映剪辑视频","字节跳动旗下有哪些产品","TikTok 怎么下载","抖音直播带货怎么做"]
```

执行 `POST $GEO_BASE/api/v1/analyze`（每条一次）后聚类结果：
```json
{
  "clusters": [
    {
      "intent": "entity",
      "queries": ["什么是字节跳动","字节跳动旗下有哪些产品"],
      "count": 2,
      "priority": "high",
      "suggested_format": "品牌百科/About 页面 + 产品矩阵页"
    },
    {
      "intent": "comparison",
      "queries": ["抖音和快手哪个好"],
      "count": 1,
      "priority": "high",
      "suggested_format": "对比表 + 差异化结论"
    },
    {
      "intent": "how-to",
      "queries": ["怎么用剪映剪辑视频","抖音直播带货怎么做"],
      "count": 2,
      "priority": "medium",
      "suggested_format": "分步教程 + FAQ"
    },
    {
      "intent": "transactional",
      "queries": ["TikTok 怎么下载"],
      "count": 1,
      "priority": "medium",
      "suggested_format": "下载引导页 + 应用商店链接"
    }
  ]
}
```
输出摘要：共 6 条查询 → 4 个意图组，high 优先级 2 组（entity / comparison），建议优先建设品牌百科与对比内容。
