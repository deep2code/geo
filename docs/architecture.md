# GEO 系统架构文档

## 1. 系统全景

```mermaid
graph TB
    subgraph 用户入口["用户入口"]
        CLI["CLI 工具链<br/>geo optimize / score / serve ..."]
        WEB["Web UI<br/>内嵌 HTML+CSS+JS"]
        API["REST API<br/>/api/v1/*"]
        MCP["MCP Server<br/>JSON-RPC 2.0"]
    end

    subgraph 核心引擎["核心引擎"]
        GEO["GEO Engine<br/>pkg/geo"]
        BRAND["Brand Engine<br/>internal/brand"]
    end

    subgraph AI引擎适配["AI 引擎适配层 (13 个)"]
        GPT["ChatGPT"]
        PPX["Perplexity"]
        GEM["Gemini"]
        CLD["Claude"]
        QWN["通义千问"]
        GLM["智谱GLM"]
        DSP["DeepSeek"]
        KMI["Kimi"]
        WEN["文心一言"]
        DBB["豆包"]
        XMI["小米"]
        XUF["讯飞星火"]
        YNB["元宝/混元"]
    end

    subgraph 数据存储["数据存储层（按功能选型）"]
        OFFDB["离线工商库<br/>SQLite / DuckDB<br/>1000万+ 条"]
        HISDB["审计历史库<br/>SQLite / MySQL<br/>时序+JSON"]
        CACHE["China-Check 缓存<br/>JSONL / Redis<br/>K/V + TTL"]
        KB["SinoFacts 知识库<br/>JSONL 只读"]
    end

    subgraph 外部服务["外部服务"]
        CC["China-Check MCP<br/>GSXT/SAMR 实时查询"]
        LLM["LLM API<br/>OpenAI 兼容协议"]
    end

    CLI --> GEO & BRAND
    WEB --> API
    API --> GEO & BRAND
    MCP --> BRAND

    GEO --> LLM
    BRAND --> AI引擎适配
    BRAND --> OFFDB & HISDB & CACHE & KB
    BRAND --> CC

    style 核心引擎 fill:#e1f5fe,stroke:#0288d1
    style 数据存储 fill:#f3e5f5,stroke:#7b1fa2
    style AI引擎适配 fill:#fff3e0,stroke:#f57c00
```

## 2. 内容优化流程

```mermaid
flowchart LR
    A["输入内容"] --> B["Analyzer<br/>信号分析"]
    B --> C["Scorer<br/>GEO 0-100 评分"]
    C --> D{有 LLM Key?}
    D -->|是| E["Optimizer<br/>9 法策略应用"]
    D -->|否| F["规则化预处理<br/>（不调 LLM）"]
    E --> G["LLM 改写"]
    F --> G
    G --> H["输出优化内容<br/>+ 评分报告"]

    style D fill:#fff9c4
    style G fill:#c8e6c9
```

### GEO 评分维度

```mermaid
pie title GEO 评分维度（6 维）
    "可引用性" : 30
    "结构" : 20
    "流畅度" : 15
    "关键词" : 15
    "独特性" : 10
    "技术性" : 10
```

### 9 法策略效果（Princeton 论文基准）

```mermaid
%%{init: {"theme": "default"}}%%
xychart-beta
    title "GEO 策略 PWC 增益 (%)"
    x-axis ["引用语", "统计数据", "流畅度", "引用来源", "结论前置", "权威语气", "结构化", "技术术语", "独特词汇"]
    y-axis "PWC 增益 %" 0 --> 50
    bar [41, 33, 29, 27, 24, 25, 22, 20, 18]
```

## 3. 品牌可见度审计流程

