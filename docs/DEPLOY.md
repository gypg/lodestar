# Lodestar 部署指南（DEPLOY）

> 适用于把 Lodestar 部署到你自己的服务器。默认 **SQLite 零依赖**即可跑；也可接你服务器上已有的 **PostgreSQL**。
> 监听端口默认 **8080**，前端已嵌入二进制（单文件）。

---

## 方式一：Docker Compose（推荐，全栈 PG + Redis）

镜像由 CI 构建并推到 GHCR（`.github/workflows/docker.yml`，**quality 全绿后才推**），服务器侧不 build，直接 pull。

```bash
# 0. 把整个仓库（或至少 docker-compose.yml、.env.example、scripts/deploy.sh）传到服务器
# 1. 准备密钥（一次性，妥善备份，勿改动）
cp .env.example .env
# 填好 LODESTAR_AUTH_JWT_SECRET / LODESTAR_SECURITY_ENCRYPTION_KEY / PG 密码 / Redis 配置
# 生成随机值：openssl rand -hex 32

# 2. 一键部署（拉镜像 + 滚动升级 + 健康检查）
./scripts/deploy.sh
# 访问 http://<server>:8080
```

**更新 / 回滚**：
```bash
./scripts/deploy.sh              # 拉 .env 里 LODESTAR_IMAGE_TAG 指定的版本（缺省 latest）
./scripts/deploy.sh latest       # 强制拉最新 main（覆盖 .env 设置，只对本次生效）
./scripts/deploy.sh sha-e52826b  # 回滚到指定提交
```

- 默认连服务器现有 PG（`YOUR_DB_HOST:5432`，独立库 `lodestar`）+ Redis（独立 `db=2`）。
- 镜像 tag 在 `.env` 里设 `LODESTAR_IMAGE_TAG`：留空或 `latest` 跟最新 main；钉 `sha-<commit>` 可回滚。
- 若目标服务器没有现成 PG/Redis，取消注释 compose 末尾的 `postgres`/`redis` 块随栈自带。
- 数据持久化在 `./data`（SQLite 回落场景的库 + 配置）；PG 数据在 PG 侧。
- 改配置：编辑 `.env` 后重跑 `./scripts/deploy.sh`。
- 脚本会等健康检查通过（最多 90s）并展示最近日志，失败时自动打印诊断信息。

> **关键提示：这个 key 是必填项**。`LODESTAR_SECURITY_ENCRYPTION_KEY` 未设置时应用
> **拒绝启动**（fail-closed）——它加密存储 Stripe/SMTP/图床等敏感设置项与远端站点凭据
> （**不含**渠道 API key，那些是明文存储），一旦有数据**不可更改**。
> 架构搭建时一次性生成并备份，事后开商业模式时无需再迁移凭据。Redis 也是同理：自用单实例可留空，
> 但既然要为商业化铺路，建议一上来就启用（多实例共享缓存/限流状态需要它）。

## 方式二：纯 Docker

```bash
docker build -t lodestar:latest --build-arg APP_VERSION=$(git rev-parse --short HEAD) .
docker run -d --name lodestar --restart unless-stopped \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  -e TZ=Asia/Shanghai \
  -e LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" \
  lodestar:latest
```

## 方式三：二进制（无 Docker）

