# Lodestar — 交接文档（HANDOFF）

> **给下一个接手的 AI（无论 Claude / GPT / Grok / DeepSeek / GLM…）**：
> 只读这一份 + `docs/CHARTER.md`（愿景）+ `docs/STATUS.md`（功能现状）就能无缝接上，
> 不依赖任何对话历史。本文件随每次重大进展更新。
>
> 最后更新：2026-06-19（HANDOFF 文档 + 地基安全债 2/3：op 层 key 隔离）。
> 行为规约：遵循项目上层目录的 `agent-spec.md`（模型无关行为规约）。

---

## 0. 30 秒速览

- **产品**：Lodestar —— 高自定义、自用优先、可一键商业化、能聚合多上游与多中转站的个人 LLM 网关。
- **底座**：octopus（lingyu 版）的 fork，但**已是独立产品**，不是 octopus。
- **代码**：`项目根/ggzero/`（目录名仍叫 ggzero，未改；模块/产品名已是 lodestar）。
- **云端**：https://github.com/gypg/lodestar （Public，AGPL-3.0）。本地 git 与远端同步。
- **跑起来**：`cd ggzero && ./lodestar.exe start` → http://localhost:8080 （SQLite，数据在 `data/`）。
- **改完代码必做**：`go test ./...` 全绿 + `go build -tags=jsoniter -o lodestar.exe .` 通过，再提交。
- **当前实例**：:8080 运行中。

---

## 1. 北极星愿景（不可偏离）

> 一个**优先自用** + **一键开关释放商业潜力** + **每用户主题/配色/UI 可不同、主题可经接口上传** +
> **聚合官方上游与其他中转站** 的 **高自定义 LLM 中转站**，且**完全属于自己**（不再是任何 fork 的影子）。

用户原话精神：「我们只是用 octopus 做基地，但不是它，做的是我们自己的。」
用户给了完全决策权（选型/规划/执行/交付）。推进时**默认行动**，但高风险/对外/不可逆操作（push、建仓、删数据、停用户在跑的实例）先说清后果再做。

---

## 2. 身份（A 线已落地，2026-06-19）

| 项 | 值 |
|---|---|
| Go module path | `github.com/gypg/lodestar`（原 `github.com/lingyuins/octopus`） |
| 产品名 | Lodestar；`conf.APP_NAME = "lodestar"` |
| API key 前缀 | `sk-lodestar-`（派生自 APP_NAME，自动跟随） |
| 兑换码前缀 | `ls-`（原 `gz-`） |
| env 前缀 | `LODESTAR_*`（动态派生 `strings.ToUpper(APP_NAME)+"_..."`，如 LODESTAR_DATA_DIR / LODESTAR_AUTH_JWT_SECRET / LODESTAR_INITIAL_ADMIN_USERNAME） |
| 二进制 | `lodestar.exe` |
| 上游血缘 | NOTICE.md 保留 octopus/lingyu AGPL 署名（**铁律：不可删**） |

**重要**：`internal/hub/octopus` 适配器目录名是**功能语义**（聚合 octopus 类型的上游站点），**不是**自身品牌，**不要改**。

---

## 3. 当前真实功能边界（代码核实，非文档臆断）

**完整可用**：全栈构建+运行、relay 多上游负载均衡+熔断、**hub 多站点聚合**（14846 行带测试，octopus 底座白拿）、用户/密钥/统计/告警/审计、**主题系统**（9 预设 + 接口上传自定义主题 + 绑账户跨设备 + winter 冬日首页）、**公开平台门面**（无鉴权 /api/v1/public/overview + 落地页公开导航）、**商业计费层**（余额/兑换码/易支付在线充值，验签+幂等+竞态安全）、**多租户**（user 角色 + key 按 UserID 隔离）、**消费级**（站内 Chat + 生图 + 每用户用量）、运营（维护模式/邀请码注册/意见反馈/SMTP 邮箱验证）。

**国内上游适配**：deepseek（底座原生支持）、moonshot、zhipu GLM 三家的 OpenAI 协议 chat 已打通（见 §5）。

---

## 4. A 线提交（身份改造，已 push）

