import type { ChannelAttempt, RelayLog, RelayLogDetail } from '../../../api/endpoints/log.ts';
import { inferCapabilities, type CapabilityType } from '../group/capabilities.ts';

const capabilityEndpointMap: Record<Exclude<CapabilityType, 'chat' | 'moderation'>, string> = {
    embeddings: 'embeddings',
    rerank: 'rerank',
    image_generation: 'image_generation',
    audio_speech: 'audio_speech',
    audio_transcription: 'audio_transcription',
    video_generation: 'video_generation',
    music_generation: 'music_generation',
    search: 'search',
};

function firstNonEmpty(...values: Array<string | null | undefined>) {
    for (const value of values) {
        const trimmed = value?.trim();
        if (trimmed) return trimmed;
    }
    return '';
}

function lastAttemptValue(
    attempts: ChannelAttempt[] | undefined,
    pick: (attempt: ChannelAttempt) => string | undefined,
) {
    if (!attempts?.length) return '';
    for (let index = attempts.length - 1; index >= 0; index -= 1) {
        const value = pick(attempts[index])?.trim();
        if (value) return value;
    }
    return '';
}

function firstNonZero(...values: Array<number | null | undefined>) {
    for (const value of values) {
        if (typeof value === 'number' && value > 0) return value;
    }
    return 0;
}

function lastAttemptChannelId(attempts: ChannelAttempt[] | undefined) {
    if (!attempts?.length) return 0;
    for (let index = attempts.length - 1; index >= 0; index -= 1) {
        const value = attempts[index]?.channel_id;
        if (typeof value === 'number' && value > 0) return value;
    }
    return 0;
}

function isStreamRequest(requestContent?: string | null) {
    if (!requestContent) return false;
    try {
        const parsed = JSON.parse(requestContent) as { stream?: unknown };
        return parsed.stream === true;
    } catch (e) { console.error(e);
        return false;
    }
}

function inferRequestTypeKey(endpointType: string, modelNames: string[], requestContent?: string | null) {
    const normalizedEndpoint = endpointType.trim().toLowerCase();
    const normalizedNames = modelNames
        .map((name) => name.trim().toLowerCase())
        .filter(Boolean);
    const streaming = isStreamRequest(requestContent);

    if (normalizedEndpoint === 'embeddings') return 'embedding';
    if (normalizedEndpoint === 'responses') return 'responses';
    if (normalizedEndpoint === 'messages') return 'anthropicMessages';
    if (normalizedEndpoint === 'gemini') return 'gemini';
    if (normalizedEndpoint === 'mimo' || normalizedNames.some((name) => name.includes('mimo'))) return 'mimoChat';
    if (normalizedNames.some((name) => name.includes('gemini'))) return 'gemini';
    if (normalizedNames.some((name) => name.includes('claude'))) return 'anthropicMessages';
    if (normalizedNames.some((name) => name.includes('doubao') || name.includes('volcengine') || name.includes('ark'))) return 'volcengine';
    if (normalizedEndpoint === 'deepseek' || normalizedEndpoint === 'chat') {
        return streaming ? 'streamingChat' : 'chat';
    }
    return normalizedEndpoint ? normalizedEndpoint : (streaming ? 'streamingChat' : 'chat');
}
function inferEndpointTypeFromModels(modelNames: string[]) {
    const normalizedNames = modelNames
        .map((name) => name.trim().toLowerCase())
        .filter(Boolean);

    if (normalizedNames.some((name) => name.includes('deepseek'))) {
        return 'deepseek';
    }
    if (normalizedNames.some((name) => name.includes('mimo'))) {
        return 'mimo';
    }

    for (const modelName of normalizedNames) {
        const capability = inferCapabilities(modelName).find((item) => item !== 'chat');
        if (!capability) continue;
        if (capability === 'moderation') return 'moderations';
        return capabilityEndpointMap[capability];
    }

    return normalizedNames.length > 0 ? 'chat' : '';
}

