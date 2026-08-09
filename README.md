# GEO — 生成式引擎优化系统

> 让你的内容更容易被 AI 搜索引擎（ChatGPT、Perplexity、Gemini、Claude、通义千问、智谱 GLM 等）引用。

基于 Princeton GEO 论文 (KDD 2024) 的 9 种优化策略，融合 AutoGEO (ICLR 2026) 的 GEO/GEU 双评分体系，面向中文市场深度本地化。

## 📖 文档

| 文档 | 说明 |
|---|---|
| [入门指南](docs/getting-started.md) | GEO 是什么、为什么需要、5 分钟上手、内容优化实战、品牌审计指南 |
| [架构文档](docs/architecture.md) | 系统全景、数据流、数据库选型、10+ Mermaid 图表 |

## 核心能力

```
┌─────────────────────────────────────────────────────────────────┐
│                        GEO 系统                                 │
│                                                                 │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────────┐ │
│  │  内容优化    │  │  品牌可见度   │  │  离线工商数据库         │ │
│  │  Optimize    │  │  Brand Audit │  │  1000 万+ 注册企业      │ │
│  │  Score       │  │  Scheduler   │  │  FTS5 全文检索          │ │
│  │  Analyze     │  │  Monitor     │  │  China-Check MCP       │ │
│  └─────────────┘  └──────────────┘  └────────────────────────┘ │
│         │                │                    │                 │
│         └────────────────┼────────────────────┘                 │
│                          ▼                                      │
│              ┌──────────────────────┐                           │
│              │   REST API + Web UI  │                           │
│              │   MCP Server         │                           │
│              │   CLI 工具链          │                           │
│              └──────────────────────┘                           │
└─────────────────────────────────────────────────────────────────┘
```

### 内容优化（pkg/geo）

- **9 法 GEO 策略**：引用来源 (+27%)、统计数据 (+33%)、权威语气、引用语 (+41%)、流畅度 (+29%)、独特词汇、技术术语、结构化、结论前置
- **0-100 GEO 评分**：6 维度评分（可引用性、结构、流畅度、关键词、独特性、技术性）
- **领域自适应**：严肃话题靠引用、软性话题靠语气、知识话题靠数据
- **引擎偏好**：13 个引擎预设权重（ChatGPT / Perplexity / Gemini / Claude / 通义千问 / 智谱 GLM / DeepSeek / Kimi / 文心 / 豆包 / 小米 / 讯飞 / 元宝）

### 品牌可见度审计（internal/brand）

- **BVS 0-100 加权健康评分**：内容质量 23% + 技术 SEO 22% + 站内 SEO 20% + Schema 10% + 性能 10% + AI 就绪 10% + 图像 5%
- **多引擎对比**：同时查询多个 AI 搜索引擎，对比品牌引用情况
- **5 类模型分歧告警**：竞品涌现、品牌消失、声量下降、位置下滑、模型分歧
- **定时扫描**：cron 表达式定时审计 + webhook 告警
- **时间序列追踪**：审计历史趋势图，BVS 评分变化分析
- **竞品引用差距热力图**：引擎 × 查询词矩阵
- **PDF 报告导出**：自包含 HTML 报告

### 离线工商数据库

- **1000 万+ 条**中国大陆工商注册数据（1978-2019，来源：guichong/-/tree/json）
- **FTS5 全文索引**：按品牌/公司/法人/地址模糊搜索 Top 20 < 50ms
- **China-Check MCP 集成**：免鉴权免费查询国家企业信用信息公示系统实时数据
- **数据优先级**：工商实时 > 离线历史 > SinoFacts 知识库 > 联网搜索 > LLM 自身知识

### 按功能选型数据库

| 模块 | 数据特征 | 零依赖（默认） | 高性能（可选） | 环境变量 |
|---|---|---|---|---|
| 离线工商库 | 千万级行 + 全文检索 + 只读 | SQLite (FTS5) | DuckDB | `GEO_OFFLINE_DB_TYPE` |
| 审计历史库 | 时序写入 + JSON 列 | SQLite | MySQL | `GEO_HISTORY_DB_TYPE` |
| China-Check 缓存 | K/V + TTL + 高频读 | JSONL 文件 | Redis | `GEO_CHINACHECK_CACHE_TYPE` |

所有模块默认使用零依赖后端（纯 Go / 本地文件），**开箱即用无需安装任何外部数据库**。

### P0/P1 扩展功能

