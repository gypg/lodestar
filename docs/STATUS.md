# Lodestar — 当前状态与运行手册（STATUS）

> 最后更新：2026-06-19（体验线 P1 + 增强波 C）。配套：`docs/CHARTER.md`、`_workspace-private/PROGRESS-MAP.md`。

## 一句话

**Lodestar**（`github.com/gypg/lodestar`）—— 高自定义 LLM 聚合中转站：主题/商业/消费级门户/运维健康已齐，远端 `main` 含 Chat 持久化、热力图、Banner、按模型用量、渠道 7 日成功率条、报刊落地页可选。

## 现在能用什么（已验证）

| 能力 | 状态 |
|------|------|
| 全栈构建 + SQLite 开箱 | ✅ |
| relay + hub + 用户/密钥/统计/告警/审计/分析台 | ✅ |
| 主题 + **winter / 报刊** 封面（`landing_layout`）+ color4bg | ✅ |
| 商业：余额/兑换/易支付/多租户 | ✅ |
| **Chat** SSE + Markdown + **会话持久化**（侧栏） | ✅ |
| 钱包：**14 日曲线** + **30 日热力图** + **按模型**（`per_model`，需历史日志） | ✅ |
| **全站 Banner**（站点信息配置） | ✅ |
| 公开 ping + Ops **波 B 健康卡片** + **渠道 7 日成功率 sparkline**（需历史日志） | ✅ |
| 维护/邀请/SMTP/反馈、CI | ✅ |

## 还没做（按需）

- **billingexpr** / 订阅 / OAuth / 2FA（商业专项，工作量大）
- **pretext** 刊头排版（P3 美学）
- 模块化 i18n（部分中文直写）
- 西安机 **Lodestar 独立部署** + PG 日志库（见 `docs/DEPLOY.md`、`scripts/verify-heatmap-server.sh`）

## 运维提示

| 项 | 说明 |
|----|------|
| 热力图 / 按模型 / sparkline 无数据 | 开启 **保留历史日志**（`relay_log_keep_enabled`）；日志库可用（SQLite 默认同库或配 PG `log_path`） |
| 封面版式 | 设置 → 站点信息 → **封面版式**：冬日目录 \| 报刊三栏 |
| 验收脚本 | `BASE=... TOKEN=... bash scripts/verify-heatmap-server.sh` |

## 怎么跑（本地）

```bash
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION=dev pnpm build && cd ..
rm -rf static/out && cp -r web/out static/out   # Windows: 用 xcopy / robocopy
go build -tags=jsoniter -o lodestar.exe .
./lodestar.exe start   # http://localhost:8080
```

## 近期提交线

`63eb38f` P1 门户 → `1e981cf` 热力图验收脚本 → **本地待 push** 增强波 C（per_model、sparkline、newspaper landing、STATUS）。