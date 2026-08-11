# 🏗 GEO 系统架构文档

> 本文档以 Mermaid 图表为主、文字为辅，全面解析 GEO 系统的架构设计。

---

## 目录

```mermaid
mindmap
  root((架构文档))
    系统全景
      四层架构
      用户入口
    内容优化
      评分6维
      9法策略
      领域自适应
    品牌可见度
      审计流程
      BVS加权7维
      5类告警
      行业识别5类
      TopSource归因
    关键词发现
      Discover工作流
      Profile构建
    数据库选型
      三层架构
      决策树
    AI引擎适配层
      13引擎星型
      敏感度权重
    功能模块
      AutoGEO双评分
      8维AI就绪度
      CIGate闸门
    数据流
      工商导入
      调度器闭环
    集成协议
      MCP时序
      API接口分层
    部署运维
      三种部署形态
      环境变量全景
```

---

## 1. 系统全景架构

```mermaid
graph TB
    subgraph 用户入口["👤 用户入口层"]
        CLI["💻 CLI 工具链<br/>geo optimize / score / audit / discover"]
        WEB["🌐 Web SPA<br/>Vite+React+TS<br/>10+ 页面 i18n"]
        API["🔌 REST API<br/>/api/v1/* (60+ 端点)"]
        MCP["🤖 MCP Server<br/>JSON-RPC 2.0 over stdio"]
    end

    subgraph 核心引擎["⚙️ 核心引擎层"]
        GEO["📝 GEO Engine<br/>pkg/geo<br/>（内容优化/评分/分析）"]
        BRAND["🏢 Brand Engine<br/>internal/brand<br/>（审计/调度/告警）"]
        DISCOVER["🔍 Discover Engine<br/>internal/brand/discover<br/>（关键词→公司→报告）"]
        CRAW["🕷 官网爬虫<br/>自动补全品牌画像"]
    end

    subgraph AI引擎适配["🧠 13 AI 引擎适配层"]
        GPT["🔵 ChatGPT"]
        PPX["🟡 Perplexity"]
        GEM["🟢 Gemini"]
        CLD["🟠 Claude"]
        QWN["🔴 通义千问"]
        GLM["🟣 智谱GLM"]
        DSP["DeepSeek 等 7 个"]
    end

    subgraph 数据存储["💾 按功能选型数据层"]
        OFFDB["📦 离线工商库<br/>SQLite / DuckDB<br/>1000万+ FTS5"]
        HISDB["📜 审计历史库<br/>SQLite / MySQL<br/>时序+JSON快照"]
        CACHE["⚡ China-Check 缓存<br/>JSONL / Redis<br/>K/V + TTL"]
        KB["📚 SinoFacts 知识库<br/>JSONL 只读 (CC BY 4.0)"]
        VEC["📐 向量检索<br/>TF-IDF + Embedding"]
    end

    subgraph 外部服务["🌐 外部服务"]
        CC["✅ China-Check MCP<br/>GSXT/SAMR 官方实时查询（免费）"]
        LLM["🧩 大模型 API<br/>统一 OpenAI 兼容协议"]
        DOM["📊 国内信号源<br/>百度/微信/知乎/抖音等"]
    end

    CLI --> GEO & BRAND & DISCOVER
    WEB --> API
    MCP --> BRAND
    API --> GEO & BRAND & DISCOVER

    GEO --> LLM
    DISCOVER --> BRAND
    BRAND --> AI引擎适配
    BRAND --> OFFDB & HISDB & CACHE & KB
    BRAND --> CC

    style 用户入口 fill:#e0f2fe,stroke:#0369a1
    style 核心引擎 fill:#fef9c3,stroke:#a16207
    style 数据存储 fill:#dcfce7,stroke:#15803d
    style AI引擎适配 fill:#f5d0fe,stroke:#a21caf
```

### 四种用户入口对比

```mermaid
graph LR
    subgraph 零配置可用["无需 API Key ✅"]
        geo_score["geo score<br/>评分"]
        geo_discover["geo discover<br/>关键词搜索"]
        geo_readiness["geo readiness<br/>就绪度检查"]
        geo_vertical["geo vertical<br/>行业识别"]
        geo_serve["geo serve<br/>Web UI"]
    end

    subgraph 需要APIKey["需引擎 Key 🔑"]
        geo_optimize["geo optimize<br/>内容改写"]
        geo_audit["geo brand-audit<br/>品牌审计"]
        geo_topsource["geo topsource<br/>归因分析"]
        geo_autorewrite["geo autorewrite<br/>规则重写"]
        geo_mcp["geo mcp-server<br/>Agent 工具"]
    end

    style 零配置可用 fill:#bbf7d0,stroke:#16a34a
    style 需要APIKey fill:#fed7aa,stroke:#ea580c
```

---

## 2. 内容优化引擎

### 优化流程

