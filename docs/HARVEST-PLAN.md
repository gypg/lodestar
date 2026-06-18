# GGZERO 参考项目素材盘点 + 执行分析（HARVEST-PLAN）

> 应要求：完整扫描三处参考项目，盘点**还没被 GGZERO 用上、值得拿来**的素材，分级总结，给出执行方案。
> 扫描范围：`参考项目/`（octopus 原版 / octopus-lingyu版 / SAPI）、`GGGZERO/`（new-api / home.html / color4bg / 文档版本.html）、`前端优化/`（pretext）。
> 最后更新：2026-06-18。**本文件是规划，不含实现。**

---

## 一、各项目画像（一句话 + 独特资产）

| 项目 | 栈 | 本质 | 独特资产 |
|------|----|------|----------|
| **octopus-lingyu版** | Go gin+gorm / Next.js | **GGZERO 现底座** | relay+transformer、hub 多站聚合、告警/统计/ops、AI路由、语义缓存、熔断、代理池、WebDAV备份 |
| **octopus 原版** | 同上 + axonhub | 上游清版 | 无独有价值（缺 hub；relay 绑外部 axonhub 反而更糟） |
| **SAPI** | Go net/http+pgx / React+MUI | 轻量**消费级**中转站 | ★**站内对话 + 生图工坊**、★**每用户用量可视化**、干净**用户门户**、邀请码/维护模式/横幅/SMTP/审计归档/建议反馈、Provider 健康看板 |
| **new-api** | Go gin+gorm / TanStack | 重型网关 | ★**40+ 上游适配器**、订阅系统、计费表达式、更多支付商、OAuth 全家桶、2FA/passkey、绘图/MJ/任务、数据导出、双主题 |
| **color4bg.js** | JS 库 | 氛围光 | ★landing 真实氛围光（你原本喜欢的那个） |
| **pretext** | TS 库 | 文本排版测量 | 高质量断行/排版（纸感美学增强） |
| **home.html / 文档版本.html** | 静态 | 首页设计 | 单屏版（已用）/ 报刊三栏长文版（未用） |

---

## 二、已经拿过的（别重复）

- octopus 底座全部：relay/hub/用户/统计/告警/ops/模型广场/代理池/WebDAV/AI路由/语义缓存/熔断。
- new-api 商业逻辑：预付余额 + 按量扣费 + 兑换码 + 易支付（已移植）。
- new-api 设计/思路：winter home.html 首页、主题系统、自用/商业模式、雪花品牌（已落地）。
- 多租户门户 + 数据隔离（`user` 角色、key 隔离、按角色 UI）。

---

## 三、★还值得拿的（分级，核心结论）

### Tier 1 — 把"开发者网关"升级成"消费级平台"（最高价值，强烈建议）
> GGZERO 现在能注册/充值/计费，但**普通用户拿到 key 后只能自己写代码调用**。Tier 1 让不懂代码的人也能直接用——这是"完全能用"的关键跃迁。

1. **站内对话 Chat**（源 `SAPI/client/src/user/ChatSection.jsx` 1370行）：浏览器内直接和模型聊天，走本站 `/v1` + 用户自己的 key。消费级平台标配。
2. **生图工坊 Image Playground**（源 `SAPI/.../ImagePlaygroundSection.jsx` 1523行）：站内文生图/改图/下载。
3. **每用户用量可视化**（源 `SAPI/.../UsageSection.jsx`+`TokenUsageChart.jsx`+`RequestHeatmap.jsx`）：用户看自己的请求/Token/花费曲线——补上我之前标的"自己的用量曲线"缺口（octopus 的 analytics 是管理向，非每用户）。
4. **color4bg 真实氛围光**（源 `GGGZERO/color4bg.js-main` 或 `AmbientLightBg.min.js`）：landing 换上你原本喜欢的实景氛围光（现在是 CSS 极光替代）。

### Tier 2 — 运营一个真站点的配套（中价值）
5. **邀请码注册**（SAPI InvitationCodes）：商业模式下控制注册。
6. **维护模式 + 站点横幅**（SAPI Maintenance/Banner）：运维开关。
7. **SMTP 邮件 + 邮箱验证**（SAPI SiteEmail/Smtp）：注册验证/通知。
8. **建议反馈**（SAPI Suggestions）：用户提反馈。
9. **Provider 健康看板可视化**（SAPI ProviderHealth/ModelHealth）：octopus 已有 ops/health 数据，SAPI 的可视化更直观，可借鉴样式。

