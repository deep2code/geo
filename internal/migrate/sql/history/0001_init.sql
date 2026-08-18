-- 审计历史库初始表结构（history 库）。

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
