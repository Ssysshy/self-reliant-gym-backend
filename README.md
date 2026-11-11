# self-reliant-gym-backend
self-reliant-gym-backend，support for that miniprogram

## 本地开发

Go 模块在 `apps/backend`，命令必须在那一层执行（仓库根目录没有 `go.mod`）：

```bash
# 1. 准备本机 MySQL：建库和账号（只需执行一次）
mysql -uroot -p -e "CREATE DATABASE IF NOT EXISTS gym DEFAULT CHARSET utf8mb4;
  CREATE USER IF NOT EXISTS 'gym'@'localhost' IDENTIFIED BY 'gymdev123';
  GRANT ALL PRIVILEGES ON gym.* TO 'gym'@'localhost'; FLUSH PRIVILEGES;"

# 2. 准备配置（.env 已被 .gitignore 忽略，从模板复制后填值）
cp .env.example .env

# 3. 启动 API，启动时自动加载 .env 并执行迁移，监听 18080
cd apps/backend && go run ./cmd/api
```

`.env` 中 `MYSQL_DSN` 默认指向 `127.0.0.1:3306`，按本机 MySQL 的账号口令改；`SESSION_SECRET` 至少 32 字节。国内网络下载依赖建议设置 `go env -w GOPROXY=https://goproxy.cn,direct`。

前端后台在 `apps/frontend`，`pnpm dev` 启动后由 Vite 把 `/api` 代理到 `127.0.0.1:18080`。

## 容器部署

部署文件都集中在 `deploy/`：`Dockerfile`（多阶段，一次构建产出 `web` 和 `api` 两个镜像）、`docker-compose.yml`、`nginx.conf`。

构建上下文是**仓库根**（`Dockerfile` 里有 `COPY apps/backend/`、`COPY deploy/nginx.conf`），所以命令必须在仓库根目录执行。Compose 默认只读 compose 文件同级的 `.env`，因此要显式带上 `--env-file`：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
docker compose --env-file .env -f deploy/docker-compose.yml logs -f api
docker compose --env-file .env -f deploy/docker-compose.yml down
```

访问 `http://127.0.0.1:18080`：Nginx 托管后台静态页，并把 `/api/` 反向代理给 `api` 服务。

编排包含 `mysql`、`api`、`web` 三个服务。MySQL 容器只用 `expose` 在容器网络内暴露 3306，不发布到宿主机——避免和本机 MySQL 抢端口。

`.env` 里的 `MYSQL_DSN` 写的是 `127.0.0.1:3306`，**这只对宿主机上的 `go run` 有效**：容器里的 `127.0.0.1` 指向容器自身。所以 compose 用 `environment.MYSQL_DSN` 把它覆盖成服务名：

```yaml
MYSQL_DSN: gym:${MYSQL_PASSWORD}@tcp(mysql:3306)/gym?charset=utf8mb4&parseTime=true&loc=UTC
```

同一个 `.env` 在两个环境各司其职，不需要改。`MYSQL_PASSWORD` 与 `MYSQL_ROOT_PASSWORD` 在 compose 里是 `${VAR:?}`，为空会直接报错退出——`mysql:8.4` 镜像初始化 root 账号必须用到后者，本地 `go run` 则不读它。

`.env`、`.env.example`、`.dockerignore` 保持留在仓库根：前两个要同时被 compose 和 `go run` 读到（`config.LoadDotEnv` 只向上查找），最后一个必须作用于构建上下文根。

## 本机构建，服务器只运行

服务器如果是 2C2G，在上面跑 `pnpm install` + `tsc` + `vite build` 会和 MySQL 抢内存直接把机器打死；而在 Apple Silicon 上直接 `docker build` 产出的是 arm64 镜像，传到 x86 服务器会 `exec format error`。用 `--platform linux/amd64` 走 QEMU 模拟编译又慢又容易崩。

所以构建拆成两步，全在本机原生完成：

```bash
./deploy/build.sh
```

脚本做三件事：