### Tier 3 — 广度/深度（较难或按需）
10. **new-api 40+ 上游适配器**（relay/channel/*）：上游 provider 广度。**难**——octopus 的 relay/transformer 架构 ≠ new-api，适配器不能直接拷；octopus 的 transformer 已覆盖 OpenAI/Anthropic/Gemini + 通用透传。仅当确需某个特殊 provider 才单独移植。
11. **new-api 订阅系统**（按月套餐，区别于预付余额）：要做"会员制"再上。
12. **new-api 计费表达式**（阶梯/动态定价）：高级计费再上。
13. **pretext 排版库**：纸感标题/正文排版增强，锦上添花。
14. **文档版本.html**（报刊长文版首页）：作为另一种 landing 风格备选。

---

## 四、执行方案（如何把 Tier 1 落地）

> 移植性说明：SAPI 是 React+MUI 的 `.jsx`，GGZERO 是 Next16+React19+Radix+Tailwind 的 `.tsx`。
> **逻辑可移植**（调用 `/v1`、流式、markdown 渲染、用量聚合），**UI 需用我们的栈重写**（MUI→Radix/Tailwind + 主题 token），并接 GGZERO 自己的鉴权/key/relay。不是拷贝粘贴，是"照着做 + 适配"。

**建议分期（每期独立、可验证、可单独 commit）：**

- **阶段 A（快赢，低风险）**
  - A1 每用户用量可视化：后端加 per-user 用量端点（聚合该用户名下 key 的 logs/stats）→ 前端用量页（曲线+热力图，复用 octopus 已有 recharts）。补全用户门户。
  - A2 color4bg 真实氛围光：把 `AmbientLightBg.min.js` 放 `web/public/`，landing 动态加载初始化（失败回退现有 CSS 极光）。
  - A3（可选）pretext：landing 标题排版增强。

- **阶段 B（消费级核心）**
  - B1 站内对话 Chat：新 `chat` 模块（用户门户内），SSE 流式调本站 `/v1/chat/completions` + 用户 key，markdown/代码高亮渲染。参照 SAPI ChatSection 的交互逻辑，UI 用 Radix/Tailwind 重写。
  - B2 生图工坊：新 `image` 模块，调 `/v1/images/generations` 等，参照 SAPI ImagePlayground。
  - （B1/B2 加进用户门户导航 USER_PORTAL_NAV。）

- **阶段 C（运营配套）**
  - C1 邀请码注册（商业模式下可选）；C2 维护模式 + 横幅；C3 SMTP 邮箱验证；C4 建议反馈。

- **阶段 D（按需，较重）**
  - D1 特定上游 provider 适配（仅当 octopus transformer 不够时，从 new-api 单独移植）；D2 订阅制；D3 计费表达式。

**优先级建议**：A（补门户、还原氛围光）→ B（站内创作，决定"消费级"成色）→ C（运营）→ D（按需）。
其中 **B（站内 Chat + 生图）是把 GGZERO 从"带计费的网关"变成"人人能上手用的 AI 平台"的关键一跃**，也是工作量最大的一块，建议作为下一个主攻方向。

---

## 五、一句话结论

> 底座/商业/主题/多租户已就位；**还值得拿的核心是 SAPI 的"站内创作（对话+生图）+ 每用户用量可视化 + 干净门户"，把 GGZERO 补成消费级平台**；color4bg 还原氛围光是顺手快赢；new-api 的 40+ 适配器是"按需、较难"的广度补充。执行按 A→B→C→D 分期，逻辑移植、UI 用我们的栈重写。

## 六、执行状态（2026-06-18 收尾）

- ✅ **A1 每用户用量可视化**：`/api/v1/wallet/usage`（聚合用户名下 key 的统计）+ 钱包内用量卡（请求/Tokens/花费 + 各 key 分项）。
- ⏭ A2 color4bg：**跳过**——少女照片全屏铺底，氛围光在其后不可见，加了无效（已由实景照片取代）。
- ✅ **B1 站内对话 Chat**：新路由 `chat`，SSE 流式调本站 `/v1/chat/completions` + 用户自己的 key，选模型/密钥、停止/清空。
- ✅ **B2 生图工坊 Image**：新路由 `image`，调 `/v1/images/generations`，预览/下载。
- ✅ **C 维护模式**：管理开关 + 非管理员维护页（登录入口保持开放，管理员可登入关闭）。
- ✅ **C 邀请码注册**：`register_invite_required` 开关 + 邀请码（一次性、竞态安全）+ 管理端生成 + 注册表单邀请框。端到端实测：无码 400 / 有效码 200 / 重用 400。
- ✅ **C 意见反馈**：用户提交 + 管理员查看列表。
- ⏭ C 其余（SMTP 邮箱验证 / 站点横幅）：**未做**——SMTP 需邮件基建且无法实测投递；横幅已被站点公告覆盖。按需续接。
- ⏭ D（40+ 适配器 / 订阅制 / 计费表达式 / pretext）：按需。

> 消费级核心（站内对话 + 生图 + 每用户用量 + 干净门户）已落地，GGZERO 从"带计费的网关"变成"人人能上手用的 AI 平台"。导航：用户固定门户（首页/对话/生图/模型/密钥/设置），管理员见全部 + 新功能自动追加。
