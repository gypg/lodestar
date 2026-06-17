import type { ModelCapability } from '@/api/endpoints/model';

export const AUTO_ENDPOINT = '*';
export const CHAT_ENDPOINT = 'chat';

// 对话族端点：deepseek / mimo 是对话端点的协议变体，responses / messages 同属对话族，
// 自动端点(*)默认也走对话。在「可用端点」视图里统一并入「对话」分组展示，避免拆成
// 多个内容高度重叠的卡片。与后端 model.IsConversationEndpointType 的归类保持一致。
export const CONVERSATION_ENDPOINTS = new Set([
    CHAT_ENDPOINT,
    'deepseek',
    'mimo',
    'responses',
    'messages',
    AUTO_ENDPOINT,
]);

// canonicalEndpoint 把对话族端点规约到统一的 chat key，其余端点原样返回。
export function canonicalEndpoint(endpoint: string): string {
    return CONVERSATION_ENDPOINTS.has(endpoint) ? CHAT_ENDPOINT : endpoint;
}

export interface EndpointGroup {
    endpoint: string;
    models: ModelCapability[];
}

/**
 * 把 capabilities（模型 -> 端点 列表）反转成 端点 -> 模型 列表，
 * 并丢弃没有任何模型的端点。每个模型可能支持多个端点，会出现在多个分组里。
 * 对话族端点（chat / deepseek / mimo / responses / messages / 自动）规约为单一
 * 「对话」分组，同一模型在该分组内去重。
 */
export function buildEndpointGroups(capabilities: ModelCapability[] | undefined): EndpointGroup[] {
    if (!capabilities || capabilities.length === 0) return [];

    const byEndpoint = new Map<string, ModelCapability[]>();
    for (const cap of capabilities) {
        const endpoints = cap.endpoints.length > 0 ? cap.endpoints : [AUTO_ENDPOINT];
        // 先规约再去重，避免同一模型在合并后的对话分组里重复出现。
        const canonical = new Set(endpoints.map(canonicalEndpoint));
        for (const ep of canonical) {
            const list = byEndpoint.get(ep) ?? [];
            list.push(cap);
            byEndpoint.set(ep, list);
        }
    }

    return Array.from(byEndpoint.entries())
        .filter(([, models]) => models.length > 0)
        .map(([endpoint, models]) => ({
            endpoint,
            models: [...models].sort((a, b) => a.name.localeCompare(b.name)),
        }))
        .sort((a, b) => {
            // 对话分组排在最前，其余按字母序。
            if (a.endpoint === CHAT_ENDPOINT) return -1;
            if (b.endpoint === CHAT_ENDPOINT) return 1;
            return a.endpoint.localeCompare(b.endpoint);
        });
}
