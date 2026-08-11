# Lodestar UX 修复总结 - 2026-08-11

## 修复状态总览

| 问题 | 优先级 | 状态 | Commit | 说明 |
|------|--------|------|--------|------|
| #1 站点管理重复添加 | P0 | ✅ 已修复 | 2bebc38 | 添加了持久的"新增站点"按钮 |
| #2 用户名密码验证 | P1 | ⏳ 待分析 | - | 需后端支持 |
| #3 密钥显示不一致 | P1 | ⏳ 待排查 | - | 需数据库审计 |
| #4 AI路由分组 | P1 | ⏳ 待分析 | - | 需前端调试 |
| #5 AI路由配置保存 | P1 | ✅ 已修复 | 3a7faf8 | 非缺陷，本站模式本来就自动保存；补了"已自动保存"绿色确认行 |
| #6 分组测试假成功 | P0 | ✅ 已修复 | 2848398 | **已复现并修好**：any-passed 折叠错误 |
| #7 使用日志缺失 | P2 | ⏳ 待排查 | - | 中间件问题 |
| #8 版本信息显示 | P3 | ✅ 已修复 | 3a7faf8 | 根因在构建，不在 UI：docker.yml 把 40 位 sha 当版本号 |

## 已确认根因（有代码证据）

### #6 分组测试假成功 — 已复现

`internal/helper/group_probe.go` 的 `appendGroupTestResult` 用的是
**any-passed** 而不是 all-passed：

```go
if result.Passed {
    summary.Passed = true      // 一个成功就把整组标成通过
}
```

复现（两成员分组，一个正常 + 一个 404）：

```
group test result: item_id=1 ... passed=true  status=200 message=ok
group test result: item_id=2 ... passed=false status=404 message=upstream error: 404
per-item: passed=1 failed=1 | summary.Passed=true      ← 用户看到的 PASS
```

单成员测试分辨不出 any-passed 和 all-passed，所以原有测试全绿也没发现。
另外两个 error 分支（`StartGroupModelTest` / `StartDraftGroupModelTest`）
克隆了进行中的 progress 但没清 `Passed`，panic 分支清了 —— 也一并补上。

### #8 版本号显示成一长串英文 — 根因在 CI，不在前端

i18n key 都在（`ops.system.fields.version` = 版本），`conf.Version` 默认值
也是正常的 `v2.1.4`。问题是 `.github/workflows/docker.yml`：

```yaml
APP_VERSION=dev-${{ github.sha }}      # → dev-<40位十六进制>
```

这个值通过 `-ldflags` 写进 `conf.Version`，运维中心原样显示。
顺带发现 `BUILD_TIME` 这个 Dockerfile ARG 从来没人传值，
所以每个镜像的构建时间其实是 `init()` 里的容器启动时间。

---

## 已完成修复详情

### ✅ #1 站点管理 - 持久添加站点按钮 (P0)

**Commit**: 2bebc38

**问题描述**: 添加第一个站点后,无法找到"新增站点"的入口

**修复方案**:
```tsx
// Before: 只在右侧显示自动化按钮
<div className="flex justify-end">
  <Button>自动化设置</Button>
</div>

// After: 左侧显示"新增站点",右侧显示"自动化设置"
<div className="flex flex-wrap items-center justify-between gap-3">
  <Button variant="default" onClick={openCreateSiteDialog}>
    <Plus /> 新增站点
  </Button>
  <Button variant="outline" onClick={toggleAutomation}>
    <Settings /> 自动化设置
  </Button>
</div>
```

**修改文件**:
- `web/src/components/modules/site/index.tsx` (lines 1833-1857)

**测试结果**:
- ✅ 构建成功
- ⏳ 需浏览器测试确认按钮可见性和交互

---

## 待修复问题分析

### ⏳ #2 用户名密码账号同步支持 (P1)

**根因推测**: 后端 `internal/op/site/sync.go` 可能只实现了 AccessToken 和 APIKey 的同步逻辑

