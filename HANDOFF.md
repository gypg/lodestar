# Lodestar 项目交接文件

> **用途**：供其他 AI 模型（或人类开发者）接手时一次读取即可全面了解项目。
> **生成时间**：2026-06-20
> **当前状态**：本地开发环境，:8080 运行中，admin/admin1234567890

---

## 一、项目身份

| 项 | 值 |
|---|---|
| 产品名 | Lodestar |
| 定位 | 高自定义、自用优先、可一键商业化的个人 AI 网关 |
| GitHub | https://github.com/gypg/lodestar |
| Go module | `github.com/gypg/lodestar` |
| 二进制 | `lodestar.exe`（Windows）/ `lodestar`（Linux） |
| 运行 | `cd ggzero && ./lodestar start` → http://localhost:8080 |
| 默认账号 | admin / admin1234567890（首次启动自动创建） |
| 代码中零 octopus 残留 | 已完全去品牌化 |

---

## 二、技术栈

| 层 | 技术 | 版本 |
|---|------|------|
| 后端 | Go + Gin + GORM + Cobra | 1.24.4 |
| 前端 | Next.js + React + Tailwind + Radix UI | 16.0.7 / 19.2.1 / 4.x |
| 状态管理 | Zustand + TanStack Query | — |
| 数据库 | SQLite / MySQL / PostgreSQL（GORM AutoMigrate） | — |
| 缓存 | Redis（可选）+ 内存缓存（默认） | go-redis/v9 |
| 构建 | `output:export` 静态导出 → `go:embed` 嵌入单二进制 | — |
| CI | GitHub Actions（Go test + Frontend build + lint + i18n parity） | — |

---

## 三、目录结构

```
ggzero/
├── main.go                     # 入口
├── cmd/                        # Cobra CLI（root, start, version）
├── internal/
│   ├── model/                  # 50+ GORM 数据模型
│   ├── op/                     # 业务操作层（30+ 子目录）
│   │   ├── subscription/       # 订阅系统
│   │   ├── billing/            # 计费引擎
│   │   ├── twofa/              # 2FA/TOTP
│   │   ├── oauth/              # OAuth（GitHub）
│   │   ├── payment/            # 支付（Epay + Stripe）
│   │   └── ...
│   ├── server/
│   │   ├── handlers/           # 50+ HTTP handler
│   │   ├── middleware/         # 认证、审计、Turnstile
│   │   ├── auth/               # JWT + 权限系统
│   │   └── router/             # 路由注册
│   ├── relay/                  # Relay 核心（50+ 文件）
│   │   ├── guardrail/          # 输入/输出过滤
│   │   ├── redact/             # PII 脱敏
│   │   └── balancer/           # 负载均衡 + 熔断
│   ├── hub/                    # 多站点聚合（8 适配器）
│   │   ├── lodestar/           # Lodestar JWT 登录适配器
│   │   ├── common/             # New API/One API 通用适配器
│   │   └── ...
│   ├── transformer/            # 协议转换（OpenAI/Anthropic/Gemini/DeepSeek/Volcengine）
│   ├── db/                     # 数据库初始化 + 迁移
│   ├── conf/                   # 配置（Viper）
│   ├── utils/
│   │   ├── cache/              # 内存缓存 + Redis 客户端
│   │   ├── crypto/             # AES-256-GCM 加密
│   │   └── semantic_cache/     # 语义缓存
│   └── task/                   # 定时任务
├── web/                        # 前端 Next.js 项目
│   ├── src/
│   │   ├── components/modules/ # 25+ 功能模块
│   │   ├── api/endpoints/      # 38 个 API 端点
│   │   ├── route/              # 路由配置（18 个路由）
│   │   ├── stores/             # Zustand stores
│   │   └── provider/           # Context providers
│   └── public/locale/          # i18n（3 语，2155 key）
├── data/                       # 运行时数据（SQLite、配置）
└── static/out/                 # 前端构建产物（go:embed）
```

---

## 四、功能清单（已实现）

### 4.1 认证体系
- JWT 登录（可配置过期时间）
- API Key 认证（sk-lodestar- 前缀，支持模型白名单/IP 限制/配额）
- WebAuthn/Passkey（无密码登录）
- 2FA/TOTP（Setup/Enable/Disable/备份码/锁定机制）
- GitHub OAuth（State/Callback/Bind/Unbind）
- 4 种角色：admin / editor / viewer / user

### 4.2 商业系统
- 订阅系统（Plan/Order/UserSubscription + 余额购买 + 过期检查）
- 支付：Epay（支付宝/微信）+ Stripe（Checkout Session + Webhook）
- 兑换码充值 + 余额扣减
- billingexpr 表达式计费（变量参考 + 实时预览 + 阶梯定价）
- 商业模式开关（一键切换商业/自用）
- 注册控制（邀请码 / 邮箱验证 / 关闭注册）

### 4.3 中转核心
- 多供应商（OpenAI/Anthropic/Gemini/DeepSeek/Volcengine/Mimo）
- 负载均衡 + 熔断 + 重试
- 语义缓存（Embedding 相似度匹配）
- 推理强度控制（Claude thinking / OpenAI reasoning / DeepSeek / Gemini）
- Guardrails（禁词 + PII 检测 + 长度限制）
- PII 脱敏（邮件/电话/信用卡/SSN）
- Cloudflare Turnstile 注册防机器人

