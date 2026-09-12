# WO-050: AI 路由配置"本站模型"选项不完整

**创建时间**: 2026-09-12  
**优先级**: P1（功能阻断）  
**状态**: SPEC  
**分配**: zcode

---

## 问题描述

在"分析中心 → 评估 → AI 路由分析 → 编辑配置"中选择"本站模型"时，下拉列表**不包含模型广场中所有可见的模型**。

### 用户报告

> 例如，我在筛选里选了一个 DeepSeek 模型，选择"全部模型"，但在分析中心评估的配置内部，并没有看到这个模型的相关选项。相当于只有一部分模型可以在"本站模型"中被选择，有一部分不让选，导致 AI 路由分析任务中有些分析任务无法进行。
> 
> 另外，在分组渠道的"AI 路由表全部分组"里，我似乎也没有看到 DeepSeek 的分组。

### 复现路径

1. 进入**模型广场** → 在筛选中选择 DeepSeek（或任何特定 provider 的模型）
2. 确认模型广场中显示了多个 DeepSeek 模型（例如 `deepseek-chat`, `deepseek-coder` 等）
3. 进入**分析中心 → 评估 → AI 路由分析** → 点击"编辑配置"
4. 在"模型"下拉框中选择"本站模型"
5. **观察**：下拉列表中**缺少**在模型广场可见的部分/全部 DeepSeek 模型

### 预期行为

- 模型广场中显示的**所有**已启用模型，都应该出现在 AI 路由配置的"本站模型"列表中
- 用户应该能够为模型广场中任何可见的模型配置 AI 路由分析

### 实际行为

- "本站模型"列表**只包含**渠道 `Model`/`CustomModel` 字段中明确配置的模型
- 渠道声明支持但未在配置字段中列出的模型，不出现在列表中
- 导致用户无法为这些模型配置 AI 路由分析

---

## 根本原因分析

### 数据源差异

**模型广场** (`/model` 页面)：
- API: `GET /api/v1/model/market`
- 前端: `useModelMarket()` → `src/api/endpoints/model.ts`
- 返回：所有已启用渠道提供的所有模型（包括渠道声明支持的任何模型）

**AI 路由配置 "本站模型"** (`AIRouteConfig.tsx`):
- API: `GET /api/v1/model/list`
- 前端: `useModelChannels()` → `src/api/endpoints/model.ts`
- 后端: `internal/op/channel/channel.go:452` → `LLMList()`
- 返回：**只包含**渠道 `Model`/`CustomModel` 字段中明确配置的模型名称

### 代码证据

**后端** `internal/op/channel/channel.go:452-476`:
```go
func LLMList(ctx context.Context) ([]model.LLMChannel, error) {
    models := []model.LLMChannel{}
    seen := make(map[string]struct{})
    for _, ch := range chCache.GetAll() {
        // ❌ 只从渠道配置的 Model/CustomModel 提取
        modelNames := xstrings.SplitTrimCompact(",", ch.Model, ch.CustomModel)
        for _, modelName := range modelNames {
            if modelName == "" {
                continue
            }
            item := model.LLMChannel{
                Name:        modelName,
                Enabled:     ch.Enabled,
                ChannelID:   ch.ID,
                ChannelName: ch.Name,
            }
            key := fmt.Sprintf("%d|%s", item.ChannelID, item.Name)
            if _, ok := seen[key]; ok {
                continue
            }
            seen[key] = struct{}{}
            models = append(models, item)
        }
    }
    return models, nil
}
```

**前端** `web/src/components/modules/analytics/AIRouteConfig.tsx:130-139`:
```tsx
const localModelOptions = useMemo(() => {
    // ❌ 只使用 modelChannels（来自 LLMList）
    const served = (modelChannels ?? []).filter((item) => item.enabled);
    if (served.length === 0) return modelsByProvider;
    const buckets: Record<string, string[]> = {};
    for (const item of served) {
        const { label } = getModelIcon(item.name);
        const key = label || 'Other';
        (buckets[key] ??= []).push(item.name);
    }
    return buckets;
}, [modelChannels, modelsByProvider]);
```

### 影响范围

**直接阻断**：
- 用户无法为模型广场中可见的部分模型配置 AI 路由分析
- AI 路由分析功能不完整，无法评估所有可路由模型

**间接影响**：
- "AI 路由表全部分组"中可能缺少某些模型的分组（如果自动分组/AI 生成路由也基于相同的渠道配置数据）

---

## 修复方案

### 方案：前端合并模型市场数据

**原理**：
- 保留现有 `modelChannels` 数据源（已配置的模型，包含 `enabled` 状态）
- 引入 `modelMarket` 数据源（所有可见模型，更全面）
- 在前端合并两个数据源，去重后按 provider 分组

