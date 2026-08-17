/**
 * useElementHeights 测量链纯逻辑测试（use-element-heights.ts:1）
 *
 * 不渲染 React 组件——通过 element-heights-core.ts 抽离的纯函数来验证不变量。
 * 这是仓库既有约定：node:test + --experimental-strip-types 不含 React 渲染环境
 * （无 @testing-library/react），任何 hook 行为测试必须把逻辑抽成纯函数再测。
 * 见 card-measure.test.ts 样板 / [[lodestar-test-assertion-gaps]]。
 *
 * 覆盖任务点名的 5 个场景：
 * 1. 稳定 ref：同一 key 多次取 getMeasureRef 返回同一函数引用（纯逻辑层面模拟）
 * 2. observer 配对：node 变化时旧 observer disconnect、新 observer 建立
 * 3. 0/负/非有限高度丢弃
 * 4. 同高返回同一引用（不触发 setState）
 * 5. 自定义判定函数生效
 *
 * 无法测的部分（如实说明）：ref 稳定是否真的阻止 React 渲染时序上的
 * measure → render → measure 死循环——这属于渲染时序，node:test 覆盖不到，
 * 靠 hook 代码结构保证。见文末「无法测的不变量」。
 */

import assert from 'node:assert/strict';
import test from 'node:test';

import {
    findCachedRef,
    nextHeights,
    planNodeChange,
    shouldRecordHeight,
    type HeightMap,
} from './element-heights-core.ts';

// ---------------------------------------------------------------------------
// 1. 稳定 ref：同一 key 多次取 getMeasureRef 返回同一函数引用
//
// 这是死循环成因 1 的回归点。把 hook 内部的 ref 缓存改成每次新建（即
// findCachedRef 始终返回 undefined），下面的「同一函数引用」断言会红——
// 这是 node:test 唯一能抓到的死循环回归点。
// ---------------------------------------------------------------------------

test('findCachedRef returns the same function reference for the same key', () => {
    const cache = new Map<number, (node: unknown) => void>();
    const refA = (node: unknown) => { void node; };
    const refB = (node: unknown) => { void node; };

    cache.set(7, refA);
    assert.equal(findCachedRef(cache, 7), refA);
    assert.equal(findCachedRef(cache, 7), findCachedRef(cache, 7), '同 key 多次取必须同一引用');
    assert.notEqual(findCachedRef(cache, 7), refB, '不同函数引用不得命中');
    assert.equal(findCachedRef(cache, 99), undefined, '未注册 key 返回 undefined');
});

test('findCachedRef distinguishes keys', () => {
    const cache = new Map<number, (node: unknown) => void>();
    const refA = (node: unknown) => { void node; };
    const refB = (node: unknown) => { void node; };
    cache.set(1, refA);
    cache.set(2, refB);

    assert.equal(findCachedRef(cache, 1), refA);
    assert.equal(findCachedRef(cache, 2), refB);
    assert.notEqual(findCachedRef(cache, 1), findCachedRef(cache, 2));
});

// 模拟「getMeasureRef 稳定缓存」的纯逻辑序列：第一次未命中 → 创建并写入 →
// 后续命中同一引用。这是 hook getMeasureRef 内部分支的纯逻辑投影。
test('stable ref cache sequence: miss then hit on subsequent reads', () => {
    const cache = new Map<number, (node: unknown) => void>();
    const key = 42;

    // 第一次：未命中
    let cached = findCachedRef(cache, key);
    assert.equal(cached, undefined);

    // hook 在未命中时会新建 ref 并写入缓存
    const created = (node: unknown) => { void node; };
    cache.set(key, created);

    // 后续：永远命中同一引用
    cached = findCachedRef(cache, key);
    assert.equal(cached, created);
    assert.equal(findCachedRef(cache, key), findCachedRef(cache, key));
});

// ---------------------------------------------------------------------------
// 2. observer 配对：node 变化时旧 observer disconnect、新 observer 建立
//
// 模拟 ref 回调的 node 切换序列。planNodeChange 返回 teardown/setup 标志，
// hook 据此执行真实副作用。这里只断言决策正确，不断言 ResizeObserver 调用
// （那是 DOM 副作用，无法在 node:test 里跑）。
// ---------------------------------------------------------------------------