### 4.4 Hub 多站聚合
- 8 个适配器：lodestar / common / ldoh / aihubmix / axonhub / claudecodehub / sapi / sub2api
- 余额捕获 + 签到 + 公告 + 兑换 + 用量同步
- AES-256-GCM 凭据加密

### 4.5 UI/前端
- 指南星主题系统（9 预设 + 每用户隔离）
- 18 个侧边栏路由（首页/对话/生图/站点/渠道/分组/模型/分析/日志/告警/运维/API密钥/钱包/订阅/设置/商业模式/用户）
- 冬日风落地页 + pretext 报刊落地页
- 站内 Chat + 生图工坊
- 多语言（zh-Hans / zh-Hant / en，2155 key）
- 响应式（桌面侧边栏 + 移动底部导航）

---

## 五、构建命令

```bash
# 前端
cd web && pnpm install && pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out

# 后端
go build -tags=jsoniter -o lodestar .

# 运行
LODESTAR_AUTH_JWT_SECRET="$(openssl rand -hex 32)" ./lodestar start

# 测试
go test ./... -count=1
cd web && pnpm lint && node tests/i18n.test.mjs
```

---

## 六、配置项（data/config.json）

```json
{
  "database": {
    "type": "sqlite",          // sqlite | mysql | postgres
    "path": "data/data.db"     // SQLite 路径，或 DSN
  },
  "redis": {
    "host": "",                // 为空 = 不启用 Redis
    "port": 6379,
    "password": "",
    "db": 0
  },
  "auth": {
    "jwt_secret": ""           // 为空时自动生成（重启失效）
  }
}
```

---

## 七、设置系统（Settings）

所有设置通过 `/api/v1/setting/set` 持久化到 DB。关键设置：

| 设置键 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `commercial_mode` | bool | false | 商业模式开关 |
| `register_invite_required` | bool | false | 注册需邀请码 |
| `register_email_required` | bool | false | 注册需邮箱验证 |
| `stripe_enabled` | bool | false | Stripe 支付开关 |
| `epay_enabled` | bool | false | Epay 支付开关 |
| `maintenance_mode` | bool | false | 维护模式 |
| `pii_redaction_enabled` | bool | false | PII 脱敏 |
| `guardrail_enabled` | bool | false | Guardrails |
| `turnstile_enabled` | bool | false | Turnstile 防机器人 |
| `billing_expr` | json | {} | 表达式计费规则 |

---

## 八、API 端点速查

| 路径 | 方法 | 说明 | 权限 |
|------|------|------|------|
| `/api/v1/user/login` | POST | JWT 登录 | 公开 |
| `/api/v1/bootstrap/status` | GET | 首页状态 | 公开 |
| `/api/v1/public/overview` | GET | 站点概览 | 公开 |
| `/api/v1/subscription/plans` | GET | 订阅方案 | auth |
| `/api/v1/subscription/purchase` | POST | 购买订阅 | auth |
| `/api/v1/wallet/balance` | GET | 钱包余额 | auth |
| `/api/v1/wallet/stripe/topup` | POST | Stripe 充值 | auth |
| `/api/v1/webhook/stripe` | POST | Stripe 回调 | **公开** |
| `/api/v1/user/2fa/setup` | POST | 2FA 设置 | auth |
| `/api/v1/oauth/github/state` | GET | GitHub OAuth 状态 | 公开 |
| `/api/v1/guardrail/config` | POST | Guardrails 配置 | admin |

---

## 九、侧边栏导航

```
首页 → 站点管理 → 渠道 → 分组 → 模型广场 → 分析中心 → 日志 → 告警 → 运维 → API 密钥 → 钱包 → 订阅 → 设置 → 商业模式 → 用户
```

- **商业模式页**：开关 + 维护模式 + 中国化模式 + Epay + Stripe + 订阅 + 计费 + 注册设置 + 邮件设置
- **钱包页**：余额 + 兑换码 + 在线充值 + 用量图表
- **设置页**：外观/账户/AI路由/策略/日志/系统/LLM同步/熔断/重试/备份/WebDAV/WebAuthn/2FA/Stripe

---

## 十、已知问题

| # | 问题 | 严重度 |
|---|------|--------|
| 1 | 测试覆盖率 14.9%（目标 80%） | 中 |
| 2 | ~823 处硬编码中文在前端组件 | 低 |
| 3 | site-channel/index.tsx 3502 行巨文件 | 低 |
| 4 | relay 端点无通用限流 | 中 |
| 5 | Docker 未实测 | 中 |
| 6 | 仅 GitHub OAuth（可扩展 Discord/OIDC） | 低 |

---

## 十一、继续开发建议

| 优先级 | 项目 | 说明 |
|--------|------|------|
| 1 | 部署到服务器 | 替换旧 newapi |
| 2 | 补充测试覆盖率 | handler/model 层 |
| 3 | i18n 残留清理 | ~823 处硬编码中文 |
| 4 | 更多 OAuth Provider | Discord/OIDC/微信 |
| 5 | API 限流 | relay 端点 |
| 6 | Docker 实测 | 验证 docker-compose |

---

*此文件由 Claude Opus 4.8 生成，2026-06-20。*
