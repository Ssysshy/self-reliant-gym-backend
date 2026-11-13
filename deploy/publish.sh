#!/usr/bin/env bash
# 一键发布：本机构建 → 传镜像 → 服务器加载 → 重启服务。
#
#   ./deploy/publish.sh user@1.2.3.4
#   ./deploy/publish.sh user@1.2.3.4 --path /opt/gym
#   ./deploy/publish.sh user@1.2.3.4 --config      # 首次部署，连 compose 和 .env 一起传
#   ./deploy/publish.sh user@1.2.3.4 --config --env .env.production   # 传生产配置
#   ./deploy/publish.sh user@1.2.3.4 --skip-build  # 复用上次的 tar
#
# 设计取舍：
# - 默认不传 .env，避免用开发机的测试配置覆盖服务器上的生产密钥。
#   服务器缺配置时脚本会报错并提示加 --config，而不是悄悄传上去。
# - --config 上传的 .env 会先过一遍生产校验（GYM_LOCAL、WX_SECRET、SESSION_SECRET 长度），
#   本地那份开发配置直接传上去会让所有用户变成同一个测试账号，必须在传之前拦住。
# - 服务器侧只用 docker load + docker compose up，不带 --build：
#   2G 机器上构建会 OOM，而且服务器上根本没有源码。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAR="$ROOT/deploy/dist/gym-images.tar"
COMPOSE="deploy/docker-compose.yml"
REMOTE_PATH="/opt/gym"
ENV_SRC=".env"
SKIP_BUILD=0
SYNC_CONFIG=0
REMOTE=""

usage() {
  cat <<'EOF'
用法: ./deploy/publish.sh <user@host> [选项]

选项:
  --path <dir>   服务器上的仓库目录，默认 /opt/gym
  --config       同时上传 compose、.env，以及 deploy/certs 里备好的证书（首次部署用）
  --env <file>   配合 --config 指定上传哪份配置，默认 .env
                 生产部署用 --env .env.production（由 .env.production.example 复制而来）
  --skip-build   跳过构建，直接发布已有的 deploy/dist/gym-images.tar
  -h, --help     显示帮助
EOF
}

