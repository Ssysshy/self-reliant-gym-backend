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

**只有一份编排**，架构已经定死在里面：数据库在宿主机上（不在容器里），`web` 同时监听 `0.0.0.0:8080`（公网 IP 打开后台）和 `127.0.0.1:18080`（SSH 隧道调试）。唯一保留的叠加文件是 `docker-compose.https.yml`，因为它依赖证书文件存在，没法并进基础文件。

构建上下文是**仓库根**（`Dockerfile` 里有 `COPY apps/backend/`、`COPY deploy/nginx.conf`），所以命令必须在仓库根目录执行。Compose 默认只读 compose 文件同级的 `.env`，因此要显式带上 `--env-file`：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
docker compose --env-file .env -f deploy/docker-compose.yml logs -f api
docker compose --env-file .env -f deploy/docker-compose.yml down
```

访问 `http://127.0.0.1:18080`（或对外的 `8080`）：Nginx 托管后台静态页，并把 `/api/` 反向代理给 `api` 服务。

编排只有 `api` 和 `web` 两个服务。`api` 通过 `host.docker.internal` 连宿主机上的 MySQL——这个主机名由 `extra_hosts: host-gateway` 解析到 docker0 网关（`172.17.0.1`）。由此有两个前置条件：

- 宿主机 MySQL 的 `bind-address` 不能是 `127.0.0.1`（要 `0.0.0.0` 或 `172.17.0.1`）
- 业务账号的 host 不能是 `localhost`（要 `%` 或 `172.17.%`）

首次准备宿主机数据库（以 root 身份执行，Ubuntu 上 `sudo mysql` 免密进）：

```sql
CREATE DATABASE IF NOT EXISTS gym DEFAULT CHARSET utf8mb4;

-- 只需要这一个账号。host 用 % 是为了同时覆盖两种来源：
--   容器 → 172.17.0.1    Navicat SSH 隧道 → 127.0.0.1
CREATE USER IF NOT EXISTS 'gym'@'%' IDENTIFIED BY '<MYSQL_PASSWORD>';
GRANT ALL PRIVILEGES ON gym.* TO 'gym'@'%';
FLUSH PRIVILEGES;
```

`GRANT ALL ON gym.*` 已经包含 `SELECT / CREATE / DROP / SHOW VIEW / TRIGGER / EVENT`，备份和迁移都够用，不需要给 root。Ubuntu 自带的 `root@localhost` 是 `auth_socket` 认证，留给 `sudo mysql` 做手工维护即可，不要改它。

表结构由 `api` 启动时的迁移自动创建，不需要手工执行 SQL。

`.env` 里的 `MYSQL_DSN` **对容器和 `go run` 都生效**（compose 只是把它从环境变量透传给容器，不再覆盖）。所以两个环境的差别只在该变量的 host：

| 场景 | MYSQL_DSN 的 host |
|---|---|
| 本机 `go run`（`.env`） | `127.0.0.1` |
| 服务器容器（`.env.production`） | `host.docker.internal` |
| 将来换云数据库 | 实例域名 / 内网地址 |

`.env`、`.env.example`、`.dockerignore` 保持留在仓库根：前两个要同时被 compose 和 `go run` 读到（`config.LoadDotEnv` 只向上查找），最后一个必须作用于构建上下文根。

### 旧版：容器里跑 MySQL

早期编排把 `mysql:8.4` 放在容器里（named volume `gym-data`）。已经移除，原因是单机 1.7G 内存跑不动两套数据库，且宿主机本来就装了 MySQL。需要回看那份写法就用 git 历史：

```bash
git log --oneline -- deploy/docker-compose.yml
```

容器库和宿主机库是两套独立数据，切换前想清楚用哪套，别来回切。


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

### 用公网 IP 访问后台（当前方式）

没有域名和证书时，用 `http://<公网IP>:8080` 打开后台。这是基础编排自带的，不需要任何叠加参数：

```bash
./deploy/publish.sh root@1.2.3.4 --config --env .env.production
```

**安全组必须放行 TCP 8080**，否则连不上。`127.0.0.1:18080` 那条同时保留，SSH 隧道调试不受影响：

```bash
ssh -L 18080:127.0.0.1:18080 user@host
```

后台页由 nginx 托管，`/api/` 反代给 `api`，前端 `fetch("/api/admin/v1...")` 是同源相对路径——不需要改前端代码，也不涉及 CORS。