```mermaid
flowchart TB
    A["品牌名 + 竞品 + 查询词"] --> B["品牌智能补全<br/>Autocomplete"]

    subgraph 数据优先级["数据优先级（高 → 低）"]
        D1["① China-Check MCP<br/>实时工商核验"]
        D2["② 离线工商库<br/>1000万历史数据"]
        D3["③ SinoFacts 知识库<br/>品牌知识"]
        D4["④ 联网搜索"]
        D5["⑤ LLM 自身知识"]
    end

    B --> D1
    D1 --> D2
    D2 --> D3
    D3 --> D4
    D4 --> D5
    D5 --> C["品牌画像<br/>BrandProfile"]

    C --> E["多引擎审计<br/>同时查询 N 个 AI 引擎"]
    E --> F["Scorer<br/>BVS 0-100 评分"]
    F --> G["Reporter<br/>报告生成"]
    G --> H["时间序列持久化<br/>审计历史库"]

    H --> I["趋势分析<br/>BVS 变化追踪"]
    H --> J["模型分歧告警<br/>5 类异常检测"]

    style 数据优先级 fill:#fce4ec,stroke:#c62828
    style F fill:#e8f5e9,stroke:#2e7d32
```

### BVS 加权健康评分维度

```mermaid
pie title BVS 7 维加权评分
    "内容质量 (23%)" : 23
    "技术 SEO (22%)" : 22
    "站内 SEO (20%)" : 20
    "Schema (10%)" : 10
    "页面性能 (10%)" : 10
    "AI 就绪 (10%)" : 10
    "图像优化 (5%)" : 5
```

### 5 类模型分歧告警

```mermaid
flowchart TD
    CURR["当前审计结果"] --> CMP{"对比上次审计"}
    PREV["历史审计记录"] --> CMP

    CMP --> A1["竞品涌现<br/>新竞品进入 Top 3"]
    CMP --> A2["品牌消失<br/>从引用列表中消失"]
    CMP --> A3["声量下降<br/>引用率下降 > 20%"]
    CMP --> A4["位置下滑<br/>引用排名下降 > 3"]
    CMP --> A5["模型分歧<br/>引擎间结论冲突"]

    A1 & A2 & A3 & A4 & A5 --> ALERT["告警信号<br/>+ Webhook 通知"]

    style ALERT fill:#ffebee,stroke:#c62828
```

## 4. 数据库选型架构

```mermaid
graph TB
    subgraph dbprovider["dbprovider 工厂层"]
        PARSE["ParseType(mod)<br/>环境变量解析"]
        PATH["PathFor(mod)<br/>路径获取"]
        EN["EnabledFor(mod)<br/>开关判断"]
    end

    subgraph 离线工商库["OfflineCompanies"]
        OS["OfflineStore 接口"]
        OSQ["sqliteStore<br/>（默认）"]
        OD["duckStore<br/>（可选）"]
    end

    subgraph 审计历史库["AuditHistory"]
        HS["Store 接口"]
        HSQ["sqliteStore<br/>（默认）"]
        HM["mysqlStore<br/>（可选）"]
    end

    subgraph 缓存["ChinaCheckCache"]
        CS["CacheStore 接口"]
        CJ["jsonlStore<br/>（默认）"]
        CR["redisStore<br/>（可选）"]
    end

    PARSE --> OS & HS & CS
    OS --> OSQ & OD
    HS --> HSQ & HM
    CS --> CJ & CR

    style dbprovider fill:#e3f2fd,stroke:#1565c0
    style 离线工商库 fill:#f3e5f5,stroke:#7b1fa2
    style 审计历史库 fill:#e8f5e9,stroke:#2e7d32
    style 缓存 fill:#fff3e0,stroke:#ef6c00
```

### 数据库选型决策表

```mermaid
flowchart TD
    START["选择数据库后端"] --> Q1{"数据规模?"}

    Q1 -->|"千万级行 + 全文检索"| Q2{"需要列式聚合?"}
    Q1 -->|"时序追加 + JSON 列"| Q3{"生产高并发?"}
    Q1 -->|"K/V + TTL + 高频读"| Q4{"需要分布式缓存?"}

    Q2 -->|"否"| OFF_S["SQLite + FTS5<br/>零依赖，50ms/次"]
    Q2 -->|"是"| OFF_D["DuckDB<br/>列式并行，更快"]

    Q3 -->|"否"| HIS_S["SQLite<br/>单机够用"]
    Q3 -->|"是"| HIS_M["MySQL<br/>生产推荐"]

    Q4 -->|"否"| CA_J["JSONL 文件<br/>零依赖"]
    Q4 -->|"是"| CA_R["Redis<br/>高并发推荐"]

    style OFF_S fill:#c8e6c9
    style HIS_S fill:#c8e6c9
    style CA_J fill:#c8e6c9
```

