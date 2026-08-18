-- deploy/initdb/01-databases.sql
--
-- GEO 系统 MySQL 初始化脚本（仅首次初始化数据卷时执行，由 mysql 镜像自动以 root 运行）。
--
-- 创建 4 个业务库并对 geo 应用用户授权：
--   geo_offline   离线工商库（千万级企业 FULLTEXT ngram）
--   geo_history   审计历史时序 + JSON 快照
--   geo_auth      账号 / 会话 / 审计日志
--   geo_chinacheck China-Check 缓存 KV
--
-- 说明：应用容器 DSN 中的连接用户为 compose 定义的 MYSQL_USER(geo)，
-- 该用户由 mysql 镜像在运行本脚本前已创建（官方 entrypoint 顺序保证）。

CREATE DATABASE IF NOT EXISTS geo_offline
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS geo_history
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS geo_auth
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS geo_chinacheck
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

GRANT ALL PRIVILEGES ON geo_offline.* TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo_history.* TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo_auth.* TO 'geo'@'%';
GRANT ALL PRIVILEGES ON geo_chinacheck.* TO 'geo'@'%';
FLUSH PRIVILEGES;