test('planNodeChange: first mount sets up without teardown', () => {
    const plan = planNodeChange(undefined, {} as HTMLElement);
    assert.equal(plan.teardown, false, '首次挂载无旧 observer 可 disconnect');
    assert.equal(plan.setup, true);
});

test('planNodeChange: same node is a no-op (avoid duplicate observe)', () => {
    const node = {} as HTMLElement;
    assert.deepEqual(planNodeChange(node, node), { teardown: false, setup: false });
});

test('planNodeChange: unmount tears down without setup', () => {
    const node = {} as HTMLElement;
    const plan = planNodeChange(node, null);
    assert.equal(plan.teardown, true, '卸载必须 disconnect 旧 observer');
    assert.equal(plan.setup, false, 'null node 不应建立新 observer');
});

test('planNodeChange: node switch tears down and sets up', () => {
    const oldNode = {} as HTMLElement;
    const newNode = {} as HTMLElement;
    const plan = planNodeChange(oldNode, newNode);
    assert.equal(plan.teardown, true, '换节点必须先 disconnect 旧 observer');
    assert.equal(plan.setup, true, '换节点必须建立新 observer');
});

// React 内联 ref 回调的死循环成因：每次渲染新函数 → React 先用 null 卸载旧 ref
// 再挂载新 ref。下面这串序列就是「稳定 ref 被破坏后」会发生的 node 切换流：
// mount → unmount(null) → mount → unmount(null) → ... 每次 unmount 都触发
// teardown，每次 mount 都触发 setup + 一次测量。这把死循环的纯逻辑投影固化下来。
test('planNodeChange: unstable ref callback sequence (mount/unmount cycle)', () => {
    const node = {} as HTMLElement;
    // 稳定 ref 下：node 不变，每次都 no-op
    assert.deepEqual(planNodeChange(node, node), { teardown: false, setup: false });
    assert.deepEqual(planNodeChange(node, node), { teardown: false, setup: false });

    // 不稳定 ref 下（React 每次新建函数）：先 null 卸载，再 mount 重新挂载
    const unmount = planNodeChange(node, null);
    const remount = planNodeChange(undefined, node);
    assert.equal(unmount.teardown && unmount.setup === false, true, 'null 卸载触发 disconnect');
    assert.equal(remount.teardown === false && remount.setup, true, '重新挂载触发 setup + 测量');
});

// ---------------------------------------------------------------------------
// 3. 0 / 负 / 非有限高度丢弃
//
// 死循环成因 2：移动端(md:hidden)与桌面(hidden md:grid)两套 DOM 同时挂载，
// 隐藏那套测出来恒为 0。若写进表里，会和真实高度反复互相覆盖。
// ---------------------------------------------------------------------------

test('shouldRecordHeight rejects zero, negative, and non-finite', () => {
    assert.equal(shouldRecordHeight(0), false);
    assert.equal(shouldRecordHeight(-1), false);
    assert.equal(shouldRecordHeight(Number.NaN), false);
    assert.equal(shouldRecordHeight(Number.POSITIVE_INFINITY), false);
    assert.equal(shouldRecordHeight(Number.NEGATIVE_INFINITY), false);
    assert.equal(shouldRecordHeight(420), true);
});

test('nextHeights ignores zero height from a display:none node', () => {
    const current: HeightMap<number> = { 7: 420 };
    assert.equal(nextHeights(current, 7, 0), current, '0 不得覆盖真实高度');
    assert.deepEqual(nextHeights(current, 7, 0), { 7: 420 });
});

test('nextHeights ignores negative and non-finite heights', () => {
    const current: HeightMap<number> = { 7: 420 };
    assert.equal(nextHeights(current, 7, -10), current);
    assert.equal(nextHeights(current, 7, Number.NaN), current);
    assert.equal(nextHeights(current, 7, Number.POSITIVE_INFINITY), current);
});

