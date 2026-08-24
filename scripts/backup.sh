#!/usr/bin/env bash
#
# backup.sh — MyGEO 生产级 MySQL 备份 / 恢复
#
# 覆盖 1 个业务库：geo（单库架构，auth/billing/history/chinacheck/offline 共用）
#   - mysqldump + gzip，时间戳命名
#   - 本地保留策略（默认保留 14 天 daily，可选 weekly/monthly 归档）
#   - 备份后 gzip -t 完整性校验
#   - 可选异地归档：配置 GEO_BACKUP_REMOTE（aws s3 / ossutil）后自动上传
#   - 安全：凭据通过 0600 临时 defaults 文件传递，不出现在进程列表 / 历史
#   - 支持 restore 子命令按库 / 按时间点恢复
#
# 用法：
#   ./backup.sh backup            # 全量备份 geo 库
#   ./backup.sh backup --db geo   # 仅备份单个库（可多库，空格分隔）
#   ./backup.sh list              # 列出本地备份
#   ./backup.sh restore --latest  # 恢复全部库到最新备份
#   ./backup.sh restore --db geo --file backups/geo-20260819-093000.sql.gz
#
# 环境变量（缺省值对应 MySQL 部署约定）：
#   GEO_MYSQL_HOST     默认 mysql
#   GEO_MYSQL_PORT     默认 3306
#   GEO_MYSQL_USER     默认 geo
#   GEO_MYSQL_PASSWORD 默认 docker2026ID@（务必在生产用强口令并通过 secret 注入）
#   GEO_BACKUP_DIR     默认 /data/geo/backups
#   GEO_BACKUP_DBS     默认 "geo"
#   GEO_BACKUP_KEEP_DAYS 默认 14
#   GEO_BACKUP_REMOTE  默认空（例：'s3://mygeo-backups' 或 'oss://mygeo-backups'）
#
set -euo pipefail

# ---------- 配置 ----------
HOST="${GEO_MYSQL_HOST:-mysql}"
PORT="${GEO_MYSQL_PORT:-3306}"
USER="${GEO_MYSQL_USER:-geo}"
# 密码必须通过环境变量显式配置，不再提供硬编码默认值（安全要求）
PASSWORD="${GEO_MYSQL_PASSWORD:-}"
if [[ -z "$PASSWORD" ]]; then
  echo "[ERROR] GEO_MYSQL_PASSWORD 环境变量未设置（备份需要数据库访问凭据）" >&2
  echo "        用法：GEO_MYSQL_PASSWORD=your_password bash scripts/backup.sh" >&2
  exit 1
fi
BACKUP_DIR="${GEO_BACKUP_DIR:-/data/geo/backups}"
DBS="${GEO_BACKUP_DBS:-geo}"
KEEP_DAYS="${GEO_BACKUP_KEEP_DAYS:-14}"
REMOTE="${GEO_BACKUP_REMOTE:-}"

TS="$(date +%Y%m%d-%H%M%S)"
LOG_PREFIX="[backup.sh $(date '+%Y-%m-%d %H:%M:%S')]"

log()  { echo "${LOG_PREFIX} $*"; }
die()  { echo "${LOG_PREFIX} ERROR: $*" >&2; exit 1; }

# ---------- 前置检查 ----------
command -v mysqldump >/dev/null 2>&1 || die "mysqldump 未安装（apt install mysql-client / apk add mysql-client）"
command -v mysql     >/dev/null 2>&1 || die "mysql 客户端未安装"
command -v gzip      >/dev/null 2>&1 || die "gzip 未安装"

mkdir -p "${BACKUP_DIR}"

# 用临时 defaults 文件传凭据，避免密码出现在 ps / shell 历史
TMP_CNF="$(mktemp)"
chmod 0600 "${TMP_CNF}"
trap 'rm -f "${TMP_CNF}"' EXIT
cat >"${TMP_CNF}" <<EOF
[client]
host=${HOST}
port=${PORT}
user=${USER}
password=${PASSWORD}
EOF

# 连通性自检
mysql --defaults-extra-file="${TMP_CNF}" -e "SELECT 1" >/dev/null 2>&1 \
  || die "无法连接 MySQL（${HOST}:${PORT} user=${USER}）。请检查网络 / 凭据 / 容器状态。"

