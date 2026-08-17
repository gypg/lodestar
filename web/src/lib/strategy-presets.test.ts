import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import {
    STRATEGY_PRESETS,
    GROUP_MODE,
    PRESET_SETTING_KEYS,
    DEFAULT_PRESET_ID,
    presetById,
    defaultPreset,
    recommendStrategyPreset,
    keysInRange,
    keyRangeLabel,
    presetSettingWrites,
} from './strategy-presets.ts';

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, '..', '..');
const repoRoot = path.resolve(webRoot, '..');

/** 读 web/ 下的文件（源码与 locale）。 */
function readWebFile(rel: string): string {
    return fs.readFileSync(path.join(webRoot, rel), 'utf-8');
}

/** 读仓库根下的文件（Go 后端源码）。 */
function readRepoFile(rel: string): string {
    return fs.readFileSync(path.join(repoRoot, rel), 'utf-8');
}

test('five presets exist with unique ids in display order', () => {
    const ids = STRATEGY_PRESETS.map((p) => p.id);
    assert.deepEqual(ids, ['guardian', 'balanced', 'velocity', 'fairshare', 'adaptive']);
    assert.equal(new Set(ids).size, ids.length, 'preset ids must be unique');
});

test('presets map to the intended GroupMode values', () => {
    const byId = Object.fromEntries(STRATEGY_PRESETS.map((p) => [p.id, p.mode]));
    // StrictPriority ≈ Failover；LeastInflight 无对应 → Random 近似。
    assert.equal(byId.guardian, GROUP_MODE.Failover);
    assert.equal(byId.balanced, GROUP_MODE.Weighted);
    assert.equal(byId.velocity, GROUP_MODE.Random);
    assert.equal(byId.fairshare, GROUP_MODE.RoundRobin);
    assert.equal(byId.adaptive, GROUP_MODE.Auto);
});

test('GROUP_MODE mirror matches the real enum in api/endpoints/group.ts', () => {
    const src = readWebFile(path.join('src', 'api', 'endpoints', 'group.ts'));
    for (const [name, value] of Object.entries(GROUP_MODE)) {
        const re = new RegExp(`${name}\\s*=\\s*(\\d+)`);
        const m = src.match(re);
        assert.ok(m, `GroupMode.${name} missing in api/endpoints/group.ts`);
        assert.equal(Number(m[1]), value, `GroupMode.${name} drifted from GROUP_MODE mirror`);
    }
});

test('preset setting keys match SettingKey strings used by the settings API', () => {
    const src = readWebFile(path.join('src', 'api', 'endpoints', 'setting.ts'));
    for (const key of Object.values(PRESET_SETTING_KEYS)) {
        assert.ok(
            src.includes(`'${key}'`) || src.includes(`"${key}"`),
            `setting key "${key}" not found in api/endpoints/setting.ts`
        );
    }
});

test('applying a preset batch-fills exactly its knob set, in order', () => {
    const guardian = presetSettingWrites(presetById('guardian')!);
    assert.deepEqual(guardian, [
        { key: 'circuit_breaker_threshold', value: '20' },
        { key: 'circuit_breaker_cooldown', value: '60' },
        { key: 'relay_retry_count', value: '3' },
        { key: 'ratelimit_cooldown', value: '600' },
        { key: 'relay_max_total_attempts', value: '8' },
    ]);
    // 宽容熔断 vs 标准：guardian 阈值必须显著高于 balanced。
    const balanced = presetSettingWrites(presetById('balanced')!);
    const threshold = (writes: { key: string; value: string }[]) =>
        Number(writes.find((w) => w.key === 'circuit_breaker_threshold')!.value);
    assert.ok(threshold(guardian) > threshold(balanced), 'guardian breaker must be more lenient');
    // fairshare 近似 fanout=1：重试 1 次即换键。
    const fairshare = presetSettingWrites(presetById('fairshare')!);
    assert.equal(fairshare.find((w) => w.key === 'relay_retry_count')!.value, '1');
    // velocity 低延迟：更短的熔断与限流冷却。
    const velocity = presetSettingWrites(presetById('velocity')!);
    assert.equal(velocity.find((w) => w.key === 'circuit_breaker_cooldown')!.value, '30');
    assert.equal(velocity.find((w) => w.key === 'ratelimit_cooldown')!.value, '60');
});