test('nextHeights does not establish an entry for zero height', () => {
    const current: HeightMap<number> = {};
    assert.equal(nextHeights(current, 9, 0), current);
    assert.deepEqual(nextHeights(current, 9, 0), {});
});

// ---------------------------------------------------------------------------
// 4. 同高返回同一引用（不触发 setState）
//
// 死循环成因 2 的另一半：高度未变时若返回新对象，依赖高度表的 useMemo 每次失效
// → 重新分栏 → 重新渲染 → 重新测量。
// ---------------------------------------------------------------------------

test('nextHeights returns the same reference when height is unchanged', () => {
    const current: HeightMap<number> = { 7: 420, 8: 300 };
    assert.equal(nextHeights(current, 7, 420), current, '同高必须返回同一引用');
});

test('nextHeights treats sub-pixel jitter as unchanged after rounding', () => {
    const current: HeightMap<number> = { 7: 420 };
    // ResizeObserver 常报 419.998 这类抖动
    assert.equal(nextHeights(current, 7, 419.998), current);
    assert.equal(nextHeights(current, 7, 420.499), current);
});

test('nextHeights returns a new reference when height actually changes', () => {
    const current: HeightMap<number> = { 7: 420, 8: 300 };
    const result = nextHeights(current, 8, 360);
    assert.notEqual(result, current);
    assert.deepEqual(result, { 7: 420, 8: 360 });
});

// ---------------------------------------------------------------------------
// 5. 自定义判定函数生效
//
// hook 接受可选 canRecord，让调用方能扩展（比如站点卡只测桌面那套 DOM）。
// ---------------------------------------------------------------------------

test('nextHeights honors a custom canRecord predicate', () => {
    // 只记录 >100 的高度
    const onlyTall = (h: number) => h > 100;
    const current: HeightMap<number> = { 7: 420 };

    // 50 会被 shouldRecordHeight 接受，但被自定义判定拒绝
    assert.equal(nextHeights(current, 7, 50, onlyTall), current);
    // 0 仍被拒绝（自定义判定不放宽默认丢弃）
    assert.equal(nextHeights(current, 7, 0, onlyTall), current);
    // 200 接受
    const result = nextHeights(current, 7, 200, onlyTall);
    assert.deepEqual(result, { 7: 200 });
});

test('custom canRecord can reject positive finite heights', () => {
    // 站点卡场景的扩展点：调用方可以按业务规则过滤某些高度
    const onlyEven = (h: number) => Math.round(h) % 2 === 0;
    const current: HeightMap<number> = { 7: 420 };
    assert.equal(nextHeights(current, 7, 101, onlyEven), current, '奇数高度被自定义判定拒绝');
    const result = nextHeights(current, 7, 102, onlyEven);
    assert.deepEqual(result, { 7: 102 });
});

// ---------------------------------------------------------------------------
// 无法测的不变量（如实说明）
//
// 以下两条死循环防御只能靠 hook 代码结构保证，node:test 无法覆盖：
//
// a) ref 函数真的按 key 稳定：上面 findCachedRef 测的是「缓存命中后返回同一引用」，
//    但「hook 是否真的在 getMeasureRef 里调了 findCachedRef、且命中时直接 return」
//    属于 hook 控制流——node:test 不渲染 React 就无法触发 ref 回调。变异实验
//    （把 hook 内 findCachedRef 改成始终返回 undefined）会让上面 findCachedRef
//    测试本身仍然绿，但 hook 行为已坏——这条只能靠人工变异 + 渲染验证抓。
//
// b) ResizeObserver 真的在 node 切换时 disconnect 旧实例：planNodeChange 测的是
//    决策标志，但「hook 真的按 plan.teardown 调了 observer.disconnect()」属于
//    DOM 副作用，node:test 无法断言。
//
// 这两条的诚实表述：测试守住的是「纯决策逻辑正确」，渲染时序的「真的不构成死循环」
// 靠 use-element-heights.ts 的代码结构（缓存命中即 return、teardown 分支存在）保证。
// ---------------------------------------------------------------------------
