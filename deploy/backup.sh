#!/usr/bin/env bash
# 数据库逻辑备份：容器内 mysqldump → gzip → 落到宿主机 backups/，按天保留。
#
#   ./deploy/backup.sh                     # 备份一次，保留 7 天
#   ./deploy/backup.sh --keep 14           # 保留 14 天
#   ./deploy/backup.sh --out /data/bak     # 换输出目录（建议挂块数据盘）
#   ./deploy/backup.sh --install-cron      # 装进 root crontab，每天 03:30 自动跑
#
# 恢复（dump 里带了 CREATE DATABASE 和 DROP TABLE IF EXISTS，会覆盖同名库的表）：
#   gunzip -c backups/gym_20261113_033000.sql.gz \
#     | docker compose --env-file .env -f deploy/docker-compose.yml exec -T mysql mysql -uroot -p
#
# 设计取舍：
# - 用逻辑备份而不是 xtrabackup 物理备份：数据量在百 MB 级，逻辑备份体积小、能单表
#   恢复、出问题时可以直接 gzip -dc 出来看内容；物理备份要额外装工具、占空间也大得多。
# - --single-transaction 走 InnoDB 一致性快照，全程不锁表，业务写入不受影响；
#   --quick 让服务端逐行发送，不把整张表先攒进内存 —— 2G 机器上这条很关键。
# - 先写 .tmp 再 mv：中途 OOM 或断连不会留下一个大小正常、内容却被截断的 .gz，
#   那种文件在真需要恢复的那天才发现，等于没备份。
# - 清理只匹配自己生成的 ${DB_NAME}_*.sql.gz，不会碰 backups/ 里手工导的那些 .sql。
# - 密码通过容器内的 MYSQL_PWD 传，不出现在 mysqldump 的 argv 里（docker exec 的 argv
#   里仍可见，但执行者是本机 root，本来就能读 .env，没有额外暴露面）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_ARGS="-f deploy/docker-compose.yml"
BACKUP_DIR="$ROOT/backups"
KEEP_DAYS=7
DB_NAME="gym"
INSTALL_CRON=0
CRON_SPEC="30 3 * * *"
LOG_MAX_BYTES=1048576

usage() {
  cat <<'EOF'
用法: ./deploy/backup.sh [选项]

选项:
  --keep <天数>   保留最近多少天的备份，默认 7；给 0 表示永不清理
  --out <目录>    备份输出目录，默认 <仓库>/backups
  --db <库名>     要备份的库，默认 gym
  --install-cron  把本脚本写进 root crontab（每天 03:30 执行），已存在则跳过
  -h, --help      显示帮助

恢复:
  gunzip -c backups/gym_<时间戳>.sql.gz \
    | docker compose --env-file .env -f deploy/docker-compose.yml exec -T mysql mysql -uroot -p

注意: 备份文件默认落在同一块盘上，磁盘故障时会连备份一起丢。
      重要数据另外 scp 回本地，或上传到对象存储。
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --keep)         [ -n "${2:-}" ] || { echo "--keep 缺少参数"; exit 1; }; KEEP_DAYS="$2"; shift 2 ;;
    --out)          [ -n "${2:-}" ] || { echo "--out 缺少参数"; exit 1; }; BACKUP_DIR="$2"; shift 2 ;;
    --db)           [ -n "${2:-}" ] || { echo "--db 缺少参数"; exit 1; }; DB_NAME="$2"; shift 2 ;;
    --install-cron) INSTALL_CRON=1; shift ;;
    -h|--help)      usage; exit 0 ;;
    *)              echo "未知参数: $1"; usage; exit 1 ;;
  esac
done

case "$KEEP_DAYS" in
  ''|*[!0-9]*) echo "--keep 必须是数字"; exit 1 ;;
esac

# compose 的相对路径（../.env、deploy/Dockerfile）都基于文件位置，必须从仓库根执行
cd "$ROOT"

dc() { docker compose --env-file .env $COMPOSE_ARGS "$@"; }

# 与 publish.sh 一致的取法：.env 只做简单文本解析，不 source —— 值里带空格或 # 时
# source 会截断，甚至执行到意料之外的东西
env_value() {
  sed -n "s/^[[:space:]]*$1=//p" .env | tail -1 | tr -d "\"'"
}

[ -f .env ] || { echo "$ROOT/.env 不存在，先按 .env.production.example 准备生产配置"; exit 1; }
command -v docker >/dev/null || { echo "没装 docker"; exit 1; }

ROOT_PW="$(env_value MYSQL_ROOT_PASSWORD)"
[ -n "$ROOT_PW" ] || { echo ".env 里 MYSQL_ROOT_PASSWORD 为空，无法备份"; exit 1; }

# 容器没起时 mysqldump 只会抛一句 "service mysql is not running"，不如提前说清楚
if [ -z "$(dc ps -q mysql)" ]; then
  echo "mysql 容器没在运行，先起服务："
  echo "  docker compose --env-file .env $COMPOSE_ARGS up -d"
  exit 1
fi

mkdir -p "$BACKUP_DIR"

# 同一时刻只允许一个备份在跑：cron 撞上手工执行时，两个 mysqldump 会把 2G 内存挤爆
LOCK="$BACKUP_DIR/.backup.lock"
LOCK_MODE=""
if command -v flock >/dev/null 2>&1; then
  exec 9>"$LOCK"
  if ! flock -n 9; then
    echo "已有一个备份在跑，本次跳过"
    exit 0
  fi
  LOCK_MODE=flock
