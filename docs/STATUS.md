# Lodestar — 当前状态与运行手册（STATUS）

> 自动推进产出。最后更新：2026-06-19。配套：`docs/CHARTER.md`（愿景/选型/铁律）、本地 `_workspace-private/PROGRESS-MAP.md`（完成度对照）。

## 一句话

一个**能跑的、属于你自己的**高自定义 LLM 聚合中转站 —— 以 octopus(lingyu) 为底座，
已改名为 **Lodestar**（`github.com/gypg/lodestar`），并具备每用户主题、商业层、消费级门户与波 A 体验增强。

## 现在能用什么（已验证）

| 能力 | 状态 | 来源 |
|------|------|------|
| 全栈构建 + 本地运行 | ✅ `lodestar.exe` 出二进制，:8080 serving，SQLite 开箱即跑 | P0 |
| 自有品牌 Lodestar | ✅ banner/标题/登录/manifest/locale，API key 前缀 `sk-lodestar-` | P1 |
| 模块路径 `gypg/lodestar` | ✅ 公开仓与 go.mod 已迁 | 身份线 |
| 多上游负载均衡 relay + 熔断 | ✅ | octopus 底座 |
| **hub 多站点聚合**（连别站看余额/签到/公告/用量/凭据） | ✅ | lingyu 底座 |
| 用户/密钥/统计/告警/审计 | ✅ | octopus 底座 |
| **每用户主题切换** + **接口上传自定义主题** + 主题绑账户 | ✅ | P3 |
| **冬日风落地页** + 公开入口 + 动态极光 / **可选 color4bg 氛围**（`landing_ambient_mode`） | ✅ | P4 + 波 A |
| **一键商业开关 + 公开注册** | ✅ | D2 |
| **商业计费**：余额(USD)、按量扣费（封顶）、兑换码、易支付、钱包 UI | ✅ | 商业核心 |
| **多租户 `user` 角色** + API Key **ListByUser** 隔离 + 导航裁剪 | ✅ | 平台多租户 |
| **地基安全**：维护模式**后端**守卫、扣费 MIN 封顶、settings 密钥泄露封堵 | ✅ | 6/19 安全债 |
| **消费级门户**：站内 Chat（SSE+Markdown+模型联想）、生图工坊（datalist）、钱包用量汇总 | ✅ | harvest |
| **钱包近 14 日用量曲线**（`daily_series`）+ **Tokens / 花费 / 请求** 切换 | ✅ | 波 A + P0 |
| **公开测速** `GET /api/v1/public/ping` + API 指引延迟展示 | ✅ | P0 参考 |
| **Staff 健康摘要**（钱包区 `PortalHealthStrip`，读 `ops/health`） | ✅ | 波 A |
| 维护模式、邀请码注册、意见反馈、SMTP 测试 | ✅ | 运营 |
| **GitHub Actions quality**（含 web ESLint） | ✅ | CI |

## 还没做（下一步）

- **Chat 会话持久化**（表 + API + 列表）— 工作量大，单独里程碑。
- **Ops 健康看板**：完整 Provider/Model 卡片（贴近 SAPI UI），运维 Tab 已有数据。
- **商业纵深**：订阅 / `billingexpr` 表达式计费 / OAuth / 2FA — 按需专项。
- **用量热力图**、按模型明细（SAPI 级）— 可选增强。
- 模块化 i18n：商业/注册等部分中文直写。
- 落地页 `winter-bg.jpg` 体积与懒加载优化。

## 运维提示

| 项 | 说明 |
|----|------|
| 用量曲线无数据 | 需在「系统 → 日志」开启 **保留历史日志**（`relay_log_keep_enabled`）；UI 会提示。 |
| 日志库方言 | `walletusage` 按库类型区分 SQLite `strftime` / Postgres `to_char`（见 `internal/op/walletusage`）。 |
| color4bg | 第三方脚本；失败回退照片模式；管理端站点信息可切换 `photo` \| `color4bg`。 |

## 怎么跑（本地）

```bash
# 1) 前端构建（Next.js 静态导出）
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
# 2) 前端产物嵌入后端
rm -rf static/out && cp -r web/out static/out
# 3) 后端构建
go build -o lodestar.exe .
# 4) 运行（默认 SQLite，数据在 ./data）
./lodestar.exe start         # 浏览器打开 http://localhost:8080
```

> 注：`web/next.config.ts` 已设 `typescript.ignoreBuildErrors`（类型检查走独立 `pnpm lint`），与上游一致。

## 怎么用主题系统

1. 登录后进 **设置 → 外观 → 主题配色**，点任一预设 → 整站实时换肤。
2. 「上传 / 自定义主题」粘贴主题 JSON → 添加后全站可选。
3. API：`PUT /api/v1/setting` body `{key:"custom_themes", value:"[…]"}`（需 settings:write）。

主题 token 形状见 `web/src/lib/theme-presets.ts` 的 `THEME_TOKEN_KEYS`。

## 提交线（近期）

`9332865` 导入 → … → `f6ee896` 波 A（用量图/健康条/color4bg）→ `58360da` walletusage PG 方言 → `c112471` CI + 健康条 → **本地 main 与 origin/main 同步**（2026-06-19）。

（push 以你本机 `git log origin/main..main` 为准。）