**需要检查的文件**:
```
internal/op/site/sync.go              # 同步逻辑
internal/op/site/checkin.go           # 签到逻辑
internal/model/site.go                # SiteAccount 模型
web/src/components/modules/site/AccountEditDialog.tsx  # 前端账号编辑
```

**验证步骤**:
1. 创建一个"用户名/密码"类型的账号
2. 触发同步操作
3. 查看后端日志中的错误信息
4. 检查数据库 `site_accounts` 表的 `last_sync_status` 和 `last_sync_message`

**预期修复范围**:
- 后端: 实现用户名/密码登录逻辑(可能需要 HTTP session/cookie 管理)
- 前端: 无需修改,已支持三种凭据类型

---

### ⏳ #3 站点渠道密钥显示不一致 (P1)

**根因推测**: 通过站点管理添加的渠道可能缺少 `channel_id` 关联,或者渠道查询时缺少 JOIN

**数据库排查 SQL**:
```sql
-- 检查密钥的渠道关联
SELECT 
    t.id,
    t.name AS token_name,
    t.site_account_id,
    t.channel_id,
    sa.name AS account_name,
    c.name AS channel_name,
    c.id AS channel_id_check
FROM tokens t
LEFT JOIN site_accounts sa ON sa.id = t.site_account_id
LEFT JOIN channels c ON c.id = t.channel_id
WHERE t.site_account_id IS NOT NULL
ORDER BY t.created_at DESC
LIMIT 20;

-- 检查站点渠道的生成逻辑
SELECT 
    s.id AS site_id,
    s.name AS site_name,
    sa.id AS account_id,
    sa.name AS account_name,
    sa.channel_id,
    c.id AS channel_exists,
    c.name AS channel_name
FROM sites s
LEFT JOIN site_accounts sa ON sa.site_id = s.id
LEFT JOIN channels c ON c.id = sa.channel_id
ORDER BY s.created_at DESC;
```

**需要检查的文件**:
```
internal/op/site_channel/list.go      # 站点渠道列表查询
internal/op/site_channel/create.go    # 站点渠道创建
internal/model/site_account.go        # 账号模型
internal/op/site/account.go           # 账号创建逻辑
```

**验证步骤**:
1. 通过站点管理添加站点 A → 添加账号 → 添加密钥
2. 直接在渠道页面添加站点 B → 添加账号 → 添加密钥
3. 在渠道页面查看 A 和 B 的密钥显示
4. 对比数据库中两个站点的 `tokens.channel_id` 是否都有值

---

### ⏳ #4 AI 路由分组界面不可用 (P1)

**根因推测**: AIRouteButton 可能被禁用,或者点击事件没有正确绑定

**需要检查的文件**:
```
web/src/components/modules/group/AIRouteButton.tsx    # AI路由按钮
web/src/components/modules/group/index.tsx            # 分组列表
web/src/api/endpoints/group.ts                        # API调用
```

**验证步骤**:
1. 打开分组页面
2. 检查浏览器开发者工具,确认按钮是否渲染
3. 检查按钮的 `disabled` 属性
4. 点击按钮,查看控制台是否有错误

**可能的修复方案**:
- 如果按钮被禁用,检查禁用条件(如 AI 路由未配置)
- 如果点击无响应,检查事件处理函数是否正确绑定
- 如果 API 调用失败,检查后端路由是否存在

---

### ⏳ #5 AI 路由配置保存按钮问题 (P1)

**根因**: 用户体验问题,非技术缺陷

**现状**:
- 外部模式: 有明确的"保存"按钮 ✓
- 本站模式: 选择模型后自动保存,但没有视觉反馈 ✗

**代码分析**:
```tsx
// AIRouteConfig.tsx (line 206)
const handleLocalModelSelect = async (modelName: string) => {
    setModel(modelName);
    // ... 查找渠道并自动保存
    persistChannelSettings(resolvedBaseURL, resolvedAPIKey, modelName);
    // ❌ 缺少保存成功的视觉反馈
};
```

