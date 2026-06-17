'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type RankSortMode = 'cost' | 'count' | 'tokens' | 'key-usage';
export type ChartMetricType = 'cost' | 'count' | 'tokens' | 'success-rate';
export type ChartPeriod = '1' | '7' | '30';
export type OverviewRange = '7d' | '30d' | '90d';

const RANK_SORT_MODES: readonly RankSortMode[] = ['cost', 'count', 'tokens', 'key-usage'];
const CHART_METRIC_TYPES: readonly ChartMetricType[] = ['cost', 'count', 'tokens', 'success-rate'];
const CHART_PERIODS: readonly ChartPeriod[] = ['1', '7', '30'];
const OVERVIEW_RANGES: readonly OverviewRange[] = ['7d', '30d', '90d'];

function normalizeRankSortMode(value: string | null | undefined): RankSortMode {
    return RANK_SORT_MODES.includes(value as RankSortMode) ? (value as RankSortMode) : 'cost';
}

function normalizeChartMetricType(value: string | null | undefined): ChartMetricType {
    return CHART_METRIC_TYPES.includes(value as ChartMetricType) ? (value as ChartMetricType) : 'cost';
}

function normalizeChartPeriod(value: string | null | undefined): ChartPeriod {
    return CHART_PERIODS.includes(value as ChartPeriod) ? (value as ChartPeriod) : '1';
}

export function normalizeOverviewRange(value: string | null | undefined): OverviewRange {
    return OVERVIEW_RANGES.includes(value as OverviewRange) ? (value as OverviewRange) : '7d';
}

// 首页 analytics 概览的 8 个指标卡，用户可显隐与排序。
export type OverviewMetricKey =
    | 'requestCount'
    | 'successRate'
    | 'totalTokens'
    | 'totalCost'
    | 'providerCount'
    | 'apiKeyCount'
    | 'modelCount'
    | 'fallbackRate';

export const OVERVIEW_METRIC_KEYS: readonly OverviewMetricKey[] = [
    'requestCount',
    'successRate',
    'totalTokens',
    'totalCost',
    'providerCount',
    'apiKeyCount',
    'modelCount',
    'fallbackRate',
];

function normalizeOverviewMetricOrder(value: unknown): OverviewMetricKey[] {
    const seen = new Set<OverviewMetricKey>();
    const result: OverviewMetricKey[] = [];
    if (Array.isArray(value)) {
        for (const item of value) {
            const key = item as OverviewMetricKey;
            if (
                typeof item === 'string' &&
                (OVERVIEW_METRIC_KEYS as readonly string[]).includes(item) &&
                !seen.has(key)
            ) {
                seen.add(key);
                result.push(key);
            }
        }
    }
    // 补全缺失或新增的指标，保证顺序数组始终覆盖全集。
    for (const key of OVERVIEW_METRIC_KEYS) {
        if (!seen.has(key)) result.push(key);
    }
    return result;
}

function normalizeOverviewHiddenMetrics(value: unknown): OverviewMetricKey[] {
    if (!Array.isArray(value)) return [];
    const result: OverviewMetricKey[] = [];
    for (const item of value) {
        const key = item as OverviewMetricKey;
        if (
            typeof item === 'string' &&
            (OVERVIEW_METRIC_KEYS as readonly string[]).includes(item) &&
            !result.includes(key)
        ) {
            result.push(key);
        }
    }
    return result;
}

interface HomeViewState {
    rankSortMode: RankSortMode;
    chartMetricType: ChartMetricType;
    chartPeriod: ChartPeriod;
    overviewRange: OverviewRange;
    overviewMetricOrder: OverviewMetricKey[];
    overviewHiddenMetrics: OverviewMetricKey[];
    setRankSortMode: (value: RankSortMode) => void;
    setChartMetricType: (value: ChartMetricType) => void;
    setChartPeriod: (value: ChartPeriod) => void;
    setOverviewRange: (value: OverviewRange) => void;
    setOverviewMetricHidden: (key: OverviewMetricKey, hidden: boolean) => void;
    moveOverviewMetric: (key: OverviewMetricKey, direction: 'up' | 'down') => void;
    resetOverviewMetrics: () => void;
}

export const useHomeViewStore = create<HomeViewState>()(
    persist(
        (set) => ({
            rankSortMode: 'cost',
            chartMetricType: 'cost',
            chartPeriod: '1',
            overviewRange: '7d',
            overviewMetricOrder: [...OVERVIEW_METRIC_KEYS],
            overviewHiddenMetrics: [],
            setRankSortMode: (value) => set({ rankSortMode: normalizeRankSortMode(value) }),
            setChartMetricType: (value) => set({ chartMetricType: normalizeChartMetricType(value) }),
            setChartPeriod: (value) => set({ chartPeriod: normalizeChartPeriod(value) }),
            setOverviewRange: (value) => set({ overviewRange: normalizeOverviewRange(value) }),
            setOverviewMetricHidden: (key, hidden) => set((state) => {
                const hiddenNow = state.overviewHiddenMetrics;
                const alreadyHidden = hiddenNow.includes(key);
                if (hidden) {
                    if (alreadyHidden) return {};
                    // 至少保留一个可见指标，避免概览区被清空。
                    const visibleCount = state.overviewMetricOrder.length - hiddenNow.length;
                    if (visibleCount <= 1) return {};
                    return { overviewHiddenMetrics: [...hiddenNow, key] };
                }
                if (!alreadyHidden) return {};
                return { overviewHiddenMetrics: hiddenNow.filter((k) => k !== key) };
            }),
            moveOverviewMetric: (key, direction) => set((state) => {
                const order = [...state.overviewMetricOrder];
                const idx = order.indexOf(key);
                if (idx < 0) return {};
                const target = direction === 'up' ? idx - 1 : idx + 1;
                if (target < 0 || target >= order.length) return {};
                [order[idx], order[target]] = [order[target], order[idx]];
                return { overviewMetricOrder: order };
            }),
            resetOverviewMetrics: () =>
                set({ overviewMetricOrder: [...OVERVIEW_METRIC_KEYS], overviewHiddenMetrics: [] }),
        }),
        {
            name: 'home-view-options-storage-v2',
            storage: createJSONStorage(() => localStorage),
            partialize: (state) => ({
                rankSortMode: state.rankSortMode,
                chartMetricType: state.chartMetricType,
                chartPeriod: state.chartPeriod,
                overviewRange: state.overviewRange,
                overviewMetricOrder: state.overviewMetricOrder,
                overviewHiddenMetrics: state.overviewHiddenMetrics,
            }),
            merge: (persistedState, currentState) => {
                const typed = (persistedState as Partial<HomeViewState> | null) ?? null;
                return {
                    ...currentState,
                    ...typed,
                    rankSortMode: normalizeRankSortMode(typed?.rankSortMode),
                    chartMetricType: normalizeChartMetricType(typed?.chartMetricType),
                    chartPeriod: normalizeChartPeriod(typed?.chartPeriod),
                    overviewRange: normalizeOverviewRange(typed?.overviewRange),
                    overviewMetricOrder: normalizeOverviewMetricOrder(typed?.overviewMetricOrder),
                    overviewHiddenMetrics: normalizeOverviewHiddenMetrics(typed?.overviewHiddenMetrics),
                };
            },
        }
    )
);
