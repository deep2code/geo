-- 离线工商注册库初始表结构（offline 库）。
-- 数据源：https://github.com/guichong/-/tree/json (1978-2019)。

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
