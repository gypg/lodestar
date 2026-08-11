# Lodestar 用户体验问题清单 - 2026-08-11

## 优先级分类
- **P0 (Critical)**: 阻塞核心功能，需立即修复
- **P1 (High)**: 严重影响用户体验，优先修复
- **P2 (Medium)**: 明显缺陷，应尽快修复
- **P3 (Low)**: 改进建议，可延后处理

---

## 问题清单

### 1. ★ 站点管理 - 无法重复添加站点
**问题描述**: 站点管理只可以添加一个站点，之后没有其他可以再次添加站点的入口

**优先级**: P0 (Critical)

**影响范围**: 站点管理 (web/src/components/modules/site/index.tsx)

**现象**:
- 首次可以添加站点
- 添加第一个站点后，无法找到"新增站点"的按钮或入口
- 用户被阻塞，无法管理多个站点

**预期行为**: 
- 应在站点列表顶部或底部提供持久的"新增站点"按钮
- 空状态和非空状态都应有明确的添加入口

**技术分析**:
- ✅ 已确认：`setSiteHandlers` 中的 `openCreateDialog` 已正确绑定(line 1206-1208)
- ✅ 已确认：空状态有"新增第一个站点"按钮(line 1971)
- ❌ 问题根因：非空状态下没有任何"新增站点"按钮
- ❌ 工具栏(Toolbar)只为 channel/group/model 页面显示，不支持 site 页面
- 需要在 CheckinPanel 上方或页面其他显著位置添加"新增站点"按钮

---

### 2. ★ 站点账号 - 仅支持 Access Token 验证
**问题描述**: 站点账号只能使用 Access Token 进行验证和签到,用户名密码无法连接同步

**优先级**: P1 (High)

**影响范围**: 
- 站点账号管理 (web/src/components/modules/site/AccountEditDialog.tsx)
- 账号同步逻辑 (internal/op/site/)

**现象**:
- 添加账号时选择"用户名/密码"类型
- 同步操作失败或跳过
- 签到功能无法使用

**预期行为**:
- 用户名/密码类型的账号应支持自动登录、同步和签到
- 应有明确的错误提示说明哪种凭据类型支持哪些功能

**技术分析**:
- 检查后端 `internal/op/site/sync.go` 的凭据类型处理
- 确认前端是否正确传递 `credential_type`
- 验证签到逻辑是否针对不同凭据类型有条件分支

---

### 3. ★ 站点密钥显示 - 通过站点管理添加的渠道无密钥显示
**问题描述**: 站点添加密钥后，只有直接在渠道页面添加的站点显示对应密钥，通过站点管理添加的渠道无法显示密钥

**优先级**: P1 (High)

**影响范围**: 
- 站点渠道模块 (web/src/components/modules/site-channel/index.tsx)
- 密钥创建逻辑 (internal/op/site_channel/)

**现象**:
- 在站点管理中添加站点 → 自动生成渠道
- 在站点管理中为账号添加密钥
- 切换到"渠道"页面，该渠道下看不到密钥
- 直接在渠道页面添加的站点可以正常显示密钥

**预期行为**:
- 所有站点(无论创建路径)在渠道页面中都应显示对应的密钥
- 密钥数据应在站点和渠道视图中保持一致

**技术分析**:
- 检查 `site_channel` 和 `site_account.tokens` 的关联逻辑
- 确认密钥是否正确关联到 channel_id
- 验证渠道列表查询是否遗漏了某些关联表的 JOIN

**排查步骤**:
1. 检查数据库中 `tokens` 表的 `channel_id` 字段
2. 对比站点管理创建的渠道和直接创建的渠道的数据结构
3. 追踪 `useSiteChannelList` 的查询逻辑

---

### 4. ★ AI 路由 - 分组界面无法使用
**问题描述**: 分组界面的 AI 路由功能无法使用

**优先级**: P1 (High)

**影响范围**: 
- 分组管理 (web/src/components/modules/group/index.tsx)
- AI 路由按钮 (web/src/components/modules/group/AIRouteButton.tsx)

**现象**:
- 点击分组界面的"AI 路由"按钮无响应
- 或者按钮不可见/禁用状态

**预期行为**:
- 点击"AI 路由"应弹出配置或执行对话框
- 应能为分组生成 AI 路由策略

**技术分析**:
- 检查 `AIRouteButton` 的 disabled 条件
- 确认 `useGenerateAIRoute` mutation 的触发逻辑
- 验证后端 `/api/v1/group/ai-route` 端点是否正常

---

### 5. ★ AI 路由分析配置 - 无法保存本站模型
**问题描述**: 在分析中心评估页面，选择本站模型时无法点击保存按钮

**优先级**: P1 (High)

**影响范围**: 
- 评估页面 (web/src/components/modules/analytics/Evaluation.tsx)
- AI 路由配置 (web/src/components/modules/analytics/AIRouteConfig.tsx)

**现象**:
- 切换到"本站模型"模式
- 从下拉列表选择模型
- 没有"保存"按钮出现，或按钮不可点击

