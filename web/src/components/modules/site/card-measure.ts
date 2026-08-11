'use client';

/**
 * 站点卡片高度测量的纯逻辑，供桌面双列 masonry 估算分栏。
 *
 * 抽出来是为了可测：React error #185（Maximum update depth exceeded）曾由此处
 * 两个缺陷引发，而仓库里没有 React 渲染测试环境，只有 node:test 跑纯逻辑。
 * 见 [[site-card-measure-loop]]。
 */

export type SiteCardHeights = Record<number, number>;

/**
 * 计算下一份高度表；返回原对象表示「无变化，不要 setState」。
 *
 * 两条不变量：
 * 1. `display:none` 的节点测出来是 0（移动端与桌面两套 DOM 同时挂载，隐藏那套恒 0）。
 *    0 不是真实布局高度，必须丢弃，否则会与真实高度反复互相覆盖成死循环。
 * 2. 高度没变时必须返回**同一个对象引用**，否则依赖它的 useMemo 每次都失效，
 *    重新分栏 → 重新渲染 → 重新测量。
 */
export function nextSiteCardHeights(
    current: SiteCardHeights,
    siteID: number,
    rawHeight: number,
): SiteCardHeights {
    const nextHeight = Math.round(rawHeight);
    if (!Number.isFinite(nextHeight) || nextHeight <= 0) {
        return current;
    }
    if (current[siteID] === nextHeight) {
        return current;
    }
    return { ...current, [siteID]: nextHeight };
}
