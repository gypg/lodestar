import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildEndpointGroups, canonicalEndpoint, CHAT_ENDPOINT } from './endpoint-grouping.ts';
import type { ModelCapability } from '@/api/endpoints/model';

function cap(name: string, endpoints: string[]): ModelCapability {
    return { name, endpoints, conversation: true, available: true };
}

test('canonicalEndpoint 把对话族端点规约为 chat', () => {
    assert.equal(canonicalEndpoint('chat'), 'chat');
    assert.equal(canonicalEndpoint('deepseek'), 'chat');
    assert.equal(canonicalEndpoint('mimo'), 'chat');
    assert.equal(canonicalEndpoint('responses'), 'chat');
    assert.equal(canonicalEndpoint('messages'), 'chat');
    assert.equal(canonicalEndpoint('*'), 'chat');
});

test('canonicalEndpoint 保留非对话端点', () => {
    assert.equal(canonicalEndpoint('embeddings'), 'embeddings');
    assert.equal(canonicalEndpoint('rerank'), 'rerank');
    assert.equal(canonicalEndpoint('image_generation'), 'image_generation');
});

test('deepseek / mimo / 自动端点合并进对话分组', () => {
    const groups = buildEndpointGroups([
        cap('deepseek-chat', ['deepseek']),
        cap('mimo-7b', ['mimo']),
        cap('gpt-4o', ['chat']),
        cap('auto-model', ['*']),
    ]);
    // 全部对话族 -> 只有一个 chat 分组
    assert.equal(groups.length, 1);
    assert.equal(groups[0].endpoint, CHAT_ENDPOINT);
    assert.deepEqual(
        groups[0].models.map((m) => m.name),
        ['auto-model', 'deepseek-chat', 'gpt-4o', 'mimo-7b'],
    );
});

test('同一模型支持多个对话族端点时在对话分组内去重', () => {
    const groups = buildEndpointGroups([cap('multi', ['chat', 'deepseek', 'mimo'])]);
    assert.equal(groups.length, 1);
    assert.equal(groups[0].endpoint, CHAT_ENDPOINT);
    assert.equal(groups[0].models.length, 1);
    assert.equal(groups[0].models[0].name, 'multi');
});

test('非对话端点保持独立分组，对话分组排在最前', () => {
    const groups = buildEndpointGroups([
        cap('embed-1', ['embeddings']),
        cap('chat-1', ['chat']),
        cap('rerank-1', ['rerank']),
    ]);
    assert.deepEqual(
        groups.map((g) => g.endpoint),
        ['chat', 'embeddings', 'rerank'],
    );
});

test('同时支持对话族与非对话端点的模型同时出现在两个分组', () => {
    const groups = buildEndpointGroups([cap('hybrid', ['deepseek', 'embeddings'])]);
    assert.equal(groups.length, 2);
    const byEndpoint = Object.fromEntries(groups.map((g) => [g.endpoint, g.models.map((m) => m.name)]));
    assert.deepEqual(byEndpoint['chat'], ['hybrid']);
    assert.deepEqual(byEndpoint['embeddings'], ['hybrid']);
});

test('空 endpoints 视为自动端点并入对话分组', () => {
    const groups = buildEndpointGroups([cap('no-endpoint', [])]);
    assert.equal(groups.length, 1);
    assert.equal(groups[0].endpoint, CHAT_ENDPOINT);
    assert.equal(groups[0].models[0].name, 'no-endpoint');
});

test('空输入返回空数组', () => {
    assert.deepEqual(buildEndpointGroups([]), []);
    assert.deepEqual(buildEndpointGroups(undefined), []);
});