## 5. AI 引擎适配层

```mermaid
graph LR
    subgraph 适配器["adapter.Adapter 接口"]
        BASE["openai_compatible<br/>基础实现"]
    end

    BASE --> GPT["ChatGPT<br/>OpenAI 原生"]
    BASE --> PPX["Perplexity<br/>OpenAI 兼容"]
    BASE --> GEM["Gemini<br/>OpenAI 兼容"]
    BASE --> CLD["Claude<br/>OpenAI 兼容"]
    BASE --> QWN["通义千问<br/>OpenAI 兼容"]
    BASE --> GLM["智谱GLM<br/>OpenAI 兼容"]
    BASE --> DSP["DeepSeek<br/>OpenAI 兼容"]
    BASE --> KMI["Kimi<br/>OpenAI 兼容"]
    BASE --> WEN["文心一言<br/>OpenAI 兼容"]
    BASE --> DBB["豆包<br/>OpenAI 兼容"]
    BASE --> XMI["小米<br/>OpenAI 兼容"]
    BASE --> XUF["讯飞星火<br/>OpenAI 兼容"]
    BASE --> YNB["元宝/混元<br/>OpenAI 兼容"]

    style BASE fill:#bbdefb,stroke:#1565c0
    style 适配器 fill:#fff3e0,stroke:#ef6c00
```

### 引擎预设权重对比

```mermaid
%%{init: {"theme": "default"}}%%
xychart-beta
    title "各引擎对 GEO 信号的敏感度权重"
    x-axis ["引用来源", "统计数据", "权威语气", "结构化", "流畅度", "结论前置"]
    y-axis "权重 (0-1)" 0 --> 1.1
    line [1.0, 0.8, 0.85, 0.65, 0.5, 0.7]
    line [0.9, 0.85, 0.6, 0.75, 0.7, 0.8]
    line [0.8, 0.9, 0.9, 0.85, 0.95, 0.75]
```

## 6. 工商数据导入流程

```mermaid
flowchart TB
    A["JSON 文件<br/>guichong/-/tree/json"] --> B{"格式探测<br/>detectJSONFormat"}

    B -->|"[" 开头"| C["JSON 数组<br/>importJSONArray<br/>Token 流式处理"]
    B -->|"{" 含数组键"| D["对象包裹数组<br/>importJSONObject<br/>{erDataList: [...]}"]
    B -->|"{" 逐行"| E["JSONL<br/>importJSONL<br/>逐行扫描"]

    C & D & E --> F["mapRec<br/>字段映射<br/>（中文 key / 英文 key 兼容）"]
    F --> G["批量 INSERT<br/>事务 2000 条/批"]
    G --> H["INSERT OR IGNORE<br/>信用代码去重"]
    H --> I["FTS5 触发器<br/>自动同步索引"]

    style B fill:#fff9c4
    style G fill:#c8e6c9
    style I fill:#e1f5fe
```

## 7. 定时审计调度器

```mermaid
flowchart TB
    A["Scheduler.Start"] --> B["解析 cron 表达式"]
    B --> C["计算下次执行时间<br/>nextRun"]
    C --> D["注册定时器<br/>time.AfterFunc"]
    D --> E{"到达执行时间"}

    E --> F["遍历 ScheduleConfig"]
    F --> G["调用 engine.Audit"]
    G --> H["写入审计历史库"]
    H --> I["Monitor 对比上次结果"]
    I --> J{"有异常?"}
    J -->|"是"| K["发送 Webhook 告警"]
    J -->|"否"| L["跳过"]
    K & L --> M["计算下次 nextRun<br/>写回库"]
    M --> D

    style D fill:#e3f2fd
    style K fill:#ffebee
    style M fill:#c8e6c9
```

## 8. MCP Server 架构

```mermaid
sequenceDiagram
    participant Client as MCP 客户端<br/>(Claude/Cursor/TraeCode)
    participant Server as geo mcp-server
    participant Brand as Brand Engine
    participant DB as 离线工商库
    participant CC as China-Check MCP

    Client->>Server: initialize (JSON-RPC)
    Server-->>Client: {protocolVersion, capabilities}

    Client->>Server: tools/list
    Server-->>Client: [brand_audit, optimize, search_companies, chinacheck, readiness]

    Client->>Server: tools/call "search_companies" {query: "腾讯"}
    Server->>DB: Search("腾讯")
    DB-->>Server: [{name:"腾讯...", code:"914403..."}]
    Server-->>Client: content[0].text = JSON

    Client->>Server: tools/call "chinacheck" {query: "腾讯"}
    Server->>CC: JSON-RPC search_chinese_company
    CC-->>Server: 工商注册快照
    Server-->>Client: content[0].text = JSON
```

