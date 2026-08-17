'use client';

/**
 * 元素高度测量链的可测纯逻辑（不依赖 React / DOM 运行时）。
 *
 * 配套 [[useElementHeights]] hook：hook 把这些纯函数组合成稳定 ref + ResizeObserver
 * 配对 + state 更新，而这里只放能用 node:test 直接断言的逻辑。
 *
 * 三条死循环防御（React error #185 成因，绝不能回归）：
 * 1. ref 函数必须按 key 稳定缓存——见 {@link findCachedRef}。
 * 2. node 切换时旧 ResizeObserver 必须 disconnect——见 {@link planNodeChange}。
 * 3. 0 / 负 / 非有限高度必须丢弃，且同高返回同一引用——见 {@link nextHeights}。
 *
 * 为什么不在这里测「渲染时序」：node:test 没有 React 渲染环境（仓库无
 * @testing-library/react），ref 稳定性是否真的阻止了 measure → render → measure
 * 循环，只能由 hook 的代码结构保证，无法用单元测试覆盖。见
 * [[site-card-measure-loop]] / [[lodestar-test-assertion-gaps]]。
 */

/**
 * 高度表。key 通常是站点 id 这类业务标识。
 */
export type HeightMap<K extends string | number> = Record<K, number>;

/**
 * 默认判定：丢弃 0 / 负 / 非有限高度。
 *
 * `display:none` 的节点（移动端与桌面两套 DOM 同时挂载时隐藏那套）测出来恒为 0，
 * 写入会与真实高度反复互相覆盖——这是死循环成因之一。
 */
export function shouldRecordHeight(rawHeight: number): boolean {
    return Number.isFinite(rawHeight) && rawHeight > 0;
}

/**
 * 计算下一份高度表；返回原对象表示「无变化，不要 setState」。
 *
 * 不变量：高度没变时必须返回**同一个对象引用**，否则依赖它的 useMemo 每次都失效，
 * 重新分栏 → 重新渲染 → 重新测量。亚像素抖动先四舍五入再比较。
 */
export function nextHeights<K extends string | number>(
    current: HeightMap<K>,
    key: K,
    rawHeight: number,
    canRecord: (rawHeight: number) => boolean = shouldRecordHeight,
): HeightMap<K> {
    if (!canRecord(rawHeight)) {
        return current;
    }
    const nextHeight = Math.round(rawHeight);
    if (nextHeight <= 0) {
        return current;
    }
    if (current[key] === nextHeight) {
        return current;
    }
    return { ...current, [key]: nextHeight };
}

/**
 * ref 函数缓存命中判定。
 *
 * hook 用 `Map<K, (node) => void>` 缓存每个 key 对应的 ref 回调；只要 key 不变，
 * {@link useElementHeights.getMeasureRef} 必须返回**同一个函数引用**，否则 React
 * 会先用 null 卸载旧 ref 再挂载新 ref，每渲染一次就重测一次。
 *
 * 把命中判定抽成纯函数，是为了让「稳定 ref」这条不变量在纯逻辑层面可测：
 * 给定同一 cache 与 key，多次调用必须命中同一份缓存——这是 node:test 唯一能抓到的
 * 死循环回归点（把 hook 内部改成每次新建 ref，下面的测试会红）。但「ref 稳定是否
 * 真的阻止了 React 渲染时序上的 measure → render → measure 循环」仍无法用
 * node:test 覆盖，那部分靠 hook 代码结构保证。
 *
 * 泛型 `N` 是 ref 回调接受的 node 类型，默认 `unknown` 以保持纯逻辑层与 DOM 解耦。
 */
export function findCachedRef<K extends string | number, N = unknown>(
    cache: Map<K, (node: N) => void>,
    key: K,
): ((node: N) => void) | undefined {
    return cache.get(key);
}

/**
 * node 切换时 observer 配对的结果。hook 拿到这个描述后执行真实副作用
 * （disconnect 旧 observer / observe 新 node）。
 *
 * 不直接做副作用，是为了把「旧 observer 必须 disconnect」这条不变量抽到纯逻辑
 * 层面可测——给定 node 序列，能断言 disconnect / observe 的发生顺序与时机。
 */
export interface NodeChangePlan {
    /** 需要先 disconnect 并清理的旧 key（node 从有值变成 null 或换成别的节点时）。 */
    readonly teardown: boolean;
    /** 需要注册新 observer 的 key（node 是一个真实元素时）。 */
    readonly setup: boolean;
}

/**
 * 根据「当前已注册的 node」与「新到达的 node」决定 observer 配对动作。
 *
 * 不变量：
 * - 当前 node 与新 node 是同一个元素时，什么都不做（避免重复 observe）。
 * - 当前有 node 但新 node 不同 / 为 null 时，必须 disconnect 旧 observer。
 * - 新 node 真实存在时，建立新 observer 并写入注册表。
 */
export function planNodeChange(
    currentNode: unknown | undefined,
    newNode: unknown | null,
): NodeChangePlan {
    if (currentNode === newNode) {
        return { teardown: false, setup: false };
    }
    const teardown = currentNode !== undefined && currentNode !== null;
    const setup = newNode !== null && newNode !== undefined;
    return { teardown, setup };
}
