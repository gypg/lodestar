# Lodestar — 工作记录（防中断交接）

> 不依赖对话历史。与 `HANDOFF.md` 互补：本文件记**谁在什么阶段做了什么**、**本地未 push 的提交**、**已知中断原因**。
> 最后更新：2026-06-19

## 背景

用户在做 Lodestar（`ggzero/`）自研网关。任务多次被 **API 中断**（context 超长、502/403 额度、stream_options、空响应等），**不是用户本地网络主因**。多个模型接力：glm-5.2 → Opus → glm-5.1 → Grok 等。

## 北极星（不变）

自用优先 + 可商业化 + 主题/UI 可定制 + 聚合多上游/中转站；底座 octopus lingyu，产品独立（`github.com/gypg/lodestar`）。

## 已完成里程碑（按主题）

| 线 | 内容 | 代表 commit / 文件 |
|----|------|-------------------|
| A | 模块路径 `gypg/lodestar`、品牌 Lodestar、云端仓库 | ffc73e1, 436fba8 |
| B | moonshot/zhipu relay 净化、Chat markdown+datalist、migration 016 site_name | 517e965…e7c743b |
| 地基 #1 | 维护模式后端 `MaintenanceGuard` | 6d88ed0 |
| 地基 #2 | `apikey.ListByUser`，listAPIKey/getUsage | af39c40 |
| 地基 #3 | `DeductQuota` MIN 封顶防负余额 | 见本次 fix(quota) 提交 |
| 文档 | `docs/HANDOFF.md` 自包含交接 | d1806ea + 持续更新 |

## 本轮（Grok / 2026-06-19）具体改动

1. **#2 落地**（若上一会话未写入磁盘，本轮已确认）：`internal/op/apikey/apikey.go`、`handlers/apikey.go`、`wallet.go`、`apikey_test.go`。
2. **#3 落地**：
   - `internal/op/user/quota.go`：`DeductQuota` 使用 `MIN(quota, ?)`。
   - `internal/op/user/quota_test.go`：负余额与不足额扣费测试。
   - `internal/op/billing/billing.go`：注释说明与封顶扣费配合。
3. **HANDOFF + 本 SESSION-LOG** 更新。

## 刻意未做（避免 scope / 热路径）

- 请求前**预授权/冻结**（需新表与 relay 配对释放）。
- `HasBalanceForKey` 按预估 cost 预检（需模型价目与请求体解析）。
- `go vet` group_probe unreachable（底座遗留）。

## 接手后第一件事

```bash
cd ggzero
git status -sb
git log --oneline -5
go test ./...
go build -tags=jsoniter -o lodestar.exe .
```

若 `ahead N`：`git push origin main`。

## 下一任务建议（HANDOFF §6）

1. B 线：BaseURL 测速 / ops health 看板（先读 health API 结构）。
2. Chat 持久化（需 schema/API 设计）。
3. 可选：billing 预冻结（若商业要严格不超卖）。

## 构建与规约

- 测试：`go test ./...`
- 构建：`go build -tags=jsoniter -o lodestar.exe .`；前端改动见 HANDOFF §8。
- 行为：`agent-spec.md`；提交 conventional commits，**不加** Co-Authored-By。