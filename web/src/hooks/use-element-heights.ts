'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

import {
    findCachedRef,
    nextHeights,
    planNodeChange,
    shouldRecordHeight,
    type HeightMap,
} from './element-heights-core.ts';

/**
 * 可复用的元素高度测量 hook。
 *
 * 替代 web/src/components/modules/site/index.tsx 里曾经散落三处的测量逻辑：
 * `measureRefsRef`（ref 函数缓存）、`cardObserversRef` + `cardElementsRef`
 * （ResizeObserver 配对 / element 注册）、`applySiteCardHeight` +
 * `getSiteCardMeasureRef`（state 更新 + 稳定 ref 生成）。
 *
 * 把三条死循环防御固化在内部，调用方拿不到裸 ref 构造入口：
 *
 * 1. **稳定 ref**：`getMeasureRef(key)` 按 key 缓存 ref 函数，同一 key 永远返回同一
 *    函数引用。调用方无法内联 `ref={(node) => ...}`，也就不会每渲染都产生新函数
 *    触发 React 先 null 卸载再重新挂载。
 * 2. **observer 配对 disconnect**：node 切换（旧 node 卸载 / 换成新 node）时，
 *    旧 ResizeObserver 必然先 disconnect 再清理，不会泄漏、不会重复 observe。
 * 3. **0 高度丢弃 + 同高返回同一引用**：默认丢弃 0 / 负 / 非有限高度；同高返回
 *    原对象引用，避免依赖高度表的 useMemo 每渲染都失效。见
 *    {@link shouldRecordHeight} / {@link nextHeights}。
 *
 * 可测性：以上不变量里，1/2/3 的纯判定逻辑抽到了
 * `element-heights-core.ts` 并用 node:test 覆盖。但「ref 稳定是否真的阻止了
 * measure → render → measure 死循环」属于 React 渲染时序，node:test 没有
 * @testing-library/react 无法覆盖——这部分靠本 hook 的代码结构保证。见
 * [[lodestar-test-assertion-gaps]]。
 *
 * @param canRecord 可选的「是否记录本次测量」判定。默认丢弃 0/负/非有限高度
 *   （`display:none` 节点测出 0，写入会与真实高度互相覆盖成死循环）。
 *   调用方可扩展，例如只测某一套 DOM。
 */
export function useElementHeights<K extends string | number>(
    canRecord: (rawHeight: number) => boolean = shouldRecordHeight,
) {
    const [heights, setHeights] = useState<HeightMap<K>>({} as HeightMap<K>);

    // 每个 key 对应的 ResizeObserver。node 切换时旧 observer 必须 disconnect。
    const observersRef = useRef<Map<K, ResizeObserver>>(new Map());
    // 每个 key 当前注册的 DOM node。供 getMeasureRef 内部判定 node 是否变化，
    // 也供调用方通过 getElement 拿到已注册节点（如 scrollIntoView）。
    const elementsRef = useRef<Map<K, HTMLElement>>(new Map());
    // ref 函数缓存：同一 key 永远返回同一函数引用——稳定 ref 不变量。
    // 故意不暴露这个 map，调用方无法绕过 getMeasureRef 自己造新 ref。
    const refCacheRef = useRef<Map<K, (node: HTMLElement | null) => void>>(
        new Map(),
    );

    const applyHeight = useCallback(
        (key: K, rawHeight: number) => {
            setHeights((current) => nextHeights(current, key, rawHeight, canRecord));
        },
        [canRecord],
    );

    const getMeasureRef = useCallback(
        (key: K) => {
            const cache = refCacheRef.current;
            const cached = findCachedRef(cache, key);
            // 稳定 ref 不变量：命中即复用，绝不每渲染新建。
            if (cached) return cached;

            const ref = (node: HTMLElement | null) => {
                const observers = observersRef.current;
                const elements = elementsRef.current;
                const currentNode = elements.get(key);

                const plan = planNodeChange(currentNode, node);

                if (plan.teardown) {
                    observers.get(key)?.disconnect();
                    observers.delete(key);
                    elements.delete(key);
                }

                if (!plan.setup || !node) {
                    return;
                }

                elements.set(key, node);
                const observer = new ResizeObserver((entries) => {
                    applyHeight(
                        key,
                        entries[0]?.contentRect.height ??
                            node.getBoundingClientRect().height,
                    );
                });
                observer.observe(node);
                observers.set(key, observer);

                applyHeight(key, node.getBoundingClientRect().height);
            };

            cache.set(key, ref);
            return ref;
        },
        [applyHeight],
    );

    /** 取已注册的 DOM node（例如 jump scroll 要 scrollIntoView）。只读访问。 */
    const getElement = useCallback((key: K): HTMLElement | undefined => {
        return elementsRef.current.get(key);
    }, []);

    // 卸载时 disconnect 所有 observer，避免泄漏。等价于原 index.tsx 的卸载清理
    // useEffect；固化在 hook 内部后调用方无需自行处理。
    useEffect(() => {
        const observers = observersRef.current;
        return () => {
            for (const observer of observers.values()) {
                observer.disconnect();
            }
            observers.clear();
        };
    }, []);

    return {
        heights,
        getMeasureRef,
        getElement,
    } as const;
}
