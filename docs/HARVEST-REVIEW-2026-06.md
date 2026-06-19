# Lodestar 参考素材复审（2026-06-19）

> **目的**：在多次换模型、任务中断后，用**当前 `ggzero/` 代码**重新对照三处参考目录，判断还能帮「我们自己的 Lodestar」做什么。  
> **原则**：octopus 只是基地；借鉴的是**能力与体验**，不是品牌或架构照搬。  
> **行为规约**：上层 `agent-spec.md`（先调查后断言、默认行动、失败换路、高风险先确认）。

**扫描范围**（路径以工作区为准）：

| 目录 | 内容 |
|------|------|
| `参考项目/` | octopus 原版、octopus-master-lingyu版、SAPI-main |
| `GGGZERO/` | new-api-main、color4bg.js-main、home.html、文档版本.html、AmbientLightBg.min.js |
| `前端优化/` | pretext-main |
| **产品本体** | `ggzero/`（Lodestar） |

旧版规划见 `docs/HARVEST-PLAN.md`（2026-06-18）。**本文件 supersede 其「还没做」部分**，保留其项目画像表作参考。

---

## 一、Push 与仓库状态（执行建议时先做）

- **建议**：`cd ggzero && git push origin main`（本地曾 **ahead 3**：HANDOFF、key 隔离、quota 封顶）。
- **未验证**：本轮自动 push 曾 `Connection was reset` / timeout——属访问 GitHub 链路问题，**不表示提交丢失**。以 `git status -sb` 为准。

---

## 二、Lodestar 现在真实有什么（相对旧 HARVEST 的修正）

以下以 **ggzero 代码 + 近期 commit** 为准，**不是**仍停留在 2026-06-18 的 HARVEST-PLAN 勾选。

### 已从参考思路落地（不必再当「Tier 1 从零移植」）

| 能力 | 旧 HARVEST 说法 | 当前 Lodestar |
|------|-----------------|---------------|
| 站内 Chat | SAPI ChatSection 待移植 | ✅ `web/.../chat`，SSE + **Markdown** + 模型 **datalist** |
| 生图工坊 | SAPI ImagePlayground 待移植 | ✅ `web/.../image`（模型多为手填，见缺口） |
| 每用户用量 | SAPI Usage 曲线/热力图 | ✅ `/api/v1/wallet/usage` + 钱包页展示（**偏汇总卡**，非 SAPI 级图表） |
| 邀请码 / 维护 / 反馈 / SMTP | SAPI Tier 2 | ✅ 后端+设置已有（invite、maintenance 守卫、verification 等） |
| 商业预付 + 易支付 | new-api | ✅ billing + wallet |
| 国内 OpenAI 兼容净化 | new-api 思路 | ✅ moonshot / zhipu（+ deepseek 底座） |
| 地基安全 3 项 | 审查债 | ✅ 维护后端守卫、ListByUser、DeductQuota MIN 封顶 |

### 明确**还没**从参考目录拿上的（仍值得做）

