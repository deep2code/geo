-- 账号体系初始表结构（auth 库）。
-- 与 internal/auth 的历史内嵌 schema 保持一致；后续变更请新增迁移文件。

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
