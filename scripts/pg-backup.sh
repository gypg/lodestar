#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# 共享 PostgreSQL 实例备份
#
# 为什么必须有：Lodestar 接付费用户后，users.quota / payment_orders /
# quota_ledgers 是**唯一**的记账依据。库丢了，余额无法重建、用户争议无法仲裁，
# 流水表的全部价值（钱的账可查证）随之归零 —— 账本和保险柜不能放同一个抽屉。
#
# 这个 postgres 容器是**多服务共享实例**（lodestar / newapi / sub2api / lodestar_e2e），
# 所以按库分别 dump：单库损坏或误删时可以只恢复一个，不必整实例回滚而牵连其他服务。
#
# 设计：
#   - 每库一个 custom-format dump（-Fc，自带压缩，支持 pg_restore 选择性恢复）
#   - 另存一份 globals（角色与权限；pg_dump 不含这些，只靠库 dump 恢复会缺角色）
#   - 先写 .part 再原子 mv：崩在半路不会留下一个看着像完整备份的截断文件
#   - dump 完立刻验证可读（pg_restore -l），坏档当场失败而不是恢复时才发现
#   - 保留 14 份日常 + 8 份每周（周日），总量约百 MB 级
#
# 用法（cron）：
#   30 4 * * * flock -n /tmp/pg-backup.lock /opt/docker/lodestar/scripts/pg-backup.sh >> /var/log/pg-backup.log 2>&1
# =============================================================================

CONTAINER="postgres"
PGUSER="admin"
BACKUP_ROOT="/opt/backups/postgres"
DAILY_DIR="$BACKUP_ROOT/daily"
WEEKLY_DIR="$BACKUP_ROOT/weekly"
KEEP_DAILY=14
KEEP_WEEKLY=8

# 磁盘低于这个值就不开始，避免备份把盘写满反而搞挂生产
MIN_FREE_MB=3072

STAMP="$(date '+%Y%m%d-%H%M%S')"
IS_SUNDAY=0
[[ "$(date '+%u')" == "7" ]] && IS_SUNDAY=1

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

fail() {
  log "✗ $*"
  exit 1
}

log "=== 备份开始 ==="

# --- 前置检查 ---------------------------------------------------------------

command -v docker >/dev/null 2>&1 || fail "docker 不可用"

docker inspect "$CONTAINER" >/dev/null 2>&1 \
  || fail "容器 $CONTAINER 不存在"

if [[ "$(docker inspect "$CONTAINER" --format '{{.State.Running}}')" != "true" ]]; then
  fail "容器 $CONTAINER 未运行"
fi

mkdir -p "$DAILY_DIR" "$WEEKLY_DIR"

FREE_MB=$(df -Pm "$BACKUP_ROOT" | awk 'NR==2 {print $4}')
if (( FREE_MB < MIN_FREE_MB )); then
  fail "剩余磁盘 ${FREE_MB}MB 低于下限 ${MIN_FREE_MB}MB，放弃本轮（先清理再跑）"
fi
log "剩余磁盘 ${FREE_MB}MB"

# --- 全局对象（角色/权限）---------------------------------------------------
#
# pg_dumpall --globals-only 不含任何表数据，但**没有它恢复会缺角色**，
# 恢复时会报 role "admin" does not exist 这类错。体积只有几 KB。

GLOBALS="$DAILY_DIR/globals-${STAMP}.sql"
log "导出 globals（角色与权限）"
if docker exec "$CONTAINER" pg_dumpall -U "$PGUSER" --globals-only > "${GLOBALS}.part" 2>/dev/null; then
  mv "${GLOBALS}.part" "$GLOBALS"
  log "  ✓ $(basename "$GLOBALS") ($(du -h "$GLOBALS" | cut -f1))"
else
  rm -f "${GLOBALS}.part"
  fail "globals 导出失败"
fi

# --- 逐库 dump --------------------------------------------------------------
#
# 库清单从实例现查，不写死：新增服务不会被静默漏掉。
# 排除 template0/template1（模板库，不该也不能 dump）。

DATABASES=$(docker exec "$CONTAINER" psql -U "$PGUSER" -d postgres -t -A \
  -c "SELECT datname FROM pg_database WHERE datistemplate = false AND datname <> 'postgres' ORDER BY datname;")

[[ -n "$DATABASES" ]] || fail "查不到任何用户库，中止（避免产出一个空备份集）"