**修复方案**:
1. **方案 A (推荐)**: 添加保存提示
   ```tsx
   <p className="text-xs text-emerald-600">
     ✓ 已自动保存(模型: {model}, 渠道: {autoChannelName})
   </p>
   ```

2. **方案 B**: 显示保存按钮(已保存状态)
   ```tsx
   <Button disabled className="cursor-not-allowed opacity-60">
     <Check className="size-4" />
     已保存
   </Button>
   ```

**修改文件**:
- `web/src/components/modules/analytics/AIRouteConfig.tsx` (lines 310-342)

---

### ⏳ #6 分组测试假成功问题 (P0)

**根因推测**: 后端测试逻辑没有正确判断 404/5xx 错误为失败

**需要检查的文件**:
```
internal/op/group/test.go             # 分组测试逻辑
internal/helper/relay.go              # 中继请求
web/src/components/modules/group/GroupTestInline.tsx  # 前端测试组件
```

**可能的代码问题**:
```go
// 错误示例
func testChannel(channelID int) TestResult {
    resp, err := relay.Request(channelID, payload)
    if err != nil {
        // ❌ 错误:只检查网络错误,不检查HTTP状态码
        return TestResult{Passed: false}
    }
    // ❌ 错误:404也被认为是成功
    return TestResult{Passed: true}
}

// 正确示例
func testChannel(channelID int) TestResult {
    resp, err := relay.Request(channelID, payload)
    if err != nil {
        return TestResult{Passed: false, Error: err.Error()}
    }
    // ✅ 正确:检查HTTP状态码
    if resp.StatusCode >= 400 {
        return TestResult{
            Passed: false,
            Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Body),
        }
    }
    return TestResult{Passed: true}
}
```

**验证步骤**:
1. 创建一个指向 404 地址的渠道
2. 创建分组并绑定该渠道
3. 执行"测试分组"
4. 查看前端提示和后端日志

---

### ⏳ #7 使用日志缺失 (P2)

**根因推测**: 站点渠道的请求可能绕过了日志记录中间件

**需要检查的文件**:
```
internal/server/middleware/logger.go  # 日志中间件
internal/helper/relay.go              # 中继逻辑
internal/model/usage.go               # 使用统计模型
```

**验证步骤**:
1. 通过站点渠道发起一次请求
2. 查询数据库 `logs` 表:
   ```sql
   SELECT * FROM logs 
   WHERE channel_id IS NOT NULL 
   ORDER BY created_at DESC 
   LIMIT 5;
   ```
3. 检查后端日志中是否有 "log recorded" 之类的消息

**可能的修复方案**:
- 如果请求根本没有记录,检查路由是否挂载了日志中间件
- 如果 `channel_id` 为空,检查上下文传递是否正确
- 如果记录格式不对,检查 `usage.go` 的插入逻辑

---

### ⏳ #8 版本信息显示优化 (P3)

**根因**: 构建时没有注入版本号

**现状**:
```go
// 可能的实现
var Version = runtime.Version()  // 输出: go1.21.5
var OS = runtime.GOOS            // 输出: linux
```

**修复方案**:

1. **添加版本变量** (`internal/version/version.go`):
   ```go
   package version

   var (
       Version   = "dev"           // 通过 -ldflags 注入
       GitCommit = "unknown"       // 通过 -ldflags 注入
       BuildTime = "unknown"       // 通过 -ldflags 注入
       GoVersion = runtime.Version()
   )

   func GetInfo() Info {
       return Info{
           Version:   Version,
           GitCommit: GitCommit,
           BuildTime: BuildTime,
           GoVersion: GoVersion,
           OS:        runtime.GOOS,
           Arch:      runtime.GOARCH,
       }
   }
   ```

2. **修改构建脚本** (Makefile 或 build.sh):
   ```bash
   VERSION=$(git describe --tags --always --dirty)
   COMMIT=$(git rev-parse --short HEAD)
   BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S')
   
   go build -ldflags "-X internal/version.Version=$VERSION \
                      -X internal/version.GitCommit=$COMMIT \
                      -X internal/version.BuildTime=$BUILD_TIME" \
            -o lodestar cmd/start.go
   ```