**优点**：
- ✅ 纯前端改动，不涉及后端 API 修改，风险低
- ✅ 用户在模型广场看到的模型，都能在 AI 路由配置中选择
- ✅ 保持向后兼容，已配置的模型仍然显示
- ✅ 允许用户评估所有可路由的模型
- ✅ 不影响现有的 LLMList API 使用方（如果有其他调用方）

**缺点**：
- ⚠️ 模型广场可能包含未实际配置的模型（渠道声明支持但未在 Model 字段中列出）
- ⚠️ 用户可能选择一个模型进行分析，但该模型实际上没有配置在任何渠道的路由中

**评估**：缺点是可接受的，因为：
1. AI 路由分析本身就是"评估"工具，允许用户测试所有潜在可用的模型
2. 如果用户选择的模型没有实际路由配置，分析任务会失败并返回错误，这是合理的反馈
3. 这比"隐藏可用模型"的当前行为更符合用户预期

---

## 实现细节

### 文件修改

**文件**: `web/src/components/modules/analytics/AIRouteConfig.tsx`

**修改点 1**: 引入 `useModelMarket` hook

在文件顶部导入区域添加：
```tsx
import { useModelMarket } from '@/api/endpoints/model';
```

**修改点 2**: 在组件内部调用 `useModelMarket`

在 `AIRouteConfig` 组件函数内部，`useModelChannels()` 调用之后添加：
```tsx
const { data: modelChannels } = useModelChannels();
const { data: modelMarket } = useModelMarket();  // ✅ 新增
```

**修改点 3**: 修改 `localModelOptions` 的计算逻辑

替换现有的 `localModelOptions` useMemo：

```tsx
const localModelOptions = useMemo(() => {
    // 1. 从 modelChannels 提取已配置的模型（保留向后兼容）
    const channelModels = new Set<string>();
    (modelChannels ?? [])
        .filter((item) => item.enabled)
        .forEach((item) => channelModels.add(item.name));

    // 2. 从 modelMarket 提取所有可见模型（更全面）
    const marketModels = new Set<string>();
    (modelMarket ?? []).forEach((item) => {
        // 只添加有启用渠道的模型
        if (item.enabled_key_count > 0) {
            marketModels.add(item.name);
        }
    });

    // 3. 合并两个数据源（并集）
    const allModels = new Set([...channelModels, ...marketModels]);

    // 4. 如果没有任何模型，回退到内置的 modelsByProvider
    if (allModels.size === 0) {
        return modelsByProvider;
    }

    // 5. 按 provider 分组
    const buckets: Record<string, string[]> = {};
    for (const modelName of allModels) {
        const { label } = getModelIcon(modelName);
        const key = label || 'Other';
        (buckets[key] ??= []).push(modelName);
    }

    return buckets;
}, [modelChannels, modelMarket, modelsByProvider]);
```

**关键逻辑说明**：
1. **优先使用 `modelMarket`**：因为它包含渠道声明的所有模型，更全面
2. **保留 `modelChannels`**：用于向后兼容，确保已配置的模型不会丢失
3. **过滤条件**：`enabled_key_count > 0` 确保只显示有启用密钥的模型（实际可用）
4. **去重**：使用 `Set` 自动去重
5. **回退机制**：如果两个数据源都为空，回退到内置的 `modelsByProvider`

---

## 验收标准

### 前置条件
- 系统中至少有一个已启用的渠道
- 该渠道在模型广场中显示了多个模型（例如 DeepSeek 相关模型）
- 该渠道的 `Model` 字段可能**没有**列出所有这些模型

### 测试步骤

**TC-1: 模型广场与 AI 路由配置一致性**
1. 进入**模型广场** (`/model`)
2. 在筛选器中选择某个 provider（例如 DeepSeek）
3. 记录所有显示的模型名称（例如：`deepseek-chat`, `deepseek-coder`, `deepseek-reasoner`）
4. 进入**分析中心 → 评估 → AI 路由分析**，点击"编辑配置"
5. 在"模型"下拉框中选择"本站模型"
6. **验证**：下拉列表中**包含**步骤 3 中记录的所有模型名称

**预期结果**：✅ 所有模型都出现在下拉列表中

**TC-2: 按 Provider 正确分组**
1. 在 AI 路由配置的"模型"下拉框中选择"本站模型"
2. **验证**：模型按 provider 分组显示（例如 "OpenAI", "Anthropic", "DeepSeek", "Other"）
3. **验证**：每个分组下的模型名称正确（与 `getModelIcon` 返回的 label 匹配）

**预期结果**：✅ 分组清晰，模型归属正确

**TC-3: 空数据回退**
1. 禁用所有渠道（或删除所有渠道）
2. 进入 AI 路由配置，选择"本站模型"
3. **验证**：下拉列表显示内置的 `modelsByProvider`（回退机制生效）

**预期结果**：✅ 显示内置模型列表，不崩溃

