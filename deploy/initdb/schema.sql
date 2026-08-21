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
--   - app_settings 表结构在此定义；其默认值/描述由应用启动时（config.InitSettings seed=true）
--     自动幂等写入，无需手工 INSERT
--
-- 注意：
--   - 下方 'geo' 账号初始密码为占位值 geoPass，生产环境请改为强口令，
--     并与各 GEO_*_MYSQL_DSN 中的密码保持一致。
--   - 若已用 MYSQL_USER/MYSQL_PASSWORD 创建过 geo 用户，
--     CREATE USER IF NOT EXISTS 不会改动其现有密码，GRANT 仍可正常执行。

-- ############################################################################
-- 0) 应用账号 + 业务库 + 授权
-- ############################################################################

CREATE USER IF NOT EXISTS 'geo'@'%'         IDENTIFIED BY 'geoPass';
CREATE USER IF NOT EXISTS 'geo'@'localhost' IDENTIFIED BY 'geoPass';

CREATE DATABASE IF NOT EXISTS geo
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

GRANT ALL PRIVILEGES ON geo.* TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo.* TO 'geo'@'localhost';

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
CREATE FULLTEXT INDEX ft_companies_name_scope ON companies(name, business_scope, legal_rep, address) WITH PARSER ngram;

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
-- 9) 系统配置（DB 变量存储：DB > 环境变量 > 默认值）
-- ############################################################################
-- 默认值 / 描述由应用启动时 config.InitSettings(seed=true) 幂等写入，无需手工 INSERT。

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

-- ============================================================================
-- 完成。全部 20 张表 + 索引就绪；应用启动不再执行任何建表迁移。
-- ============================================================================
