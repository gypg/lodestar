/**
 * AI 路由分析「来源」开关的恢复规则测试。
 *
 * 缺陷原貌（用户实测报修）：`mode` 只是组件内 useState('external')，从不落库。
 * 运营者切到「本站模型」选好模型后，模型名从 settings 读得回来、开关却每次挂载都
 * 复位成「外部连接」—— 界面于是显示「选的是本站模型，但已切换为外部服务」，两句
 * 自相矛盾。本文件钉死"开关位置必须来自持久化值"。
 *
 * 两个方向都要覆盖：只测 local 能恢复，会让"恒返回 local"的实现也通过，而那会把
 * 外部连接配置显示成本站模式，是同样严重的反向缺陷。
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { resolveAIRouteSourceMode } from './ai-route-source-mode.ts';

const KEY = 'ai_route_source_mode';

test('restores local mode from the persisted row (the bug: this reset to external)', () => {
    assert.equal(resolveAIRouteSourceMode([{ key: KEY, value: 'local' }]), 'local');
});

test('restores external mode from the persisted row', () => {
    assert.equal(resolveAIRouteSourceMode([{ key: KEY, value: 'external' }]), 'external');
});

test('picks the row by key rather than by position', () => {
    const settings = [
        { key: 'ai_route_model', value: 'local' },
        { key: 'ai_route_base_url', value: 'https://example.com/v1' },
        { key: KEY, value: 'local' },
    ];
    assert.equal(resolveAIRouteSourceMode(settings), 'local');

    // A neighbouring row holding the word "external" must not win.
    const decoyed = [
        { key: 'ai_route_model', value: 'external' },
        { key: KEY, value: 'local' },
    ];
    assert.equal(resolveAIRouteSourceMode(decoyed), 'local');
});

test('falls back to external when the setting was never written', () => {
    assert.equal(resolveAIRouteSourceMode([]), 'external');
    assert.equal(resolveAIRouteSourceMode([{ key: 'ai_route_model', value: 'gpt-4o' }]), 'external');
});

test('falls back to external while settings are still loading', () => {
    assert.equal(resolveAIRouteSourceMode(undefined), 'external');
    assert.equal(resolveAIRouteSourceMode(null), 'external');
});

test('unknown wire values fall back to external instead of throwing or returning them', () => {
    // Setting.Validate rejects these on write; reaching here means an out-of-band
    // row. Returning the raw string would put the toggle in neither position.
    for (const value of ['hybrid', 'Local', ' local', 'true', '']) {
        assert.equal(
            resolveAIRouteSourceMode([{ key: KEY, value }]),
            'external',
            `value ${JSON.stringify(value)} should fall back to external`,
        );
    }
});

/*
 * Wiring assertions.
 *
 * The behavioural tests above pass just as well when AIRouteConfig never calls
 * the helper — which is exactly the state the bug was in. WO-034 was burned by
 * a wiring test that only pinned the call shape, so these pin the two edges that
 * carry the fix: the resolver must be fed the settings list, and the toggle must
 * go through the persisting handler rather than the bare state setter.
 */
const componentSource = readFileSync(join(import.meta.dirname, 'AIRouteConfig.tsx'), 'utf8');

test('the component restores its toggle through the resolver, fed by settings', () => {
    assert.match(
        componentSource,
        /resolveAIRouteSourceMode\(settings\)/,
        'AIRouteConfig must call resolveAIRouteSourceMode(settings); a hardcoded default reintroduces the reset-to-external bug',
    );
});

test('the toggle persists instead of only moving local state', () => {
    const switchHandler = componentSource.match(/onCheckedChange=\{([^}]*)\}/)?.[1] ?? '';
    assert.match(
        switchHandler,
        /handleModeChange/,
        'the Switch must call handleModeChange so the choice is written; calling setMode directly leaves it unpersisted',
    );
    assert.doesNotMatch(
        switchHandler,
        /setMode\(/,
        'the Switch must not bypass handleModeChange by calling setMode directly',
    );
    assert.match(
        componentSource,
        /key:\s*SettingKey\.AIRouteSourceMode/,
        'handleModeChange must write the AIRouteSourceMode setting',
    );
});

test('the local-mode dead end offers a real way out', () => {
    // The old copy claimed "已切换为外部服务" while no code path called setMode:
    // it described a switch that never happened, and local mode has no fields for
    // base_url/api_key, so the "needs configuring" banner stayed up forever.
    assert.match(
        componentSource,
        /onClick=\{\(\)\s*=>\s*handleModeChange\('external'\)\}/,
        'the incomplete-channel branch must offer an action that actually switches mode',
    );
});