| 功能 | 说明 | CLI 命令 |
|---|---|---|
| Top Source 归因 | 识别 LLM 引用的权威域名 | `geo topsource` |
| 行业类型识别 | SaaS/本地服务/电商/媒体/代理五类 | `geo vertical detect` |
| Local SEO/GMB | NAP 一致性、商家资料审计 | `geo localseo` |
| AutoGEO 规则重写 | 自动发现引擎偏好 + GEU 检查 | `geo autorewrite` |
| 8 维 AI 就绪度 CI 闸门 | robots.txt/llms.txt/Schema/sitemap 等 | `geo readiness` |
| 外部信号分析 | 社媒情感、KOL 情报 | `geo externalsignals` |

## 快速开始

### 安装

```bash
# 方式一：Go install
go install ./cmd/geo

# 方式二：Makefile 编译
make build    # 产物在 bin/geo

# 方式三：Docker
make docker-up    # http://localhost:8080
```

### 基本用法

```bash
# 优化内容（需配置 LLM Key）
geo optimize -f content.md --engine chatgpt --engine perplexity

# 评分（无需 LLM Key）
echo "你的内容" | geo score

# 分析 GEO 信号
geo analyze -f content.md

# 列出所有策略
geo strategies

# 启动 Web 服务
geo serve -p 8080
# 或使用脚本（自动编译 + 杀旧进程 + 后台启动）
bash scripts/run.sh
```

### 品牌审计

```bash
# 初始化离线工商库
geo brand-db init

# 导入工商数据（支持 JSON 数组 / JSONL / 对象包裹数组）
geo brand-db import -d ./Enterprise-Registration-Data

# 搜索企业
geo brand-db search "腾讯"

# 品牌审计
geo brand-audit --brand "腾讯" --domain tencent.com \
  --prompts "最好的互联网公司" --engines chatgpt,perplexity

# 预热 China-Check 缓存
geo brand-cache warm --queries "腾讯,阿里巴巴,字节跳动"
```

### 环境变量配置

```bash
cp .env.example .env

# 核心 LLM 配置
GEO_LLM_KEY=sk-xxx
GEO_LLM_BASE=https://api.openai.com
GEO_LLM_MODEL=gpt-4o-mini

# 国内大模型（任选其一）
# 通义千问: GEO_LLM_BASE=https://dashscope.aliyuncs.com/compatible-mode  GEO_LLM_MODEL=qwen-plus
# 智谱GLM:  GEO_LLM_BASE=https://open.bigmodel.cn/api/paas/v4           GEO_LLM_MODEL=glm-4-flash
# DeepSeek: GEO_LLM_BASE=https://api.deepseek.com                        GEO_LLM_MODEL=deepseek-chat
```

各引擎独立 API Key 环境变量：`GEO_OPENAI_KEY` / `GEO_PERPLEXITY_KEY` / `GEO_GEMINI_KEY` / `GEO_CLAUDE_KEY` / `GEO_QWEN_KEY` / `GEO_GLM_KEY` / `GEO_DEEPSEEK_KEY` / `GEO_KIMI_KEY` / `GEO_WENXIN_KEY` / `GEO_DOUBAO_KEY` / `GEO_XIAOMI_KEY` / `GEO_XUNFEI_KEY` / `GEO_YUANBAO_KEY`

## CLI 命令一览

```
geo optimize        优化内容，提升 AI 搜索引擎可见度
geo score           评估内容的 GEO 评分（0-100）
geo analyze         分析内容的 GEO 信号
geo strategies      列出全部可用 GEO 优化策略
geo serve           启动 REST API 服务
geo brand-audit     品牌可见度审计
geo brand-db        离线工商数据库管理（init/import/search/stats/clear）
geo brand-cache     China-Check 缓存管理（stats/clear/compact/warm）
geo mcp-server      启动 MCP Server 模式
geo readiness       8 维 AI 就绪度 CI 闸门
geo topsource       Top Source 归因分析
geo vertical        行业类型自动识别
geo localseo        Local SEO / GMB 审计
geo externalsignals 外部信号分析（社媒情感 + KOL）
geo autorewrite     AutoGEO 规则提取与重写
geo discover        关键词→公司推断→自动 GEO 报告
```