else
  # 没有 flock（macOS 默认就没有）时退化成 mkdir 原子锁。这里必须区分“抢不到锁”和
  # “工具不存在”——把后者也报成前者，会让人以为有进程在跑，实际一次都没备份成。
  # 目录名带 .d 后缀，避免和 flock 那条路径留下的 .backup.lock 文件撞名
  if ! mkdir "$LOCK.d" 2>/dev/null; then
    echo "已有一个备份在跑，本次跳过"
    echo "（确认没有进程在跑时，删掉 $LOCK.d 再重试）"
    exit 0
  fi
  LOCK_MODE=mkdir
fi

TS="$(date +%Y%m%d_%H%M%S)"
OUT="$BACKUP_DIR/${DB_NAME}_${TS}.sql.gz"
TMP="$OUT.tmp"

cleanup() {
  rm -f "$TMP"
  # mkdir 锁不会随进程退出自动释放，异常退出全靠这里清掉；被 kill -9 时只能手工删目录
  if [ "$LOCK_MODE" = "mkdir" ]; then rmdir "$LOCK.d" 2>/dev/null || true; fi
}
trap cleanup EXIT

echo "==> 备份 $DB_NAME → $OUT"
# set +e 是为了拿到 PIPESTATUS：pipefail 只知道“失败了”，不知道是 mysqldump 报错
# 还是 gzip 写满磁盘，日志里分清楚才好排查
set +e
dc exec -T -e MYSQL_PWD="$ROOT_PW" mysql \
  mysqldump -uroot \
    --single-transaction \
    --quick \
    --no-tablespaces \
    --routines \
    --triggers \
    --events \
    --set-gtid-purged=OFF \
    --default-character-set=utf8mb4 \
    --databases "$DB_NAME" | gzip -6 >"$TMP"
# 必须一次性抓走整个数组：下一条命令（哪怕是赋值）都会把 PIPESTATUS 刷新掉
RC=("${PIPESTATUS[@]}")
RC_MYSQLDUMP="${RC[0]}"
RC_GZIP="${RC[1]:-0}"
set -e

if [ "$RC_MYSQLDUMP" -ne 0 ] || [ "$RC_GZIP" -ne 0 ]; then
  echo "备份失败：mysqldump=$RC_MYSQLDUMP gzip=$RC_GZIP"
  echo "dump 非 0 多为口令错/库不存在；gzip 非 0 多为磁盘满，先 df -h 看一眼"
  exit 1
fi

# 两道校验：压缩包完整性 + dump 的结束标记。mysqldump 中途被杀时最后一行没有
# "-- Dump completed"，而 gzip -t 依然能过（截断的流未必报错），所以两个都要查
if ! gzip -t "$TMP" 2>/dev/null; then
  echo "压缩包损坏，已丢弃"
  exit 1
fi
# gzip -dc 而不是 zcat：macOS 的 /usr/bin/zcat 是老的 compress 版本，会去找 xxx.gz.Z，
# 读出来是空的，检查会误判成“备份不完整”
TAIL_TEXT="$(gzip -dc "$TMP" 2>/dev/null | tail -n 5 || true)"
if ! printf '%s' "$TAIL_TEXT" | grep -q "Dump completed"; then
  echo "备份不完整（缺少 Dump completed 结尾标记），已丢弃"
  exit 1
fi

mv "$TMP" "$OUT"
SIZE="$(ls -lh "$OUT" | awk '{print $5}')"
echo "==> 完成，$SIZE"

if [ "$KEEP_DAYS" -gt 0 ]; then
  echo "==> 清理 ${KEEP_DAYS} 天前的备份"
  find "$BACKUP_DIR" -maxdepth 1 -type f -name "${DB_NAME}_*.sql.gz" -mtime +"$KEEP_DAYS" -print -delete
  echo "    当前保留 $(find "$BACKUP_DIR" -maxdepth 1 -type f -name "${DB_NAME}_*.sql.gz" | wc -l | tr -d ' ') 份"
fi

# cron 输出重定向到 backup.log，不截断的话一年后能吃掉几个 G 的盘
LOG="$BACKUP_DIR/backup.log"
if [ -f "$LOG" ] && [ "$(wc -c <"$LOG" | tr -d ' ')" -gt "$LOG_MAX_BYTES" ]; then
  tail -c "$((LOG_MAX_BYTES / 2))" "$LOG" >"$LOG.tmp" && mv "$LOG.tmp" "$LOG"
fi

if [ "$INSTALL_CRON" -eq 1 ]; then
  LINE="$CRON_SPEC $ROOT/deploy/backup.sh >> $LOG 2>&1"
  if crontab -l 2>/dev/null | grep -Fq "$ROOT/deploy/backup.sh"; then
    echo "==> crontab 里已有该脚本的条目，跳过"
  else
    { crontab -l 2>/dev/null || true; echo "$LINE"; } | crontab -
    echo "==> 已写入 root crontab：$LINE"
  fi
fi

echo
echo "本机恢复："
echo "  gunzip -c $OUT | docker compose --env-file .env $COMPOSE_ARGS exec -T mysql mysql -uroot -p"
echo "异地留一份（备份和数据库在同一块盘上，盘坏了会一起没）："
echo "  scp $OUT <你的电脑>:$HOME/gym-backups/"