# 拒绝把本地开发配置传到服务器：这类事故不会报错，只会让所有人悄悄变成同一个用户
check_env_for_prod() {
  local f="$1" key value
  [ -f "$f" ] || { echo "$f 不存在"; return 1; }

  if grep -q '__CHANGE_ME' "$f"; then
    echo "$f 里还有 __CHANGE_ME 占位符没替换，先把口令和密钥填成真实值。"
    return 1
  fi

  if grep -qE '^[[:space:]]*GYM_LOCAL=1[[:space:]]*$' "$f"; then
    echo "$f 里 GYM_LOCAL=1，这是本地开发配置：没配 WX_SECRET 时登录会退化成固定测试账号，"
    echo "线上所有人都会变成同一个用户。请把 GYM_LOCAL 改成 0 再发布。"
    return 1
  fi

  value="$(sed -n 's/^[[:space:]]*WX_SECRET=//p' "$f" | tail -1 | tr -d "\"'")"
  if [ -z "$value" ]; then
    echo "$f 里 WX_SECRET 为空，线上无法用微信登录（接口会返回「微信登录失败」）。"
    echo "去微信公众平台 → 开发管理 → 开发设置 → AppSecret 取值填进去。"
    return 1
  fi

  value="$(sed -n 's/^[[:space:]]*SESSION_SECRET=//p' "$f" | tail -1 | tr -d "\"'")"
  if [ "${#value}" -lt 32 ]; then
    echo "$f 里 SESSION_SECRET 不足 32 字节，进程会直接退出。生成：openssl rand -base64 48"
    return 1
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --path)       [ -n "${2:-}" ] || { echo "--path 缺少参数"; exit 1; }; REMOTE_PATH="$2"; shift 2 ;;
    --env)        [ -n "${2:-}" ] || { echo "--env 缺少参数"; exit 1; }; ENV_SRC="$2"; shift 2 ;;
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
  # 先校验再传：免得 compose 已经覆盖、配置却传了份不能用的，服务器停在半坏状态
  if [ -f "$ROOT/$ENV_SRC" ]; then
    echo "    校验 $ENV_SRC"
    check_env_for_prod "$ROOT/$ENV_SRC"
  fi

  scp "$ROOT/$COMPOSE" "$REMOTE:$REMOTE_PATH/$COMPOSE"
  # 本地备好证书才认为要开 HTTPS：没证书就传叠加文件，nginx 挂不上证书会直接起不来。
  # 三样东西必须成套传（证书 + 叠加文件 + https 版 nginx 配置），少一样都会 502 或启动失败
  if [ -f "$ROOT/deploy/certs/fullchain.pem" ] && [ -f "$ROOT/deploy/certs/privkey.pem" ]; then
    echo "    检测到 deploy/certs，同时上传 HTTPS 配置"
    ssh "$REMOTE" "mkdir -p '$REMOTE_PATH/deploy/certs'"
    scp "$ROOT/deploy/certs/fullchain.pem" "$ROOT/deploy/certs/privkey.pem" "$REMOTE:$REMOTE_PATH/deploy/certs/"
    scp "$ROOT/deploy/docker-compose.https.yml" "$REMOTE:$REMOTE_PATH/deploy/docker-compose.https.yml"
    scp "$ROOT/deploy/nginx.https.conf" "$REMOTE:$REMOTE_PATH/deploy/nginx.https.conf"
  fi
  if [ -f "$ROOT/$ENV_SRC" ]; then
    # 本地文件叫什么无所谓，服务器上必须是 .env：compose 的 --env-file 和 go 的 LoadDotEnv 都认这个名字
    scp "$ROOT/$ENV_SRC" "$REMOTE:$REMOTE_PATH/.env"
    echo "    $ENV_SRC → 服务器 .env"
  else
    echo "    本地没有 $ENV_SRC，跳过（服务器需要自己准备）"
  fi
else
  # 关键前置：服务器上只有镜像、没有源码，compose 和 .env 必须在
  if ! ssh "$REMOTE" "test -f '$REMOTE_PATH/$COMPOSE' && test -f '$REMOTE_PATH/.env'"; then
    echo "$REMOTE_PATH 下缺 $COMPOSE 或 .env"
    echo "首次部署请加 --config 上传，或手动放好后重试"
    exit 1
  fi
fi

# 服务器上已启用 HTTPS 时自动带上叠加文件。
# 只跑基础 compose 会重建 web 容器，把 80/443 和证书挂载一起丢掉——这种"发布完 https 没了"
# 的故障排查成本很高，所以按文件存在与否判断，而不是要求每次都记得加参数
COMPOSE_ARGS="-f $COMPOSE"
if ssh "$REMOTE" "test -f '$REMOTE_PATH/deploy/docker-compose.https.yml'"; then
  COMPOSE_ARGS="$COMPOSE_ARGS -f deploy/docker-compose.https.yml"
  echo "==> 检测到 HTTPS 叠加配置，按 80/443 方式启动"
fi

echo "==> 上传镜像包"
scp "$TAR" "$REMOTE:/tmp/gym-images.tar"

echo "==> 服务器加载镜像"
ssh "$REMOTE" 'docker load -i /tmp/gym-images.tar && rm -f /tmp/gym-images.tar'

echo "==> 重建并启动服务"
ssh "$REMOTE" "set -e; cd '$REMOTE_PATH' && docker compose --env-file .env $COMPOSE_ARGS up -d"

echo "==> 服务状态"
ssh "$REMOTE" "cd '$REMOTE_PATH' && docker compose --env-file .env $COMPOSE_ARGS ps"
echo
ssh "$REMOTE" "docker stats --no-stream --format '{{.Name}}: {{.MemUsage}}'"

echo
echo "发布完成。跟踪日志："
echo "  ssh $REMOTE \"cd $REMOTE_PATH && docker compose --env-file .env $COMPOSE_ARGS logs -f api\""