**TC-4: 选择模型并保存配置**
1. 在 AI 路由配置中选择一个从模型广场来源的模型（之前不在"本站模型"中）
2. 填写 Base URL 和 API Key
3. 点击"保存配置"
4. **验证**：配置成功保存（前端显示成功 toast）
5. 刷新页面，再次打开配置
6. **验证**：之前选择的模型仍然正确显示

**预期结果**：✅ 配置保存和加载正常

**TC-5: 数据源合并去重**
1. 确保某个模型同时存在于 `modelChannels` 和 `modelMarket` 中
2. 打开 AI 路由配置的"本站模型"下拉框
3. **验证**：该模型只出现一次（去重生效）

**预期结果**：✅ 无重复项

---

## 测试数据期望

### 修改前（当前行为）

**模型广场显示**（示例）：
```
DeepSeek:
  - deepseek-chat (3 渠道, 启用密钥 5)
  - deepseek-coder (2 渠道, 启用密钥 3)
  - deepseek-reasoner (1 渠道, 启用密钥 1)
```

**AI 路由配置"本站模型"**（示例）：
```
DeepSeek:
  - deepseek-chat  ✅ (因为某个渠道的 Model 字段包含此模型)
  ❌ deepseek-coder 缺失
  ❌ deepseek-reasoner 缺失
```

### 修改后（预期行为）

**AI 路由配置"本站模型"**：
```
DeepSeek:
  - deepseek-chat  ✅
  - deepseek-coder  ✅ (新增)
  - deepseek-reasoner  ✅ (新增)
```

---

## 边界情况处理

### 1. `modelMarket` 加载失败
- 回退到只使用 `modelChannels`（现有行为）
- 不影响基本功能

### 2. `modelChannels` 加载失败
- 仍然使用 `modelMarket` 数据
- 如果 `modelMarket` 也失败，回退到 `modelsByProvider`

### 3. 模型存在于 `modelMarket` 但 `enabled_key_count = 0`
- **不显示**该模型（过滤掉）
- 原因：没有启用的密钥，无法实际使用

### 4. 用户选择的模型在保存配置时不存在
- 后端验证逻辑不变，会返回错误
- 前端显示错误提示（现有行为）

---

## 注意事项

1. **不修改后端**：保持 `LLMList()` API 不变，避免影响其他调用方
2. **性能考虑**：
   - `modelMarket` 数据量可能较大（数百个模型）
   - `useMemo` 依赖正确，避免不必要的重新计算
   - 使用 `Set` 进行去重，时间复杂度 O(n)
3. **类型安全**：
   - `modelMarket` 的类型是 `ModelMarketItem[]`（包含 `enabled_key_count` 等字段）
   - `modelChannels` 的类型是 `LLMChannel[]`（包含 `enabled` 等字段）
   - 两者的 `name` 字段类型一致，可以安全合并
4. **国际化**：
   - Provider 分组名称（如 "OpenAI", "DeepSeek"）来自 `getModelIcon()`，不需要翻译
   - 如果未来需要翻译 provider 名称，在 `getModelIcon()` 中处理

---

## 潜在扩展（本工单不实现）

1. **显示模型可用性状态**：
   - 在下拉框中标记哪些模型有配置（来自 `modelChannels`）
   - 哪些模型只在市场中可见但未配置（来自 `modelMarket`）
   - 例如：`deepseek-chat ✓` vs `deepseek-coder ⚠️`

2. **后端优化**：
   - 修改 `LLMList()` 直接返回所有渠道声明的模型
   - 统一数据源，避免前端合并逻辑
   - 需要评估对其他调用方的影响

3. **增强过滤**：
   - 允许用户在 AI 路由配置中进一步过滤模型（按 provider、按可用性等）
   - 当前实现已经按 provider 分组，为此功能打下基础

---

## 交付检查清单

- [ ] 代码修改：`web/src/components/modules/analytics/AIRouteConfig.tsx`
- [ ] 功能测试：TC-1 ~ TC-5 全部通过
- [ ] 前端编译：`pnpm build` 无错误
- [ ] 前端类型检查：`pnpm typecheck` 无错误
- [ ] 前端 Lint：`pnpm lint` 无警告
- [ ] 手动测试：在浏览器中验证 UI 行为
- [ ] 提交消息：遵循 conventional commits 格式
- [ ] 无回归：现有的 AI 路由配置功能不受影响

---

## 参考

- 用户报告：本工单顶部"问题描述"部分
- 相关文件：
  - `web/src/components/modules/analytics/AIRouteConfig.tsx` (主要修改)
  - `web/src/api/endpoints/model.ts` (数据源 hooks)
  - `internal/op/channel/channel.go` (后端 LLMList 实现)
- 相关 API：
  - `GET /api/v1/model/list` (modelChannels)
  - `GET /api/v1/model/market` (modelMarket)
