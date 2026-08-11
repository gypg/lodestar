import test from 'node:test';
import assert from 'node:assert/strict';

import type { RelayLog, RelayLogDetail } from '@/api/endpoints/log';
import { formatJsonForCopy, resolveLogDisplayFields, resolveEndpointTypeLabel } from './display.ts';
import fs from 'node:fs';
import path from 'node:path';

function buildLog(overrides: Partial<RelayLog> = {}): RelayLog {
    return {
        id: 1,
        time: 0,
        request_model_name: 'deepseek-v4-pro-max',
        request_api_key_name: '',
        endpoint_type: '',
        channel: 0,
        channel_name: '',
        actual_model_name: '',
        input_tokens: 0,
        output_tokens: 0,
        ftut: 0,
        use_time: 0,
        cost: 0,
        error: '',
        attempts: [],
        total_attempts: 0,
        ...overrides,
    };
}

test('resolveLogDisplayFields infers deepseek endpoint and uses attempt channel fallback', () => {
    const log = buildLog({
        attempts: [
            {
                channel_id: 12,
                channel_name: 'DeepSeek Channel',
                model_name: 'deepseek-v4-pro-max',
                attempt_num: 1,
                status: 'success',
                duration: 10,
            },
        ],
    });

    const result = resolveLogDisplayFields(log);
    assert.equal(result.endpointType, 'deepseek');
    assert.equal(result.channelName, 'DeepSeek Channel');
    assert.equal(result.actualModelName, 'deepseek-v4-pro-max');
});

test('resolveLogDisplayFields prefers detail payload over list payload', () => {
    const log = buildLog({
        endpoint_type: '',
        channel_name: '',
        actual_model_name: '',
    });
    const detail: RelayLogDetail = {
        ...log,
        endpoint_type: 'deepseek',
        channel_name: 'Relay Channel',
        actual_model_name: 'deepseek-v4-pro-max',
        request_content: '{}',
        response_content: '{}',
    };

    const result = resolveLogDisplayFields(log, detail);
    assert.equal(result.endpointType, 'deepseek');
    assert.equal(result.channelName, 'Relay Channel');
    assert.equal(result.actualModelName, 'deepseek-v4-pro-max');
});

test('resolveLogDisplayFields falls back to channel id mapping when channel names are empty', () => {
    const log = buildLog({
        channel: 42,
        channel_name: '',
        attempts: [
            {
                channel_id: 42,
                channel_name: '',
                model_name: 'deepseek-v4-pro-max',
                attempt_num: 1,
                status: 'success',
                duration: 10,
            },
        ],
    });

    const result = resolveLogDisplayFields(log, null, new Map([[42, 'DeepSeek Fallback Channel']]));
    assert.equal(result.channelId, 42);
    assert.equal(result.channelName, 'DeepSeek Fallback Channel');
});

test('resolveLogDisplayFields falls back to channel id label when no channel name sources exist', () => {
    const log = buildLog({
        channel: 77,
        channel_name: '',
        attempts: [
            {
                channel_id: 77,
                channel_name: '',
                model_name: 'gpt-4o-mini',
                attempt_num: 1,
                status: 'success',
                duration: 10,
            },
        ],
    });

    const result = resolveLogDisplayFields(log);
    assert.equal(result.channelId, 77);
    assert.equal(result.channelName, 'channel_fallback');
});

test('resolveLogDisplayFields falls back to chat when only generic chat models exist', () => {
    const log = buildLog({
        request_model_name: 'gpt-4o-mini',
        actual_model_name: 'gpt-4o-mini',
    });

    const result = resolveLogDisplayFields(log);
    assert.equal(result.endpointType, 'chat');
});

test('resolveLogDisplayFields exposes cache read tokens from detail or list payload', () => {
    const log = buildLog({
        cache_read_tokens: 120,
    });

    const fromList = resolveLogDisplayFields(log);
    assert.equal(fromList.cacheReadTokens, 120);

    const detail: RelayLogDetail = {
        ...log,
        cache_read_tokens: 240,
        request_content: '{}',
        response_content: '{}',
    };
    const fromDetail = resolveLogDisplayFields(log, detail);
    assert.equal(fromDetail.cacheReadTokens, 240);
});