# ---------- 备份 ----------
do_backup() {
  local only_db="${1:-}"
  local ok=0 fail=0
  for db in ${DBS}; do
    [ -n "${only_db}" ] && [ "${db}" != "${only_db}" ] && continue

    local out="${BACKUP_DIR}/${db}-${TS}.sql.gz"
    log "dumping ${db} -> ${out}"
    if mysqldump --defaults-extra-file="${TMP_CNF}" \
         --single-transaction --routines --triggers --events \
         --default-character-set=utf8mb4 "${db}" \
         | gzip -9 >"${out}.tmp"; then
      mv "${out}.tmp" "${out}"
      # 完整性校验：gzip -t 解压验证
      if gzip -t "${out}"; then
        local size
        size="$(du -h "${out}" | cut -f1)"
        log "OK ${db} (${size})"
        ok=$((ok+1))
      else
        rm -f "${out}"
        log "FAIL ${db} gzip 校验失败，已删除损坏文件"
        fail=$((fail+1))
      fi
    else
      rm -f "${out}.tmp"
      log "FAIL ${db} mysqldump 失败"
      fail=$((fail+1))
    fi
  done

  [ "${fail}" -eq 0 ] || die "${fail} 个库备份失败"

  # 异地归档（可选）
  if [ -n "${REMOTE}" ]; then
    log "归档到远端 ${REMOTE}"
    for f in "${BACKUP_DIR}"/*.sql.gz; do
      [ -e "$f" ] || continue
      if [[ "${REMOTE}" == s3://* ]]; then
        command -v aws >/dev/null 2>&1 || die "REMOTE 为 S3 但 aws cli 未安装"
        aws s3 cp "$f" "${REMOTE}/$(basename "$f")" || die "S3 上传失败: $f"
      elif [[ "${REMOTE}" == oss://* ]]; then
        command -v ossutil >/dev/null 2>&1 || die "REMOTE 为 OSS 但 ossutil 未安装"
        ossutil cp "$f" "${REMOTE}/$(basename "$f")" || die "OSS 上传失败: $f"
      else
        die "未知 REMOTE 协议（仅支持 s3:// 与 oss://）: ${REMOTE}"
      fi
    done
  fi

  log "备份完成：${ok} 个库成功"
  prune
}

# ---------- 保留策略：删除超过 KEEP_DAYS 天的本地备份 ----------
prune() {
  log "清理 ${KEEP_DAYS} 天前的本地备份"
  find "${BACKUP_DIR}" -maxdepth 1 -name '*.sql.gz' -type f -mtime "+${KEEP_DAYS}" -print -delete || true
}

# ---------- 列出 ----------
do_list() {
  log "本地备份（${BACKUP_DIR}）："
  ls -lh "${BACKUP_DIR}"/*.sql.gz 2>/dev/null | awk '{print $9, $5, $6, $7, $8}' || echo "  (无)"
}

# ---------- 恢复 ----------
do_restore() {
  local latest=0 only_db="" file=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --latest) latest=1 ;;
      --db)     only_db="$2"; shift ;;
      --file)   file="$2"; shift ;;
      *) die "未知 restore 参数: $1" ;;
    esac
    shift
  done

  [ -n "${file}" ] && [ "${latest}" -eq 1 ] && die "--latest 与 --file 互斥"

  if [ -n "${file}" ]; then
    [ -f "${file}" ] || die "备份文件不存在: ${file}"
    restore_one "${file}" "${only_db}"
    return
  fi

  if [ "${latest}" -eq 1 ]; then
    for db in ${DBS}; do
      [ -n "${only_db}" ] && [ "${db}" != "${only_db}" ] && continue
      local f
      f="$(ls -1t "${BACKUP_DIR}/${db}"-*.sql.gz 2>/dev/null | head -n1)"
      [ -n "${f}" ] || die "未找到 ${db} 的本地备份"
      restore_one "${f}" "${db}"
    done
  fi
}

restore_one() {
  local file="$1" db="$2"
  if [ -z "${db}" ]; then
    # 从文件名推断库名：<db>-<ts>.sql.gz
    db="$(basename "${file}" | sed -E 's/-[0-9]{8}-[0-9]{6}\.sql\.gz$//')"
  fi
  log "恢复 ${db} <- ${file}"
  # 确保目标库存在（compose initdb 已建；此处兜底）
  mysql --defaults-extra-file="${TMP_CNF}" -e "CREATE DATABASE IF NOT EXISTS \`${db}\` CHARACTER SET utf8mb4" \
    || die "无法创建/选择库 ${db}"
  gunzip -c "${file}" | mysql --defaults-extra-file="${TMP_CNF}" --default-character-set=utf8mb4 "${db}" \
    || die "恢复失败: ${db}"
  log "恢复完成 ${db}"
}

# ---------- 入口 ----------
case "${1:-backup}" in
  backup)  do_backup "${2:-}" ;;
  list)    do_list ;;
  restore) shift; do_restore "$@" ;;
  -h|--help|help) sed -n '2,40p' "$0" ;;
  *) die "未知子命令: $1（支持 backup / list / restore）" ;;
esac