1. `pnpm exec tsc --noEmit && vite build` → `deploy/dist/web`（静态页与 CPU 架构无关）
2. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` → `deploy/dist/gym`（静态链接的 x86-64 二进制）
3. `deploy/Dockerfile.pack` 只做 `COPY`，打成 amd64 镜像并 `docker save` 成 `deploy/dist/gym-images.tar`（约 42MB）

`Dockerfile.pack` 里没有任何编译步骤，构建只需几秒，也不需要模拟。仓库根原有的 `deploy/Dockerfile` 保留，给有完整构建资源的机器（CI 等）用。

服务器侧：

```bash
scp deploy/dist/gym-images.tar user@<服务器>:/tmp/
ssh user@<服务器> 'docker load -i /tmp/gym-images.tar'
ssh user@<服务器> 'cd <仓库路径> && docker compose --env-file .env -f deploy/docker-compose.yml up -d'
```

镜像 tag 是 `self-reliant-gym-backend-api:latest` 和 `-web:latest`，与 compose 由 `name` + 服务名推导出的名字一致，`docker load` 后 compose 直接复用。**`up` 时不要带 `--build`**，否则会无视已加载的镜像重新构建。

### 一键发布

上面那堆命令可以交给脚本：

```bash
./deploy/publish.sh user@1.2.3.4              # 日常发布
./deploy/publish.sh user@1.2.3.4 --config     # 首次部署，连 compose 和 .env 一起传
```

它按顺序做：连通性检查 → 调 `build.sh` 构建 → 建远程目录 → `scp` 上传镜像包 → `docker load` → `docker compose up -d`（不带 `--build`）→ 打印 `ps` 和内存占用。任何一步失败立即退出，不会留下半启动状态。

| 参数 | 作用 |
|---|---|
| `--path <dir>` | 服务器上的目录，默认 `/opt/gym`，必须是绝对路径 |
| `--config` | 同时上传 `deploy/docker-compose.yml` 和 `.env`，首次部署用 |
| `--skip-build` | 跳过构建，直接发布已有的 `deploy/dist/gym-images.tar` |

两个刻意的设计：

- **默认不传 `.env`**。开发机上的 `.env` 通常是测试口令，直接覆盖服务器的生产密钥是会出事的，所以丢配置时脚本是报错退出、提示你加 `--config`，而不是悄悄传上去。
- **不用 `rsync`**，只用 `scp` + `ssh`，服务器不需要额外装东西。

建议先配好免密钥登录（`ssh-copy-id user@host`），否则连通性检查、建目录、传包、load、up、ps 每一步都要输一次密码。

### 基础镜像拉不动时

Docker Hub 直连超时、加速器失效的情况下（`docker.mirrors.ustc.edu.cn`、`hub-mirror.c.163.com` 等已停服），先从可用代理取镜像再打回标准标签，`build.sh` 就能离线复用：

```bash
docker pull --platform linux/amd64 docker.1ms.run/library/alpine:3
docker tag docker.1ms.run/library/alpine:3 alpine:3
docker pull --platform linux/amd64 docker.1ms.run/library/nginx:alpine
docker tag docker.1ms.run/library/nginx:alpine nginx:alpine
```

服务器上还需要 `mysql:8.4`，同样方式拉取，或单独 `docker save` 后一起 `docker load`。

## 2C2G 机器的内存调优

`deploy/docker-compose.yml` 已按 2G 内存配好预算，不需要再改：

| 服务 | mem_limit | 实测常驻 |
|---|---|---|
| mysql | 640m | 约 197MB（关掉 `performance_schema` 后） |
| api | 128m | 约 30MB |
| web | 64m | 约 12MB |

宿主机系统 + dockerd 约 350MB，容器合计上限 832MB，剩下 800MB 左右留给文件缓存。

MySQL 的几个启动参数都在 compose 的 `command` 里，改动前注意 `mysqld` 必须写在第一位 —— mysql 官方镜像的 `command` 会整体替换默认的 `CMD ["mysqld"]`：

- `--performance-schema=OFF`：省内存的大头。默认开启时会持续采集一万多个指标，实测常驻从约 450MB 降到 197MB。本项目没有依赖它做监控，关掉没有副作用。
- `--innodb-buffer-pool-size=128M`：8.4 默认就是这个值，显式写出来防止未来版本改默认值。
- `--max-connections=50`：默认 151，每个连接都占线程栈和缓冲区，这个业务的量级用不到。
- `--skip-name-resolve`：只按 IP 匹配，跳过反向 DNS。应用账号是 `'gym'@'%'`，不受影响。

### swap 与内核参数

首次部署时在服务器上跑一次（幂等，可重复执行）：

```bash
sudo ./deploy/server-init.sh
```

它会创建 2GB swapfile 并写入 `/etc/fstab`（已有 swap 则跳过），同时把 `vm.swappiness` 调到 10。默认值 60 会在内存还宽裕时就把 MySQL 的 buffer pool 换出到磁盘，查询延迟会从毫秒级跳到几十毫秒；10 表示只在真的吃紧时才用 swap，把它当兜底而不是日常资源。

起完服务后用 `docker stats --no-stream` 看实际占用，如果 mysql 长期贴着 640m 上限，说明数据量已经超出这台机器的容量，该考虑拆库了。
