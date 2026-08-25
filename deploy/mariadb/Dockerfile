# 把 schema.sql 固化进 mariadb 镜像，彻底摆脱「目标机器必须存在外部 SQL 文件」的依赖。
#
# 为什么这样做：
#   原本 docker-compose.yml 用 bind mount 把宿主机的 ./deploy/initdb 挂进
#   /docker-entrypoint-initdb.d。但 MariaDB 的 entrypoint 只在【数据卷为空】时执行一次
#   initdb.d，而 SQL 来自宿主机——只要目标机器上该文件缺失或仍是旧版（MySQL8 语法），
#   geo 账号就建不出来，且卷一旦初始化就再也不重跑，导致全新机器仍 Access denied。
#   烤进镜像后，schema.sql 随镜像版本走，全新部署必然拿到正确版本。
#
# 注意（旧卷陷阱，与挂载方式无关）：
#   initdb.d 仅在 mariadb-data 卷为空时跑一次。若某台机器的卷已被「旧版坏 schema」定型，
#   换镜像也不会重跑——需 docker compose down -v && up -d 重建，或手动导入：
#     docker compose exec -T mariadb mysql -u root -pgeoRootPass < deploy/initdb/schema.sql
#
# schema.sql 本身幂等（CREATE USER IF NOT EXISTS / ALTER USER / CREATE TABLE IF NOT EXISTS），
# 重跑只补缺、不删数据。
#
# 构建上下文为 deploy/（见 scripts/publish.sh 的 build_mariadb_image：
#   docker buildx build -f deploy/mariadb/Dockerfile -t <geo:mariadb> deploy），
# 因此本 Dockerfile 用相对路径 initdb/schema.sql。仓库根 .dockerignore 不含 deploy/，
# 故 mariadb 构建不受其影响，schema.sql 可正常 COPY。
FROM mariadb:11
COPY initdb/schema.sql /docker-entrypoint-initdb.d/01-schema.sql
