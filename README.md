<div align="center">

<img src="web/public/logo.svg" alt="Lodestar" width="120" height="120">

# Lodestar

**自用优先 · 高自定义 · 可聚合的个人 AI 中转站**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white)](https://nextjs.org/)
[![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED?logo=docker&logoColor=white)](https://ghcr.io/gypg/lodestar)
[![CI](https://img.shields.io/github/actions/workflow/status/gypg/lodestar/quality.yml?label=CI&branch=main)](https://github.com/gypg/lodestar/actions)

简体中文 · [快速开始](#-快速开始) · [文档](#-文档) · [Release](https://github.com/gypg/lodestar/releases)

</div>

---

## ✨ 特性

- 🔀 **多渠道聚合** — 连接多个 LLM 供应商，统一管理
- 🔑 **多 Key 支持** — 单渠道多 Key，自动轮换
- ⚡ **智能选路** — 多端点自动选择延迟最低的
- ⚖️ **负载均衡** — 轮询 / 随机 / 故障转移 / 加权 / Auto 策略
- 🤖 **Auto 策略** — 滑动窗口自动优选高成功率渠道，熔断器指数退避 + 自动恢复
- 🧠 **AI 路由 + 自动分组** — 一键生成全量路由表，支持 force 全量重建
- 🔄 **协议转换** — OpenAI Chat / OpenAI Responses / Anthropic / Gemini / DeepSeek / MiMo 格式互转
- 🌐 **多 Provider** — 内置 OpenAI 兼容 / Anthropic / Cloudflare / Gemini / 火山引擎 / MiMo 渠道
- 🖼️ **全端点中转** — Chat / Images / Audio / Video / Music / Embeddings / Rerank / Moderations
- 🧾 **API Key 治理** — 模型白名单 / 限额 / RPM·TPM / per-model 配额 / IP 白名单
- 🔐 **角色权限** — admin / editor / viewer 三角色服务端权限
- 🔑 **WebAuthn / Passkey** — 无密码登录 + 可配置 RP
- 🔒 **2FA 两步验证** — TOTP 支持，登录时强制校验
- 🚨 **告警通知** — 错误率 / 成本 / 配额 / 渠道宕机告警，支持 Webhook / Gotify / 邮件 / Telegram / 飞书 / 钉钉 / 企业微信 / ntfy
- 💎 **模型广场** — 统一模型目录，含定价 / 渠道覆盖 / 延迟 / 成功率，按供应商筛选
- 🔃 **模型同步** — 自动同步渠道可用模型列表
- 📊 **分析中心** — 概览 / 供应商·模型·Key 利用率 / 路由健康 / 延迟分布（按模型） / 语义缓存 / Provider Prompt Cache
- 🛠️ **运维审计** — 遥测 / 配额 / 健康 / 系统 / 审计面板 + 管理写操作审计日志
- 🧠 **语义缓存** — Embedding 向量缓存，支持流式和非流式请求，运行时状态和效果指标
- 🖼️ **图床集成** — 生成图片自动上传外部图床 + 联通测试
- 🛰️ **站点管理（Hub）** — 连接 New API / One API / Sub2API 等上游，多账户 / 自动同步 / 自动签到 / 余额监控
- 🔁 **模型映射** — 全局模型名重写，精确 / 通配 / 正则匹配，优先级排序
- ☁️ **WebDAV 备份** — 自动云备份 + 可配置调度 + 一键恢复
- 🔑 **API 凭据配置** — 可复用 Base URL + API Key 配置，含健康探针
- 📤 **CLI 配置导出** — 生成 Claude Code / Codex / Gemini CLI / Cherry Studio 配置片段
- 🧭 **可配置导航** — 控制台页面顺序和可见性持久化，跨浏览器同步
- 💾 **运行时状态持久化** — Auto 策略窗口、熔断器状态持久化到数据库
- 🎨 **每用户主题** — 5 套内置主题（含 ❄ 冬日），OKLCH 实时换肤，API 可上传自定义主题
- 🗄️ **多数据库** — SQLite / PostgreSQL / MySQL，支持运行时迁移

## 🚀 快速开始

### 🐳 Docker Compose（推荐）

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

### 🐳 Docker 单容器

```bash
docker run -d --name lodestar \
  --restart unless-stopped \
  -p 8080:8080 \
  -v lodestar-data:/app/data \
  -e LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" \
  -e LODESTAR_SECURITY_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
  ghcr.io/gypg/lodestar:latest
```

> **Windows PowerShell:**
> ```powershell
> docker run -d --name lodestar `
>   --restart unless-stopped `
>   -p 8080:8080 `
>   -v lodestar-data:/app/data `
>   -e LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" `
>   -e LODESTAR_SECURITY_ENCRYPTION_KEY="$(openssl rand -hex 32)" `
>   ghcr.io/gypg/lodestar:latest
> ```

> **注意：** 官方镜像以非 root 用户 `lodestar`（UID/GID `1000`）运行。使用 named volume 可避免大部分宿主权限问题。如果 bind-mount 宿主目录到 `/app/data`，确保该目录对 UID/GID `1000` 可写，否则启动时创建 `config.json` 或 `data.db` 会报 `permission denied`。

### 📦 从 Release 下载

从 [Releases](https://github.com/gypg/lodestar/releases) 下载对应平台的二进制，然后运行：

```bash
LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

### 🛠️ 从源码构建

**前置要求：** Go 1.24 · Node.js 20+ · pnpm

```bash
# 克隆仓库
git clone https://github.com/gypg/lodestar.git
cd lodestar

# 构建前端
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out

# 构建后端
go build -tags=jsoniter -o lodestar .

# 启动
LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

> 如果 `static/out/` 已包含构建好的前端资源，Go 二进制会直接提供管理 UI。否则 API 端点仍可正常工作，但管理 UI 不可用。

**开发模式：**

```bash
# 终端 1：后端（热重载）
go run main.go start

# 终端 2：前端（开发服务器）
cd web && pnpm dev
```

## ⚙️ 环境变量

| 变量 | 说明 | 默认值 |
|---|---|---|
| `LODESTAR_AUTH_JWT_SECRET` | JWT 签名密钥（**必填**） | 空（重启掉登录） |
| `LODESTAR_SECURITY_ENCRYPTION_KEY` | 敏感凭据加密密钥（**一次定终身**，有数据后不可改） | 生成临时随机密钥（重启失效） |
| `LODESTAR_DATABASE_TYPE` | 数据库类型：`sqlite` / `postgres` / `mysql` | `sqlite` |
| `LODESTAR_DATABASE_PATH` | 数据库连接串 | `./data/lodestar.db` |
| `LODESTAR_REDIS_HOST` | Redis 地址（留空跳过，回落内存缓存） | 空 |
| `LODESTAR_REDIS_PORT` | Redis 端口 | `6379` |
| `LODESTAR_REDIS_PASSWORD` | Redis 密码 | 空 |
| `LODESTAR_REDIS_DB` | Redis DB 索引 | `0` |
| `LODESTAR_LOG_DB_TYPE` | 日志数据库类型（可独立） | 空（用主库） |
| `LODESTAR_LOG_DB_PATH` | 日志数据库连接串 | 空 |

> 完整变量见 [`.env.example`](.env.example)

## 🔌 客户端集成

Lodestar 兼容 OpenAI API 格式，直接替换 base URL 即可：

```bash
# 之前
https://api.openai.com/v1/chat/completions

# 之后
https://your-lodestar-domain/v1/chat/completions
```

<details>
<summary><b>Claude Code</b></summary>

```bash
export ANTHROPIC_BASE_URL=https://your-lodestar-domain
export ANTHROPIC_API_KEY=sk-lodestar-your-key
```
</details>

<details>
<summary><b>OpenAI SDK (Python)</b></summary>

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://your-lodestar-domain/v1",
    api_key="sk-lodestar-your-key"
)
```
</details>

<details>
<summary><b>curl</b></summary>

```bash
curl https://your-lodestar-domain/v1/chat/completions \
  -H "Authorization: Bearer sk-lodestar-your-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```
</details>

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

## 📸 截图

> 欢迎提交 PR 补充截图

## 📖 文档

- [部署指南](docs/DEPLOY.md) — Docker / 二进制 / 源码构建 + 环境变量 + 反代配置
- [运维手册](docs/STATUS.md) — 运行状态 + 健康检查 + 日志
- [项目宪章](docs/CHARTER.md) — 愿景 + 技术选型 + 设计原则
- [Release Notes](https://github.com/gypg/lodestar/releases) — 版本变更记录

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

## 🔐 安全

- **非 root 运行**：Docker 镜像以 UID/GID `1000` 运行
- **密钥加密**：`LODESTAR_SECURITY_ENCRYPTION_KEY` 加密存储所有渠道 API Key
- **JWT 过期**：可配置 Token 过期时间 + Remember Me 长期 Token
- **审计日志**：所有管理写操作自动记录
- **PII 脱敏**：响应关键词过滤 + Relay 日志敏感信息脱敏
- **输入检查**：Guardrail 热路径输入内容安全检查

如发现安全漏洞，请通过 [Security Advisories](https://github.com/gypg/lodestar/security/advisories) 报告。

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

## 🙏 致谢

Lodestar 衍生自 [octopus](https://github.com/bestruirui/octopus) 及其 [lingyuins/octopus](https://github.com/lingyuins/octopus) fork，在其基础上发展出独立的产品方向。署名见 [NOTICE.md](NOTICE.md)。

## 📄 许可证

[AGPL-3.0](LICENSE)

---

<div align="center">

**Lodestar** — 你的 AI，你的中转站 ⭐

如果觉得有用，请点个 Star 支持一下！

</div>
