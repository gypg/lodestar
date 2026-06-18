# Lodestar — 项目工作宪章（自研聚合 LLM 中转站）

> **代号** `lodestar`（产品显示名待你定；我会把显示名做成可改设置，符合"高自定义"愿景）。
> **本文件是工作约束书**：任何 AI / 会话接手先读这份再动手。锁定愿景、选型、铁律、当前状态、分期 DoD。
> 最后更新：2026-06-18（项目立项 + 底座选定 + 构建验证中）。

---

## 1. 北极星愿景（用户原话提炼）

> 一个**优先自用**、**每个用户的功能/主题/配色/UI 都能不同**、**主题可通过接口上传**、
> **一键切换释放商业潜力**、**能聚合官方与其他中转站**的**高自定义 LLM 中转站**。

四个支柱：
1. **自用优先 + 商业可切**：默认自用形态，一个开关释放全部商业能力（注册/计费/订阅/支付）。
2. **极致自定义**：每个用户可拥有不同的主题/配色/UI/可见功能；主题能经 API 上传，运行时切换。
3. **聚合**：既聚合官方上游（多渠道负载均衡），也聚合"别的中转站"（hub：余额/签到/公告/用量）。
4. **属于自己**：完全自主代码，不再兼容/升级 newapi；可重新部署，无升级包袱。

**成功判据（用户的话）**：最后能给出一个**能用的成品**，且体量≥ octopus-lingyu 水准。

---

## 2. 底座选型（已定，附验证）

**底座 = octopus（lingyu 版）**，本地工作区 `lodestar/`，从 `参考项目/octopus-master-lingyu版` 导入。

**为什么是它（实测对比，非拍脑袋）**：
| 候选 | 结论 |
|------|------|
| **octopus lingyu版** ✅ | 后端 `go build ./internal/...` 仅缺前端 out/（已解决），**依赖全解析、自包含**；**hub 已内建 14846 行带测试**；gin+gorm/三DB；正是"个人聚合+负载均衡站"定位 |
| octopus 原版 ✗ | `go.mod` replace `axonhub/llm => ../axonhub/llm` 依赖**本地未发布兄弟库**，无法独立构建；relay 绑死外部 axonhub |
| SAPI ✗ | 干净但仅 PG、是中转站非聚合器、hub 要从零建 |
| new-api ✗ | 用户明确放弃；20 万行庞然大物 |

**前端构建生死关已过**：Next.js 16 + React 19 + reactCompiler 在本地 `next build`（output:export）成功产出静态页（先在原版验证，lingyu 版同栈）。

**技术栈**：后端 Go 1.24（gin + gorm + 三 DB，cobra CLI，`//go:embed all:out` 嵌前端）；
前端 Next.js 16 / React 19 / Tailwind / Radix UI / TanStack Query / next-themes / next-intl / zustand（pnpm monorepo）。

---

## 3. 底座已有能力（白拿，无需重建）

- **Relay**：`internal/relay`（balancer/circuit/iterator/session）+ `internal/transformer`（OpenAI/Anthropic/Gemini 协议互转）。多渠道负载均衡 + 熔断。
- **Hub 多站点聚合**（杀手锏，已内建带测试）：`internal/hub`（adapters: aihubmix/axonhub/claudecodehub/sapi/sub2api/octopus/common + registry + httpclient）+ `internal/sitesync`（balance/sync/schedule/storage/detect/create_key/route_probe）+ `internal/site`。
- **用户/鉴权**：`internal/model/user.go` + `internal/server/auth` + WebAuthn（passkey）。
- **数据层**：model（hub 全套表：RemoteSite/BalanceSnapshot/CheckInRecord/APICredentialProfile/SiteAnnouncement/RedemptionRecord/RemoteUsageRecord/Site…）+ op（缓存+操作）+ 版本化迁移 `internal/db/migrate/001-015.go`。
- **运营**：告警（AlertRule/NotifChannel/History）、统计（StatsTotal/Daily/Hourly/Model/Channel）、价格 `internal/price`、备份、审计日志。
- **设置系统**：`internal/op/setting` 类型化 KV（GetBool/SetBool/GetString…）带缓存 → **商业开关/默认主题直接挂这里**。

---

## 4. 我要建的增量（lingyu 没有的差异化）

