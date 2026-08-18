-- 工商核验结果缓存表（chinacheck 库）。

CREATE TABLE IF NOT EXISTS chinacheck_cache (
    cache_key VARCHAR(512) PRIMARY KEY,
    value MEDIUMBLOB NOT NULL,
    saved_at BIGINT NOT NULL,
    expire_at BIGINT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_expire ON chinacheck_cache(expire_at);
CREATE INDEX idx_saved ON chinacheck_cache(saved_at);
