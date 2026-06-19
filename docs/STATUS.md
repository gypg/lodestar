# Lodestar — 当前状态与运行手册（STATUS）

> 最后更新：2026-06-19（波 D billingexpr 完成、报刊版已删除）。配套：`docs/CHARTER.md`、`_workspace-private/HANDOFF.md`。

## 一句话

**Lodestar**（`github.com/gypg/lodestar`）—— 冬日风唯一首页 + Chat 持久化 + 热力图 + 按模型用量 + 7d sparkline + billingexpr 表达式计费 + 全站 Banner。

## 现在能用什么

| 能力 | 状态 |
|------|------|
| 全栈构建 + SQLite 开箱 | ✅ |
| relay + hub + 用户/密钥/统计/告警/审计/分析台 | ✅ |
| **冬日风落地页**（唯一首页，承自 `首页文件/home.html`）| ✅ |
| color4bg / classic 背景可选（`landing_ambient_mode`）| ✅ |
| 商业：余额/兑换/易支付/多租户 | ✅ |
| **billingexpr 表达式计费**（设置→表达式计费）| ✅ |
| **Chat** SSE + Markdown + **会话持久化**（侧栏）| ✅ |
| 钱包：**14 日曲线** + **30 日热力图** + **按模型** | ✅ |
| **全站 Banner** | ✅ |
| Ops **波 B 健康卡片** + **渠道 7 日成功率 sparkline** | ✅ |
| 维护/邀请/SMTP/反馈、CI | ✅ |

## 还没做

- billingexpr 可视编辑器（当前为 JSON 输入）
- Ops 30d 可用性条
- i18n 全模块
- pretext 刊头排版（P3）
- OAuth / 订阅 / 2FA（卖会员时）
- 西安服务器部署 Lodestar

## 运维提示

| 项 | 说明 |
|----|------|
| 热力图/按模型/sparkline 无数据 | 开启 `relay_log_keep_enabled` |
| 表达式计费 | 设置→表达式计费→添加模型+表达式→保存→relay 自动生效 |
| 验收脚本 | `bash scripts/verify-heatmap-server.sh` |

## 怎么跑

```bash
cd ggzero
go build -tags=jsoniter -o lodestar.exe .
./lodestar.exe start   # http://localhost:8080
```

## 提交线

`58360da` → ... → `d8e832e` billingexpr → `107ef97` 删除报刊版 ← 最新