```
ffc73e1 refactor: rename module path lingyuins/octopus -> gypg/lodestar (333 文件 1069 处)
436fba8 refactor: rebrand ggzero -> Lodestar (84 文件：APP_NAME/key前缀/env/Docker/docs)
a11742e chore: drop redundant octopus*.exe gitignore entries
```
（之前还有 80c4faf 审计白名单修复、f53b68f 测试 fixture 修复。）

---

## 5. B 线提交（消费级 + 聚合深度 + 地基，已 push）

```
517e965 feat(relay): moonshot + zhipu GLM OpenAI-compat 请求净化
        - moonshot: kimi-k2.6 强制 temperature=1.0
        - zhipu: TopP>=1 截断到 0.99 + image_url data-URI 前缀剥离
        - 落点 internal/transformer/outbound/openai/provider_compat.go
        - 关键认知：octopus 按"协议"(OpenAI/Anthropic/Gemini)分 outbound，不按 provider；
          国内 provider 靠 base URL/模型名识别做特判，挂在 SanitizeRequestForOpenAICompat。
          deepseek 已有完整支持（outbound/openai/deepseek.go）。
c14e9c3 feat(chat): 模型名 datalist 联想（复用 usePublicOverview 的 models[]）
bed616e feat(chat): assistant 消息 markdown 渲染（react-markdown + remark-gfm +
        rehype-sanitize 防 XSS）。新依赖已 pin。组件 web/src/components/modules/chat/Markdown.tsx
e7c743b fix(migrate): DB site_name GGZERO->Lodestar（migration 016，幂等，只改旧默认值，
        保留用户自定义）。升级路径修复。
6d88ed0 feat(maintenance): 维护模式后端守卫（原只有前端 gate，可绕过）
        - MaintenanceGuard 全局中间件，挡非 staff 的 /api/v1/ 写，返回 503
        - 豁免 login/register/bootstrap/public/health；放行 staff；不挡 GET、不挡 relay(/v1/*)
        - internal/server/middleware/maintenance.go
```

---

## 6. 待办（按优先级，下一个接手从这里继续）

### 地基安全债（审查发现，优先于新功能 —— 完成 2/3）
1. ✅ **维护模式后端守卫**（6d88ed0）
2. ✅ **op 层 API Key 隔离**：`apikey.ListByUser(uid)` + `listAPIKey`/`getUsage` 用户路径改用之；`List` 仍供 staff/ops/analytics。测试 `internal/op/apikey/apikey_test.go`。
3. ⬜ **余额事后扣费无预授权**：`op/billing.ChargeKey` 是请求**完成后**扣费（relay/metrics.go:155、media_relay.go:402），`op/user/quota.go DeductQuota` 不检查 `quota >= cost`，并发请求可把余额打到负数。建议加预授权/冻结，或在 DeductQuota 加下限保护 + 余额闸更早触发。动前先想清楚对 relay 热路径的性能影响。

### 功能增量（B 线剩余，按需）
- BaseURL 延迟测速（前端 fetch /api/v1/ops/health 自测链路，零后端成本 —— 先确认 health 端点返回结构）
- Provider/Model 健康看板（SAPI 前端可视化 + octopus 已有 ops/health 数据）
- Chat 消息持久化（需后端存储设计，较大）
- Image 模型下拉（chat 已做；image 模型≠chat 模型，需单独数据源）

### 更大的（参考项目已盘点，见 docs/HARVEST-PLAN.md）
- billingexpr 表达式计费（new-api，540 行独立 pkg，阶梯/缓存/多模态精准计费）
- OAuth 全家桶（generic + registry，管理员热配第三方登录）
- 订阅制（按月套餐，正交于预付余额）
- 2FA/Passkey、TaskAdaptor 任务系统（视频/音乐/绘图）

### 收尾债
- `go vet` 报 `internal/helper/group_probe.go:621 unreachable code`（**底座遗留**，非本项目引入，未修以避免 scope creep）
- winter-bg.jpg 2.1MB 未压缩/未懒加载

---

## 7. 关键技术点（接手必读，避免踩坑）