```mermaid
flowchart LR
    A["📄 输入内容<br/>(Markdown/纯文本)"] --> B["🔬 Analyzer<br/>信号提取<br/>引用/结构/关键词"]
    B --> C["🎯 Scorer<br/>6维 0-100 评分"]
    C --> D{配置 LLM Key?}
    D -->|是| E["⚡ Optimizer<br/>9法策略组合应用"]
    D -->|否| F["📐 规则化预处理<br/>零依赖也可用"]
    E --> G["🤖 LLM 改写<br/>(GPT/DeepSeek/GLM)"]
    F --> G
    G --> H["✅ 输出<br/>优化内容 + 评分报告 + PWC增益"]

    style D fill:#fef9c3,stroke:#a16207
    style G fill:#bbf7d0,stroke:#16a34a
```

### GEO 6 维评分饼图

```mermaid
pie title GEO 6 维评分权重
    "可引用性 (30%)" : 30
    "结构 (20%)" : 20
    "流畅度 (15%)" : 15
    "关键词 (15%)" : 15
    "独特性 (10%)" : 10
    "技术性 (10%)" : 10
```

### 9 法策略 PWC 增益（Princeton KDD 2024 实测）

```mermaid
%%{init: {"theme": "default"}}%%
xychart-beta
    title "GEO 9 法策略理论 PWC 增益 (%)"
    x-axis ["引用语", "统计数据", "流畅度", "引用来源", "权威语气", "结论前置", "结构化", "技术术语", "独特词汇"]
    y-axis "PWC %" 0 --> 50
    bar [41, 33, 29, 27, 25, 24, 22, 20, 18]
```

### 三大领域自适应策略

```mermaid
graph TB
    subgraph Serious["严肃领域<br/>(法律/医疗/政府)"]
        S1["+60% 引用来源权重"]
        S2["+40% 统计数据权重"]
        S3["+30% 权威语气权重"]
    end

    subgraph Soft["软性领域<br/>(时尚/娱乐/生活)"]
        F1["+60% 流畅度权重"]
        F2["+50% 独特词汇权重"]
        F3["+40% 结论前置权重"]
    end

    subgraph Knowledge["知识领域<br/>(历史/科技/事实)"]
        K1["+60% 引用语权重"]
        K2["+50% 技术术语权重"]
        K3["+50% 统计数据权重"]
    end

    style Serious fill:#fee2e2,stroke:#dc2626
    style Soft fill:#e0f2fe,stroke:#0284c7
    style Knowledge fill:#dcfce7,stroke:#16a34a
```

---

## 3. 品牌可见度引擎

### 完整审计流程

```mermaid
flowchart TB
    IN["🧾 输入<br/>品牌名/竞品/查询词"] --> AC["🔮 Autocomplete<br/>智能补全"]

    subgraph 数据优先级链["数据优先级链（高→低）"]
        direction LR
        P1["① 工商实时<br/>ChinaCheck"] --> P2["② 离线历史<br/>1000万+数据"]
        P2 --> P3["③ SinoFacts<br/>知识库"]
        P3 --> P4["④ 联网搜索"]
        P4 --> P5["⑤ LLM自身"]
    end

    AC --> P1
    P5 --> BP["🎨 BrandProfile<br/>品牌画像"]

    BP --> MON["🔍 Monitor.Run<br/>并发查询 N 个引擎"]
    MON --> SCR["📊 Scorer<br/>BVS 0-100 加权评分"]
    SCR --> REP["📝 Reporter<br/>运营行动建议"]
    REP --> SAVE["💾 审计历史库持久化"]

    SAVE --> TREND["📈 趋势分析"]
    SAVE --> DIV["⚠️ 5类异常检测"]

    style 数据优先级链 fill:#fce7f3,stroke:#be185d
```

### BVS 7 维加权评分（参考 Claude SEO）

```mermaid
pie title BVS 加权评分（0-100）
    "内容质量 23%" : 23
    "技术 SEO 22%" : 22
    "站内 SEO 20%" : 20
    "Schema 10%" : 10
    "页面性能 10%" : 10
    "AI 就绪 10%" : 10
    "图像优化 5%" : 5
```

### 5 类模型分歧告警决策流

```mermaid
flowchart TD
    CURR["✅ 当前审计"] --> CMP{"⇄ 对比历史记录"}
    PREV["📜 上一次审计"] --> CMP

    CMP --> A1["🚨 竞品涌现<br/>新竞品进入Top-3"]
    CMP --> A2["🚨 品牌消失<br/>从引用中消失"]
    CMP --> A3["🚨 声量下降<br/>引用率下降>20%"]
    CMP --> A4["🚨 位置下滑<br/>引用排名下降>3"]
    CMP --> A5["🚨 模型分歧<br/>引擎结论冲突"]

    A1 & A2 & A3 & A4 & A5 --> WEBHOOK["📨 Webhook 通知<br/>Slack/飞书/钉钉"]

    style WEBHOOK fill:#fecaca,stroke:#dc2626
```

### 行业类型识别（5 类垂直）

```mermaid
graph LR
    subgraph V1["🖥 SaaS 软件"]
        V1d["检测词：SaaS/平台/云/API/软件<br/>重点：Schema/定价页/API文档"]
    end
    subgraph V2["🏠 本地服务"]
        V2d["检测词：门店/上门/本地/同城<br/>重点：NAP一致性/GMB"]
    end
    subgraph V3["🛒 电商"]
        V3d["检测词：购物/商城/产品/价格<br/>重点：Product Schema/评价"]
    end
    subgraph V4["📰 媒体出版"]
        V4d["检测词：新闻/文章/媒体/出版<br/>重点：E-E-A-T/Article Schema"]
    end
    subgraph V5["👥 代理咨询"]
        V5d["检测词：咨询/代理/服务/顾问<br/>重点：案例/客户/权威信号"]
    end

    style V1 fill:#dbeafe,stroke:#2563eb
    style V2 fill:#dcfce7,stroke:#16a34a
    style V3 fill:#fef3c7,stroke:#d97706
    style V4 fill:#f3e8ff,stroke:#7c3aed
    style V5 fill:#fee2e2,stroke:#dc2626
```

