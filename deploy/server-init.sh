#!/usr/bin/env bash
# 服务器一次性初始化：swap + 内核参数。只在该机器上执行一次，可重复执行（幂等）。
#   sudo ./deploy/server-init.sh
#
# 只在 Linux 服务器上跑，不要在 macOS 开发机执行。
#
# 作用：
#   1. 没有 swap 时创建 2GB swapfile 并写进 fstab（重启后仍生效）
#   2. 把 vm.swappiness 降到 10 —— 默认 60 会在内存还宽裕时就把 MySQL 的
#      buffer pool 换出到磁盘，查询延迟直接从毫秒级跳到几十毫秒
set -euo pipefail

SWAP_FILE=/swapfile
SWAP_MB=2048

[ "$(uname -s)" = "Linux" ] || { echo "这个脚本只用于 Linux 服务器"; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "请用 sudo 执行：sudo $0"; exit 1; }

echo "==> 初始化前"
free -h

if swapon --show | grep -q .; then
  echo "==> 已存在 swap，跳过创建"
else
  echo "==> 创建 ${SWAP_MB}MB swapfile"
  # 某些文件系统（部分云盘挂载的 overlay/xfs 配置）不支持 fallocate，退回 dd
  if ! fallocate -l "${SWAP_MB}M" "$SWAP_FILE" 2>/dev/null; then
    echo "    fallocate 不可用，改用 dd（稍慢）"
    dd if=/dev/zero of="$SWAP_FILE" bs=1M count="$SWAP_MB" status=none
  fi
  chmod 600 "$SWAP_FILE"
  mkswap "$SWAP_FILE" >/dev/null
  swapon "$SWAP_FILE"
  # 幂等：fstab 里已有同一条目就不再追加
  grep -q "^${SWAP_FILE}[[:space:]]" /etc/fstab || echo "${SWAP_FILE} none swap sw 0 0" >> /etc/fstab
  echo "    已写入 /etc/fstab，重启后自动挂载"
fi

echo "==> 设置 vm.swappiness=10"
if grep -q '^vm.swappiness' /etc/sysctl.conf 2>/dev/null; then
  sed -i 's/^vm.swappiness.*/vm.swappiness=10/' /etc/sysctl.conf
else
  echo 'vm.swappiness=10' >> /etc/sysctl.conf
fi
sysctl -w vm.swappiness=10 >/dev/null

echo
echo "==> 初始化后"
free -h
echo "swappiness = $(cat /proc/sys/vm/swappiness)"
echo
echo "接下来起服务："
echo "  docker compose --env-file .env -f deploy/docker-compose.yml up -d"
echo "起来后看容器实际占用："
echo "  docker stats --no-stream"
