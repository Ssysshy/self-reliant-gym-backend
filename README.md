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

两份环境模板，别用错：

| 文件 | 用途 | 关键差异 |
|---|---|---|
| `.env.example` | 本机开发，`cp .env.example .env` | `GYM_LOCAL=1`、`WX_SECRET` 可空（登录退化为固定测试账号）、`MYSQL_DSN` 指 `127.0.0.1:3306` |
| `.env.production.example` | 服务器 | `GYM_LOCAL=0`、`WX_SECRET` 必填、口令全换真值 |

服务器上 `GYM_LOCAL=1` 且 `WX_SECRET` 为空时，所有登录都会落到同一个测试用户 `local_dev_user`，数据串成一片——这是最容易漏的一条。

切环境不用改文件，用 `ENV_FILE` 指定读哪一份，默认仍是向上查找 `.env`：

```bash
cd apps/backend
go run ./cmd/api                                  # 读 .env（开发）
ENV_FILE=../../.env.production go run ./cmd/api   # 只读这份，指错路径直接退出，不退回 .env
```

`.env`、`.env.production`、`.env.local` 都在 `.gitignore` 里，只提交两个 `*.example` 模板。

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
./deploy/publish.sh user@1.2.3.4 --config --env .env.production   # 传生产配置
```

它按顺序做：连通性检查 → 调 `build.sh` 构建 → 建远程目录 → `scp` 上传镜像包 → `docker load` → `docker compose up -d`（不带 `--build`）→ 打印 `ps` 和内存占用。任何一步失败立即退出，不会留下半启动状态。

| 参数 | 作用 |
|---|---|
| `--path <dir>` | 服务器上的目录，默认 `/opt/gym`，必须是绝对路径 |
| `--config` | 同时上传 `deploy/docker-compose.yml`、配置文件和已备好的证书，首次部署用 |
| `--env <file>` | 配合 `--config` 指定上传哪份配置，默认 `.env`；生产部署用 `--env .env.production` |
| `--skip-build` | 跳过构建，直接发布已有的 `deploy/dist/gym-images.tar` |

三个刻意的设计：

- **默认不传 `.env`**。开发机上的 `.env` 通常是测试口令，直接覆盖服务器的生产密钥是会出事的，所以丢配置时脚本是报错退出、提示你加 `--config`，而不是悄悄传上去。
- **`--config` 上传前先校验**：`GYM_LOCAL=1`、`WX_SECRET` 为空、`SESSION_SECRET` 不足 32 字节、还留着 `__CHANGE_ME` 占位符，任何一条命中就直接退出并说明原因。这几项错了不会报错，只会静默地让所有人变成同一个测试用户，必须在传之前拦住。
- **不用 `rsync`**，只用 `scp` + `ssh`，服务器不需要额外装东西。

建议先配好免密钥登录（`ssh-copy-id user@host`），否则连通性检查、建目录、传包、load、up、ps 每一步都要输一次密码。

### 对外 HTTPS（有域名之后）

默认 compose 把 web 绑在 `127.0.0.1:18080`，公网访问不到，只能走 SSH 隧道：

```bash
ssh -L 18080:127.0.0.1:18080 user@host        # 本机访问 http://127.0.0.1:18080
```

小程序只接受 HTTPS + 已登记的合法域名，所以对外必须补一层证书。用叠加文件，不动基础编排：

```bash
# 1. 把 deploy/nginx.https.conf 里的 api.example.com 全部换成自己的域名
# 2. 放证书：deploy/certs/fullchain.pem（服务器证书 + 中间证书，顺序不能反）
#            deploy/certs/privkey.pem
# 3. 启动
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.https.yml up -d
```

叠加文件只做三件事：暴露 80/443、把 `nginx.https.conf` 挂进容器替换默认配置、挂载 `deploy/certs`（只读）。回滚就去掉 `-f deploy/docker-compose.https.yml` 重新 `up -d`。

走 `publish.sh` 的话不用手敲这些：

- 首次部署带 `--config`，脚本发现 `deploy/certs/fullchain.pem` 与 `privkey.pem` 都在，就把证书、叠加文件、`nginx.https.conf` 成套传上去（没证书就不传，避免 nginx 挂不上证书起不来）
- 之后日常 `publish.sh` 会检测服务器上有没有 `deploy/docker-compose.https.yml`，有就自动追加 `-f`，否则一次普通发布就会把 443 和证书挂载悄悄弄丢

`nginx.https.conf` 里 80 端口只做 301 跳转（保留 `/.well-known/acme-challenge/` 给 certbot 续期），443 上只开 TLS 1.2/1.3——小程序不接受更低版本。证书和 certbot webroot 目录都已写进 `.gitignore`，不进仓库。

证书来源：云厂商免费 DV 证书（下载 Nginx 格式直接拷）、`certbot certonly --webroot -w deploy/certbot/www -d api.example.com`、或挂在自己的 CDN/负载均衡上（此时不叠加那层 compose，改成回源 `18080:80` 并限制安全组）。

证书链不完整是小程序侧最常见的故障：`request:fail ssl hand shake error` 基本都是只传了服务器证书、漏了中间证书。

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

## 备份与恢复

`gym-data` 是 Docker named volume，`docker compose down`、重新发布、重启容器都不会动它。但它**不是备份**：误删数据、迁移脚本写错、业务逻辑把数据刷坏、磁盘故障，这几种情况都没有退路。所以需要定时导出一份逻辑备份。

```bash
./deploy/backup.sh                  # 备份一次，保留 7 天
./deploy/backup.sh --keep 14        # 保留 14 天；--keep 0 表示永不清理
./deploy/backup.sh --out /data/bak  # 换个输出目录，比如挂载的数据盘
./deploy/backup.sh --install-cron   # 写进 root crontab，每天 03:30 自动跑（已存在则跳过）
```

产物是 `backups/gym_<时间戳>.sql.gz`，用 `mysqldump --single-transaction --quick` 导出：走 InnoDB 一致性快照不锁表，逐行发送不占内存，2G 机器上跑得动。生成时先写 `.tmp`、校验通过才 `mv` 成正式文件名，被 OOM Killer 杀掉也不会留下一个看起来正常、实际截断的包。

校验有两条：`gzip -t` 过压缩完整性，再检查结尾有没有 `-- Dump completed`。只查前者不够——截断的 gzip 流未必报错。

恢复（dump 里带 `CREATE DATABASE` 和 `DROP TABLE IF EXISTS`，会覆盖同名库的表）：

```bash
gunzip -c backups/gym_20261113_033000.sql.gz \
  | docker compose --env-file .env -f deploy/docker-compose.yml exec -T mysql mysql -uroot -p
```

恢复前先 `cp` 一份现在的库，或者拿一个临时库名试一遍——`DROP TABLE IF EXISTS` 是直接生效的，没有二次确认。

**备份和数据库在同一块盘上，盘坏了会一起没。** 定期把 `backups/` 里最新的那份 `scp` 回本地，或者传对象存储，才算完整。

清理只匹配 `gym_*.sql.gz`，`backups/` 里手工导出的 `.sql`（项目里那几份 `plans_*.sql`）不会被删。
