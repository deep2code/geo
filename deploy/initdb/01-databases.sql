-- deploy/initdb/01-databases.sql
--
-- GEO 系统 MySQL 初始化脚本（自包含、幂等）
--
-- 适用两种场景：
--   1) 作为 mysql 容器 /docker-entrypoint-initdb.d 挂载：首次初始化数据卷时，
--      由 mysql 官方镜像以 root 自动执行（仅执行一次，数据卷已有数据则跳过）。
--   2) 手动导入到已有的「独立 MySQL 实例」：
--         mysql -h<host> -u<root> -p < deploy/initdb/01-databases.sql
--
-- 本脚本只负责：建应用账号（若不存在）+ 建 1 个业务库 + 授权。
-- 各业务表由 deploy/initdb/02-schema.sql 创建（应用内不内嵌迁移）。
--
-- 业务库：
--   geo  单库架构，全部模块共用：
--        auth（users/workspaces/...）、billing（subscriptions/orders/...）、
--        history（audit_history）、chinacheck（chinacheck_cache）、
--        offline（companies 千万级 FULLTEXT ngram）
--
-- 安全提示：
--   - 下方 'geo' 账号初始密码为占位值 geoPass，必须与 GEO_*_MYSQL_DSN 中密码一致；
--     生产环境请改为强口令，并与 .env 中 DSN 保持一致。
--   - 若你的独立 MySQL 已用 MYSQL_USER/MYSQL_PASSWORD 创建过 geo 用户，
--     CREATE USER IF NOT EXISTS 不会改动其现有密码，GRANT 仍可正常执行。

-- 1) 应用账号（若不存在则创建；密码占位，请按生产要求修改）
CREATE USER IF NOT EXISTS 'geo'@'%'       IDENTIFIED BY 'geoPass';
CREATE USER IF NOT EXISTS 'geo'@'localhost' IDENTIFIED BY 'geoPass';

-- 2) 业务库（单库）
CREATE DATABASE IF NOT EXISTS geo
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 3) 授权（% 与 localhost 都覆盖，避免本地/远程连接差异）
GRANT ALL PRIVILEGES ON geo.*    TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo.*    TO 'geo'@'localhost';

FLUSH PRIVILEGES;
