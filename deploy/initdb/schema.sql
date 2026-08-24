-- deploy/initdb/schema.sql
--
-- GEO 系统【全量全新 schema】（单文件，替代原 01-databases.sql + 02-schema.sql）
--
-- 适用场景：全新部署 / 首次初始化（本项目尚未上传数据，直接整库初始化即可）。
--
--   用法一（手动导入独立 MySQL）：
--       mysql -h<host> -u<root> -p < deploy/initdb/schema.sql
--   用法二（docker compose / mysql 容器挂载）：
--       docker compose 中把本文件挂到 /docker-entrypoint-initdb.d/ 下，
--       首次初始化数据卷时由 mysql 官方镜像以 root 自动执行。
--
-- 特性：
--   - 自包含：建应用账号 + 建 geo 库 + 授权 + 全部业务表 + 索引，一个文件跑完
--   - 幂等：全部 CREATE xxx IF NOT EXISTS，可重复执行
--   - 单库架构：auth / billing / history / chinacheck / offline 各模块表全部并入 geo 库
--   - 应用内零建表迁移：DDL 完全由本文件负责，应用启动只做数据读写
--   - app_settings 表结构与其默认值种子数据（INSERT IGNORE）都在本文件定义，
--     建库即植入默认值；应用启动仅做读取与用户修改，不再写默认值。
--     种子数据由 scripts/gen_app_settings_seed 生成（新增配置项后重跑并同步）。
--
-- 注意：
--   - 下方 'geo' 账号初始密码为 docker2026ID@（生产环境仍建议改为更强口令，并同步各 GEO_MYSQL_DSN），
--     单库架构下所有模块共用同一 DSN，只需保证此处密码与连接串中的密码一致即可。
--   - 若已用 MYSQL_USER/MYSQL_PASSWORD 创建过 geo 用户，
--     CREATE USER IF NOT EXISTS 不会改动其现有密码，GRANT 仍可正常执行。

-- ############################################################################
-- 0) 应用账号 + 业务库 + 授权
-- ############################################################################

-- 账号口令统一为 docker2026ID@，与下方 GEO_MYSQL_DSN 一致。
-- 注意：必须用 MariaDB 原生语法 IDENTIFIED BY（MariaDB 默认认证插件即
-- mysql_native_password，被 go-sql-driver/mysql v1.10 完整支持）。不要写成
-- MySQL 8 的 “IDENTIFIED WITH mysql_native_password BY '...'”——该语法在
-- MariaDB 11 上会报语法错，导致初始化脚本中断、geo 账号建不出来，全新部署也 denied。
CREATE USER IF NOT EXISTS 'geo'@'%'         IDENTIFIED BY 'docker2026ID@';
CREATE USER IF NOT EXISTS 'geo'@'localhost' IDENTIFIED BY 'docker2026ID@';

CREATE DATABASE IF NOT EXISTS geo
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

GRANT ALL PRIVILEGES ON geo.* TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo.* TO 'geo'@'localhost';

-- 兜底：IF NOT EXISTS 不会重置已有账号的密码。若数据卷是旧的（geo 账号早建、
-- 口令已被改过），上面的 CREATE USER 不会改密码，DSN 一连就 Access denied。
-- 这里强制把密码与连接串对齐，无论账号新建还是已存在，密码都落到 docker2026ID@。
-- ALTER USER 使用 MariaDB 原生 IDENTIFIED BY 语法（同样不要用 WITH ... BY）。
ALTER USER 'geo'@'%'         IDENTIFIED BY 'docker2026ID@';
ALTER USER 'geo'@'localhost' IDENTIFIED BY 'docker2026ID@';

FLUSH PRIVILEGES;

USE geo;

-- ############################################################################
-- 1) 账号体系（原 geo_auth）
-- ############################################################################

CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(64) PRIMARY KEY,
    email         VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255) NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL,
    last_login_at BIGINT NOT NULL DEFAULT 0,
    verified      TINYINT(1) NOT NULL DEFAULT 0,
    -- 改密/管理员手动吊销时 +1，使该用户此前签发的全部 JWT 立即失效
    token_version BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS workspaces (
    id         VARCHAR(64) PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    owner_id   VARCHAR(64) NOT NULL,
    plan       VARCHAR(255) NOT NULL DEFAULT 'free'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS memberships (
    user_id      VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    role         VARCHAR(255) NOT NULL,
    joined_at    BIGINT NOT NULL,
    PRIMARY KEY (user_id, workspace_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_memberships_user ON memberships(user_id);
CREATE INDEX idx_memberships_ws ON memberships(workspace_id);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    jti          VARCHAR(64) PRIMARY KEY,
    user_id      VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(64),
    expires_at   BIGINT NOT NULL,
    created_at   BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_refresh_user ON refresh_tokens(user_id);

CREATE TABLE IF NOT EXISTS admin_audit_log (
    id           VARCHAR(64) PRIMARY KEY,
    timestamp    BIGINT NOT NULL,
    actor_id     VARCHAR(64),
    actor        VARCHAR(255),
    action       VARCHAR(255) NOT NULL,
    target       VARCHAR(255),
    details_json TEXT,
    ip           VARCHAR(255),
    user_agent   TEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_audit_time ON admin_audit_log(timestamp DESC);
CREATE INDEX idx_audit_action ON admin_audit_log(action);

-- ############################################################################
-- 2) 计费 / 订阅 / 订单 / 发票（原 geo_billing）
-- ############################################################################

CREATE TABLE IF NOT EXISTS subscriptions (
    id                   VARCHAR(64)  PRIMARY KEY,
    workspace_id         VARCHAR(64)  NOT NULL,
    plan                 VARCHAR(32)  NOT NULL DEFAULT 'free',
    status               VARCHAR(16)  NOT NULL DEFAULT 'active'
        COMMENT 'active / trialing / past_due / cancelled',
    current_period_start BIGINT       NOT NULL DEFAULT 0,
    current_period_end   BIGINT       NOT NULL DEFAULT 0,
    activated_by         VARCHAR(64)  NOT NULL DEFAULT '',
    activated_at         BIGINT       NOT NULL DEFAULT 0,
    created_at           BIGINT       NOT NULL,
    cancelled_at         BIGINT       NOT NULL DEFAULT 0,
    UNIQUE KEY uk_workspace (workspace_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS usage_meters (
    id           BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    workspace_id VARCHAR(64) NOT NULL,
    meter        VARCHAR(32) NOT NULL
        COMMENT 'audits / brands / scheduled_audits / exports / history_retention_days',
    period_month VARCHAR(7)  NOT NULL COMMENT 'YYYY-MM',
    used         BIGINT      NOT NULL DEFAULT 0,
    `limit`      BIGINT      NOT NULL DEFAULT -1 COMMENT '-1 表示不限',
    updated_at   BIGINT      NOT NULL,
    UNIQUE KEY uk_ws_meter_month (workspace_id, meter, period_month)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS payment_orders (
    id                VARCHAR(64)  PRIMARY KEY,
    workspace_id      VARCHAR(64)  NOT NULL,
    provider          VARCHAR(16)  NOT NULL DEFAULT ''
        COMMENT 'wechatpay / alipay / stripe / manual（未配置支付渠道时）',
    plan              VARCHAR(32)  NOT NULL,
    amount_cents      BIGINT       NOT NULL DEFAULT 0,
    currency          VARCHAR(8)   NOT NULL DEFAULT 'CNY',
    status            VARCHAR(16)  NOT NULL DEFAULT 'created'
        COMMENT 'created / paid / failed / refunded',
    provider_order_id VARCHAR(255) NOT NULL DEFAULT '',
    checkout_url      VARCHAR(1024) NOT NULL DEFAULT '',
    created_at        BIGINT       NOT NULL,
    paid_at           BIGINT       NOT NULL DEFAULT 0,
    metadata          MEDIUMTEXT,
    KEY idx_ws (workspace_id),
    KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS invoices (
    id           VARCHAR(64)  PRIMARY KEY,
    workspace_id VARCHAR(64)  NOT NULL,
    order_id     VARCHAR(64)  NOT NULL DEFAULT '',
    amount_cents BIGINT       NOT NULL DEFAULT 0,
    currency     VARCHAR(8)   NOT NULL DEFAULT 'CNY',
    status       VARCHAR(16)  NOT NULL DEFAULT 'issued'
        COMMENT 'issued / paid / void',
    issued_at    BIGINT       NOT NULL,
    url          VARCHAR(1024) NOT NULL DEFAULT '',
    KEY idx_ws (workspace_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ############################################################################
-- 3) 工商核验缓存（原 geo_chinacheck）
-- ############################################################################

CREATE TABLE IF NOT EXISTS chinacheck_cache (
    cache_key VARCHAR(512) PRIMARY KEY,
    value MEDIUMBLOB NOT NULL,
    saved_at BIGINT NOT NULL,
    expire_at BIGINT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_expire ON chinacheck_cache(expire_at);
CREATE INDEX idx_saved ON chinacheck_cache(saved_at);

-- ############################################################################
-- 4) 审计历史（原 geo_history）
-- ############################################################################

CREATE TABLE IF NOT EXISTS audit_history (
    id                    BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    workspace_id          VARCHAR(255),
    brand_name            VARCHAR(255) NOT NULL,
    generated_at          BIGINT       NOT NULL,
    score                 DOUBLE       NOT NULL,
    grade                 VARCHAR(255) NOT NULL,
    tier                  VARCHAR(255) NOT NULL,
    entity_completeness   DOUBLE       NOT NULL DEFAULT 0,
    mention_rate          DOUBLE       NOT NULL DEFAULT 0,
    citation_rate         DOUBLE       NOT NULL DEFAULT 0,
    share_of_voice        DOUBLE       NOT NULL DEFAULT 0,
    citation_position     DOUBLE       NOT NULL DEFAULT 0,
    sentiment             DOUBLE       NOT NULL DEFAULT 0,
    entity_recognition    DOUBLE       NOT NULL DEFAULT 0,
    content_gaps_count    INT          NOT NULL DEFAULT 0,
    competitor_count      INT          NOT NULL DEFAULT 0,
    negative_count        INT          NOT NULL DEFAULT 0,
    action_count          INT          NOT NULL DEFAULT 0,
    report_json           MEDIUMTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_history_ws_brand_time ON audit_history(workspace_id, brand_name, generated_at);
CREATE INDEX idx_history_ws_time ON audit_history(workspace_id, generated_at);
CREATE INDEX idx_history_brand_time ON audit_history(brand_name, generated_at);
CREATE INDEX idx_history_time ON audit_history(generated_at);
CREATE INDEX idx_history_ws_brand ON audit_history(workspace_id, brand_name);

-- ############################################################################
-- 4.5) 引擎来源偏好研究（大模型引用来源的记录与历史趋势）
--     append-only：每次审计完成后把 results[].citations 的来源域名写入。
--     唯一键 record_id+result_index+citation_url 保证同一次审计不重复计数。
-- ############################################################################

CREATE TABLE IF NOT EXISTS engine_source_citations (
    id              BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    workspace_id    VARCHAR(255),
    engine          VARCHAR(32)   NOT NULL,
    source_domain   VARCHAR(255)  NOT NULL,
    source_category VARCHAR(32)   NOT NULL DEFAULT 'other',
    brand_name      VARCHAR(255)  NOT NULL,
    prompt          VARCHAR(255)  NOT NULL DEFAULT '',
    record_id       BIGINT UNSIGNED NOT NULL,
    result_index    INT           NOT NULL DEFAULT 0,
    citation_url    VARCHAR(1024) NOT NULL,
    cited_at        BIGINT        NOT NULL,
    UNIQUE KEY uq_src_record_result_url (record_id, result_index, citation_url(255))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_src_engine_domain_time ON engine_source_citations(workspace_id, engine, source_domain, cited_at);
CREATE INDEX idx_src_engine_time ON engine_source_citations(workspace_id, engine, cited_at);
CREATE INDEX idx_src_brand_time ON engine_source_citations(workspace_id, brand_name, cited_at);
CREATE INDEX idx_src_domain_time ON engine_source_citations(source_domain, cited_at);

-- ############################################################################
-- 21) 外部系统提交的大模型对话采集与分析（external_submissions）
--     外部系统（如浏览器插件、Chat 前端）提交「大模型名称 / 问题 / 回答 / 分享链接」，
--     后台定时抽取结构化结论（情感 / 主题 / 来源域名 / 实体提及 / 摘要）。
CREATE TABLE IF NOT EXISTS external_submissions (
    id              BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    model_name      VARCHAR(64)     NOT NULL DEFAULT '',
    question        MEDIUMTEXT      NOT NULL,
    answer          MEDIUMTEXT      NOT NULL,
    share_link      VARCHAR(1024)   NOT NULL DEFAULT '',
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending', -- pending / analyzed / failed
    summary         TEXT,
    sentiment       VARCHAR(16)     NOT NULL DEFAULT '',        -- positive / neutral / negative
    category        VARCHAR(64)     NOT NULL DEFAULT '',
    mentions        TEXT,                                    -- JSON 数组：被提及的实体
    source_domains  TEXT,                                    -- JSON 数组：回答内引用域名
    analysis_json   TEXT,                                    -- 完整结构化分析结果（JSON）
    error_msg       VARCHAR(512)    NOT NULL DEFAULT '',
    workspace_id    VARCHAR(255),
    created_at      BIGINT          NOT NULL,
    analyzed_at     BIGINT          NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_ext_status_time ON external_submissions(status, created_at);
CREATE INDEX idx_ext_model_time   ON external_submissions(model_name, created_at);
CREATE INDEX idx_ext_cat_time     ON external_submissions(category, created_at);

-- ############################################################################
-- 5) 离线工商注册库（原 geo_offline）
-- ############################################################################

CREATE TABLE IF NOT EXISTS companies (
    id                  BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    name                VARCHAR(255) NOT NULL,
    code                VARCHAR(32),
    established_date    VARCHAR(32),
    industry            VARCHAR(255),
    legal_rep           VARCHAR(255),
    registered_capital  VARCHAR(128),
    business_scope      MEDIUMTEXT,
    province            VARCHAR(255),
    city                VARCHAR(255),
    district            VARCHAR(255),
    address             VARCHAR(255),
    status              VARCHAR(64),
    created_at          BIGINT NOT NULL,
    updated_at          BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE UNIQUE INDEX idx_companies_code ON companies(code);
CREATE INDEX idx_companies_province ON companies(province);
CREATE INDEX idx_companies_city ON companies(city);
-- 中文全文检索已迁移至外部 Meilisearch（MariaDB 不支持 MySQL 的 ngram 解析器）。
-- companies 表仅作主存储（单一事实来源），搜索经 GEO_MEILISEARCH_URL 指向的
-- Meilisearch 完成；本表不再建全文索引，普通索引仅供 Stats/Provinces 聚合查询使用。

-- ############################################################################
-- 6) 商业化增强：AI 引荐流量 / ROI 归因（P0-2）
-- ############################################################################

CREATE TABLE IF NOT EXISTS ai_traffic (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    brand_id    VARCHAR(191) NOT NULL,
    day         DATE NOT NULL,
    source      VARCHAR(64)  NOT NULL DEFAULT 'ga4',
    sessions    INT NOT NULL DEFAULT 0,
    conversions INT NOT NULL DEFAULT 0,
    revenue     DECIMAL(14,2) NOT NULL DEFAULT 0,
    ai_sourced  TINYINT(1) NOT NULL DEFAULT 0,
    created_at  BIGINT NOT NULL,
    INDEX idx_ai_traffic_brand_day (brand_id, day)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ai_conversion (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    brand_id    VARCHAR(191) NOT NULL,
    day         DATE NOT NULL,
    source      VARCHAR(64)  NOT NULL DEFAULT 'ga4',
    conversions INT NOT NULL DEFAULT 0,
    revenue     DECIMAL(14,2) NOT NULL DEFAULT 0,
    ai_sourced  TINYINT(1) NOT NULL DEFAULT 0,
    created_at  BIGINT NOT NULL,
    INDEX idx_ai_conversion_brand_day (brand_id, day)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ############################################################################
-- 7) Prompt 版本管理 + 实验归因（P1-e）
-- ############################################################################

CREATE TABLE IF NOT EXISTS tracked_prompts (
    id              VARCHAR(191) PRIMARY KEY,
    brand_id        VARCHAR(191) NOT NULL,
    text            TEXT NOT NULL,
    market          VARCHAR(32)  NOT NULL DEFAULT '',
    language        VARCHAR(16)  NOT NULL DEFAULT '',
    current_version INT NOT NULL DEFAULT 1,
    created_at      BIGINT NOT NULL,
    INDEX idx_tracked_prompts_brand (brand_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS prompt_versions (
    id                  BIGINT AUTO_INCREMENT PRIMARY KEY,
    prompt_id           VARCHAR(191) NOT NULL,
    version             INT NOT NULL,
    content             TEXT NOT NULL,
    baseline_visibility DECIMAL(6,2) NOT NULL DEFAULT 0,
    note                VARCHAR(512) NOT NULL DEFAULT '',
    created_at          BIGINT NOT NULL,
    INDEX idx_prompt_versions_pid (prompt_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS prompt_experiments (
    id                BIGINT AUTO_INCREMENT PRIMARY KEY,
    prompt_id         VARCHAR(191) NOT NULL,
    from_version      INT NOT NULL,
    to_version        INT NOT NULL,
    start_at          BIGINT NOT NULL,
    end_at            BIGINT NOT NULL DEFAULT 0,
    before_visibility DECIMAL(6,2) NOT NULL DEFAULT 0,
    after_visibility  DECIMAL(6,2) NOT NULL DEFAULT 0,
    sample_size       INT NOT NULL DEFAULT 0,
    INDEX idx_prompt_experiments_pid (prompt_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ############################################################################
-- 8) 买家人设定义（P1-c）
-- ############################################################################

CREATE TABLE IF NOT EXISTS personas (
    id          VARCHAR(191) PRIMARY KEY,
    brand_id    VARCHAR(191) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    prompts     JSON,
    keywords    JSON,
    created_at  BIGINT NOT NULL,
    INDEX idx_personas_brand (brand_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ############################################################################
-- 9) 系统配置（DB 变量存储：运行参数只读 DB，默认值由下方种子数据在建库时植入）
-- ############################################################################
-- 默认值种子（INSERT IGNORE：已存在的行不覆盖，用户修改保留）。
-- 由 scripts/gen_app_settings_seed 生成，勿手改；新增配置项后重跑该工具并同步。

CREATE TABLE IF NOT EXISTS app_settings (
    skey             VARCHAR(191) PRIMARY KEY,
    svalue           TEXT,
    default_value    TEXT NOT NULL,
    description      VARCHAR(512) NOT NULL DEFAULT '',
    category         VARCHAR(64)  NOT NULL DEFAULT 'general',
    stype            VARCHAR(32)  NOT NULL DEFAULT 'string',
    is_secret        TINYINT(1)   NOT NULL DEFAULT 0,
    is_bootstrap     TINYINT(1)   NOT NULL DEFAULT 0,
    requires_restart TINYINT(1)   NOT NULL DEFAULT 0,
    updated_at       BIGINT NOT NULL,
    KEY idx_settings_category (category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO app_settings
  (skey, svalue, default_value, description, category, stype, is_secret, is_bootstrap, requires_restart, updated_at)
VALUES
  ('GEO_ADMIN_EMAIL', '', '', '', 'admin', 'string', 0, 1, 0, 0),
  ('GEO_ADMIN_PASSWORD', '', '', '', 'admin', 'secret', 1, 1, 0, 0),
  ('GEO_ALIPAY_NOTIFY_URL', '', '', '', 'billing', 'string', 0, 0, 0, 0),
  ('GEO_ALLOW_REGISTER', 'false', 'false', '注册通道开关（GEO_ALLOW_REGISTER，默认 false 关闭；管理员由部署预置）', 'auth', 'bool', 0, 0, 0, 0),
  ('GEO_AUTH_ENABLED', '', '', '账号体系开关（引导类：环境变量设置 + 重启生效；true 启用 JWT/RBAC）', 'auth', 'bool', 0, 1, 0, 0),
  ('GEO_BAIDU_INDEX_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_BILLING_RETURN_URL', '', '', '', 'billing', 'string', 0, 0, 0, 0),
  ('GEO_CC_BASE_URL', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_CHINACHECK_CACHE_ENABLED', 'true', 'true', '---------- 缓存层（默认启用 MySQL K/V）----------', 'chinacheck', 'bool', 0, 0, 0, 0),
  ('GEO_CHINACHECK_CACHE_MAX_ITEMS', '', '', '', 'chinacheck', 'string', 0, 0, 0, 0),
  ('GEO_CHINACHECK_CACHE_TTL_HOURS', '', '', '', 'chinacheck', 'string', 0, 0, 0, 0),
  ('GEO_CHINACHECK_ENABLED', 'true', 'true', '', 'chinacheck', 'bool', 0, 0, 0, 0),
  ('GEO_CHINACHECK_LANG', '', '', '', 'chinacheck', 'string', 0, 0, 0, 0),
  ('GEO_CHINACHECK_URL', '', '', '', 'chinacheck', 'string', 0, 0, 0, 0),
  ('GEO_CHROME_PATH', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_CORS_ORIGINS', '', '', 'CORS 白名单（逗号分隔 Origin；默认仅 localhost）', 'general', 'string', 0, 0, 1, 0),
  ('GEO_CRM_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_CRM_TYPE', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_DFS_APIKEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_DFS_BASE_URL', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_DFS_EMAIL', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_DOUYIN_OCEAN_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_EMBEDDING_BASE', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_EMBEDDING_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_EMBEDDING_MODEL', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_HISTORY_DB_ENABLED', 'true', 'true', '', 'history', 'bool', 0, 0, 0, 0),
  ('GEO_JWT_SECRET', '', '', 'JWT 签名密钥（≥32 字节，引导类：环境变量设置 + 重启生效；改动后所有会话失效）', 'auth', 'secret', 1, 1, 1, 0),
  ('GEO_LLM_BASE', 'https://api.openai.com/v1', 'https://api.openai.com/v1', '默认 LLM API 基地址', 'llm', 'string', 0, 0, 1, 0),
  ('GEO_LLM_BUDGET_USD', '', '', 'LLM 月度预算上限（USD，0=不限）', 'llm', 'float', 0, 0, 0, 0),
  ('GEO_LLM_KEY', '', '', '默认 LLM API Key', 'llm', 'secret', 1, 0, 0, 0),
  ('GEO_LLM_MODEL', 'gpt-4o-mini', 'gpt-4o-mini', '默认 LLM 模型名', 'llm', 'string', 0, 0, 1, 0),
  ('GEO_LOG_FORMAT', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_LOG_LEVEL', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_MCP_API_KEY', '', '', '', 'mcp', 'secret', 1, 0, 0, 0),
  ('GEO_MCP_PORT', '9090', '9090', 'MCP Server 监听端口', 'mcp', 'int', 0, 0, 1, 0),
  ('GEO_MYSQL_DSN', '', '', '单库架构唯一 MySQL DSN（全部模块共用 geo 库）', 'db', 'secret', 1, 1, 0, 0),
  ('GEO_NEWSWIRE_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_OFFLINE_DB_ENABLED', 'true', 'true', '', 'offline', 'bool', 0, 0, 0, 0),
  ('GEO_OPENAPI_KEY', '', '', '开放测量 API 鉴权 Key（X-GEO-API-Key）', 'admin', 'secret', 1, 0, 0, 0),
  ('GEO_PDF_WAIT_MS', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_PORT', '7070', '7070', 'HTTP 监听端口', 'server', 'int', 0, 0, 1, 0),
  ('GEO_READINESS_INSECURE_TLS', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_REDIS_ADDR', '127.0.0.1:6379', '127.0.0.1:6379', 'Redis 地址（asynq 队列）', 'queue', 'string', 0, 1, 1, 0),
  ('GEO_RULES', '', '', '', 'server', 'string', 0, 0, 0, 0),
  ('GEO_SCHEDULER_CONFIG', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_SCHEDULER_ENABLED', 'false', 'false', '', 'general', 'bool', 0, 0, 0, 0),
  ('GEO_SCHEDULER_WEBHOOK', '', '', '', 'general', 'string', 0, 0, 0, 0),
  ('GEO_SMTP_FROM', '', '', '', 'mail', 'string', 0, 0, 0, 0),
  ('GEO_SMTP_HOST', '', '', 'SMTP 服务器', 'mail', 'string', 0, 0, 1, 0),
  ('GEO_SMTP_PASSWORD', '', '', 'SMTP 密码', 'mail', 'secret', 1, 0, 1, 0),
  ('GEO_SMTP_PORT', '587', '587', 'SMTP 端口', 'mail', 'int', 0, 0, 1, 0),
  ('GEO_SMTP_TLS', 'auto', 'auto', '', 'mail', 'string', 0, 0, 0, 0),
  ('GEO_SMTP_USER', '', '', 'SMTP 用户名', 'mail', 'string', 0, 0, 1, 0),
  ('GEO_TRUSTED_PROXIES', '', '', '可信代理 IP/CIDR（逗号分隔；用于正确解析客户端 IP）', 'general', 'string', 0, 0, 1, 0),
  ('GEO_WECHAT_INDEX_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_WL_AGENCY_NAME', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_BRAND_NAME', 'GEO', 'GEO', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_DOMAIN', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_FAVICON_URL', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_LOGO_URL', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_PRIMARY_COLOR', '#3B82F6', '#3B82F6', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_SUPPORT_EMAIL', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WL_TENANT_ID', '', '', '', 'whitelabel', 'string', 0, 0, 0, 0),
  ('GEO_WXPAY_NOTIFY_URL', '', '', '', 'billing', 'string', 0, 0, 0, 0),
  ('GEO_XHS_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_ZHIHU_HOT_KEY', '', '', '', 'general', 'secret', 1, 0, 0, 0),
  ('GEO_OPENAI_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_PERPLEXITY_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_GEMINI_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_CLAUDE_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_GROK_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_QWEN_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_GLM_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_DEEPSEEK_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_KIMI_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_ERNIE_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_DOUBAO_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_XIAOMI_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_XUNFEI_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_YUANBAO_KEY', '', '', '', 'engines', 'secret', 1, 0, 0, 0),
  ('GEO_OPENAI_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_PERPLEXITY_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_GEMINI_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_CLAUDE_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_GROK_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_QWEN_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_GLM_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_DEEPSEEK_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_KIMI_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_ERNIE_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_DOUBAO_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_XIAOMI_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_XUNFEI_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_YUANBAO_WEB_SEARCH', 'true', 'true', '引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）', 'engines', 'bool', 0, 0, 0, 0),
  ('GEO_ERNIE_BASE', '', '', '引擎 API 基地址（默认走内置官方地址）', 'engines', '', 0, 0, 0, 0),
  ('GEO_QWEN_BASE', '', '', '引擎 API 基地址（默认走内置官方地址）', 'engines', '', 0, 0, 0, 0),
  ('GEO_KIMI_BASE', '', '', '引擎 API 基地址（默认走内置官方地址）', 'engines', '', 0, 0, 0, 0),
  ('GEO_DOUBAO_BASE', '', '', '引擎 API 基地址（默认走内置官方地址）', 'engines', '', 0, 0, 0, 0),
  ('GEO_ERNIE_MODEL', '', '', '引擎模型名（默认走内置默认模型）', 'engines', '', 0, 0, 0, 0),
  ('GEO_QWEN_MODEL', '', '', '引擎模型名（默认走内置默认模型）', 'engines', '', 0, 0, 0, 0),
  ('GEO_KIMI_MODEL', '', '', '引擎模型名（默认走内置默认模型）', 'engines', '', 0, 0, 0, 0),
  ('GEO_DOUBAO_MODEL', '', '', '引擎模型名（默认走内置默认模型）', 'engines', '', 0, 0, 0, 0),
  ('GEO_AUDIT_SAMPLES', '1', '1', '品牌审计采样次数（每个查询×引擎重复查询 N 次多数票判定，1=单次；建议 3；单个请求可用 profile.samples 覆盖）', 'server', 'int', 0, 0, 1, 0),
  ('GEO_REDIS_PASSWORD', '', '', 'Redis 密码', 'queue', '', 1, 1, 1, 0),
  ('GEO_REDIS_DB', '', '', 'Redis DB 编号', 'queue', 'int', 0, 0, 1, 0),
  ('GEO_LLM_KEY_OPENAI', '', '', 'OpenAI 兼容密钥（LLM 管理器）', 'llm', 'secret', 1, 0, 0, 0),
  ('GEO_LLM_MODEL_OPENAI', '', '', 'OpenAI 兼容模型', 'llm', '', 0, 0, 1, 0),
  ('GEO_EXTERNAL_API_KEY', '', '', '外部提交接口鉴权 Key（X-GEO-External-Key；留空则该接口 401）', 'admin', 'secret', 1, 0, 0, 0);

-- ============================================================================
-- 完成。全部 21 张表 + app_settings 默认值种子 + 索引就绪；应用启动不再执行任何建表迁移。
-- ============================================================================