**预期行为**:
- 选择本站模型后自动保存
- 或显示明确的保存按钮

**技术分析**:
- 检查 `AIRouteConfig` 组件的 `mode='local'` 分支
- 确认 `handleLocalModelSelect` 是否正确触发保存
- 验证是否缺少保存成功的视觉反馈

**根因分析**:
- `mode='local'` 时通过 `handleLocalModelSelect` 自动保存
- 无需手动保存按钮
- 可能是用户预期与实际行为不符(UX 问题)

**解决方案**:
- 添加"已自动保存"的提示文案
- 或在本站模式下也显示保存按钮(即使已自动保存)

---

### 6. ★ 分组测试 - 错误的成功提示
**问题描述**: 分组模型可用性测试显示"测试成功"，但日志显示 `upstream error: 404`

**优先级**: P0 (Critical)

**影响范围**: 
- 分组测试逻辑 (internal/op/group/test.go)
- 前端测试组件 (web/src/components/modules/group/)

**现象**:
- 点击"测试分组"按钮
- 界面提示"测试成功"或"所有模型可用"
- 后端日志显示 404 错误
- 实际上渠道不可用

**预期行为**:
- 测试应真实反映渠道的可用性
- 404 错误应标记为测试失败
- 前端显示准确的测试结果

**技术分析**:
- 检查后端测试逻辑是否捕获了 HTTP 404
- 确认是否有"假成功"的默认返回值
- 验证前端是否正确解析测试结果的 `passed` 字段

**可能根因**:
- 测试逻辑对 404 的处理有误(未判定为失败)
- 前端忽略了 `error` 字段，只看 `status`
- 或存在 try-catch 吞掉了异常

---

### 7. ★ 使用日志缺失
**问题描述**: 对应渠道站点的使用日志是没有记录的

**优先级**: P2 (Medium)

**影响范围**: 
- 日志记录中间件 (internal/server/middleware/)
- 使用统计 (internal/model/usage.go)

**现象**:
- 通过站点渠道发起请求
- 在"使用日志"或"统计"页面查询
- 找不到对应的记录

**预期行为**:
- 所有通过渠道的请求都应记录到使用日志
- 日志应包含渠道 ID、模型、tokens、费用等信息

**技术分析**:
- 检查 relay 中间件是否调用了日志记录函数
- 确认站点渠道的请求路径是否绕过了日志中间件
- 验证数据库 `logs` 表的插入逻辑

---

### 8. ★ 运维中心 - 版本和系统信息显示异常
**问题描述**: 运维中心的系统信息中，版本和系统都是很大一串英文，不像正式的版本号

**优先级**: P3 (Low)

**影响范围**: 
- 系统信息组件 (web/src/components/modules/ops/System.tsx)
- 后端版本信息 (internal/version/ 或 cmd/start.go)

**现象**:
- 显示类似 `go1.21.5 linux/amd64` 或 Git commit hash
- 而非 `v1.2.3` 这样的语义化版本

**预期行为**:
- 显示清晰的版本号(如 `v0.1.0` 或 `Lodestar v1.0.0`)
- 系统信息简洁明了(如 `Linux x64`)

**技术分析**:
- 检查后端 `/api/v1/ops/system` 的返回值
- 确认构建时是否注入了 `-ldflags` 版本信息
- 前端格式化逻辑是否需要优化

**解决方案**:
- 在构建脚本中注入版本号: `-ldflags "-X main.Version=v0.1.0"`
- 后端解析 `runtime.Version()` 并格式化
- 前端对过长字符串进行截断或美化

---

## 修复优先级建议

### 第一批(本周)- P0 + 核心 P1
1. [P0] 站点管理无法重复添加站点 → 阻塞多站点管理
2. [P0] 分组测试假成功 → 误导用户，导致生产故障
3. [P1] 站点密钥显示不一致 → 数据关联问题
4. [P1] 用户名密码账号无法同步 → 限制凭据类型

### 第二批(下周)- 其余 P1 + P2
5. [P1] AI 路由分组界面不可用
6. [P1] AI 路由配置保存按钮问题
7. [P2] 使用日志缺失

### 第三批(后续优化)- P3
8. [P3] 版本信息显示优化

---

## 测试验证计划

### 站点管理流程
1. 删除所有站点
2. 添加第一个站点 → 检查是否成功
3. 尝试添加第二个站点 → 验证入口是否存在
4. 检查工具栏、页面头部、空状态提示

### 账号同步流程
1. 添加站点,选择"用户名/密码"类型账号
2. 触发同步 → 检查后端日志
3. 验证数据库中账号的 `last_sync_status`
4. 对比 Access Token 类型的同步行为

### 密钥关联流程
1. 通过站点管理添加站点 A
2. 为站点 A 的账号添加密钥
3. 切换到"渠道"页面 → 检查站点 A 的渠道是否显示密钥
4. 直接在渠道页面添加站点 B,添加密钥 → 对比差异
5. 检查数据库 `tokens` 表的 `channel_id` 字段