| # | 模块 | 本质 | 落点 |
|---|------|------|------|
| **D1** | **去身份化 + 改名** | 把 lingyuins/octopus 变成我们自己的产品 | module 路径、app 名、logo、README、显示名（保留 LICENSE） |
| **D2** | **一键商业开关** | 自用⇄商业运行时切换 | 加 `SettingKeyCommercialMode`(bool) + 后端守卫 + 前端按 flag 隐藏商业面 |
| **D3** | **每用户可上传主题系统** ★核心 | 主题包经 API 上传、运行时切换、每用户独立 | 新表（theme + user_theme_pref）+ 上传/列举/激活 API + 前端主题加载器（CSS 变量注入，承接我在 newapi 上的设计） |
| **D4** | **winter 首页/主题** | 作为首个内置主题 | 承接 `首页文件/home.html` + newapi winter-landing 设计，适配 Next.js |
| **D5** | **排版增强（可选）** | pretext 文本测量库提升纸感排版质量 | `@chenglou/pretext`（MIT），按需 |

**D3 是北极星的心脏**——其余皆为铺垫。设计承接 `THEME-SYSTEM-DESIGN.md`（newapi 时期产出）：CSS 变量预设 + 上传式主题双轨。

---

## 5. 铁律红线

1. **保 LICENSE 与上游署名**：octopus 是 AGPL，fork 改造合法，但 **LICENSE 文件保留**，NOTICE 标明 octopus/lingyu 血缘。改的是产品身份，不是抹除来源。
2. **下结论先验证**：对系统行为下判断前先读码/跑构建。没验证标"未验证"。（本宪章选型即此原则产物——靠实测构建翻转了原版→lingyu版。）
3. **失败两次即停换路**：同法失败两次退一步换根本不同方法。
4. **改动可控**：每个里程碑独立 commit（本地 git）。不 push 到任何远端除非用户要求。
5. **凭据安全**：hub 存别站凭据已有 `APICredentialProfile`；新增凭据走加密（AES-256-GCM，逻辑见我已写的 `service/hub/crypto.go`，移植适配）。不回显明文。
6. **不破坏已有测试**：lingyu 版前后端测试丰富，改动后跑测试保绿。

---

## 6. 分期计划（DoD）

| 期 | 目标 | DoD |
|----|------|-----|
| **P0 构建基线** | 前端+后端全量 build 出二进制并本地跑起来 | `lodestar.exe` 启动、浏览器进首页、能登录/看控制台 ✅ 为后续前提 |
| **P1 去身份化改名** | 产品变成我们的 | module 路径不强改(成本高，评估)、app 显示名/logo/README/标题改我们的；构建仍过 |
| **P2 商业开关** | 自用⇄商业一键切 | `CommercialMode` 设置项 + 后端守卫 + 前端隐藏商业面；切换可逆；测试绿 |
| **P3 主题系统 M1** ★ | 主题上传+激活+每用户切换最小闭环 | 新表迁移过、上传/列举/激活 API 通、前端能切主题整站变色；带测试 |
| **P4 winter 主题** | winter 作为首个上传主题跑通 | 进站冬日首页、整站冬日配色 |
| **P5 收尾** | 文档 + 部署说明 + 对话存档 | 能交付的成品 + README + 部署指引 |

> **今晚（到 12 点）现实目标**：P0 必达（能跑的成品基线）；P1/P2 尽量；P3 起步。完整 P3/P4 多日。
> 不撒"今晚全做完"的谎——但保证你到点能看到一个**能跑、是我们自己的、带聚合 hub 的中转站**。

---

## 7. 当前状态

- 工作区 `lodestar/` 从 lingyu 版导入，git 初始化（`9332865`）。
- 前端 `pnpm install + next build` 后台进行中（大依赖树）。
- 已摸清：迁移系统（版本化 + AutoMigrate db.go:216）、设置系统（类型化 KV）、hub 模块全貌、前端主题地基（`provider/theme.tsx` + `setting/Appearance.tsx` + next-themes）。
- 下一步：build 完成 → 复刻 `web/out→static/out` → `go build` 出二进制 → 本地跑（P0）。

---

## 8. 给接手 AI 的元指令

先读本宪章 §1 愿景 + §2 选型 + §5 铁律，再看 §6 定位当前期、§7 当前状态。D3（每用户可上传主题）是核心，别本末倒置去重做 hub（已有）。验证驱动，里程碑 commit，不 push。