### Top Source 归因流程图

```mermaid
flowchart LR
    A["🧩 品牌审计报告<br/>(EngineStats + Citations)"] --> B["域名解析<br/>URL → TLD"]
    B --> C["引用频率统计<br/>各引擎×查询词"]
    C --> D["Top 10 权威域名<br/>按引用次数排序"]
    D --> E["领域分类<br/>媒体/KOL/行业/竞品"]
    E --> F["📋 建议清单<br/>重点投放/外链合作/PR资源"]

    style D fill:#fef9c3,stroke:#a16207
```

### 竞品引用差距热力图矩阵（引擎 × 查询词）

```mermaid
graph LR
    subgraph Legend["热力图颜色图例"]
        direction TB
        L1["🟩 绿色 = 我方占优<br/>(品牌引用率 > 竞品)"]
        L2["🟨 黄色 = 势均力敌<br/>(差距 < 10%)"]
        L3["🟥 红色 = 竞品占优<br/>(引用率落后明显)"]
    end

    subgraph Matrix["3引擎 × 4查询词 热力矩阵"]
        direction TB
        R1["查询词 →<br/>引擎 ↓"]
        R1 --> M1["&nbsp;&nbsp;&nbsp;最好的CRM&nbsp;&nbsp;|&nbsp;&nbsp;推荐CRM&nbsp;&nbsp;|&nbsp;&nbsp;CRM对比&nbsp;&nbsp;|&nbsp;&nbsp;企业CRM&nbsp;&nbsp;"]
        M1 --> M2["ChatGPT&nbsp;&nbsp;&nbsp;🟩&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟨&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟥&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟩"]
        M2 --> M3["Perplexity&nbsp;&nbsp;🟨&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟥&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟥&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟨"]
        M3 --> M4["智谱GLM&nbsp;&nbsp;&nbsp;🟩&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟩&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟨&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;🟩"]
    end

    subgraph Insight["📊 洞察结论"]
        direction TB
        I1["① ChatGPT/GLM 表现较好"]
        I2["② Perplexity 明显需加强"]
        I3["③ 「CRM对比」全链路落后<br/>→ 优先做对比页 SEO+GEO"]
        I4["④ 企业级查询尚可"]
    end

    style Legend fill:#f1f5f9,stroke:#475569
    style Matrix fill:#fff,stroke:#7c3aed
    style Insight fill:#fef9c3,stroke:#a16207
```

### 审计报告内部结构

```mermaid
graph TB
    subgraph Report["📄 BrandVisibilityReport"]
        direction TB
        HEADER["🔖 头部元信息<br/>品牌名 / 审计时间 / 引擎数 / 查询词数"]

        subgraph Profile["🎨 BrandProfile（输入）"]
            P1["name / aliases / domain"]
            P2["products / category / industry"]
            P3["prompts[] / competitors[]"]
        end

        subgraph PerEngine["🔬 Per-Engine 明细（N 个引擎）"]
            direction TB
            PE1["engine_type / model"]
            PE2["引用率 / 排名分布 / 位置均值"]
            PE3["Citations[] × 每个查询词<br/>{query, rank, quoted, snippet, source_url}"]
        end

        subgraph BVS["📊 BVS 综合评分 0-100"]
            B1["7 维分量 + 权重加总"]
            B2["等级 A/B/C/D/F"]
            B3["theoreticalPWCBoost%"]
        end

        subgraph Diff["⚠️ 异常检测（5 类）"]
            D1["competitor_emerged"]
            D2["brand_disappeared"]
            D3["share_drop_gt_20"]
            D4["rank_drop_gt_3"]
            D5["model_divergence"]
        end

        subgraph Actions["💡 运营建议"]
            A1["优先修复项(按严重级)"]
            A2["Top Source 投放建议"]
            A3["内容优化策略组合"]
        end

        HEADER --> Profile --> PerEngine --> BVS --> Diff --> Actions
    end

    style Report fill:#fef3c7,stroke:#d97706
```

### Autocomplete 多来源融合流程

