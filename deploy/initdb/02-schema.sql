-- deploy/initdb/02-schema.sql
--
-- GEO 系统建表初始化脚本（自包含、幂等）
--
-- 依赖：先执行 01-databases.sql（建 geo 账号 + 5 个业务库 + 授权）。
--
-- 本文件做什么：
--   在单库 geo 中创建全部业务表（结构来自原 internal/migrate/sql/<db>/NNNN_*.sql，
--   已全部合并于此；应用内不再内嵌 migration，建表完全由本文件负责）。
--   5 库合并为 1 库：geo_auth / geo_billing / geo_history / geo_chinacheck / geo_offline
--   → geo（表名互不冲突，直接合并）。
--
-- 适用场景：
--   1) mysql 容器 /docker-entrypoint-initdb.d：与 01 一起在首次初始化数据卷时
--      自动执行（文件按名称排序：01 → 02）。
--   2) 手动导入已有独立 MySQL 实例（先 01 后 02）：
--         mysql -h<host> -u<root> -p < deploy/initdb/01-databases.sql
--         mysql -h<host> -u<root> -p < deploy/initdb/02-schema.sql
--
-- 后续演进：新增业务表/字段时，把新的 CREATE/ALTER 语句追加到对应库的区块
-- （注意保持幂等或只在初始化时执行；线上库变更建议单独出增量 SQL 并人工执行）。

-- ############################################################################
-- 1) 账号体系（原 geo_auth）
-- ############################################################################
USE geo;

CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(64) PRIMARY KEY,
    email         VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255) NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL,
    last_login_at BIGINT NOT NULL DEFAULT 0,
    verified      TINYINT(1) NOT NULL DEFAULT 0
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

-- users.token_version：改密/管理员手动吊销时 +1，使该用户此前签发的全部 JWT 立即失效
ALTER TABLE users ADD COLUMN token_version BIGINT NOT NULL DEFAULT 0;

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

-- 完成。应用启动不再执行任何建表迁移；schema 完全由 01 + 02 初始化。
