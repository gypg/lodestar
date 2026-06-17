'use client';

import { motion } from 'motion/react';
import {
    BarChart3,
    Boxes,
    ChevronDown,
    ChevronUp,
    CircleCheckBig,
    Coins,
    DollarSign,
    GitBranchPlus,
    KeyRound,
    Radio,
    RotateCcw,
    ScanLine,
    SlidersHorizontal,
    Sparkles,
} from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { useAnalyticsOverview } from '@/api/endpoints/analytics';
import { QueryState, formatPercent } from '@/components/modules/analytics/shared';
import { AnimatedNumber } from '@/components/common/AnimatedNumber';
import { EASING } from '@/lib/animations/fluid-transitions';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn, formatCount, formatMoney } from '@/lib/utils';
import { useHomeViewStore, type OverviewMetricKey, type OverviewRange } from './store';

const RANGE_OPTIONS: readonly OverviewRange[] = ['7d', '30d', '90d'];

type OverviewData = NonNullable<ReturnType<typeof useAnalyticsOverview>['data']>;
type MetricIcon = typeof BarChart3;

type MetricDef = {
    key: OverviewMetricKey;
    title: string;
    value: string;
    unit: string;
    icon: MetricIcon;
    accentClassName?: string;
};

export function HomeAnalyticsOverview() {
    const t = useTranslations('home.overview');
    const range = useHomeViewStore((state) => state.overviewRange);
    const setRange = useHomeViewStore((state) => state.setOverviewRange);
    const metricOrder = useHomeViewStore((state) => state.overviewMetricOrder);
    const hiddenMetrics = useHomeViewStore((state) => state.overviewHiddenMetrics);
    const setOverviewMetricHidden = useHomeViewStore((state) => state.setOverviewMetricHidden);
    const moveOverviewMetric = useHomeViewStore((state) => state.moveOverviewMetric);
    const resetOverviewMetrics = useHomeViewStore((state) => state.resetOverviewMetrics);
    const { data, isLoading, error } = useAnalyticsOverview(range);

    // 标题不依赖接口数据，配置面板在数据未加载时也能列出全部指标。
    const titleByKey: Record<OverviewMetricKey, string> = {
        requestCount: t('metrics.requestCount'),
        successRate: t('metrics.successRate'),
        totalTokens: t('metrics.totalTokens'),
        totalCost: t('metrics.totalCost'),
        providerCount: t('metrics.providerCount'),
        apiKeyCount: t('metrics.apiKeyCount'),
        modelCount: t('metrics.modelCount'),
        fallbackRate: t('metrics.fallbackRate'),
    };

    const buildCard = (key: OverviewMetricKey, d: OverviewData): MetricDef => {
        const base = { key, title: titleByKey[key] };
        switch (key) {
            case 'requestCount':
                return {
                    ...base,
                    value: formatCount(d.request_count).formatted.value,
                    unit: formatCount(d.request_count).formatted.unit,
                    icon: BarChart3,
                };
            case 'successRate':
                return {
                    ...base,
                    value: formatPercent(d.success_rate).formatted.value,
                    unit: formatPercent(d.success_rate).formatted.unit,
                    icon: CircleCheckBig,
                    accentClassName: 'bg-emerald-500/10 text-emerald-600',
                };
            case 'totalTokens':
                return {
                    ...base,
                    value: formatCount(d.total_tokens).formatted.value,
                    unit: formatCount(d.total_tokens).formatted.unit,
                    icon: Coins,
                    accentClassName: 'bg-sky-500/10 text-sky-600',
                };
            case 'totalCost':
                return {
                    ...base,
                    value: formatMoney(d.total_cost).formatted.value,
                    unit: formatMoney(d.total_cost).formatted.unit,
                    icon: DollarSign,
                    accentClassName: 'bg-amber-500/10 text-amber-600',
                };
            case 'providerCount':
                return {
                    ...base,
                    value: formatCount(d.provider_count).formatted.value,
                    unit: formatCount(d.provider_count).formatted.unit,
                    icon: Radio,
                };
            case 'apiKeyCount':
                return {
                    ...base,
                    value: formatCount(d.api_key_count).formatted.value,
                    unit: formatCount(d.api_key_count).formatted.unit,
                    icon: KeyRound,
                };
            case 'modelCount':
                return {
                    ...base,
                    value: formatCount(d.model_count).formatted.value,
                    unit: formatCount(d.model_count).formatted.unit,
                    icon: Boxes,
                };
            case 'fallbackRate':
                return {
                    ...base,
                    value: formatPercent(d.fallback_rate).formatted.value,
                    unit: formatPercent(d.fallback_rate).formatted.unit,
                    icon: GitBranchPlus,
                    accentClassName: 'bg-violet-500/10 text-violet-600',
                };
        }
    };

    const hiddenSet = new Set(hiddenMetrics);
    const visibleCount = metricOrder.length - hiddenSet.size;
    const cards: MetricDef[] = data
        ? metricOrder.map((key) => buildCard(key, data)).filter((card) => !hiddenSet.has(card.key))
        : [];

    return (
        <motion.section
            className="relative rounded-xl border border-border bg-card p-5 text-card-foreground md:p-6"
            initial={{ opacity: 0, y: 18 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.45, ease: EASING.easeOutExpo }}
        >
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                <div className="space-y-2">
                    <div className="inline-flex items-center gap-2 rounded-md border border-primary/15 bg-card px-2.5 py-1 text-xs font-medium text-primary">
                        <Sparkles className="h-3.5 w-3.5" strokeWidth={1.5} />
                        <span>{t('badge')}</span>
                    </div>
                    <div>
                        <h2 className="text-xl font-semibold tracking-tight md:text-2xl">{t('title')}</h2>
                        <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{t('description')}</p>
                    </div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                    <Popover>
                        <PopoverTrigger asChild>
                            <button
                                type="button"
                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:border-border/80 hover:text-foreground"
                            >
                                <SlidersHorizontal className="h-3.5 w-3.5" strokeWidth={1.5} />
                                <span>{t('customize')}</span>
                            </button>
                        </PopoverTrigger>
                        <PopoverContent align="end" className="w-72 rounded-lg border border-border bg-popover p-2 text-popover-foreground shadow-md">
                            <div className="flex items-center justify-between px-2 py-1">
                                <span className="text-xs font-semibold">{t('customizeTitle')}</span>
                                <button
                                    type="button"
                                    onClick={resetOverviewMetrics}
                                    className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                >
                                    <RotateCcw className="h-3 w-3" />
                                    {t('customizeReset')}
                                </button>
                            </div>
                            <div className="mt-1 max-h-80 space-y-0.5 overflow-y-auto">
                                {metricOrder.map((key, index) => {
                                    const isHidden = hiddenSet.has(key);
                                    const disableHide = !isHidden && visibleCount <= 1;
                                    return (
                                        <div
                                            key={key}
                                            className="flex items-center gap-1 rounded-md px-1.5 py-0.5 hover:bg-muted/50"
                                        >
                                            <label
                                                className={cn(
                                                    'flex min-w-0 flex-1 items-center gap-2 py-1',
                                                    disableHide ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'
                                                )}
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={!isHidden}
                                                    disabled={disableHide}
                                                    onChange={(event) => setOverviewMetricHidden(key, !event.target.checked)}
                                                    className="h-3.5 w-3.5 rounded border-border accent-primary"
                                                />
                                                <span className="truncate text-xs">{titleByKey[key]}</span>
                                            </label>
                                            <div className="flex shrink-0 items-center">
                                                <button
                                                    type="button"
                                                    onClick={() => moveOverviewMetric(key, 'up')}
                                                    disabled={index === 0}
                                                    className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30"
                                                    aria-label="move up"
                                                >
                                                    <ChevronUp className="h-3.5 w-3.5" />
                                                </button>
                                                <button
                                                    type="button"
                                                    onClick={() => moveOverviewMetric(key, 'down')}
                                                    disabled={index === metricOrder.length - 1}
                                                    className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30"
                                                    aria-label="move down"
                                                >
                                                    <ChevronDown className="h-3.5 w-3.5" />
                                                </button>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </PopoverContent>
                    </Popover>

                    <Tabs value={range} onValueChange={(value) => setRange(value as OverviewRange)}>
                        <TabsList className="w-max rounded-lg border border-border bg-card p-1">
                            {RANGE_OPTIONS.map((option) => (
                                <TabsTrigger key={option} value={option}>
                                    {t(`range.${option}`)}
                                </TabsTrigger>
                            ))}
                        </TabsList>
                    </Tabs>
                </div>
            </div>

            <div className="mt-5">
                <QueryState
                    loading={isLoading}
                    error={error}
                    empty={!data}
                    emptyLabel={t('empty')}
                >
                    <div className="grid grid-cols-2 gap-2.5 sm:gap-3 xl:grid-cols-4">
                        {cards.map((card) => (
                            <article
                                key={card.key}
                                className={cn(
                                    'group rounded-lg border border-border bg-card p-3 transition-[transform,border-color] duration-200 hover:-translate-y-0.5 hover:border-border/80 sm:p-4',
                                )}
                            >
                                <div className="flex items-start justify-between gap-2 sm:gap-3">
                                    <div className="min-w-0">
                                        <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground sm:mb-3 sm:gap-2">
                                            <ScanLine className="h-3.5 w-3.5 shrink-0 text-primary/50" strokeWidth={1.5} />
                                            <span className="truncate">{card.title}</span>
                                        </div>
                                        <div className="flex items-baseline gap-1">
                                            <span className="text-lg font-semibold tracking-tight sm:text-xl md:text-2xl">
                                                <AnimatedNumber value={card.value} />
                                            </span>
                                            {card.unit ? <span className="text-sm text-muted-foreground">{card.unit}</span> : null}
                                        </div>
                                    </div>
                                    <div
                                        className={cn(
                                            'flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary sm:h-10 sm:w-10',
                                            card.accentClassName,
                                        )}
                                    >
                                        <card.icon className="h-[1.125rem] w-[1.125rem]" strokeWidth={1.5} />
                                    </div>
                                </div>
                            </article>
                        ))}
                    </div>
                </QueryState>
            </div>
        </motion.section>
    );
}