```mermaid
flowchart LR
    INPUT(["🧾 用户输入<br/>前缀关键词"]) --> Q["📋 Query 标准化<br/>trim / lower / 同义归一"]

    Q --> P1["① China-Check 实时<br/>SAMR 工商实时核验<br/>优先级最高"]
    Q --> P2["② 离线工商库<br/>FTS5 MATCH 模糊检索<br/>TopN=8"]
    Q --> P3["③ SinoFacts 知识库<br/>JSONL 前缀/包含匹配"]
    Q --> P4["④ LLM 联想补全<br/>（仅当其余为空+有Key）"]

    P1 --> R1["Result[]<br/>tag: CC-实时"]
    P2 --> R2["Result[]<br/>tag: 离线-历史"]
    P3 --> R3["Result[]<br/>tag: 知识库"]
    P4 --> R4["Result[]<br/>tag: LLM-联想"]

    R1 & R2 & R3 & R4 --> MERGE["🔗 mergeOfflineIntoBrand<br/>按统一信用代码去重<br/>空字段用低优先级兜底"]
    MERGE --> SORT["📊 稳定性排序<br/>实时 > 离线 > 知识库 > LLM"]
    SORT --> TAG["🏷 多来源 Tag 标记<br/>展示来源徽章"]
    TAG --> OUT(["✅ 前端 Autocomplete 下拉<br/>3 列：名称 / 法人 / 行业 + 来源标签"])

    style P1 fill:#fee2e2,stroke:#dc2626
    style P2 fill:#dcfce7,stroke:#16a34a
    style P3 fill:#e0f2fe,stroke:#0369a1
    style P4 fill:#f3e8ff,stroke:#7c3aed
```

### UI 响应捕获（双模式：API + Playwright UI）

```mermaid
flowchart TB
    START(["🔍 引擎查询触发"]) --> SW{"📌 UI 验证模式?"}

    SW -->|"❌ 默认 API 模式"| API["🔌 HTTP API<br/>走 OpenAI 兼容协议<br/>快 / 稳定 / 低成本"]
    API --> PARSEA["📝 JSON 解析<br/>citations[] 字段"]

    SW -->|"✅ Playwright UI 模式"| PW["🎭 headless Chromium<br/>模拟真实用户访问<br/>UI 验证码/人工兜底"]
    PW --> SNAP["📸 UI 快照<br/>DOM + 截图 + 坐标"]
    SNAP --> PARSEB["🧠 OCR + DOM 抽取<br/>还原引用片段 + 来源链接"]

    PARSEA --> DIFF
    PARSEB --> DIFF{"⇄ 双模式结果比对?"}

    DIFF -->|"一致"| OK["✅ 结果可信<br/>正常入库"]
    DIFF -->|"分歧"| ALERT["🚨 UI vs API 分歧告警<br/>前端高亮差异"]
    ALERT --> FLAG["🏷 ui_snapshot JSON 列<br/>审计历史保存快照"]

    style API fill:#dcfce7,stroke:#16a34a
    style PW fill:#fef3c7,stroke:#d97706
    style ALERT fill:#fee2e2,stroke:#dc2626
```

---

## 4. 关键词发现引擎（Discover）

### 完整工作流

```mermaid
flowchart TD
    START([👤 输入关键词<br/>「短视频」「云计算」…]) --> DS["🔎 双重搜索<br/>📦 工商库 FTS5 + 📚 知识库"]
    DS --> N{"匹配数量?"}
    N -- "0" --> EMPTY["❌ 无结果<br/>提示导入数据"]
    N -- "1" --> AUTO["✅ 自动选中"]
    N -- "2-10" --> LIST["📋 候选列表<br/>用户点击选择"]

    AUTO --> PROF
    LIST --> PROF["🎨 BuildProfile<br/>自动构建品牌画像"]

    PROF --> VD["🧠 vertical.Detect<br/>行业类型识别"]
    PROF --> BR["🏢 品牌可见度审计<br/>BVS 0-100"]
    PROF --> RR["🤖 AI 就绪度检查<br/>8 维评分"]

    VD & BR & RR --> SUG["💡 综合优化建议"]
    SUG --> END([📄 完整 GEO 报告])

    style START fill:#7c3aed,color:#fff
    style END fill:#16a34a,color:#fff
    style N fill:#fef9c3,stroke:#a16207
```

### 品牌画像自动构建逻辑

```mermaid
graph TB
    CAND["🤝 Candidate 输入<br/>{name, domain, industry, scope...}"] --> NAME["📛 品牌名<br/>公司简称提取"]
    CAND --> ALIAS["🔖 Aliases<br/>知识库别名 + 简称"]
    CAND --> DOMAIN["🌐 Domain<br/>如存在直接复用"]
    CAND --> IND["🏭 行业推断<br/>经营范围 + 知识库"]
    CAND --> PROD["🛒 Products<br/>知识库提取 + 范围推断"]

    NAME --> P["🧱 BrandProfile"]
    ALIAS --> P
    DOMAIN --> P
    IND --> P
    PROD --> P

    P --> PROMPT["💬 Prompts 自动生成<br/>7-9个高意图查询词"]
    PROMPT --> EXAMPLE1["最好的 XX"]
    PROMPT --> EXAMPLE2["XX推荐"]
    PROMPT --> EXAMPLE3["XX对比"]
    PROMPT --> EXAMPLE4["XX怎么样"]
    PROMPT --> EXAMPLE5["行业特定词"]

    style P fill:#bbf7d0,stroke:#16a34a
```

---

## 5. 数据库选型架构

### 三层抽象架构