export function resolveLogDisplayFields(
    log: RelayLog,
    detail?: RelayLogDetail | null,
    channelNameById?: ReadonlyMap<number, string>,
) {
    const mergedAttempts = detail?.attempts?.length ? detail.attempts : log.attempts;

    const requestModelName = firstNonEmpty(detail?.request_model_name, log.request_model_name);
    const requestContent = firstNonEmpty(detail?.request_content, '');
    const actualModelName = firstNonEmpty(
        detail?.actual_model_name,
        log.actual_model_name,
        lastAttemptValue(mergedAttempts, (attempt) => attempt.model_name),
        requestModelName,
    );
    const endpointType = firstNonEmpty(
        detail?.endpoint_type,
        log.endpoint_type,
        inferEndpointTypeFromModels([
            actualModelName,
            requestModelName,
            lastAttemptValue(mergedAttempts, (attempt) => attempt.model_name),
        ]),
    );
    const channelId = firstNonZero(detail?.channel, log.channel, lastAttemptChannelId(mergedAttempts));
    const channelName = firstNonEmpty(
        detail?.channel_name,
        log.channel_name,
        lastAttemptValue(mergedAttempts, (attempt) => attempt.channel_name),
        channelId > 0 ? channelNameById?.get(channelId) : '',
        channelId > 0 ? 'channel_fallback' : '',
    );

    return {
        requestAPIKeyName: firstNonEmpty(detail?.request_api_key_name, log.request_api_key_name),
        requestModelName,
        actualModelName,
        endpointType,
        requestTypeKey: inferRequestTypeKey(endpointType, [actualModelName, requestModelName], requestContent),
        channelId,
        channelName,
        semanticCacheHit: detail?.semantic_cache_hit ?? log.semantic_cache_hit ?? false,
        cacheReadTokens: detail?.cache_read_tokens ?? log.cache_read_tokens ?? 0,
    };
}

/**
 * 解析日志条目「端点类型」列要显示的文案。
 *
 * 两级回退：先查 log.card.requestTypeLabels（只有 8 个词条，覆盖对话族/embedding
 * 等常见类型），查不到再退到 group.form.endpointType.options（覆盖全部 11 个端点）。
 *
 * ⚠️ 关键点：inferRequestTypeKey 兜底时会把原始 endpointType 原样当 key 返回，
 * 所以第一级必然会有一批 miss（rerank / image_generation / audio_* / video_* /
 * music_generation / search）。next-intl 在 miss 时返回**完整键路径**
 * （'log.card.requestTypeLabels.rerank'），不是传入的短路径，因此判定 miss 必须
 * 用 endsWith 而不是 ===。用 === 的话守卫永远不成立，键路径会被当标签显示，
 * 而且第二级回退再也走不到 —— 这正是"一半界面显示内部端点名"的成因。
 *
 * 注入 t / tGroup 而不是直接调 hook，是为了能在 node:test 里不渲染组件就验证。
 */
export function resolveEndpointTypeLabel(params: {
    requestTypeKey?: string;
    endpointType?: string;
    t: (key: string) => string;
    tGroup: (key: string) => string;
    endpointTypeLabelKey: (value?: string | null) => string | undefined;
}): string {
    const { requestTypeKey, endpointType, t, tGroup, endpointTypeLabelKey } = params;

    if (requestTypeKey) {
        const shortPath = `requestTypeLabels.${requestTypeKey}`;
        const label = t(shortPath);
        if (label && !label.endsWith(shortPath)) return label;
    }

    if (!endpointType) return '-';
    const labelKey = endpointTypeLabelKey(endpointType);
    if (!labelKey) return endpointType;

    const groupLabel = tGroup(labelKey);
    // 第二级同样可能 miss（词条被删/改名），此时宁可显示原始端点名也不要键路径。
    return groupLabel.endsWith(labelKey) ? endpointType : groupLabel;
}

// formatJsonForCopy pretty-prints JSON content for clipboard use so that copied
// request/response bodies keep their newlines and indentation instead of being
// pasted as a single minified line. Non-JSON content is returned unchanged.
export function formatJsonForCopy(content: string | undefined | null): string {
    if (!content) return '';
    try {
        return JSON.stringify(JSON.parse(content), null, 2);
    } catch (e) { console.error(e);
        return content;
    }
}