### 分组测试流程
1. 创建分组,绑定一个404的渠道
2. 执行"测试分组"
3. 对比前端提示和后端日志
4. 检查 `/api/v1/group/test` 的响应体

---

## 数据库排查 SQL

```sql
-- 检查站点和渠道的关联
SELECT 
    s.id AS site_id,
    s.name AS site_name,
    sa.id AS account_id,
    sa.name AS account_name,
    c.id AS channel_id,
    c.name AS channel_name,
    COUNT(t.id) AS token_count
FROM sites s
LEFT JOIN site_accounts sa ON sa.site_id = s.id
LEFT JOIN channels c ON c.id = sa.channel_id
LEFT JOIN tokens t ON t.site_account_id = sa.id
GROUP BY s.id, sa.id, c.id
ORDER BY s.id, sa.id;

-- 检查密钥的渠道关联
SELECT 
    t.id,
    t.name AS token_name,
    t.site_account_id,
    t.channel_id,
    sa.name AS account_name,
    c.name AS channel_name
FROM tokens t
LEFT JOIN site_accounts sa ON sa.id = t.site_account_id
LEFT JOIN channels c ON c.id = t.channel_id
ORDER BY t.id DESC
LIMIT 20;

-- 检查使用日志
SELECT 
    created_at,
    channel_id,
    model_name,
    prompt_tokens,
    completion_tokens,
    cost
FROM logs
WHERE channel_id IS NOT NULL
ORDER BY created_at DESC
LIMIT 10;
```

---

## 开发环境复现步骤

### 前置条件
```bash
cd /d/project/Claudeproject/已完成基本开发/newapi自用版本首页设计/自研\ Lodestar/ggzero
git status  # 确认当前分支
git log -1  # 确认当前 commit
```

### 启动服务
```bash
# 后端
cd /d/project/Claudeproject/已完成基本开发/newapi自用版本首页设计/自研\ Lodestar/ggzero
go run cmd/start.go

# 前端
cd web
pnpm dev
```

### 访问测试
- 前端: http://localhost:3000
- 后端: http://localhost:8080
- 数据库: sqlite3 data/lodestar.db

---

## 修复工单生成模板

每个问题对应一个独立工单(WO-XXX),由 DeepSeek 执行,CC 验证。

### WO-015: 站点管理重复添加入口缺失
- **文件**: web/src/components/modules/site/index.tsx, 工具栏相关组件
- **任务**: 在页面头部或工具栏添加"新增站点"按钮,确保空状态和非空状态都可见
- **验收**: 添加站点后,仍能看到"新增站点"按钮并可点击

### WO-016: 用户名密码账号同步支持
- **文件**: internal/op/site/sync.go, internal/op/site/checkin.go
- **任务**: 为用户名/密码类型账号实现登录、同步和签到逻辑
- **验收**: 添加用户名/密码账号,同步成功,签到成功

### WO-017: 站点渠道密钥显示不一致
- **文件**: internal/op/site_channel/, web/src/components/modules/site-channel/
- **任务**: 确保所有站点(无论创建路径)在渠道页面都显示密钥
- **验收**: 站点管理添加的渠道,在渠道页面可见对应密钥

### WO-018: 分组测试假成功修复
- **文件**: internal/op/group/test.go, web/src/components/modules/group/
- **任务**: 404/5xx 等错误应标记为测试失败,前端准确显示
- **验收**: 测试不可用渠道,前端显示失败,不显示成功

### WO-019: AI 路由分组界面修复
- **文件**: web/src/components/modules/group/AIRouteButton.tsx
- **任务**: 修复按钮不可用或无响应问题
- **验收**: 点击"AI 路由"按钮,弹出配置或执行对话框

### WO-020: AI 路由配置保存 UX 优化
- **文件**: web/src/components/modules/analytics/AIRouteConfig.tsx
- **任务**: 本站模式添加"已自动保存"提示,或显示保存按钮
- **验收**: 选择本站模型后,有明确的保存反馈

### WO-021: 使用日志记录修复
- **文件**: internal/server/middleware/, internal/helper/relay.go
- **任务**: 确保站点渠道的请求都记录到使用日志
- **验收**: 通过站点渠道发起请求,在日志页面可查到记录

### WO-022: 版本信息显示优化
- **文件**: internal/version/, web/src/components/modules/ops/System.tsx
- **任务**: 显示语义化版本号和简洁的系统信息
- **验收**: 运维中心显示 `Lodestar v0.1.0` 和 `Linux x64`

---

## 下一步行动

1. **CC 确认优先级**: 用户确认上述问题清单和优先级排序
2. **数据库排查**: 执行 SQL 确认问题 3(密钥显示)的根因
3. **生成第一批工单**: WO-015 ~ WO-018
4. **DeepSeek 执行**: 逐个修复
5. **CC 验证**: 每个工单独立验证后再进行下一个

---

**文档创建时间**: 2026-08-11  
**文档状态**: 待用户确认  
**预计修复周期**: P0+P1 共 6 个工单,预计 2-3 天