```mermaid
graph TB
    subgraph 工厂层["🏭 dbprovider 工厂层"]
        PT["ParseType()<br/>env → 类型"]
        PF["PathFor()<br/>默认路径计算"]
        EN["EnabledFor()<br/>开关判断"]
        RS["Resolve()<br/>类型校验+回退"]
    end

    subgraph 接口层["📋 抽象接口层"]
        IO["OfflineStore<br/>Search/Import/Stats"]
        IH["Store<br/>Save/List/Latest/Stats"]
        IC["CacheStore<br/>Get/Set/Clear/Compact"]
    end

    subgraph 后端实现层["🔧 后端实现层"]
        subgraph 离线工商 ["OfflineCompanies"]
            OSQL["sqliteStore<br/>✅ 默认零依赖"]
            ODUC["duckStore<br/>🚀 列式高性能"]
        end
        subgraph 审计历史 ["AuditHistory"]
            HSQL["sqliteStore<br/>✅ 默认零依赖"]
            HMY["mysqlStore<br/>🚀 生产推荐"]
        end
        subgraph ChinaCheck ["查询缓存"]
            CJ["jsonlStore<br/>✅ 默认零依赖"]
            CRED["redisStore<br/>🚀 分布式"]
        end
    end

    工厂层 --> 接口层
    接口层 --> 后端实现层

    style 工厂层 fill:#dbeafe,stroke:#1d4ed8
    style 接口层 fill:#e9d5ff,stroke:#7c3aed
    style 后端实现层 fill:#dcfce7,stroke:#15803d
```

### 数据库选型决策树

```mermaid
flowchart TD
    START(["🚪 选择数据库后端"]) --> Q1{"📦 什么场景?"}

    Q1 -->|"千万级行+全文+只读"| Q2{"🚀 需要列式聚合?"}
    Q1 -->|"时序追加+JSON+中等"| Q3{"💼 生产高并发?"}
    Q1 -->|"K/V + TTL + 高频读"| Q4{"🌐 需要分布式?"}

    Q2 -->|"❌ 否"| OFFA["✅ SQLite + FTS5<br/>纯 Go 零依赖<br/>Top20<50ms"]
    Q2 -->|"✅ 是"| OFFB["🚀 DuckDB<br/>列式并行，更快"]

    Q3 -->|"❌ 否"| HISA["✅ SQLite<br/>单机足够"]
    Q3 -->|"✅ 是"| HISB["🚀 MySQL<br/>生产推荐"]

    Q4 -->|"❌ 否"| CAA["✅ JSONL 文件<br/>本地零依赖"]
    Q4 -->|"✅ 是"| CAB["🚀 Redis<br/>高并发推荐"]

    style OFFA fill:#bbf7d0,stroke:#16a34a
    style HISA fill:#bbf7d0,stroke:#16a34a
    style CAA fill:#bbf7d0,stroke:#16a34a
```

---

## 6. AI 引擎适配层

### 13 引擎星型结构（共用 OpenAI 兼容基类）

```mermaid
graph TD
    B["🔵 openai_compatible<br/>公共基类<br/>（发送请求 / 解析响应 / Token计算）"]

    B --> E1["ChatGPT"]
    B --> E2["Perplexity"]
    B --> E3["Gemini"]
    B --> E4["Claude"]
    B --> E5["通义千问"]
    B --> E6["智谱GLM"]
    B --> E7["DeepSeek"]
    B --> E8["Kimi"]
    B --> E9["文心一言"]
    B --> E10["豆包"]
    B --> E11["小米"]
    B --> E12["讯飞星火"]
    B --> E13["元宝/混元"]

    style B fill:#bfdbfe,stroke:#1d4ed8
```

### 各引擎对 GEO 信号敏感度权重对比

```mermaid
xychart-beta
    title "各引擎 GEO 信号敏感度（越高越重视）"
    x-axis ["引用来源", "统计数据", "权威语气", "结构化", "流畅度", "结论前置"]
    y-axis "权重 (0-1)" 0 --> 1.1
    line [1.00, 0.80, 0.85, 0.65, 0.50, 0.70]
    line [0.90, 0.85, 0.60, 0.75, 0.70, 0.80]
    line [0.80, 0.90, 0.90, 0.85, 0.95, 0.75]
```

> 蓝线=搜索增强型(Perplexity)，橙线=对话型(ChatGPT)，绿线=学术型(智谱GLM)

---

## 7. 核心高级功能

### AutoGEO 规则提取 + GEU 双评分

```mermaid
flowchart LR
    A["🧩 多条优化前后对比样本"] --> B["📐 规则提取器<br/>统计高频改写模式"]
    B --> RULES["📜 偏好规则库<br/>按引擎分类归档"]

    RULES --> C["✍️ 双模式重写<br/>API LLM / 本地微调"]
    C --> D["✅ 生成内容"]

    D --> GEO["📊 GEO 评分<br/>可见度指标"]
    D --> GEU["🎯 GEU 效用检查<br/>Precision/Recall/Clarity/Insight"]

    GEO --> PASS{"均不降级?"}
    GEU --> PASS
    PASS -->|"✅ 是"| OUT["👍 采纳新内容"]
    PASS -->|"❌ 否"| REJ["👎 回滚保留旧版"]

    style PASS fill:#fef9c3,stroke:#a16207
```

### 8 维 AI 就绪度检查

