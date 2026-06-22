<div align="center">

<img src="web/public/logo.svg" alt="Lodestar" width="120" height="120">

# Lodestar

**自用优先 · 高自定义 · 可聚合的个人 AI 中转站**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white)](https://nextjs.org/)
[![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED?logo=docker&logoColor=white)](https://ghcr.io/gypg/lodestar)
[![CI](https://img.shields.io/github/actions/workflow/status/gypg/lodestar/quality.yml?label=CI&branch=main)](https://github.com/gypg/lodestar/actions)

[English](#english) · 简体中文 · [快速开始](#快速开始) · [文档](docs/DEPLOY.md) · [Release](https://github.com/gypg/lodestar/releases)

</div>

---

## ✨ 特性一览

<table>
<tr>
<td width="50%">

### 🔀 中转 / 聚合

- **多渠道聚合** + 多 Key + 智能选路
- **负载均衡**：轮询 / 随机 / 故障转移 / 加权 / Auto
- **熔断器**：指数退避 + 自动恢复
- **协议互转**：OpenAI ↔ Anthropic ↔ Gemini
- **全端点覆盖**：Chat / Image / Audio / Video / Music / Embeddings / Rerank
- **图床集成**：生成图片自动上传外部图床
- **语义缓存**：相似请求命中缓存，节省 Token

</td>
<td width="50%">

### 🛰️ 站点管理（Hub）

- **多站聚合**：连别的中转站当远程账户
- **自动同步**：分组 / 模型 / 渠道一键拉取
- **自动签到**：支持随机延迟 + 多平台
- **余额监控**：实时查看所有站点余额
- **凭据管理**：Access Token / API Key / 用户密码
- **批量操作**：启用 / 禁用 / 删除 / 归档

</td>
</tr>
<tr>
<td width="50%">

### 🔑 安全 / 治理

- **API Key 治理**：模型白名单 / 限额 / RPM·TPM / IP 白名单
- **Per-Model 配额**：每个模型独立限额
- **2FA 两步验证**：TOTP 支持
- **WebAuthn / Passkey**：硬件密钥登录
- **Guardrail**：输入内容安全检查
- **PII 脱敏**：响应关键词过滤
- **审计日志**：管理操作全记录

</td>
<td width="50%">

### 📊 分析 / 监控

- **渠道 × 模型**：交叉维度成功率 / Token / Cost
- **延迟分布**：按模型筛选的 P50/P95/P99
- **模型广场**：按供应商筛选 + 成功率排序
- **Auto 策略**：滑动窗口自动选路
- **语义缓存监控**：命中率 / 使用率 / TTL
- **告警通知**：多渠道告警 + 自定义规则
- **Relay 日志**：按尝试维度完整记录

</td>
</tr>
<tr>
<td width="50%">

### 🎨 前端 / UI

- **每用户主题**：5 套内置（含 ❄ 冬日），OKLCH 实时换肤
- **自定义主题**：API 上传 JSON，全站可选
- **模型下拉**：按供应商分组选择器
- **响应式**：移动端适配
- **虚拟化列表**：大数据量流畅滚动

</td>
<td width="50%">

### 🚀 部署

- **单二进制**：前端 `go:embed` 嵌入
- **Docker Compose**：GHCR 镜像 pull 即用
- **三数据库**：SQLite（开箱）/ PostgreSQL / MySQL
- **Redis 可选**：多实例共享缓存
- **低资源**：~20MB 内存运行
- **CI/CD**：GitHub Actions 自动构建

</td>
</tr>
</table>

## 📸 截图

> 欢迎提交 PR 补充截图

## 快速开始

### Docker Compose（推荐）

```bash
# 1. 创建部署目录
mkdir -p /opt/lodestar && cd /opt/lodestar

# 2. 下载配置文件
curl -O https://raw.githubusercontent.com/gypg/lodestar/main/docker-compose.yml
curl -O https://raw.githubusercontent.com/gypg/lodestar/main/.env.example
cp .env.example .env

# 3. 填写密钥
sed -i "s/LODESTAR_AUTH_JWT_SECRET=.*/LODESTAR_AUTH_JWT_SECRET=$(openssl rand -hex 32)/" .env
sed -i "s/LODESTAR_SECURITY_ENCRYPTION_KEY=.*/LODESTAR_SECURITY_ENCRYPTION_KEY=$(openssl rand -hex 32)/" .env

# 4. 启动
docker compose up -d

# 5. 访问 http://localhost:8080 初始化管理员
```

### Docker 单容器

```bash
docker run -d \
  --name lodestar \
  -p 8080:8080 \
  -v lodestar-data:/app/data \
  -e LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/gypg/lodestar:latest
```

### 本地构建

```bash
# 前端
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out

# 后端
go build -tags=jsoniter -o lodestar .

# 启动
LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

> 详见 [`docs/DEPLOY.md`](docs/DEPLOY.md)（部署 + 环境变量 + 反代配置）

## ⚙️ 环境变量

| 变量 | 说明 | 默认值 |
|---|---|---|
| `LODESTAR_AUTH_JWT_SECRET` | JWT 签名密钥（必填） | 空（重启掉登录） |
| `LODESTAR_SECURITY_ENCRYPTION_KEY` | 敏感凭据加密密钥（一次定终身） | 回落到 JWT Secret |
| `LODESTAR_DATABASE_TYPE` | 数据库类型：`sqlite` / `postgres` / `mysql` | `sqlite` |
| `LODESTAR_DATABASE_PATH` | 数据库连接串 | `./data/lodestar.db` |
| `LODESTAR_REDIS_HOST` | Redis 地址（留空跳过） | 空 |
| `LODESTAR_REDIS_PORT` | Redis 端口 | `6379` |
| `LODESTAR_REDIS_DB` | Redis DB 索引 | `0` |

> 完整变量见 [`.env.example`](.env.example)

## 🏗️ 架构

```
┌─────────────────────────────────────────────────────────┐
│                     Client (Browser)                     │
│           Next.js 16 / React 19 / Radix UI              │
└────────────────────────┬────────────────────────────────┘
                         │ HTTP / SSE
┌────────────────────────▼────────────────────────────────┐
│                    Lodestar Gateway                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────┐ │
│  │   Auth   │  │  Relay   │  │ Balancer │  │  Cache  │ │
│  │ JWT+2FA  │  │ Protocol │  │  Auto    │  │ Semantic│ │
│  │ WebAuthn │  │ Convert  │  │ Circuit  │  │  Redis  │ │
│  └──────────┘  └──────────┘  └──────────┘  └─────────┘ │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────┐ │
│  │   Hub    │  │  Alert   │  │  Stats   │  │ Guard   │ │
│  │ Multi-   │  │  Rules   │  │ Analytics│  │  Rail   │ │
│  │  Site    │  │  Notify  │  │  Logs    │  │  PII    │ │
│  └──────────┘  └──────────┘  └──────────┘  └─────────┘ │
└────────────────────────┬────────────────────────────────┘
                         │
    ┌────────────────────┼────────────────────┐
    ▼                    ▼                    ▼
┌────────┐        ┌────────────┐       ┌──────────┐
│  SQLite│        │ PostgreSQL │       │  MySQL   │
│  (默认) │        │  (生产推荐) │       │          │
└────────┘        └────────────┘       └──────────┘
```

## 🔌 API 兼容

Lodestar 兼容 OpenAI API 格式，可直接替换 base URL：

```bash
# 之前
https://api.openai.com/v1/chat/completions

# 之后
https://your-lodestar-domain/v1/chat/completions
```

支持的端点：
- `POST /v1/chat/completions` — 对话补全
- `POST /v1/images/generations` — 图片生成
- `POST /v1/audio/speech` — 语音合成
- `POST /v1/audio/transcriptions` — 语音转文字
- `POST /v1/embeddings` — 向量嵌入
- `POST /v1/videos/generations` — 视频生成
- `POST /v1/music/generations` — 音乐生成
- `POST /v1/search` — 搜索
- `POST /v1/rerank` — 重排序
- `POST /v1/moderations` — 内容审核

## 🛠️ 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.24 · Gin · GORM |
| 前端 | Next.js 16 · React 19 · Tailwind CSS · Radix UI |
| 数据库 | SQLite · PostgreSQL · MySQL |
| 缓存 | Redis（可选）· 内存缓存 |
| 构建 | 多阶段 Docker · `go:embed` 单二进制 |
| CI/CD | GitHub Actions · GHCR |

## 🤝 参与贡献

欢迎提交 Issue 和 Pull Request！

1. Fork 本仓库
2. 创建特性分支：`git checkout -b feature/amazing-feature`
3. 提交改动：`git commit -m 'feat: add amazing feature'`
4. 推送分支：`git push origin feature/amazing-feature`
5. 提交 Pull Request

## 📄 许可证

[AGPL-3.0](LICENSE) · 署名见 [NOTICE.md](NOTICE.md)

---

<div align="center">

**Lodestar** — 你的 AI，你的中转站 ⭐

如果觉得有用，请点个 Star 支持一下！

</div>