- **路由注册**：各 handler `init()` 自注册 `router.NewGroupRouter("/api/v1/x").Use(middleware.Auth())...`。全局中间件在 `internal/server/server.go` 的 `r.Use(...)`，顺序：SecurityHeaders → Cors → **MaintenanceGuard** → AuditManagementWrite → Static。
- **审计白名单**：新增管理写路由（POST/PUT/PATCH/DELETE under /api/v1/）必须登记 `internal/server/middleware/audit.go` 的 `auditedManagementWriteRoutes`，或在 `internal/server/audit_route_test.go` 的 `exemptFromAudit` 写明豁免理由——否则 `TestAllManagementWriteRoutesAreAudited` 失败。公开端点（无 session）走 exempt，登录/管理端点走白名单。
- **设置系统**：`internal/model/setting.go` `DefaultSettings()` 预 seed KV；`internal/op/setting` 的 GetBool/GetString/GetInt/SetString。GetBool 只读内存 `settingCache`（不直接碰 DB），测试可 `setting.GetCache().Set(key, val)` 注入。
- **DB 迁移**：`internal/db/migrate/0NN.go`，`RegisterAfterAutoMigration(Migration{Version, Up})`，幂等。新迁移用下一个版本号（当前最高 016）。
- **主题**：CSS 变量预设 `web/src/lib/theme-presets.ts`（9 个 BUILTIN_PRESETS + THEME_TOKEN_KEYS）；applier `web/src/provider/theme.tsx`；上传式自定义主题走 `custom_themes` 设置 + `web/src/stores/custom-themes.ts`。
- **上游 transformer**：`internal/transformer/outbound/<协议>/`（openai/anthropic/gemini/...）。按**协议**分类不按 provider。provider 特判挂在 `outbound/openai/chat.go` 的 `SanitizeRequestForOpenAICompat`，按 base URL/模型名识别。
- **计费**：`op/billing.ChargeKey`（relay/metrics.go、media_relay.go 调用）+ `middleware/auth.go` 的 HasBalanceForKey 余额闸。绑 `commercial_mode` 设置。
- **角色**：admin/editor = staff（完整控制台）；viewer = 只读 staff；**user** = 最小权限商业注册用户（apikeys:read/write + stats:read，**无 settings:read** 防 epay_key 泄露）。后端 staff 判断 = role == admin || editor。
- **前端构建**：`web/next.config.ts` 设了 `typescript.ignoreBuildErrors`。`npx tsc --noEmit` 会报一批**底座遗留**类型错误（next.config/group-progress/log/alert 测试），不是你引入的，别去修。
- **Windows 构建坑**：`go build` 会产生 `lodestar.exe~` 原子写入临时文件，`.gitignore` 已加 `*.exe~`。提交前确认 `git ls-files | grep exe` 无 .exe/.exe~ 被追踪。

---

## 8. 怎么继续工作（标准流程）

1. 读本文件 + CHARTER + STATUS，定位 §6 待办。
2. 改代码前先读相关现有代码（先调查后断言），匹配既有风格。
3. 改完：`go test ./...`（41+ 包全绿）→ `go build -tags=jsoniter -o lodestar.exe .`。
4. 前端改动：`pnpm --dir web build && rm -rf static/out && cp -r web/out static/out`，再 go build（前端产物经 `//go:embed` 嵌入）。
5. 提交：conventional commits（feat/fix/refactor/docs/chore），写清"为什么"。**全局规则：attribution 已禁用，不加 Co-Authored-By。**
6. push：`git push origin main`（用户已授权 push 到 gypg/lodestar）。
7. 重大进展后更新本文件 + `~/.claude/projects/.../memory/ggzero-project.md`（auto-memory）。

---

## 9. 当前状态快照（2026-06-19）

- **49 commits**，本地 = 远端 `gypg/lodestar` main 同步。
- 实例 :8080 运行中（SQLite，data/data.db）。
- `go test ./...` 全绿（0 FAIL）。
- 地基安全债 **2/3** 完成；下一个接手从 §6 的 **#3（余额预授权/扣费下限）** 继续。
- 若 `git status` 显示 **ahead of origin**：先 `git push origin main`（此前 HANDOFF 曾因 GitHub 连接 reset 未推上）。