| 素材来源 | 能力 | Lodestar 缺口 | 移植难度 |
|----------|------|---------------|----------|
| **SAPI** `BaseUrlLatencySection.jsx` | 用户侧 BaseURL **时延自测**（浏览器 fetch health，3 次采样） | 无对等 UI；ops 有数据但用户门户未暴露 | **低**（几乎零后端，逻辑照抄改 Tailwind） |
| **SAPI** `ProviderHealthSection` / `ModelHealthSection` | Provider/Model **健康看板**可视化 | 有 `ops` 模块 + 后端 health，**可视化不如 SAPI 直观** | **中**（读 ggzero ops API，UI 用 Radix 重写） |
| **SAPI** `UsageSection` + 图表/热力图 | 用量 **曲线 + 热力图** | 钱包只有汇总数字 | **中**（需确认 stats/logs 是否够聚合 per-user 时间序列） |
| **GGGZERO** `AmbientLightBg.min.js` / color4bg | Landing **实景氛围光** | winter 用 CSS 极光 + 大图；**未**接 color4bg | **低–中**（动态 script + 失败回退现有背景） |
| **GGGZERO** `文档版本.html` | 报刊三栏 **长文 landing** | 仅 winter 单屏 + public overview | **低**（静态结构借鉴，主题 token 适配） |
| **前端优化** pretext | 标题/正文 **排版测量**（纸感） | 未引用 | **低**（可选 npm 依赖，landing/公告区） |
| **new-api** `pkg/billingexpr` | **表达式计费**（阶梯、缓存价、多模态细项） | 仅 float USD 按 metrics 扣费 | **高**（独立 pkg 可 vendoring，需接 Lodestar price/relay 结算点） |
| **new-api** relay/channel/* | 40+ **上游适配器** | octopus **按协议 transformer**，非按 vendor 拷贝 | **高**（仅缺哪家补哪家，如已做的 moonshot/zhipu 模式） |
| **new-api** | OAuth / 订阅 / 2FA / Task 绘图任务 | 未做 | **高**（按需专项） |

### 刻意不做或延后（与身份一致）

- 不把产品名/模块改回 octopus；不动 `hub/octopus` **功能目录名**（聚合语义）。
- 不整包替换 relay 为 new-api 架构。
- 余额 **预冻结**（防超卖精确到请求）：未做；当前事后扣费 + 不负余额。

---

## 三、按「帮我们完成 Lodestar」重新排优先级

结合 `CHARTER` / `HANDOFF`：**自用 + 可商用 + 主题/UI 差异化 + 聚合**。

### P0 — 快赢、直接改善「真用户」体验（建议接下来 1–2 周）

1. ~~**BaseURL 时延自测**~~ ✅ `GET /api/v1/public/ping` + `BaseUrlLatencyPanel`（API 指引页）。

2. **用量可视化升级**（在现有 `useUsage` 上）  
   - 若后端只有汇总：先加 **按日聚合** 端点（用户名下 keys 的 stats/logs），再 recharts 曲线（octopus analytics 已有 recharts 经验）。

3. ~~**Image 模型数据源**~~ ✅ `filterImageModelNames` + datalist（公开模型表启发式）。

4. **git push** 把 ahead 提交同步到 `gypg/lodestar`，避免只有本机有地基 #2/#3。

### P1 — 运维与观感（中等工作量）

5. **Ops 健康看板 UX**（借鉴 SAPI ProviderHealthCard 信息密度，数据仍走 Lodestar ops API）。  
6. **color4bg / AmbientLight** 可选开关：设置项「landing 氛围光引擎」= css | color4bg，默认 css（`GGGZERO/AmbientLightBg.min.js` 已在本机）。  
7. **winter-bg.jpg** 压缩/懒加载（HANDOFF 收尾债）。  
8. **Chat 会话持久化**（产品级）：需新表 + API + 前端列表；参考 SAPI 交互，**Lodestar 自己 schema**。

### P2 — 商业深度（按需）

9. **billingexpr**（`GGGZERO/new-api-main/pkg/billingexpr`，~540 行级独立包）— 仅当要阶梯价/缓存计费/复杂多模态计价。  
10. **订阅制**（与预付余额正交）— new-api 有完整模型，工作量大。  
11. **单点 upstream 适配** — 缺哪家再从 new-api 读 adaptor，挂到 octopus openai/anthropic outbound 特判（延续 moonshot/zhipu 做法）。

### P3 — 锦上添花

12. pretext 用于 landing 刊头/长文标题。  
13. `文档版本.html` 第二套 landing 模板（主题变量驱动）。

---

## 四、各参考库「一句话」是否还值得打开

| 库 | 2026-06-19 结论 |
|----|-----------------|
| octopus-lingyu版 | **已是底座**；仅当要对比上游新功能时 diff，不整体合并。 |
| octopus 原版 | **可忽略**（无 hub、绑 axonhub）。 |
| SAPI-main | **最有剩余价值**：时延自测、健康看板 UI、用量图表交互。 |
| new-api-main | **计费表达式 + 支付/OAuth/订阅 + 稀有 adaptor**；勿整体迁 relay。 |
| GGGZERO 静态/JS | **视觉资产**（color4bg、home/文档版 HTML）；new-api 当逻辑仓库。 |
| pretext-main | **可选美学**；与核心网关能力无关。 |

---

## 五、与 `agent-spec.md` 的对齐（给后续任意模型）

在 Lodestar 仓库工作时，建议把 agent-spec 槽位填为：

- **角色**：资深全栈（Go gin + Next 静态导出 + 商业/多租户）。  
- **单一目标**：在 **不变成 octopus/new-api 复制品** 的前提下，把 Lodestar 做成可自用、可商用、体验完整的自有网关。  
- **纪律**：改计费/隔离/鉴权先读 `HANDOFF.md` §7；结论写清「已读文件 / 已跑测试」；同一招失败两次换方案。  
- **高风险**：push、删 `data/`、改生产 env、大范围重命名 — 先说明后果。  
- **换模型续作**：**只读** `docs/HANDOFF.md` + `docs/SESSION-LOG.md` + **本文件**，不要依赖长对话历史（避免 context 超限中断）。

---

## 六、建议的下一 commit 主题（可选）

用户确认优先级后，可按独立 commit 推进，例如：

- `feat(wallet): base URL latency self-test (SAPI-inspired)`  
- `feat(usage): per-user daily usage series for charts`  
- `feat(ops): provider health cards (UI parity with SAPI)`  
- `feat(landing): optional color4bg ambient with CSS fallback`

---

**维护**：完成功能后同步更新 `HANDOFF.md` §6、`STATUS.md`、本文件 P0 勾选状态。