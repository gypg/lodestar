# Lodestar 部署指南（DEPLOY）

> 适用于把 Lodestar 部署到你自己的服务器。默认 **SQLite 零依赖**即可跑；也可接你服务器上已有的 **PostgreSQL**。
> 监听端口默认 **8080**，前端已嵌入二进制（单文件）。

---

## 方式一：Docker Compose（推荐）

```bash
# 在仓库根目录（含 Dockerfile / docker-compose.yml）
docker compose up -d --build
# 首次构建较久（前端 Next 构建 + Go 编译）。完成后访问 http://<server>:8080
```
- 数据持久化在 `./data`（SQLite 库 + 配置）。
- 改配置：编辑 `docker-compose.yml` 的 `environment` 后 `docker compose up -d`。

## 方式二：纯 Docker

```bash
docker build -t lodestar:latest --build-arg APP_VERSION=$(git rev-parse --short HEAD) .
docker run -d --name lodestar --restart unless-stopped \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  -e TZ=Asia/Shanghai \
  -e Lodestar_AUTH_JWT_SECRET="$(openssl rand -hex 32)" \
  lodestar:latest
```

## 方式三：二进制（无 Docker）

```bash
# 本地/服务器构建（需 Go 1.24+、Node 20+、pnpm）
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out
go build -tags=jsoniter -o lodestar .
# 运行（数据默认在 ./data；可用 Lodestar_DATA_DIR 指定）
Lodestar_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

---

## 环境变量（前缀 `Lodestar_`，点号→下划线）

| 变量 | 作用 | 默认 / 示例 |
|------|------|-------------|
| `Lodestar_SERVER_PORT` | 监听端口 | `8080` |
| `Lodestar_SERVER_HOST` | 监听地址 | 全网卡 |
| `Lodestar_DATA_DIR` | 数据/配置目录 | `./data`（容器内 `/app/data`） |
| `Lodestar_AUTH_JWT_SECRET` | JWT 签名密钥（**务必设**，否则重启掉登录） | 随机长字符串 |
| `Lodestar_DATABASE_TYPE` | `sqlite`(默认) / `postgres` / `mysql` | `sqlite` |
| `Lodestar_DATABASE_PATH` | DB 连接串：sqlite=文件路径；postgres/mysql=DSN | 见下 |
| `TZ` | 时区 | `Asia/Shanghai` |

**接你服务器现有 PostgreSQL**：
```
Lodestar_DATABASE_TYPE=postgres
Lodestar_DATABASE_PATH=host=172.16.0.87 port=5432 user=lodestar password=*** dbname=lodestar sslmode=disable
```
> 建库后首启会自动迁移建表（GORM AutoMigrate + 版本化迁移），无需手动建表。

配置也可写在 `data/config.json`（JSON），env 优先级高于文件。

---

## 首次启动

1. 访问 `http://<server>:8080`，进入**首次初始化（First-Run Setup）**创建管理员账号。
2. 登录后进 **设置 → 外观 → 主题配色** 选/上传主题；首页即冬日封面（点目录进各模块、点右上进数据概览）。
3. API key 形如 `sk-lodestar-...`；中转端点见登录后控制台。

---

## 反向代理（你的链路：Cloudflare 隧道 → 容器:8080）

容器/进程监听 `:8080`，把隧道/反代指向它即可。注意放行 SSE（流式响应）——关闭代理缓冲、`proxy_read_timeout` 调大。
健康检查端点：`GET /api/v1/bootstrap/status`。

---

## 与旧 newapi 线上的关系

Lodestar 是**全新自研栈**（非 newapi 容器），重新部署、独立数据库，不与旧 `ghcr.io/futureppo/new-api` 镜像/库冲突。
切换时建议新库新域名灰度，确认无误再切流量。凭据请走 env / secret，勿写入仓库。

> 注：Docker 构建未在本机实测（本机 Docker 守护进程未运行）；Dockerfile/compose 已按 Lodestar 改名校对（二进制名、`Lodestar_DATA_DIR`、Author）。首次 `docker compose up -d --build` 若遇问题，多为前端 `pnpm install --frozen-lockfile` 的 lockfile 漂移——可临时去掉 `--frozen-lockfile`。
