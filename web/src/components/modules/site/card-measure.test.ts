import assert from 'node:assert/strict';
import test from 'node:test';

import { nextSiteCardHeights, type SiteCardHeights } from './card-measure.ts';

test('nextSiteCardHeights records a real measured height', () => {
    const result = nextSiteCardHeights({}, 7, 420);
    assert.deepEqual(result, { 7: 420 });
});

test('nextSiteCardHeights rounds sub-pixel heights', () => {
    const result = nextSiteCardHeights({}, 7, 419.6);
    assert.deepEqual(result, { 7: 420 });
});

// 死循环成因 1：隐藏的那套 DOM（md:hidden / hidden md:grid 同时挂载）测出 0，
// 若写进表里就会与真实高度反复互相覆盖。
test('nextSiteCardHeights ignores zero height from a display:none node', () => {
    const current: SiteCardHeights = { 7: 420 };
    const result = nextSiteCardHeights(current, 7, 0);
    assert.equal(result, current, '必须返回同一引用，且不得把 420 覆盖成 0');
    assert.deepEqual(result, { 7: 420 });
});

test('nextSiteCardHeights ignores negative and non-finite heights', () => {
    const current: SiteCardHeights = { 7: 420 };
    assert.equal(nextSiteCardHeights(current, 7, -10), current);
    assert.equal(nextSiteCardHeights(current, 7, Number.NaN), current);
    assert.equal(nextSiteCardHeights(current, 7, Number.POSITIVE_INFINITY), current);
});

test('nextSiteCardHeights does not write zero even for an unmeasured site', () => {
    const current: SiteCardHeights = {};
    const result = nextSiteCardHeights(current, 9, 0);
    assert.equal(result, current);
    assert.deepEqual(result, {}, '0 高度不得建立条目');
});

// 死循环成因 2：高度未变时返回新对象会让 masonry 的 useMemo 每次失效，
// 触发 重新分栏 → 渲染 → 再测量 的循环。
test('nextSiteCardHeights returns the same reference when height is unchanged', () => {
    const current: SiteCardHeights = { 7: 420, 8: 300 };
    const result = nextSiteCardHeights(current, 7, 420);
    assert.equal(result, current, '同高必须返回同一引用，否则 useMemo 每渲染都失效');
});

test('nextSiteCardHeights treats a sub-pixel jitter as unchanged after rounding', () => {
    const current: SiteCardHeights = { 7: 420 };
    // ResizeObserver 常报 419.998 这类抖动；四舍五入后同值，必须视为无变化。
    const result = nextSiteCardHeights(current, 7, 419.998);
    assert.equal(result, current);
});

test('nextSiteCardHeights preserves other sites when one changes', () => {
    const current: SiteCardHeights = { 7: 420, 8: 300 };
    const result = nextSiteCardHeights(current, 8, 360);
    assert.notEqual(result, current);
    assert.deepEqual(result, { 7: 420, 8: 360 });
});