```mermaid
flowchart TB
    URL["🌐 网站 URL"] --> T1["🤖 robots.txt<br/>AI 爬虫屏蔽?(Critical)"]
    URL --> T2["📋 llms.txt<br/>LLM 摘要文件?(High)"]
    URL --> T3["🏷 结构化数据<br/>JSON-LD Schema?(High)"]
    URL --> T4["🗺 sitemap.xml<br/>站点地图?(Medium)"]
    URL --> T5["⏱ 页面性能<br/>TTFB < 600ms?(Medium)"]
    URL --> T6["📑 H1 标题<br/>唯一+层级清晰?(Medium)"]
    URL --> T7["❓ FAQ 质量<br/>FAQPage Schema?(Low)"]
    URL --> T8["👤 实体身份<br/>Organization Schema?(Low)"]

    T1 & T2 & T3 & T4 & T5 & T6 & T7 & T8 --> SC["📊 0-100 综合评分<br/>+ 等级(A-F)"]
    SC --> CI{"🚧 CI Gate?<br/>--ci-gate 80"}
    CI -->|"✅ ≥80"| OK["✅ 流水线通过"]
    CI -->|"❌ <80"| FAIL["❌ exit 1 阻断"]

    style T1 fill:#fecaca,stroke:#dc2626
    style FAIL fill:#fecaca,stroke:#dc2626
```

### BVS 等级分布 + 建议行动

```mermaid
graph LR
    subgraph A ["🟢 A 90-100"]
        A1["行动：持续监测 + 定期复查"]
    end
    subgraph B ["🔵 B 80-89"]
        B1["行动：补强薄弱维度"]
    end
    subgraph C ["🟡 C 70-79"]
        C1["行动：重点优化结构化 + Schema"]
    end
    subgraph D ["🟠 D 60-69"]
        D1["行动：全面审计整改"]
    end
    subgraph F ["🔴 F 0-59"]
        F1["行动：紧急阻断索引 + 深度优化"]
    end

    style A fill:#bbf7d0,stroke:#16a34a
    style B fill:#bfdbfe,stroke:#2563eb
    style C fill:#fef08a,stroke:#ca8a04
    style D fill:#fed7aa,stroke:#ea580c
    style F fill:#fecaca,stroke:#dc2626
```

---

## 8. 核心数据流

### 工商数据导入流程

```mermaid
flowchart TB
    FILES["📁 JSON 源文件<br/>guichong/-/tree/json<br/>按省/年分目录"] --> FORMAT{"🔍 格式探测<br/>detectJSONFormat()"}

    FORMAT -->|"'['开头"| ARR["🧾 JSON 数组<br/>decoder.Token流式"]
    FORMAT -->|"Object含数组"| OBJ["📦 对象包裹数组<br/>{erDataList:[...]}"]
    FORMAT -->|"'{'逐行"| JSONL["📋 JSONL<br/>逐行扫描解析"]

    ARR & OBJ & JSONL --> MAP["🗺 mapRec<br/>字段映射(中/英key兼容)"]
    MAP --> TX["⚡ 事务批量 INSERT<br/>2000条/批"]
    TX --> DUP["🎯 INSERT OR IGNORE<br/>信用代码去重"]
    DUP --> FTS["🔎 FTS5 触发器<br/>自动同步全文索引"]

    style FORMAT fill:#fef9c3,stroke:#a16207
    style TX fill:#bbf7d0,stroke:#16a34a
```

### 定时审计调度器闭环

```mermaid
flowchart TB
    Start["⏰ Scheduler.Start"] --> Cron["📝 解析 cron 表达式<br/>默认：每 7 天"]
    Cron --> Next["📆 nextRun = 计算下次时间"]
    Next --> Wait["⌛ time.AfterFunc 注册定时器"]
    Wait --> Fire{"🚀 到达触发时间?"}
    Fire -- "是" --> Loop["🔁 遍历 ScheduleConfig 列表"]
    Loop --> Audit["🏢 engine.Audit(ctx, profile)"]
    Audit --> Save["💾 审计历史库写入"]
    Save --> Diff["📊 Monitor 对比上次结果"]
    Diff --> Warn{"⚠️ 触发异常信号?"}
    Warn -- "是" --> Hook["📨 Webhook 告警推送"]
    Warn -- "否" --> Cont["✅ 跳过告警"]
    Hook & Cont --> ReNext["📆 重新计算 nextRun"]
    ReNext --> Wait

    style Fire fill:#fef9c3,stroke:#a16207
    style Hook fill:#fecaca,stroke:#dc2626
```

---

## 9. 集成协议

### MCP Server 交互时序图

```mermaid
sequenceDiagram
    participant C as 🖥️ MCP 客户端<br/>Claude / Cursor / TraeCode
    participant S as 🧩 geo mcp-server
    participant BE as 🏢 Brand Engine
    participant DB as 📦 离线工商库
    participant CC as ✅ China-Check MCP

    C->>S: {method:initialize, id:1}
    S-->>C: {protocolVersion, tools capabilities}

    C->>S: {method:tools/list}
    S-->>C: [brand_audit, optimize, search_companies, chinacheck, readiness]

    Note over C,CC: 示例 1: 工商库搜索
    C->>S: tools/call "search_companies" {query:"腾讯"}
    S->>DB: Search("腾讯", TopN=5)
    DB-->>S: [5 条匹配记录]
    S-->>C: content=[{text:"JSON 数组"}]

    Note over C,CC: 示例 2: 实时工商核验
    C->>S: tools/call "chinacheck" {query:"腾讯"}
    S->>CC: JSON-RPC search_chinese_company
    CC-->>S: 官方最新工商快照
    S-->>C: content=[{text:"JSON 快照"}]

    Note over C,CC: 示例 3: 品牌审计
    C->>S: tools/call "brand_audit" {profile}
    S->>BE: engine.Audit(ctx, profile)
    BE-->>S: VisibilityReport + BVS Score
    S-->>C: content=[{text:"完整审计报告 JSON"}]
```

