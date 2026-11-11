#!/usr/bin/env bash
# 一键发布：本机构建 → 传镜像 → 服务器加载 → 重启服务。
#
#   ./deploy/publish.sh user@1.2.3.4
#   ./deploy/publish.sh user@1.2.3.4 --path /opt/gym
#   ./deploy/publish.sh user@1.2.3.4 --config      # 首次部署，连 compose 和 .env 一起传
#   ./deploy/publish.sh user@1.2.3.4 --skip-build  # 复用上次的 tar
#
# 设计取舍：
# - 默认不传 .env，避免用开发机的测试配置覆盖服务器上的生产密钥。
#   服务器缺配置时脚本会报错并提示加 --config，而不是悄悄传上去。
# - 服务器侧只用 docker load + docker compose up，不带 --build：
#   2G 机器上构建会 OOM，而且服务器上根本没有源码。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAR="$ROOT/deploy/dist/gym-images.tar"
COMPOSE="deploy/docker-compose.yml"
REMOTE_PATH="/opt/gym"
SKIP_BUILD=0
SYNC_CONFIG=0
REMOTE=""

usage() {
  cat <<'EOF'
用法: ./deploy/publish.sh <user@host> [选项]

选项:
  --path <dir>   服务器上的仓库目录，默认 /opt/gym
  --config       同时上传 deploy/docker-compose.yml 和 .env（首次部署用）
  --skip-build   跳过构建，直接发布已有的 deploy/dist/gym-images.tar
  -h, --help     显示帮助
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --path)       [ -n "${2:-}" ] || { echo "--path 缺少参数"; exit 1; }; REMOTE_PATH="$2"; shift 2 ;;
    --config)     SYNC_CONFIG=1; shift ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help)    usage; exit 0 ;;
    -*)           echo "未知参数: $1"; usage; exit 1 ;;
    *)            REMOTE="$1"; shift ;;
  esac
done

[ -n "$REMOTE" ] || { usage; exit 1; }

# 目标目录必须是绝对路径，否则后面 ssh 里 cd 会相对到登录用户的 home，位置不可预期
case "$REMOTE_PATH" in
  /*) ;;
  *) echo "--path 必须写成绝对路径，例如 /opt/gym"; exit 1 ;;
esac

echo "==> 检查到 $REMOTE 的连通性"
ssh -o ConnectTimeout=8 "$REMOTE" 'command -v docker >/dev/null' \
  || { echo "连不上 ${REMOTE}，或对方没装 docker"; exit 1; }

if [ "$SKIP_BUILD" -eq 1 ]; then
  [ -f "$TAR" ] || { echo "--skip-build 但没有 ${TAR}，先去掉该参数完整构建一次"; exit 1; }
  echo "==> 跳过构建，复用 $(ls -lh "$TAR" | awk '{print $5}') 的镜像包"
else
  echo "==> 本机构建"
  "$ROOT/deploy/build.sh"
fi

echo "==> 准备远程目录 $REMOTE_PATH"
# compose 相对路径是 deploy/xxx，子目录要先建好，否则 scp 会报 No such file
ssh "$REMOTE" "mkdir -p '$REMOTE_PATH/deploy'"

if [ "$SYNC_CONFIG" -eq 1 ]; then
  echo "==> 上传 compose 与 .env"
  scp "$ROOT/$COMPOSE" "$REMOTE:$REMOTE_PATH/$COMPOSE"
  if [ -f "$ROOT/.env" ]; then
    scp "$ROOT/.env" "$REMOTE:$REMOTE_PATH/.env"
  else
    echo "    本地没有 .env，跳过（服务器需要自己准备）"
  fi
else
  # 关键前置：服务器上只有镜像、没有源码，compose 和 .env 必须在
  if ! ssh "$REMOTE" "test -f '$REMOTE_PATH/$COMPOSE' && test -f '$REMOTE_PATH/.env'"; then
    echo "$REMOTE_PATH 下缺 $COMPOSE 或 .env"
    echo "首次部署请加 --config 上传，或手动放好后重试"
    exit 1
  fi
fi

echo "==> 上传镜像包"
scp "$TAR" "$REMOTE:/tmp/gym-images.tar"

echo "==> 服务器加载镜像"
ssh "$REMOTE" 'docker load -i /tmp/gym-images.tar && rm -f /tmp/gym-images.tar'

echo "==> 重建并启动服务"
ssh "$REMOTE" "set -e; cd '$REMOTE_PATH' && docker compose --env-file .env -f $COMPOSE up -d"

echo "==> 服务状态"
ssh "$REMOTE" "cd '$REMOTE_PATH' && docker compose --env-file .env -f $COMPOSE ps"
echo
ssh "$REMOTE" "docker stats --no-stream --format '{{.Name}}: {{.MemUsage}}'"

echo
echo "发布完成。跟踪日志："
echo "  ssh $REMOTE \"cd $REMOTE_PATH && docker compose --env-file .env -f $COMPOSE logs -f api\""