test('balanced preset values equal the backend factory defaults', () => {
    // 后端默认值声明在 internal/model/setting.go：常量块给出 常量名→字符串键，
    // 默认值列表给出 常量名→Value。两段解析后对齐，防止预设与出厂默认漂移。
    const src = readRepoFile(path.join('internal', 'model', 'setting.go'));
    const keyToConst = new Map<string, string>();
    for (const line of src.split('\n')) {
        const m = line.match(/(SettingKey[A-Za-z]+)\s+SettingKey\s*=\s*"([^"]+)"/);
        if (m) keyToConst.set(m[2], m[1]);
    }
    const backendDefault = (key: string): string => {
        const constName = keyToConst.get(key);
        if (!constName) return '';
        const line = src.split('\n').find((l) => l.includes(constName) && l.includes('Value:'));
        return line?.match(/Value:\s*"([0-9]+)"/)?.[1] ?? '';
    };
    for (const write of presetSettingWrites(defaultPreset())) {
        const def = backendDefault(write.key);
        assert.ok(def, `backend default for ${write.key} not found in setting.go`);
        assert.equal(write.value, def, `balanced preset ${write.key} drifted from backend default`);
    }
});

test('recommend by key count follows the N-SLMCRS thresholds', () => {
    for (const n of [0, 1, 2, 3]) assert.equal(recommendStrategyPreset(n), 'guardian', `keyCount=${n}`);
    for (const n of [4, 5, 6, 7]) assert.equal(recommendStrategyPreset(n), 'balanced', `keyCount=${n}`);
    for (const n of [8, 20, 1000]) assert.equal(recommendStrategyPreset(n), 'velocity', `keyCount=${n}`);
});

test('key range helpers expose bounds and labels', () => {
    const guardian = presetById('guardian')!;
    const balanced = presetById('balanced')!;
    const velocity = presetById('velocity')!;
    const fairshare = presetById('fairshare')!;
    const adaptive = presetById('adaptive')!;

    assert.equal(keyRangeLabel(guardian), '≤ 3');
    assert.equal(keyRangeLabel(balanced), '4 – 7');
    assert.equal(keyRangeLabel(velocity), '≥ 8');
    assert.equal(keyRangeLabel(fairshare), '≥ 2');
    assert.equal(keyRangeLabel(adaptive), '');

    assert.ok(keysInRange(guardian, 0) && keysInRange(guardian, 3));
    assert.ok(!keysInRange(guardian, 4));
    assert.ok(keysInRange(balanced, 4) && keysInRange(balanced, 7));
    assert.ok(!keysInRange(balanced, 3) && !keysInRange(balanced, 8));
    // 无上界（0）表示不设限。
    assert.ok(keysInRange(velocity, 1000));
    assert.ok(keysInRange(adaptive, 1) && keysInRange(adaptive, 9999));
});

test('default preset is balanced and presetById resolves all ids', () => {
    assert.equal(DEFAULT_PRESET_ID, 'balanced');
    assert.equal(defaultPreset().id, 'balanced');
    for (const p of STRATEGY_PRESETS) {
        assert.equal(presetById(p.id)!.id, p.id);
    }
    assert.equal(presetById('nonexistent'), undefined);
});

test('approximation notes are attached only to approximated presets', () => {
    assert.equal(presetById('guardian')!.approximationKey, 'setting.strategyPresets.approximation.strictPriority');
    assert.equal(presetById('velocity')!.approximationKey, 'setting.strategyPresets.approximation.leastInflight');
    for (const id of ['balanced', 'fairshare', 'adaptive'] as const) {
        assert.equal(presetById(id)!.approximationKey, undefined, `${id} should not carry an approximation note`);
    }
});

test('every referenced locale key exists and is non-empty in all three locales', () => {
    const locales = ['zh_hans', 'en', 'zh_hant'];
    for (const loc of locales) {
        const dict = JSON.parse(readWebFile(path.join('src', 'locales', `${loc}.json`)));
        const resolve = (dotted: string): unknown =>
            dotted.split('.').reduce<unknown>((acc, part) => (acc as Record<string, unknown>)?.[part], dict);
        for (const preset of STRATEGY_PRESETS) {
            for (const key of [preset.nameKey, preset.characterKey, preset.scenarioKey, preset.approximationKey]) {
                if (!key) continue;
                const value = resolve(key);
                assert.ok(typeof value === 'string' && value.trim().length > 0, `${loc}: locale key "${key}" missing or empty`);
            }
        }
        // UI 依赖的固定文案键。
        for (const key of [
            'setting.strategyPresets.title',
            'setting.strategyPresets.hint',
            'setting.strategyPresets.apply',
            'setting.strategyPresets.keyCount',
            'setting.strategyPresets.modeLabel',
            'setting.strategyPresets.knobsLabel',
            'group.presets.label',
            'group.presets.recommended',
        ]) {
            const value = resolve(key);
            assert.ok(typeof value === 'string' && value.trim().length > 0, `${loc}: locale key "${key}" missing or empty`);
        }
    }
});