三个前提：

- **只能打开后台，小程序照旧不通**。微信小程序的 `request` 只接受 HTTPS + 已登记域名，IP + HTTP 一律被拒。
- **是明文 HTTP**。登录口令和 session cookie 在公网上明文传输，务必先把 `ADMIN_PASSWORD`、`OPERATOR_PASSWORD` 换成强口令；更稳的做法是安全组里把 8080 的来源 IP 限制成自己的出口 IP。
- **`COOKIE_SECURE=0` 是配套设置**。不带它，浏览器会丢弃明文 HTTP 下的 `Secure` cookie，症状是登录接口返回成功、下一个接口却提示「请登录」。接入 HTTPS 后改回 `1`。

想只留 SSH 隧道、关掉公网入口，把 compose 里的 `- "0.0.0.0:8080:80"` 删掉重新发布即可。

### 对外 HTTPS（有域名之后）

小程序只接受 HTTPS + 已登记的合法域名，所以拿到域名后必须补一层证书。用叠加文件，不动基础编排：

```bash
# 1. 把 deploy/nginx.https.conf 里的 api.example.com 全部换成自己的域名
# 2. 放证书：deploy/certs/fullchain.pem（服务器证书 + 中间证书，顺序不能反）
#            deploy/certs/privkey.pem
# 3. 启动
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.https.yml up -d
# 4. 把 .env 里的 COOKIE_SECURE 改回 1，然后重启 api
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

只需要两个基础镜像：`alpine:3`（api 的底座）和 `nginx:alpine`（web 的底座）。数据库在宿主机上，不再需要拉 `mysql:8.4`。

## 内置数据与批量导入

动作库和官方模板不再写死在 Go 代码里，数据放仓库根的 `seed/`：

| 文件 | 内容 |
|---|---|
| `seed/exercises.json` | 73 条动作（部位、器械分类、要领、器械设置） |
| `seed/plans.json` | 2 个官方模板（`owner=0`、`published=true`），训练安排按动作名引用 |

`api` **启动时不读这些文件**，库里有什么就是什么。需要时通过后台的导入入口手动推一次（见下）。导入是幂等的：动作靠 `name` 唯一索引判重，模板靠显式 `ID`（11、12）判重，**只补不覆盖**——已存在的行原样保留，运营在后台改过的内容不会被冲掉。

> **新库/重装后必须手动导入一次**，否则动作库和模板都是空的，小程序里看不到任何内容。

镜像里也带了一份，位置 `/app/seed`，方便直接取出来用：

```bash
docker cp $(docker ps -qf name=api):/app/seed/exercises.json .
```

### 导入与导出

界面上，「训练资料」「训练模板」两页各有一组**导入 / 导出**按钮：

- **导入**：选 JSON 文件推送，弹窗里先显示能解析多少条
- **导出**：浏览器直接下载 `exercises-20261118.json` / `plans-20261118.json`，结构和 `seed/*.json` 一致，可以原样拿去另一个环境导入

接口层面，都需要管理员账号：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/v1/import/exercises` | 导入动作 |
| POST | `/api/admin/v1/import/plans` | 导入模板 |
| GET | `/api/admin/v1/export/exercises` | 导出全部动作（按 ID 升序） |
| GET | `/api/admin/v1/export/plans` | 导出官方模板（`owner=0`）；加 `?all=1` 连用户计划一起导 |

导入的请求体两种写法都接受：

```bash
curl -X POST http://127.0.0.1:8080/api/admin/v1/import/exercises \
  -H 'Content-Type: application/json' -H 'X-Gym-Admin: 1' \
  --cookie 'gym_admin=...' \
  -d @seed/exercises.json            # 裸数组，也可以是 {"items":[...]}
```

响应是 `{"created":n,"skipped":n,"total":n}`。

两条注意：

- **模板不带 `ID` 时每次都新建一条**，重复导入同一份文件会产生多份。要幂等就在 JSON 里写上 `ID`
- 导入的动作/模板不会覆盖已有记录，内容要改走后台编辑

跨环境搬运营数据的完整流程：源环境「导出」→ 目标环境「导入」。**先动作后模板**，因为模板按动作名引用。

## 1.7G 机器的内存调优

`deploy/docker-compose.yml` 已按 1.7G 内存（2G 规格）配好容器预算：

| 服务 | mem_limit | 实测常驻 |
|---|---|---|
| api | 128m | 约 30MB |
| web | 64m | 约 12MB |

宿主机系统 + dockerd 约 350MB，容器合计 192MB，剩下的留给**宿主 MySQL** 和文件缓存。数据库不在容器里，它的内存要靠 `/etc/mysql/mysql.conf.d/mysqld.cnf` 的 `[mysqld]` 段控制：

```ini
bind-address = 0.0.0.0
performance_schema = OFF
innodb_buffer_pool_size = 128M
max_connections = 50
skip-name-resolve
```

- `performance_schema = OFF`：省内存的大头。默认开启时持续采集一万多个指标，常驻会从约 450MB 降到 200MB 以下。本项目没有依赖它做监控。
- `innodb_buffer_pool_size = 128M`：8.0 和 8.4 默认都是 128M，显式写出来防止未来版本改默认值时被悄悄放大。
- `max_connections = 50`：默认 151，每个连接都占线程栈和缓冲区，这个业务的量级用不到。
- `skip-name-resolve`：只按 IP 匹配，跳过反向 DNS。
- `bind-address`：容器从 `172.17.0.1` 过来，写 `127.0.0.1` 会连不上。放开后**务必确认安全组没对公网放行 3306**。

### swap 与内核参数

首次部署时在服务器上跑一次（幂等，可重复执行）：

```bash
sudo ./deploy/server-init.sh
```

它会创建 2GB swapfile 并写入 `/etc/fstab`（已有 swap 则跳过），同时把 `vm.swappiness` 调到 10。默认值 60 会在内存还宽裕时就把 MySQL 的 buffer pool 换出到磁盘，查询延迟会从毫秒级跳到几十毫秒；10 表示只在真的吃紧时才用 swap，把它当兜底而不是日常资源。

起完服务后用 `docker stats --no-stream` 看容器占用，再用 `systemctl status mysql` 或 `mysqladmin status` 看宿主 MySQL 的常驻。两边加起来逼近物理内存，就该考虑把数据库拆到独立实例了。

## 备份与恢复

数据在宿主机 MySQL 里，`docker compose down`、重新发布、重启容器都不影响它。但这**不等于备份**：误删数据、迁移脚本写错、业务逻辑把数据刷坏、磁盘故障，这几种情况都没有退路。所以需要定时导出一份逻辑备份。

```bash
./deploy/backup.sh                  # 备份一次，保留 7 天
./deploy/backup.sh --keep 14        # 保留 14 天；--keep 0 表示永不清理
./deploy/backup.sh --out /data/bak  # 换个输出目录，比如挂载的数据盘
./deploy/backup.sh --install-cron   # 写进 root crontab，每天 03:30 自动跑（已存在则跳过）
```

产物是 `backups/gym_<时间戳>.sql.gz`，用宿主机的 `mysqldump --single-transaction --quick` 连 `127.0.0.1:3306` 导出：走 InnoDB 一致性快照不锁表，逐行发送不占内存。生成时先写 `.tmp`、校验通过才 `mv` 成正式文件名，被 OOM Killer 杀掉也不会留下一个看起来正常、实际截断的包。

校验有两条：`gzip -t` 过压缩完整性，再检查结尾有没有 `-- Dump completed`。只查前者不够——截断的 gzip 流未必报错。

前提是宿主机装了 `mysqldump`（`mysql-server` / `mysql-client` 都带），且 `.env` 里的 `MYSQL_PASSWORD` 与 `gym` 账号口令一致。脚本用 `gym` 连接，不需要 root：`GRANT ALL ON gym.*` 已覆盖备份所需的全部权限，本项目的迁移只建表、没有存储过程，所以不传 `--routines`。

恢复（dump 里带 `CREATE DATABASE` 和 `DROP TABLE IF EXISTS`，会覆盖同名库的表）：

```bash
gunzip -c backups/gym_20261113_033000.sql.gz | mysql -h127.0.0.1 -uroot -p
```

恢复前先 `cp` 一份现在的库，或者拿一个临时库名试一遍——`DROP TABLE IF EXISTS` 是直接生效的，没有二次确认。

**备份和数据库在同一块盘上，盘坏了会一起没。** 定期把 `backups/` 里最新的那份 `scp` 回本地，或者传对象存储，才算完整。

清理只匹配 `gym_*.sql.gz`，`backups/` 里手工导出的 `.sql`（项目里那几份 `plans_*.sql`）不会被删。