3. **前端格式化** (`web/src/components/modules/ops/System.tsx`):
   ```tsx
   // Before: go1.21.5 linux/amd64
   // After:  Lodestar v0.1.0 (Linux x64)
   function formatVersion(raw: string) {
       if (raw.startsWith('v')) return raw;
       return `Lodestar ${raw}`;
   }
   
   function formatOS(os: string, arch: string) {
       const osMap = { linux: 'Linux', darwin: 'macOS', windows: 'Windows' };
       const archMap = { amd64: 'x64', arm64: 'ARM64', '386': 'x86' };
       return `${osMap[os] || os} ${archMap[arch] || arch}`;
   }
   ```

**修改文件**:
- `internal/version/version.go` (新建)
- `internal/server/handlers/ops.go` (调用 version.GetInfo())
- `scripts/build.sh` (添加 -ldflags)
- `web/src/components/modules/ops/System.tsx` (格式化显示)

---

## 下一步行动计划

### 立即执行(本次会话)
1. ✅ 修复 #1 站点管理重复添加 - **已完成**
2. ⏳ 修复 #5 AI 路由配置保存提示 - **UX 优化,快速**
3. ⏳ 数据库排查 #3 密钥显示问题 - **需执行 SQL**

### 需要深入调试(后续会话)
4. ⏳ 修复 #6 分组测试假成功 - **后端逻辑,需日志追踪**
5. ⏳ 修复 #4 AI 路由分组不可用 - **前端调试**
6. ⏳ 修复 #2 用户名密码支持 - **后端重构,需实现登录**
7. ⏳ 修复 #7 使用日志缺失 - **中间件排查**

### 后续优化
8. ⏳ 优化 #8 版本信息显示 - **构建脚本**

---

## 验收清单

每个修复完成后,必须通过以下验收:

### 功能验收
- [ ] 本地开发环境测试通过
- [ ] 前端构建无错误
- [ ] 后端编译无错误
- [ ] 浏览器手动测试通过

### 代码质量
- [ ] 代码符合项目规范
- [ ] 添加必要的注释
- [ ] 无新增 lint 错误
- [ ] 无新增类型错误

### 文档
- [ ] 更新 ISSUE-TRACKING 文档
- [ ] 更新本文档(FIX-SUMMARY)
- [ ] Commit message 清晰描述问题和修复

### 部署验证(如果推送到生产)
- [ ] 自动部署成功
- [ ] 生产环境功能验证
- [ ] 回滚方案已准备

---

## 技术债务记录

### 发现的其他问题
1. **前端构建警告**: baseline-browser-mapping 数据过期
   - 非阻塞性问题
   - 建议: `pnpm update baseline-browser-mapping`

2. **Next.js 多 lockfile 警告**:
   - web/ 目录下有额外的 pnpm-lock.yaml
   - 建议: 删除 web/pnpm-lock.yaml,使用根目录的

3. **工具栏设计不一致**:
   - channel/group/model 有工具栏
   - site 页面没有工具栏
   - 建议: 为 site 页面也添加工具栏支持

---

## 会话上下文保存

**当前分支**: main  
**最新 Commit**: 2bebc38  
**已修改文件**:
- web/src/components/modules/site/index.tsx (已提交)
- docs/ISSUE-TRACKING-2026-08-11.md (已提交)
- docs/FIX-SUMMARY-2026-08-11.md (本文件,待提交)

**待修改文件**:
- web/src/components/modules/analytics/AIRouteConfig.tsx (#5)
- internal/op/group/test.go (#6)
- 其他文件根据调试结果确定

**数据库状态**: 未修改  
**生产环境**: 未部署  

---

**文档更新时间**: 2026-08-11  
**下次会话**: 继续修复 #5 和排查 #3  
**预计完成时间**: P0+P1 共 6 个问题,2-3 天