FAILED=""
for DB in $DATABASES; do
  DB=$(echo "$DB" | tr -d '\r')
  [[ -n "$DB" ]] || continue

  OUT="$DAILY_DIR/${DB}-${STAMP}.dump"
  TMP_IN_CONTAINER="/tmp/pgbackup-${DB}.dump"
  log "dump 库 $DB"

  # dump 与验证都在**容器内的真实文件路径**上做，然后才 docker cp 出来。
  #
  # 为什么不直接把 pg_dump 的 stdout 重定向到宿主文件再验：custom format 是可随机
  # 访问的归档，`pg_restore -l` 需要在文件里 seek，而管道/`/dev/stdin` 不可 seek，
  # 于是验证会对**完全正常**的 dump 报错。宿主也没装 pg_restore。
  # 第一版就是这么写的，结果四个库的好档全被判成坏档删掉了。

  # -Fc = custom format：自带压缩，且支持 pg_restore 单表/选择性恢复。
  if ! docker exec "$CONTAINER" sh -c "pg_dump -U '$PGUSER' -Fc --no-owner --no-acl '$DB' > '$TMP_IN_CONTAINER'" 2>/dev/null; then
    docker exec "$CONTAINER" rm -f "$TMP_IN_CONTAINER" 2>/dev/null || true
    log "  ✗ $DB dump 失败"
    FAILED="$FAILED $DB"
    continue
  fi

  # 立刻验证这个 dump 真能被 pg_restore 读懂（用真实路径，可 seek）。
  # 不验的话，坏档要等到真出事去恢复时才发现 —— 那时已经太晚。
  if ! docker exec "$CONTAINER" pg_restore -l "$TMP_IN_CONTAINER" >/dev/null 2>&1; then
    docker exec "$CONTAINER" rm -f "$TMP_IN_CONTAINER" 2>/dev/null || true
    log "  ✗ $DB dump 产出无法被 pg_restore 解析，判为坏档已丢弃"
    FAILED="$FAILED $DB"
    continue
  fi

  # 搬到宿主：先落 .part 再原子 mv，崩在半路不会留下看着完整的截断文件。
  if ! docker cp "${CONTAINER}:${TMP_IN_CONTAINER}" "${OUT}.part" 2>/dev/null; then
    rm -f "${OUT}.part"
    docker exec "$CONTAINER" rm -f "$TMP_IN_CONTAINER" 2>/dev/null || true
    log "  ✗ $DB 从容器复制出来失败"
    FAILED="$FAILED $DB"
    continue
  fi
  docker exec "$CONTAINER" rm -f "$TMP_IN_CONTAINER" 2>/dev/null || true

  mv "${OUT}.part" "$OUT"
  log "  ✓ $(basename "$OUT") ($(du -h "$OUT" | cut -f1))"

  # 周日额外留一份长期档
  if (( IS_SUNDAY )); then
    cp -p "$OUT" "$WEEKLY_DIR/$(basename "$OUT")"
  fi
done

if (( IS_SUNDAY )); then
  cp -p "$GLOBALS" "$WEEKLY_DIR/$(basename "$GLOBALS")" 2>/dev/null || true
  log "周日：已复制一份到 weekly/"
fi

# --- 清理旧档 ---------------------------------------------------------------
#
# 按文件名里的时间戳算「第几份」，而不是按 mtime —— cp -p 保留时间戳，
# 用 mtime 排序会把 weekly 的副本排错位置。

prune() {
  local dir="$1" keep="$2" pattern="$3"
  local n
  # shellcheck disable=SC2012
  n=$(ls -1 "$dir"/$pattern 2>/dev/null | wc -l)
  if (( n > keep )); then
    # shellcheck disable=SC2012
    ls -1 "$dir"/$pattern 2>/dev/null | sort | head -n "$(( n - keep ))" | while read -r old; do
      rm -f "$old"
      log "  清理 $(basename "$old")"
    done
  fi
}

log "清理旧档（daily 保留 ${KEEP_DAILY} 份 / weekly 保留 ${KEEP_WEEKLY} 份，按库分别计数）"
for DB in $DATABASES globals; do
  DB=$(echo "$DB" | tr -d '\r')
  [[ -n "$DB" ]] || continue
  if [[ "$DB" == "globals" ]]; then
    prune "$DAILY_DIR" "$KEEP_DAILY" "globals-*.sql"
    prune "$WEEKLY_DIR" "$KEEP_WEEKLY" "globals-*.sql"
  else
    prune "$DAILY_DIR" "$KEEP_DAILY" "${DB}-*.dump"
    prune "$WEEKLY_DIR" "$KEEP_WEEKLY" "${DB}-*.dump"
  fi
done

# --- 收尾 -------------------------------------------------------------------

log "当前备份占用：$(du -sh "$BACKUP_ROOT" | cut -f1)"

if [[ -n "$FAILED" ]]; then
  fail "以下库备份失败：$FAILED"
fi

log "=== ✓ 备份完成 ==="