### REST API 分层路由

```mermaid
graph TB
    subgraph 入口层["🌐 入口层 /api/v1/"]
        H["health GET<br/>健康检查"]
        ST["strategies GET<br/>策略列表"]
    end

    subgraph 内容层["📝 内容优化 /api/v1/"]
        ANA["analyze POST<br/>信号分析"]
        SCO["score POST<br/>评分"]
        OPT["optimize POST<br/>优化改写"]
    end

    subgraph 品牌层["🏢 品牌审计 /api/v1/brand/"]
        ACD["discover POST"]
        ACR["discover/report POST"]
        AUD["audit POST<br/>品牌审计"]
        AUT["autocomplete GET<br/>补全"]
        REP["report/html GET<br/>HTML报告"]
    end

    subgraph 扩展层["🔥 P0/P1 扩展模块"]
        TS["topsource/analyze POST"]
        VR["vertical/detect POST"]
        LS["localseo/audit POST"]
        ES["externalsignals/report POST"]
        AR["autorewriter/* POST"]
        RD["readiness/ci-gate GET"]
    end

    subgraph 商业化["💼 商业化与交付 /api/v1/"]
        AD["admin/* GET/POST<br/>管理员后台"]
        TK["tickets GET/POST<br/>工单系统"]
        HP["help/* GET<br/>帮助中心"]
        PR["pricing/* GET<br/>定价方案"]
        LD["landing/* GET<br/>落地页"]
    end

    subgraph 安全["🛡 安全与白标 /api/v1/"]
        SE["security/audit GET<br/>安全审计"]
        WL["meta/whitelabel GET<br/>白标配置"]
    end

    subgraph 对标["📊 对标与排行 /api/v1/"]
        CP["brand/compare GET<br/>竞品对标"]
        LB["leaderboard GET<br/>GEO 排行榜"]
        CMS["cms/* GET/POST<br/>CMS 检查"]
    end

    style 入口层 fill:#ecfeff,stroke:#0891b2
    style 内容层 fill:#dbeafe,stroke:#1d4ed8
    style 品牌层 fill:#dcfce7,stroke:#15803d
    style 扩展层 fill:#fef3c7,stroke:#b45309
    style 商业化 fill:#e0e7ff,stroke:#4f46e5
    style 安全 fill:#fee2e2,stroke:#b91c1c
    style 对标 fill:#d1fae5,stroke:#047857
```

---

## 10. 部署与运维

### 三种部署形态

```mermaid
graph TB
    subgraph 单机["🟢 单机部署（推荐个人）"]
        direction LR
        BIN["📦 geo 二进制<br/>含 SPA 前端 + 降级缓存"]
        BIN --> SQL["💿 SQLite 文件<br/>~/.local/share/geo/"]
        BIN --> JL["⚡ JSONL 缓存<br/>~/.cache/geo/"]
    end

    subgraph Docker["🔵 Docker 部署（推荐团队）"]
        DC["⚙️ docker-compose"]
        DC --> C["🐳 Alpine 容器<br/>非root用户"]
        C --> V["💾 Volume 挂载<br/>持久化数据"]
        V --> SQL2["💿 SQLite + JSONL"]
    end

    subgraph 生产["🟠 生产部署（大规模）"]
        direction LR
        BIN2["🛠 geo 二进制 / K8s Pods"]
        BIN2 --"GEO_HISTORY_DB_TYPE=mysql"--> MYSQL["🐬 MySQL 集群<br/>审计历史"]
        BIN2 --"GEO_CHINACHECK_CACHE_TYPE=redis"--> REDIS["🔴 Redis 哨兵<br/>缓存层"]
        BIN2 --"GEO_OFFLINE_DB_TYPE=duckdb"--> DUCK["🦆 DuckDB 挂载 SSD<br/>千万级数据"]
        BIN2 --"GEO_SCHEDULER_WEBHOOK"--> WEB["📨 Webhook<br/>告警到飞书/钉钉"]
    end

    style 单机 fill:#bbf7d0,stroke:#16a34a
    style Docker fill:#bfdbfe,stroke:#2563eb
    style 生产 fill:#fed7aa,stroke:#ea580c
```

### 环境变量分组全景