## API 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/health` | 健康检查 |
| GET | `/api/v1/strategies` | 列出可用策略 |
| POST | `/api/v1/analyze` | 分析内容信号 |
| POST | `/api/v1/score` | GEO 评分 |
| POST | `/api/v1/optimize` | 优化内容 |
| GET | `/api/v1/brand/autocomplete` | 品牌智能补全 |
| POST | `/api/v1/brand/audit` | 品牌可见度审计 |
| GET | `/api/v1/brand/history` | 审计历史趋势 |
| POST | `/api/v1/brand/scheduler/trigger` | 手动触发定时审计 |
| GET | `/api/v1/brand/offlinedb/search` | 离线工商库搜索 |
| GET | `/api/v1/brand/offlinedb/stats` | 离线工商库统计 |
| POST | `/api/v1/brand/topsource/analyze` | Top Source 归因 |
| POST | `/api/v1/brand/vertical/detect` | 行业类型识别 |
| POST | `/api/v1/brand/localseo/audit` | Local SEO 审计 |
| POST | `/api/v1/autorewriter/rewrite` | AutoGEO 规则重写 |
| GET | `/api/v1/brand/readiness/ci-gate` | AI 就绪度 CI 闸门 |
| POST | `/api/v1/brand/discover` | 关键词搜索匹配公司 |
| POST | `/api/v1/brand/discover/report` | 选中公司后生成完整 GEO 报告 |

## MCP Server

系统可作为 MCP Server 被 Claude / Cursor / TraeCode 等 MCP 客户端调用：

```bash
geo mcp-server
```

暴露的核心工具：`brand_audit` / `optimize` / `search_companies` / `chinacheck` / `readiness`

## 技术架构

详细架构图和数据流请见 [docs/architecture.md](docs/architecture.md)。

### 项目结构

```
my-geo/
├── cmd/geo/                 # CLI 入口 + 子命令
├── pkg/geo/                 # 公开 API（Optimize/Score/Analyze）
├── internal/
│   ├── adapter/             # 13 个 AI 引擎适配器
│   ├── analyzer/            # GEO 信号分析器
│   ├── brand/               # 品牌可见度审计
│   │   ├── chinacheck/      # China-Check MCP 客户端 + 缓存
│   │   ├── history/         # 审计历史时序存储（SQLite/MySQL）
│   │   ├── knowledge/       # SinoFacts 知识库
│   │   ├── offlinedb/       # 离线工商库（SQLite/DuckDB）
│   │   ├── scheduler/       # 定时审计调度器
│   │   ├── readiness/       # 8 维 AI 就绪度
│   │   ├── topsource/       # Top Source 归因
│   │   ├── vertical/        # 行业类型识别
│   │   ├── localseo/        # Local SEO
│   │   ├── externalsignals/  # 外部信号
│   │   ├── social/          # 社媒情感
│   │   ├── kol/             # KOL 情报
│   │   ├── report/          # PDF 报告导出
│   │   └── mcpserver/       # MCP Server
│   ├── config/              # 引擎预设 + 环境变量
│   ├── dbprovider/          # 数据库后端工厂
│   ├── llm/                 # LLM 管理器
│   ├── models/              # 核心数据模型
│   ├── optimizer/           # 优化器 + 9 法策略 + AutoGEO
│   ├── scorer/              # GEO 评分器
│   └── server/              # REST API + Web UI（go:embed）
├── scripts/                 # 运行/部署脚本
├── Dockerfile               # 多阶段构建
├── Makefile                 # 构建/测试/部署
└── .env.example             # 环境变量模板
```

### 关键设计决策

- **零依赖部署**：Web 前端通过 `go:embed` 内嵌为单文件，SQLite 使用纯 Go 驱动（`modernc.org/sqlite`），编译后单个二进制即可运行
- **按功能选型数据库**：不同模块按数据特征选择最合适的后端，通过环境变量切换，默认全部零依赖
- **数据优先级策略**：工商实时 > 离线历史 > 知识库 > 联网搜索 > LLM 自身知识
- **接口抽象**：所有数据库模块通过 `Store` / `OfflineStore` / `CacheStore` 接口解耦，上游零改动切换后端

## CI/CD

项目内置两个 GitHub Actions 工作流：

### CI（自动检查）
- **触发**：每次 push 到 main / 每次 PR
- **内容**：`go vet` + `go build` + `go test -race -cover` + 6 平台交叉编译验证

### Release（打包发布）
- **触发**：推送 `v*` 标签，或手动在 Actions 页面触发
- **内容**：测试通过后，交叉编译 6 平台二进制，打包 tar.gz/zip + SHA256 校验，自动创建 GitHub Release

```bash
# 发布新版本
git tag v0.1.0
git push origin v0.1.0

# 或手动触发（GitHub Actions 页面 → Release → Run workflow）
```

支持的平台：

| OS | amd64 | arm64 |
|---|---|---|
| Linux | tar.gz | tar.gz |
| macOS | tar.gz | tar.gz (Apple Silicon) |
| Windows | zip | zip |

## 学术参考

- **Princeton GEO** (KDD 2024) — 9 种 GEO 优化策略与效果系数
- **AutoGEO** (CMU/ICLR 2026) — GEO/GEU 双评分体系与规则提取
- **Claude SEO** — BVS 0-100 加权健康评分维度参考

## 许可证

MIT License