## 9. 部署架构

```mermaid
graph TB
    subgraph 单机部署["单机部署（默认）"]
        BIN["geo 二进制<br/>单个文件"]
        SQLITE["SQLite 文件<br/>~/.local/share/geo/"]
        JSONL["JSONL 缓存<br/>~/.cache/geo/"]
    end

    subgraph Docker部署["Docker 部署"]
        DC["docker-compose"]
        CONTAINER["Alpine 容器<br/>非 root 用户"]
        VOL["Volume 挂载"]
    end

    subgraph 生产部署["生产部署（可选）"]
        MYS["MySQL<br/>审计历史"]
        RED["Redis<br/>缓存"]
        DUC["DuckDB<br/>离线工商"]
        WH["Webhook<br/>告警通知"]
    end

    BIN --> SQLITE & JSONL
    DC --> CONTAINER --> VOL
    VOL --> SQLITE & JSONL

    BIN -.->|"GEO_HISTORY_DB_TYPE=mysql"| MYS
    BIN -.->|"GEO_CHINACHECK_CACHE_TYPE=redis"| RED
    BIN -.->|"GEO_OFFLINE_DB_TYPE=duckdb"| DUC
    BIN -.->|"GEO_SCHEDULER_WEBHOOK"| WH

    style 单机部署 fill:#c8e6c9,stroke:#2e7d32
    style Docker部署 fill:#e3f2fd,stroke:#1565c0
    style 生产部署 fill:#fff3e0,stroke:#ef6c00
```

## 10. 环境变量全景

```mermaid
graph LR
    subgraph 服务["服务配置"]
        PORT["GEO_PORT"]
        TZ["TZ"]
    end

    subgraph LLM["LLM 配置"]
        KEY["GEO_LLM_KEY"]
        BASE["GEO_LLM_BASE"]
        MODEL["GEO_LLM_MODEL"]
    end

    subgraph 引擎["引擎 API Key"]
        E1["GEO_OPENAI_KEY"]
        E2["GEO_PERPLEXITY_KEY"]
        E3["GEO_GEMINI_KEY"]
        E4["GEO_CLAUDE_KEY"]
        E5["GEO_QWEN_KEY ..."]
    end

    subgraph 离线库["离线工商库"]
        O1["GEO_OFFLINE_DB_ENABLED"]
        O2["GEO_OFFLINE_DB_PATH"]
        O3["GEO_OFFLINE_DB_TYPE<br/>sqlite/duckdb"]
    end

    subgraph 历史["审计历史库"]
        H1["GEO_HISTORY_DB_ENABLED"]
        H2["GEO_HISTORY_DB_PATH"]
        H3["GEO_HISTORY_DB_TYPE<br/>sqlite/mysql"]
    end

    subgraph 缓存["China-Check 缓存"]
        C1["GEO_CHINACHECK_ENABLED"]
        C2["GEO_CHINACHECK_URL"]
        C3["GEO_CHINACHECK_CACHE_TYPE<br/>jsonl/redis"]
        C4["GEO_CHINACHECK_CACHE_PATH"]
        C5["GEO_CHINACHECK_CACHE_MAX_ITEMS"]
        C6["GEO_CHINACHECK_CACHE_TTL_HOURS"]
    end

    subgraph 调度["定时调度"]
        S1["GEO_SCHEDULER_ENABLED"]
        S2["GEO_SCHEDULER_WEBHOOK"]
        S3["GEO_SCHEDULER_CONFIG"]
    end

    style 服务 fill:#e3f2fd
    style LLM fill:#f3e5f5
    style 引擎 fill:#fff3e0
    style 离线库 fill:#e8f5e9
    style 历史 fill:#e8f5e9
    style 缓存 fill:#e8f5e9
    style 调度 fill:#fce4ec
```