test('resolveLogDisplayFields exposes semantic cache hit flag from detail or list payload', () => {
    const log = buildLog({
        semantic_cache_hit: true,
    });

    const fromList = resolveLogDisplayFields(log);
    assert.equal(fromList.semanticCacheHit, true);

    const detail: RelayLogDetail = {
        ...log,
        semantic_cache_hit: false,
        request_content: '{}',
        response_content: '{}',
    };
    const fromDetail = resolveLogDisplayFields(log, detail);
    assert.equal(fromDetail.semanticCacheHit, false);
});

test('resolveLogDisplayFields infers MiMo Chat request type label', () => {
    const log = buildLog({
        request_model_name: 'mimo-v2.5-pro',
        actual_model_name: 'mimo-v2.5-pro',
    });

    const result = resolveLogDisplayFields(log);
    assert.equal(result.requestTypeKey, 'mimoChat');
});

test('resolveLogDisplayFields infers streaming chat request type label from request content', () => {
    const log = buildLog({
        request_model_name: 'gpt-4o-mini',
        actual_model_name: 'gpt-4o-mini',
    });
    const detail: RelayLogDetail = {
        ...log,
        request_content: '{"stream":true}',
        response_content: '{}',
    };

    const result = resolveLogDisplayFields(log, detail);
    assert.equal(result.requestTypeKey, 'streamingChat');
});

test('resolveLogDisplayFields infers embedding request type label', () => {
    const log = buildLog({
        endpoint_type: 'embeddings',
        request_model_name: 'text-embedding-3-small',
        actual_model_name: 'text-embedding-3-small',
    });

    const result = resolveLogDisplayFields(log);
    assert.equal(result.requestTypeKey, 'embedding');
});

test('formatJsonForCopy pretty-prints minified JSON with two-space indent', () => {
    const result = formatJsonForCopy('{"role":"system","content":"hi"}');
    assert.equal(result, '{\n  "role": "system",\n  "content": "hi"\n}');
});

test('formatJsonForCopy preserves already-formatted JSON semantics', () => {
    const result = formatJsonForCopy('{\n  "a": 1\n}');
    assert.deepEqual(JSON.parse(result), { a: 1 });
});

test('formatJsonForCopy returns non-JSON content unchanged', () => {
    const raw = 'not json { broken';
    assert.equal(formatJsonForCopy(raw), raw);
});

test('formatJsonForCopy returns empty string for empty or missing input', () => {
    assert.equal(formatJsonForCopy(''), '');
    assert.equal(formatJsonForCopy(undefined), '');
    assert.equal(formatJsonForCopy(null), '');
});

// ---- 端点类型标签解析 ----------------------------------------------------
// 用真实 locale 文件驱动：写死 messages 的话，词条缺失/改名就测不出来。
const LOCALE = JSON.parse(
    fs.readFileSync(path.join(process.cwd(), 'src', 'locales', 'zh_hans.json'), 'utf8'),
) as Record<string, unknown>;

function lookup(root: Record<string, unknown>, dotted: string): string | undefined {
    let cur: unknown = root;
    for (const seg of dotted.split('.')) {
        if (!cur || typeof cur !== 'object') return undefined;
        cur = (cur as Record<string, unknown>)[seg];
    }
    return typeof cur === 'string' ? cur : undefined;
}

// 复刻 next-intl 的 miss 行为：返回**完整**键路径（含命名空间前缀），
// 这正是原 bug 的触发条件。实测已确认（createTranslator 探针）。
function makeT(namespace: string) {
    return (key: string) => lookup(LOCALE, `${namespace}.${key}`) ?? `${namespace}.${key}`;
}
// group/utils.ts 里 ENDPOINT_TYPE_OPTIONS 的真实内容（该文件运行时 import 了
// @/api 别名，裸 node test runner 解析不了，故从源码文本解析而非直接 import，
// 保证这张表一改测试就会跟着变，不会退化成写死的副本）。
const GROUP_UTILS_SRC = fs.readFileSync(
    path.join(process.cwd(), 'src', 'components', 'modules', 'group', 'utils.ts'),
    'utf8',
);
const ENDPOINT_TYPE_OPTIONS: Array<{ labelKey: string; value: string }> = [
    ...GROUP_UTILS_SRC.slice(
        GROUP_UTILS_SRC.indexOf('ENDPOINT_TYPE_OPTIONS'),
        GROUP_UTILS_SRC.indexOf('MUSIC_ENDPOINT_PROVIDER_OPTIONS'),
    ).matchAll(/labelKey: '([^']+)', value: '([^']+)'/g),
].map((m) => ({ labelKey: m[1], value: m[2] }));

