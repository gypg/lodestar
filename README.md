<div align="center">

<img src="web/public/logo.svg" alt="Lodestar" width="110" height="110">

### Lodestar

**自用优先 · 高自定义 · 可聚合的个人 AI 中转站**

简体中文 · 自研栈（Go + Next.js）

</div>

---

## 这是什么

Lodestar 是一个**完全属于自己**的 LLM 网关 / 中转站：

- **自用优先**：默认面向个人，admin 建号管理；未来可一键切向商业形态。
- **高自定义（招牌）**：**每个用户的主题 / 配色 / UI 都能不同**，主题还能**经接口上传**、运行时切换、整站实时换肤。
- **聚合**：既聚合官方上游（多渠道负载均衡 + 熔断 + 故障转移），也能把**别的中转站当远程账户接进来**（hub：余额 / 签到 / 公告 / 用量 / 凭据）。
- **自研可部署**：单二进制（前端已嵌入），SQLite 开箱即跑，也可接 PostgreSQL / MySQL。

> Lodestar 衍生自 **octopus**（及其 `lingyuins/octopus` fork，贡献了多站聚合 hub），遵循 **AGPL v3** 并保留上游署名，见 [`NOTICE.md`](NOTICE.md)。
> 在其基础上新增了 Lodestar 的核心差异化——**每用户可上传 / 可切换的主题系统** 与 **冬日风落地页**。

## 核心能力

**Lodestar 新增**
- 🎨 **每用户主题预设**：内置 5 套（含 ❄ 冬日 Winter），点一下整站 OKLCH 实时换肤，按用户独立。
- ⬆️ **接口可上传自定义主题**：`custom_themes` 设置存主题 JSON，上传即全站可选（设置页粘贴 JSON，或 `PUT /api/v1/setting`）。
- 📰 **冬日风落地页**：进站封面——刊头「雪落无声」+ 飘雪 + 活体时钟 + 编号目录软路由进各模块。

**继承自底座（开箱即用）**
- 🔀 多渠道聚合 + 多 Key + 智能选路 + 负载均衡（轮询/随机/故障转移/加权/Auto）
- 🛰️ 多站点 **hub** 聚合（连别的中转站看余额/自动签到/聚合公告/用量/凭据）
- 🔄 协议转换（OpenAI / Anthropic / Gemini 互转）+ 多 Provider
- 🔑 API Key 治理（模型白名单 / 限额 / RPM·TPM / IP 白名单）+ 🔐 角色权限 + WebAuthn/Passkey
- 🚨 告警通知 + 💎 模型广场 + 📊 用量统计 + 🩺 渠道健康

## 快速开始

```bash
# Docker Compose（推荐）
docker compose up -d --build      # 访问 http://localhost:8080

# 或本地二进制
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out
go build -tags=jsoniter -o lodestar .
Lodestar_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start
```

首启进 `http://localhost:8080` 初始化管理员；登录后 **设置 → 外观 → 主题配色** 切换 / 上传主题。
详见 [`docs/DEPLOY.md`](docs/DEPLOY.md)（部署 + 环境变量）、[`docs/STATUS.md`](docs/STATUS.md)（现状 + 运行）、[`docs/CHARTER.md`](docs/CHARTER.md)（愿景 + 选型）。

## 技术栈

Go 1.24（gin + gorm，三 DB）· Next.js 16 / React 19 / Tailwind / Radix · 前端 `output:export` 经 `go:embed` 嵌入单二进制。

## License

AGPL v3（见 [`LICENSE`](LICENSE)）。衍生自 octopus / lingyuins-octopus，署名见 [`NOTICE.md`](NOTICE.md)。