```bash
# 本地/服务器构建（需 Go 1.24+、Node 20+、pnpm）
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out
go build -tags=jsoniter -o lodestar .
# 运行（数据默认在 ./data；可用 LODESTAR_DATA_DIR 指定）
LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

---

## 环境变量（前缀 `LODESTAR_`，点号→下划线）

| 变量 | 作用 | 默认 / 示例 |
|------|------|-------------|
| `LODESTAR_SERVER_PORT` | 监听端口 | `8080` |
| `LODESTAR_SERVER_HOST` | 监听地址 | 全网卡 |
| `LODESTAR_SERVER_TRUSTED_PROXIES` | 可信代理网段（逗号分隔）。只有来自这些地址的 `X-Forwarded-For` / `X-Real-IP` 会被采信——它决定 API key 的 IP 白名单与按 IP 限流看到的是谁。默认已覆盖回环与私网，**反代在另一台机器上时必须显式列出它的 IP**，否则限流会按反代地址聚合 | `127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,...` |
| `LODESTAR_DATA_DIR` | 数据/配置目录 | `./data`（容器内 `/app/data`） |
| `LODESTAR_AUTH_JWT_SECRET` | JWT 签名密钥（**务必设**，否则重启掉登录） | 随机长字符串 |
| `LODESTAR_SECURITY_ENCRYPTION_KEY` | 敏感凭据加密密钥（**必填，未设置则拒绝启动**）。加密 8 个敏感设置项（Stripe/易支付/SMTP/Turnstile/GitHub OAuth/AI 路由/图床）与远端站点凭据、API 凭证档案；**渠道 API key 不加密**。**一旦有数据不可改**，架构搭建时一次性生成并备份 | 随机长字符串 |
| `LODESTAR_DATABASE_TYPE` | `sqlite`(默认) / `postgres` / `mysql` | `sqlite` |
| `LODESTAR_DATABASE_PATH` | DB 连接串：sqlite=文件路径；postgres/mysql=DSN | 见下 |
| `LODESTAR_DATABASE_LOG_TYPE` / `_LOG_PATH` | 可选独立日志库（仅 relay_logs；留空共用主库） | 空 |
| `LODESTAR_REDIS_HOST` / `_PORT` / `_PASSWORD` / `_DB` | Redis；留空 host 则跳过、回落内存缓存 | 空 |
| `TZ` | 时区 | `Asia/Shanghai` |

**接你服务器现有 PostgreSQL**：
```
LODESTAR_DATABASE_TYPE=postgres
LODESTAR_DATABASE_PATH=host=YOUR_DB_HOST port=5432 user=lodestar password=*** dbname=lodestar sslmode=disable
```
> 为 Lodestar 新建独立库 `lodestar` + 独立角色（新鲜密码，勿复用暴露过的 postgres 超管密码）。
> 首启会自动迁移建表（GORM AutoMigrate + 版本化迁移），无需手动建表。

**接你服务器现有 Redis**：用独立 `db` 索引与旧 newapi 隔离（旧用 `/1`，Lodestar 用 `/2`）。
```
LODESTAR_REDIS_HOST=YOUR_DB_HOST
LODESTAR_REDIS_PORT=6379
LODESTAR_REDIS_PASSWORD=
LODESTAR_REDIS_DB=2
```

配置也可写在 `data/config.json`（JSON），env 优先级高于文件。
**生产部署建议所有密钥走 `.env`**（见 `.env.example`，`.env` 已 gitignore，`.env.example` 入库做模板）。

---

## 首次启动

1. 访问 `http://<server>:8080`，进入**首次初始化（First-Run Setup）**创建管理员账号。
2. 登录后进 **设置 → 外观 → 主题配色** 选/上传主题；首页即冬日封面（点目录进各模块、点右上进数据概览）。
3. API key 形如 `sk-lodestar-...`；中转端点见登录后控制台。

---

## 反向代理（你的链路：Cloudflare 隧道 → 容器:8080）

容器/进程监听 `:8080`，把隧道/反代指向它即可。注意放行 SSE（流式响应）——关闭代理缓冲、`proxy_read_timeout` 调大。
健康检查端点：`GET /api/v1/bootstrap/status`。

**客户端 IP**：反代必须转发 `X-Forwarded-For`，且反代自身地址要落在 `LODESTAR_SERVER_TRUSTED_PROXIES` 里，否则 API key 的 IP 白名单和按 IP 限流会看到反代的地址而不是真实客户端。本链路（隧道与容器同机/同容器网）在默认值覆盖范围内，无需额外配置；反代独立成机时要把它的 IP 加进去。反过来，**服务若直接暴露到公网就不要放宽这个列表**——信任范围内的来源可以随意伪造客户端 IP。

部署后验收（热力图 / `per_model` / Banner 字段）：

```bash
docker compose pull && docker compose up -d
export BASE=http://127.0.0.1:8080
export TOKEN='<登录 JWT>'
bash scripts/verify-heatmap-server.sh
```

需在设置中开启 **保留 relay 历史日志**，钱包热力图与渠道 sparkline 才有按日数据。

---

## 与旧 newapi 线上的关系

Lodestar 是**全新自研栈**（非 newapi 容器），重新部署、独立数据库（独立 PG 库 `lodestar` + Redis `db=2`），不与旧 `ghcr.io/futureppo/new-api` 镜像/库冲突。
切换时建议新库新域名灰度，确认无误再切流量。凭据请走 env / secret，勿写入仓库。

> 注：Docker 构建在 CI 完成（`.github/workflows/docker.yml`），镜像推 GHCR；服务器侧 `docker compose pull` 即用，不在弱机上 build。首次 `docker compose pull` 若拉取慢，可配国内镜像加速或预拉。旧 newapi 也是拉 `ghcr.io`，链路已验证。

---

## 商业化架构就绪清单

当前自用模式下（`commercial_mode=false`）商业面/计费/充值/多租户全部 gate 住、零影响。但下列「一次定终身的」基础设施务必在首次部署时配好，否则日后开商业模式需迁移：

- [ ] `LODESTAR_SECURITY_ENCRYPTION_KEY`：加密 Stripe/SMTP/图床等敏感设置项与远端站点凭据（渠道 API key 明文存储，不在范围内），**未设置则拒绝启动、有数据后不可改**——架构期生成并备份。
- [ ] PostgreSQL 接好：多用户/计费/兑换/订单均依赖关系库（SQLite 单连接不扛商业并发）。
- [ ] Redis 接好：多实例共享缓存、限流、会话状态需要它（单实例自用可空，但商业建议启用）。
- [ ] `LODESTAR_AUTH_JWT_SECRET`：固定值，否则重启全员掉登录。
- [ ] 独立日志库（可选）：`LODESTAR_DATABASE_LOG_*`，relay 日志量大时可秒级清理而不动主库。

切换商业模式：设置 → 开 `commercial_mode`（可逆）。详见 `docs/COMMERCIAL-PORT.md`。
