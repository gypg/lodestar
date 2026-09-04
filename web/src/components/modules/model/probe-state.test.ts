/**
 * isProbeFailed 的守卫测试。
 *
 * 缺陷原貌（WO-033 生产实测）：三处调用点都写 `!!model.probe_failed_at`，
 * 而后端把该字段声明为非指针 time.Time + omitempty（对结构体无效），
 * 每个健康模型都收到 "0001-01-01T00:00:00Z"。非空字符串 truthy →
 * 广场默认视图把全部模型隐藏，横幅谎报"94 个模型连续探测失败"，
 * 当时探测开关关闭、状态表零行，从未探测过。含真实出活过的 Qwen3-Max。
 *
 * 后端已改 *time.Time（healthy → 字段缺席），本 helper 是第二道防线。
 */
import test from 'node:test';
import assert from 'node:assert/strict';

import { isProbeFailed } from './probe-state.ts';

test('Go zero time is NOT a probe failure (this is the bug that hid every model)', () => {
    assert.equal(isProbeFailed({ probe_failed_at: '0001-01-01T00:00:00Z' }), false);
});

test('Go zero time with an offset is also not a failure', () => {
    assert.equal(isProbeFailed({ probe_failed_at: '0001-01-01T00:00:00+08:00' }), false);
    assert.equal(isProbeFailed({ probe_failed_at: '0001-01-01T00:00:00.000Z' }), false);
});

test('absent / empty / blank field is not a failure', () => {
    assert.equal(isProbeFailed({}), false);
    assert.equal(isProbeFailed({ probe_failed_at: undefined }), false);
    assert.equal(isProbeFailed({ probe_failed_at: '' }), false);
    assert.equal(isProbeFailed({ probe_failed_at: '   ' }), false);
});

test('unix epoch zero is not a failure', () => {
    assert.equal(isProbeFailed({ probe_failed_at: '0' }), false);
    assert.equal(isProbeFailed({ probe_failed_at: '1970-01-01T00:00:00Z' }), false);
});

test('unparseable value is not a failure (never hide a usable model on a bad value)', () => {
    assert.equal(isProbeFailed({ probe_failed_at: 'not-a-date' }), false);
});

test('a real failure timestamp IS a failure', () => {
    assert.equal(isProbeFailed({ probe_failed_at: '2026-09-04T12:00:00Z' }), true);
    assert.equal(isProbeFailed({ probe_failed_at: '2026-09-04T20:00:00+08:00' }), true);
});

/**
 * 对称性：上面最后一条防的是"过修"——如果有人把 helper 改成恒返 false，
 * 隐藏机制整体失效（探测判定为死的模型照样出现在默认视图里），
 * 那是另一个方向的缺陷。这条测试专门守它。
 */
test('the helper must not be a constant false (that would break hiding entirely)', () => {
    const real = isProbeFailed({ probe_failed_at: '2026-01-01T00:00:00Z' });
    const zero = isProbeFailed({ probe_failed_at: '0001-01-01T00:00:00Z' });
    assert.notEqual(real, zero, 'isProbeFailed must distinguish a real failure from the zero value');
});
