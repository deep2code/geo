# Skill: geo-project-setup

## Description
GEO 项目初始化技能。通过品牌智能补全与市场列表接口，引导用户完成一个新的 GEO（生成式引擎优化）项目配置：确定品牌领域、品牌名、竞品列表、目标查询与目标市场，最终产出一份可直接复用的项目配置 JSON 文件，作为后续审计、聚类、改写等技能的统一输入。

## Trigger
当用户表达以下意图时触发：初始化 GEO 项目、新建品牌项目、配置品牌画像、设置 GEO 项目、project setup、品牌初始化、帮我建一个 GEO 项目、品牌补全。

## Prerequisites
- GEO 服务正在运行，地址由环境变量 `GEO_PORT` 决定（默认 8080）。设 `GEO_BASE=http://localhost:${GEO_PORT:-8080}`。
- 品牌审计引擎已初始化（autocomplete 接口需要品牌引擎；markets 接口不依赖引擎，零配置可调用）。
- 已知品牌名称（中文或英文均可）。

## Workflow

### Step 1: 收集品牌基础信息
向用户询问以下基础信息（缺失项可留空，由后续补全接口填充）：
- 品牌名称（必填）
- 品牌官网域名（可选）
- 所属行业 / 品类（可选）
- 主要产品或服务（可选）
- 已知竞品列表（可选）
- 目标查询 / 提示词（可选，用于审计）

### Step 2: 调用市场列表接口
调用 `GET $GEO_BASE/api/v1/brand/markets` 获取全部支持的市场，让用户选择目标市场与查询语言。

返回结构示例：
```json
{
  "markets": [
    {"code": "cn", "name": "中国大陆", "languages": ["zh"], "engines": ["doubao","kimi","wenxin","yuanbao","qwen","xunfei","glm","xiaomi"]},
    {"code": "us", "name": "美国", "languages": ["en"], "engines": ["chatgpt","perplexity","gemini","claude"]}
  ],
  "count": 7
}
```

### Step 3: 调用品牌智能补全接口
调用 `POST $GEO_BASE/api/v1/brand/autocomplete`，请求体：
```json
{"brand_name": "用户提供的品牌名"}
```
返回 `AutocompleteCandidate` 候选画像（name/domain/aliases/industry/category/products/company/competitors/prompts/summary）。将其与用户在 Step 1 提供的信息合并：用户显式提供的字段优先，缺失字段采用补全结果。若补全返回的 `prompts` 为空，需向用户确认或生成默认目标查询。

### Step 4: 组装 BrandProfile 并落盘
将合并后的信息组装为 `brand.BrandProfile` 结构的项目配置 JSON，写入项目目录（建议路径 `<项目根>/geo-project.json` 或用户指定路径）。配置应包含：
- `name`、`aliases`、`domain`、`industry`、`category`、`products`
- `competitors`（每项含 name/aliases/domain/company）
- `prompts`（目标查询列表）
- `target_engines`（根据所选市场推导）
- `market`、`language`（来自 Step 2）

## Output
一份项目配置 JSON 文件（`brand.BrandProfile` 结构），后续所有技能均以该文件作为输入。同时在对话中输出配置摘要：品牌名、域名、行业、竞品数、目标查询数、目标市场与引擎。

## Example
用户：“帮我新建一个 GEO 项目，品牌是字节跳动，主要看海外市场。”

执行：
1. 询问补充信息（已知竞品可选）。
2. `GET $GEO_BASE/api/v1/brand/markets` → 用户选择 `us`（美国，语言 en，引擎 chatgpt/perplexity/gemini/claude）。
3. `POST $GEO_BASE/api/v1/brand/autocomplete` body `{"brand_name":"字节跳动"}` → 返回候选画像含 domain=bytedance.com、competitors=[腾讯,快手]、prompts=["什么是字节跳动","字节跳动旗下有哪些产品"] 等。
4. 合并写入 `geo-project.json`：
```json
{
  "name": "字节跳动",
  "aliases": ["ByteDance"],
  "domain": "bytedance.com",
  "industry": "互联网/内容科技",
  "category": "短视频/内容平台",
  "products": ["抖音","TikTok","今日头条","剪映"],
  "competitors": [{"name":"腾讯","domain":"tencent.com"},{"name":"快手","domain":"kuaishou.com"}],
  "prompts": ["什么是字节跳动","字节跳动旗下有哪些产品","ByteDance 和 TikTok 的关系"],
  "target_engines": ["chatgpt","perplexity","gemini","claude"],
  "market": "us",
  "language": "en"
}
```
输出摘要：品牌「字节跳动」｜域名 bytedance.com ｜竞品 2 个 ｜目标查询 3 条 ｜市场 us/en ｜已写入 geo-project.json。