test('测试自身前提：从源码解析出的端点选项表非空', () => {
    assert.ok(ENDPOINT_TYPE_OPTIONS.length >= 11, `只解析到 ${ENDPOINT_TYPE_OPTIONS.length} 项`);
});

function normalizeEndpointType(value?: string | null) {
    const n = value?.trim().toLowerCase();
    if (n === 'responses' || n === 'messages' || n === 'deepseek' || n === 'mimo') return 'chat';
    return n || '*';
}
function endpointTypeLabelKey(value?: string | null) {
    const ep = normalizeEndpointType(value);
    return ENDPOINT_TYPE_OPTIONS.find((o) => o.value === ep)?.labelKey;
}

const t = makeT('log.card');
const tGroup = makeT('group');

function label(endpointType: string, requestTypeKey?: string) {
    return resolveEndpointTypeLabel({ requestTypeKey, endpointType, t, tGroup, endpointTypeLabelKey });
}

test('resolveEndpointTypeLabel 命中 requestTypeLabels 时直接用该词条', () => {
    assert.equal(label('chat', 'chat'), '对话');
    assert.equal(label('embeddings', 'embedding'), 'Embedding');
    assert.equal(label('mimo', 'mimoChat'), 'MiMo Chat');
});

test('resolveEndpointTypeLabel 对 requestTypeLabels 缺失的端点回退到分组词条', () => {
    // inferRequestTypeKey 兜底会把原始端点名当 key（requestTypeLabels 下无此词条）。
    // 修复前这些全部显示成 'log.card.requestTypeLabels.xxx' 键路径。
    const cases: Array<[string, string]> = [
        ['rerank', 'Rerank'],
        ['moderations', 'Moderations'],
        ['image_generation', '图片生成'],
        ['audio_speech', '语音合成'],
        ['audio_transcription', '音频转写'],
        ['video_generation', '视频生成'],
        ['music_generation', '音乐生成'],
        ['search', '搜索'],
    ];
    for (const [endpointType, want] of cases) {
        assert.equal(label(endpointType, endpointType), want, `endpointType=${endpointType}`);
    }
});

test('resolveEndpointTypeLabel 任何情况下都不把翻译键路径当标签显示', () => {
    const endpointTypes = [
        '*', 'chat', 'deepseek', 'mimo', 'responses', 'messages', 'embeddings',
        'rerank', 'moderations', 'image_generation', 'audio_speech',
        'audio_transcription', 'video_generation', 'music_generation', 'search',
    ];
    for (const ep of endpointTypes) {
        // requestTypeKey 传原始端点名 = inferRequestTypeKey 的兜底分支
        const out = label(ep, ep);
        assert.ok(
            !out.includes('requestTypeLabels.') && !out.includes('form.endpointType.'),
            `endpointType=${ep} 渗出了翻译键路径: ${out}`,
        );
        assert.notEqual(out.trim(), '', `endpointType=${ep} 标签为空`);
    }
});

test('resolveEndpointTypeLabel 端点类型为空时显示占位符', () => {
    assert.equal(label(''), '-');
});

test('resolveEndpointTypeLabel 两级词条都缺失时显示原始端点名而非键路径', () => {
    const missing = () => 'group.form.endpointType.options.chat';
    const out = resolveEndpointTypeLabel({
        requestTypeKey: 'chat',
        endpointType: 'chat',
        t: (k) => `log.card.${k}`,
        tGroup: missing,
        endpointTypeLabelKey,
    });
    assert.equal(out, 'chat');
});
