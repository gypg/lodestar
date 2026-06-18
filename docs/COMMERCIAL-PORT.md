# GGZERO 商业层移植（COMMERCIAL-PORT）

> 应"商业纵深直接套用 new-api 逻辑"——本文件记录把 new-api 成熟的预付费计费逻辑移植到 ggzero
> （octopus 基底）的方案、已完成部分、与下阶段。最后更新：2026-06-18。

## 一、架构差异（为什么不是拷贝粘贴）

| 维度 | new-api（SaaS 计费） | octopus（管理网关） |
|------|--------------------|--------------------|
| 用户余额 | `User.Quota`（整数 quota，QuotaPerUnit/$1） | 无 |
| Key 归属 | Token 属于用户(`UserId`)，有 RemainQuota | APIKey **无 UserID**，仅 MaxCost 上限 |
| 计费 | 请求按成本扣用户/Token 余额 | 算成本只记统计，**不扣费** |
| 充值 | topup + 兑换码 + 支付(Epay/Stripe/Creem/Waffo) | 无（redemption 是 hub 远程功能） |

→ 移植的是**逻辑**（预付余额 + 按量扣费 + 兑换码充值），**适配** octopus 的 float-USD 成本（比 new-api 整数 quota 更贴合 octopus 既有的 `StatsMetrics.Input/OutputCost`）。

## 二、已完成并验证（commit 3617260）

**绑定商业开关**：所有计费仅在 `commercial_mode=true` 时生效；自用模式下零影响（admin 免费用）。

1. **用户余额**：`User.Quota`/`UsedQuota`(float USD)；`op/user/quota.go`（Get/Add/Deduct/Set，原子更新）。
2. **Key 归属**：`APIKey.UserID`；建 key 时归属当前用户（`createAPIKey`）。
3. **relay 计费**（`op/billing`）：
   - 请求前（`middleware/auth.go`）：商业模式且 owner 余额≤0 → 402 拒绝（无主键/自用模式放行，fail-open 防误伤热路径）。
   - 请求后（`relay/metrics.go` + `media_relay.go` 的 `APIKeyUpdate` 处）：按 `InputCost+OutputCost` 扣 owner 余额。
4. **充值·兑换码**（`model/topup_code.go` + `op/topup` + `handlers/wallet.go`）：admin 生成 N 个 $X 码；用户兑换入账。**事务 + 条件更新 + RowsAffected 校验**，竞态安全、不可重复兑换。
5. **钱包 UI**（`SettingWallet.tsx`）：余额展示 + 兑换码充值 + 管理员生成码。
6. **API**：`GET /wallet/balance`、`POST /wallet/redeem`（自助）；`POST /wallet/codes`、`GET /wallet/codes`、`POST /wallet/grant`（管理）。

**端到端实测**：注册→余额0→兑换$5→余额5→重复兑换被拒(400)。✅

7. **在线支付·易支付(Epay)**（commit 2889621，移植 new-api `controller/topup.go`，复用同款 `github.com/Calcium-Ion/go-epay` 库）：
   - 设置：`epay_enabled/pay_address/epay_pid/epay_key/topup_rate/payment_callback_base`（管理员后台「在线支付·易支付」卡配置；**建功能不需凭据，凭据是运行时配置**，对齐 new-api）。
   - 模型 `PaymentOrder` + `op/payment`：下单（`Purchase` 出签名 URL）→ 用户付款 → 公开回调 `GET/POST /api/v1/wallet/epay/notify`（`Verify` 验签 + TRADE_SUCCESS）→ **事务幂等入账**（pending→success 条件更新，重复回调不重复入账）。
   - 钱包 UI：在线充值（金额 + 支付宝/微信 → 构造表单跳转网关）。
   - 实测：表 `payment_orders` 建好、公开回调可达、未配置返 "fail"、下单鉴权门控。真实支付流需管理员填商户凭据 + 公网回调后联调。

## 三、下阶段（按需）

1. **其他支付商**（Stripe/Creem/Waffo）：Epay 已覆盖国内主流；如需国际卡支付，按相同模式（设置+下单+webhook 验签入账）从 new-api 续接移植。
2. **多租户数据隔离**：octopus 全 UI 面向 admin（看全部 key/日志/统计）。商业模式下注册用户应只见自己的数据——需在 `listAPIKey`/log/stats 等按 `UserID` + 角色过滤。这是把"管理网关"彻底变"多租户 SaaS"的较大正确性工程。**当前**：计费经济闭环 + 兑换码 + 在线支付已通；单人/小团队自用+售卖额度已够用，公开多租户运营前需补隔离。
3. **配额展示/告警**：余额不足提醒、用量曲线（公开模型广场已有价格表）。

> 一句话：**new-api 的"预付余额+按量扣费+兑换码+易支付在线充值"商业闭环已移植落地；其他支付商与多租户数据隔离按需续接。**