```mermaid
graph LR
    subgraph 基础["🟦 服务基础"]
        PORT["GEO_PORT=8080"]
    end

    subgraph LLM["🟣 LLM 配置"]
        K["GEO_LLM_KEY"]
        B["GEO_LLM_BASE"]
        M["GEO_LLM_MODEL"]
    end

    subgraph Engines["🟠 引擎 Keys ×13"]
        K1["GEO_OPENAI_KEY"]
        K2["GEO_GLM_KEY"]
        K3["GEO_DEEPSEEK_KEY"]
        K4["GEO_QWEN_KEY 等..."]
    end

    subgraph DBs["🟢 数据库 ×3 模块"]
        OT["GEO_OFFLINE_DB_TYPE<br/>sqlite/duckdb"]
        HT["GEO_HISTORY_DB_TYPE<br/>sqlite/mysql"]
        CT["GEO_CHINACHECK_CACHE_TYPE<br/>jsonl/redis"]
    end

    subgraph 调度["🔴 定时调度"]
        SE["GEO_SCHEDULER_ENABLED=true"]
        WK["GEO_SCHEDULER_WEBHOOK=xxx"]
    end

    subgraph 安全白标["🛡 安全与白标"]
        AK["GEO_API_KEY"]
        ADM["GEO_ADMIN_KEY"]
        CO["GEO_CORS_ORIGINS"]
        RL["GEO_RATE_LIMIT_GLOBAL"]
        WB["GEO_WHITELABEL_BRAND_NAME"]
        WC["GEO_WHITELABEL_PRIMARY_COLOR"]
    end

    subgraph 国内信号["📊 国内信号源"]
        BI["GEO_BAIDU_INDEX_KEY"]
        WI["GEO_WECHAT_INDEX_KEY"]
        ZH["GEO_ZHIHU_HOT_KEY"]
        XH["GEO_XHS_KEY"]
        DY["GEO_DOUYIN_OCEAN_KEY"]
        NW["GEO_NEWSWIRE_KEY"]
        CR["GEO_CRM_KEY"]
    end

    style 基础 fill:#e0f2fe,stroke:#0369a1
    style LLM fill:#f3e8ff,stroke:#7c3aed
    style Engines fill:#fed7aa,stroke:#ea580c
    style DBs fill:#dcfce7,stroke:#16a34a
    style 调度 fill:#fee2e2,stroke:#dc2626
    style 安全白标 fill:#ede9fe,stroke:#6d28d9
    style 国内信号 fill:#dbeafe,stroke:#1d4ed8
```

---

## 图表清单

本架构文档共计 **31** 张 Mermaid 图表：

| # | 类型 | 章节 | 说明 |
|---|---|---|---|
| 1 | mindmap | 目录 | 文档内容导航 mindmap |
| 2 | graph TB | 1.系统全景 | 四层架构图（用户→引擎→AI→数据→外部） |
| 3 | graph LR | 1.系统全景 | 四种入口对比（需Key/零配置） |
| 4 | flowchart LR | 2.内容优化 | 内容优化 6 步流程 |
| 5 | pie | 2.内容优化 | GEO 6 维评分权重饼图 |
| 6 | xychart-beta | 2.内容优化 | 9 法策略 PWC 增益柱状图 |
| 7 | graph TB | 2.内容优化 | 三大领域自适应策略 |
| 8 | flowchart TB | 3.品牌可见度 | 完整审计流程+数据优先级链 |
| 9 | pie | 3.品牌可见度 | BVS 7 维加权评分饼图 |
| 10 | flowchart TD | 3.品牌可见度 | 5 类模型分歧告警决策流 |
| 11 | graph LR | 3.品牌可见度 | 5 类行业垂直识别 |
| 12 | flowchart LR | 3.品牌可见度 | Top Source 归因流程 |
| 13 | graph LR | 3.品牌可见度 | **竞品引用差距热力图矩阵**（新） |
| 14 | graph TB | 3.品牌可见度 | **审计报告内部结构**（新） |
| 15 | flowchart LR | 3.品牌可见度 | **Autocomplete 多来源融合流程**（新） |
| 16 | flowchart TB | 3.品牌可见度 | **UI 双模式响应捕获（API + Playwright）**（新） |
| 17 | flowchart TD | 4.关键词发现 | Discover 完整工作流 |
| 18 | graph TB | 4.关键词发现 | BrandProfile 自动构建逻辑 |
| 19 | graph TB | 5.数据库选型 | 三层抽象架构（工厂→接口→实现） |
| 20 | flowchart TD | 5.数据库选型 | 选型决策树 |
| 21 | graph TD | 6.AI适配层 | 13 引擎星型结构（共用基类） |
| 22 | xychart-beta | 6.AI适配层 | 引擎敏感度权重折线对比 |
| 23 | flowchart LR | 7.高级功能 | AutoGEO 规则提取 + GEU 双评分 |
| 24 | flowchart TB | 7.高级功能 | 8 维 AI 就绪度 + CI 闸门 |
| 25 | graph LR | 7.高级功能 | BVS 等级分层与行动指南 |
| 26 | flowchart TB | 8.数据流 | 工商数据导入流程（3种格式+批量事务） |
| 27 | flowchart TB | 8.数据流 | 定时审计调度器闭环（Cron+告警） |
| 28 | sequenceDiagram | 9.集成协议 | MCP Server 完整交互时序 |
| 29 | graph TB | 9.集成协议 | REST API 四层路由 |
| 30 | graph TB | 10.部署运维 | 三种部署形态（单机/Docker/生产） |
| 31 | graph LR | 10.部署运维 | 环境变量分组全景（5 大类） |
