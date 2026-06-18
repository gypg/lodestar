# Lodestar — 当前状态与运行手册（STATUS）

> 自动推进产出。最后更新：2026-06-18。配套：`docs/CHARTER.md`（愿景/选型/铁律）。

## 一句话

一个**能跑的、属于你自己的**高自定义 LLM 聚合中转站 —— 以 octopus(lingyu) 为底座，
已改名为 Lodestar，并新增了愿景核心的「每用户可切换 / 可经接口上传」的主题系统。

## 现在能用什么（已验证）

| 能力 | 状态 | 来源 |
|------|------|------|
| 全栈构建 + 本地运行 | ✅ `lodestar.exe` 出二进制，:8080 serving，SQLite 开箱即跑 | 本项目 P0 |
| 自有品牌 Lodestar | ✅ banner/标题/登录/manifest/locale，API key 前缀 `sk-lodestar-` | P1 |
| 多上游负载均衡 relay + 熔断 | ✅ | octopus 底座 |
| **hub 多站点聚合**（连别站看余额/签到/公告/用量/凭据） | ✅ 14846 行带测试 | lingyu 底座 |
| 用户/密钥/统计/告警/审计 | ✅ | octopus 底座 |
| **每用户主题切换**（5 预设含冬日，整站实时换肤，按账户独立） | ✅ | P3a |
| **接口可上传自定义主题**（`custom_themes` 设置，全站可选 + 粘贴 JSON 上传 UI） | ✅ | P3b |
| **冬日风落地页**（home 封面：刊头雪落无声 + 飘雪 + 活体时钟 + 编号目录软路由 + 进概览切换） | ✅ | P4 |
| **冬日封面作公开入口**（未登录即见封面 + 漂亮主题，点击唤出登录）+ winter 设为默认预设 | ✅ | P4b |
| **动态极光氛围光**（落地页随主题着色的呼吸式背景，reduced-motion 安全） | ✅ | P4c |
| **一键商业开关 + 公开注册**（commercial_mode 设置 + 管理端开关；关→注册 403，开→访客自助注册并自动登录） | ✅ | D2 |
| **主题绑账户**（User.Preferences + /user/preferences API；登录应用、选主题即存，跨设备一致） | ✅ | D3c |
| **★公开平台门面**（无鉴权 `/api/v1/public/overview`；落地页左侧导航点开 公告/模型广场/用量概览/关于，私密项才登录）+ **可配置站点身份**（名称/简介/公告/页脚 设置，落地页实时反映） | ✅ | 平台层 |
| **Lodestar 雪花品牌**（Logo 组件/加载屏/logo.svg/favicon 全换，去章鱼）+ 冬日封面还原蓝色少女实景照片（固定纸感冷蓝，不随明暗变灰黑） | ✅ | 品牌层 |
| **★商业计费层**（移植 new-api 预付费逻辑）：用户余额(USD) + Key 归属 + relay 按量扣费/余额闸（绑 commercial_mode）+ 兑换码充值 + **易支付在线充值** + 钱包 UI；端到端实测通过 | ✅ | 商业核心 |
| **★多租户用户门户 + 数据隔离**：新增最小权限 `user` 角色（注册用户）；API Key 按用户隔离（只见/管自己的）；按角色精简导航/首页/设置（用户只见 主题/钱包/如何使用/我的密钥，管理项隐藏）；`/me` 端点；堵住 settings 密钥泄露 | ✅ | 平台多租户（实测：user 读渠道/设置 403、读自己 key 200） |
| **API 使用指引**（OpenAI 兼容 Base URL + curl/python 示例 + 复制） | ✅ | 上手引导 |
| **★消费级：站内对话 + 生图 + 每用户用量**（harvest 自 SAPI 思路）：`chat`/`image` 路由用自己的 key 流式调本站 `/v1`；钱包内用量卡 | ✅ | 让不懂代码的人也能直接用 |
| **维护模式**（管理开关 + 非管理员维护页，登录保持开放） | ✅ | 运营 |
| **邀请码注册 + 意见反馈**（注册可选要求邀请码、一次性竞态安全 + 管理发码；用户反馈+管理查看） | ✅ | 运营 |

## 还没做（下一步）

- **商业能力纵深**：D2 已落地"开放注册"这第一步；完整商业化还需计费/订阅/支付/配额（多步专项，按需开工）。
- **落地页实景图**（可选）：当前是动态极光氛围（随主题、零依赖）；如要原图（松树少女雪景 2MB）+ color4bg 实景库，再加 public 资源（视觉件，建议你看过再定）。
- 模块路径 `github.com/lingyuins/octopus` 未改（用户不可见、高 churn，暂留）。
- 模块化 i18n：商业开关/注册等新增 UI 文案目前中文直写，未来可抽进 locale。

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
> 注：`web/next.config.ts` 已设 `typescript.ignoreBuildErrors`（类型检查走独立 `pnpm lint`，与上游一致），让 `next build` 专注产出静态包。

## 怎么用主题系统

1. 登录后进 **设置 → 外观 → 主题配色**，点任一预设（Scandi/冬日/玫瑰/紫罗兰/琥珀）→ 整站实时换肤。
2. 「上传 / 自定义主题」粘贴一段主题 JSON（含 `id`/`name` 和 `light`/`dark` 的 OKLCH 颜色 token）→ 添加后全站可选。
3. 等价 API：`PUT /api/v1/setting` body `{key:"custom_themes", value:"[…themes…]"}`（需 settings:write 权限）。

主题 token 形状见 `web/src/lib/theme-presets.ts` 的 `THEME_TOKEN_KEYS`。

## 提交线

`9332865` 导入 → `6893eda` P0 → `d62357a` P1 改名 → `40afe15` P3a 主题引擎 → `5be6599` P3b 可上传主题 → `73b38d6` P4 落地页 → `74c1a0b` 部署 → `1f2b31d` README → `ac2056f` P4b 公开入口/winter默认 → `b2be5cb` D2 商业开关+注册 → `9ca394d` D3c 主题绑账户 → `3f0bf15` P4c 极光氛围。
（本地 git，未 push。）
