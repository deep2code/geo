-- 用户 token 版本号：改密 / 管理员手动吊销时 +1，
-- 使该用户此前签发的全部 JWT access token 立即失效（手动吊销能力）。
-- 新增列默认 0，存量用户无需回填。
ALTER TABLE users ADD COLUMN token_version BIGINT NOT NULL DEFAULT 0;
