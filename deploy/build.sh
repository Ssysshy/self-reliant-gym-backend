#!/usr/bin/env bash
# 在本机构建 linux/amd64 镜像，产出一个可直接拷到服务器的 tar。
#
# 为什么不在服务器上 build：2C2G 跑 pnpm install + tsc + vite build 会 OOM。
# 为什么不直接 docker build：Apple Silicon 上不指定平台会产出 arm64 镜像，
# 传到 x86 服务器上会 exec format error；而用 --platform linux/amd64 走 QEMU
# 模拟编译，node 工具链容易段错误且要十几分钟。
# 所以拆成两步：本机原生编译出架构无关的产物（静态页 + 静态链接的 Go 二进制），
# 再纯 COPY 打成 amd64 镜像。
#
# 用法：
#   ./deploy/build.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="$ROOT/deploy/dist"
TAR="$STAGE/gym-images.tar"
PLATFORM="linux/amd64"
# 必须与 deploy/docker-compose.yml 的 name + service 推导结果一致，
# 否则服务器上 compose 认不出镜像，还是会自己 build。
API_IMAGE="self-reliant-gym-backend-api:latest"
WEB_IMAGE="self-reliant-gym-backend-web:latest"

command -v go >/dev/null || { echo "缺少 go"; exit 1; }
command -v pnpm >/dev/null || { echo "缺少 pnpm"; exit 1; }
command -v docker >/dev/null || { echo "缺少 docker"; exit 1; }

rm -rf "$STAGE/web" "$STAGE/gym"
mkdir -p "$STAGE"

echo "==> 1/3 前端静态页"
cd "$ROOT/apps/frontend"
# 锁文件变了才需要重装，正常情况直接复用 node_modules
if [ ! -d node_modules ]; then pnpm install --frozen-lockfile; fi
pnpm exec tsc --noEmit
pnpm exec vite build --outDir "$STAGE/web" --emptyOutDir

echo "==> 2/3 Go 二进制（linux/amd64，静态链接）"
cd "$ROOT/apps/backend"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$STAGE/gym" ./cmd/api
file "$STAGE/gym" | sed 's/^/    /'

echo "==> 3/3 打包镜像"
cd "$ROOT"
docker buildx build --platform "$PLATFORM" --provenance=false \
  -f deploy/Dockerfile.pack --target api -t "$API_IMAGE" --load .
docker buildx build --platform "$PLATFORM" --provenance=false \
  -f deploy/Dockerfile.pack --target web -t "$WEB_IMAGE" --load .

docker save -o "$TAR" "$API_IMAGE" "$WEB_IMAGE"
echo
echo "    架构校验："
docker image inspect "$API_IMAGE" --format '    api={{.Architecture}}/{{.Os}} size={{.Size}}'
docker image inspect "$WEB_IMAGE" --format '    web={{.Architecture}}/{{.Os}} size={{.Size}}'
ls -lh "$TAR" | sed 's/^/    /'

cat <<EOF

已生成 ${TAR}，拷到服务器执行：

  scp $TAR user@<你的服务器>:/tmp/gym-images.tar
  ssh user@<你的服务器> 'docker load -i /tmp/gym-images.tar'
  ssh user@<你的服务器> 'cd <仓库路径> && docker compose --env-file .env -f deploy/docker-compose.yml up -d'

注意 up 时不要带 --build，否则 compose 发现镜像已存在也会重新构建。
EOF
