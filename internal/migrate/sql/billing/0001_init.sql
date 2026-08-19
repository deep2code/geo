-- 计费与订阅核心表（billing 模块；默认复用 auth 库的 MySQL 实例）。
-- 与 internal/billing 的 Go 结构保持一致；后续变更请新增迁移文件，禁止原地修改。
-- 设计原则：不依赖 workspaces 表（workspace_id 仅作字符串关联），
-- 即使账号体系未启用（GEO_AUTH_ENABLED=false）也能独立运行计费逻辑。

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
