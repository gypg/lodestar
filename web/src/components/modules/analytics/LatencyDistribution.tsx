'use client';

import { useMemo, useState } from 'react';
import { LayoutGrid, List, Search, Timer, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useAnalyticsLatencyDistribution,
    useAnalyticsLatencyModels,
    type AnalyticsRange,
} from '@/api/endpoints/analytics';
import { ObservatorySection, QueryState } from './shared';
import { cn } from '@/lib/utils';

type SortOrder = 'asc' | 'desc';
type ViewMode = 'table' | 'list';

function MetricCard({ label, value, unit }: { label: string; value: number; unit: string }) {
    return (
        <div className="flex flex-col items-center justify-center rounded-lg border border-border/30 bg-muted/30 p-3">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="text-lg font-bold tabular-nums">
                {value}
                <span className="ml-1 text-xs text-muted-foreground">{unit}</span>
            </div>
        </div>
    );
}

export function LatencyDistribution({ range }: { range: AnalyticsRange }) {
    const t = useTranslations('analytics');
    const [selectedModel, setSelectedModel] = useState<string>('');
    const [searchQuery, setSearchQuery] = useState('');
    const [sortOrder, setSortOrder] = useState<SortOrder>('asc');
    const [viewMode, setViewMode] = useState<ViewMode>('list');
    const { data: models = [] } = useAnalyticsLatencyModels(range);
    const { data, isLoading, error } = useAnalyticsLatencyDistribution(range, selectedModel || undefined);

    const maxBucketCount = data ? Math.max(...data.buckets.map((b) => b.count), 1) : 1;

    const filteredModels = useMemo(() => {
        const query = searchQuery.toLowerCase();
        const filtered = query
            ? models.filter((m) => m.toLowerCase().includes(query))
            : models;
        const sorted = [...filtered].sort((a, b) =>
            sortOrder === 'asc' ? a.localeCompare(b) : b.localeCompare(a),
        );
        return sorted;
    }, [models, searchQuery, sortOrder]);

    return (
        <ObservatorySection
            eyebrow={t('latency.title')}
            title={t('latency.title')}
            icon={Timer}
            actions={
                models.length > 0 ? (
                    <div className="flex items-center gap-2">
                        <select
                            value={selectedModel}
                            onChange={(e) => setSelectedModel(e.target.value)}
                            className="h-7 rounded-md border border-border/50 bg-background px-2 text-xs outline-none focus:border-primary/30"
                        >
                            <option value="">{t('latency.allModels')}</option>
                            {models.map((m) => (
                                <option key={m} value={m}>
                                    {m}
                                </option>
                            ))}
                        </select>
                    </div>
                ) : undefined
            }
        >
            <QueryState
                loading={isLoading}
                error={error}
                empty={!data}
                emptyLabel={isLoading ? t('states.loading') : t('states.empty')}
            >
                {data && (
                    <div className="space-y-6">
                        <div className="grid gap-4 md:grid-cols-2">
                            <div className="space-y-4">
                                <div>
                                    <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.useTime')}</h4>
                                    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                        <MetricCard label={t('latency.avg')} value={data.avg_ms} unit="ms" />
                                        <MetricCard label={t('latency.p50')} value={data.p50_ms} unit="ms" />
                                        <MetricCard label={t('latency.p95')} value={data.p95_ms} unit="ms" />
                                        <MetricCard label={t('latency.p99')} value={data.p99_ms} unit="ms" />
                                    </div>
                                </div>

                                <div>
                                    <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.ftut')}</h4>
                                    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                        <MetricCard label={t('latency.avg')} value={data.ftut_avg_ms} unit="ms" />
                                        <MetricCard label={t('latency.p50')} value={data.ftut_p50_ms} unit="ms" />
                                        <MetricCard label={t('latency.p95')} value={data.ftut_p95_ms} unit="ms" />
                                        <MetricCard label={t('latency.p99')} value={data.ftut_p99_ms} unit="ms" />
                                    </div>
                                </div>

                                <div className="text-xs text-muted-foreground">
                                    {t('latency.totalRequests', { count: data.total_requests })}
                                </div>
                            </div>

                            <div>
                                <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.histogram')}</h4>
                                <div className="space-y-2">
                                    {data.buckets.map((bucket) => {
                                        const percentage = (bucket.count / maxBucketCount) * 100;
                                        return (
                                            <div key={bucket.label} className="flex items-center gap-3">
                                                <div className="w-20 text-xs text-muted-foreground">{bucket.label}</div>
                                                <div className="flex-1">
                                                    <div className="h-6 rounded bg-muted">
                                                        <div
                                                            className="h-full rounded bg-primary transition-all"
                                                            style={{ width: `${percentage}%` }}
                                                        />
                                                    </div>
                                                </div>
                                                <div className="w-16 text-right text-sm tabular-nums">{bucket.count}</div>
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>
                        </div>

                        {/* Model Browser */}
                        {models.length > 0 && (
                            <div className="space-y-3">
                                <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                                    <h4 className="text-sm font-medium text-muted-foreground">{t('latency.modelBrowser')}</h4>
                                    <div className="flex items-center gap-2">
                                        {/* Search */}
                                        <div className="relative">
                                            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                                            <input
                                                type="text"
                                                value={searchQuery}
                                                onChange={(e) => setSearchQuery(e.target.value)}
                                                placeholder={t('latency.searchModels')}
                                                className="h-7 w-36 rounded-md border border-border/50 bg-background py-1 pl-7 pr-7 text-xs outline-none focus:border-primary/30 sm:w-44"
                                            />
                                            {searchQuery && (
                                                <button
                                                    type="button"
                                                    onClick={() => setSearchQuery('')}
                                                    className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
                                                >
                                                    <X className="h-3 w-3" />
                                                </button>
                                            )}
                                        </div>

                                        {/* Sort */}
                                        <select
                                            value={sortOrder}
                                            onChange={(e) => setSortOrder(e.target.value as SortOrder)}
                                            className="h-7 rounded-md border border-border/50 bg-background px-2 text-xs outline-none focus:border-primary/30"
                                        >
                                            <option value="asc">{t('latency.sortByName')} A-Z</option>
                                            <option value="desc">{t('latency.sortByName')} Z-A</option>
                                        </select>

                                        {/* View toggle */}
                                        <div className="flex items-center rounded-md border border-border/50">
                                            <button
                                                type="button"
                                                onClick={() => setViewMode('list')}
                                                className={cn(
                                                    'flex h-7 w-7 items-center justify-center rounded-l-md text-muted-foreground transition-colors',
                                                    viewMode === 'list' && 'bg-primary/10 text-primary',
                                                )}
                                                title={t('latency.viewList')}
                                            >
                                                <List className="h-3.5 w-3.5" />
                                            </button>
                                            <button
                                                type="button"
                                                onClick={() => setViewMode('table')}
                                                className={cn(
                                                    'flex h-7 w-7 items-center justify-center rounded-r-md text-muted-foreground transition-colors',
                                                    viewMode === 'table' && 'bg-primary/10 text-primary',
                                                )}
                                                title={t('latency.viewTable')}
                                            >
                                                <LayoutGrid className="h-3.5 w-3.5" />
                                            </button>
                                        </div>
                                    </div>
                                </div>

                                {/* Model list - list view */}
                                {viewMode === 'list' && (
                                    <div className="max-h-60 space-y-1 overflow-y-auto rounded-lg border border-border/30 bg-muted/10 p-2">
                                        {filteredModels.length === 0 ? (
                                            <div className="py-4 text-center text-xs text-muted-foreground">
                                                {t('states.empty')}
                                            </div>
                                        ) : (
                                            filteredModels.map((model) => (
                                                <button
                                                    key={model}
                                                    type="button"
                                                    onClick={() => setSelectedModel(model === selectedModel ? '' : model)}
                                                    className={cn(
                                                        'flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors',
                                                        model === selectedModel
                                                            ? 'bg-primary/10 text-primary font-medium'
                                                            : 'text-foreground hover:bg-muted/50',
                                                    )}
                                                >
                                                    {model === selectedModel && (
                                                        <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />
                                                    )}
                                                    <span className="truncate">{model}</span>
                                                </button>
                                            ))
                                        )}
                                    </div>
                                )}

                                {/* Model list - table view */}
                                {viewMode === 'table' && (
                                    <div className="max-h-60 overflow-y-auto rounded-lg border border-border/30">
                                        <table className="w-full text-sm">
                                            <thead>
                                                <tr className="border-b border-border/30 bg-muted/30">
                                                    <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                                                        {t('latency.sortByName')}
                                                    </th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {filteredModels.length === 0 ? (
                                                    <tr>
                                                        <td className="px-3 py-4 text-center text-xs text-muted-foreground">
                                                            {t('states.empty')}
                                                        </td>
                                                    </tr>
                                                ) : (
                                                    filteredModels.map((model) => (
                                                        <tr
                                                            key={model}
                                                            onClick={() => setSelectedModel(model === selectedModel ? '' : model)}
                                                            className={cn(
                                                                'cursor-pointer border-b border-border/20 transition-colors last:border-b-0',
                                                                model === selectedModel
                                                                    ? 'bg-primary/10'
                                                                    : 'hover:bg-muted/30',
                                                            )}
                                                        >
                                                            <td className="flex items-center gap-2 px-3 py-2">
                                                                {model === selectedModel && (
                                                                    <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />
                                                                )}
                                                                <span
                                                                    className={cn(
                                                                        'truncate',
                                                                        model === selectedModel && 'font-medium text-primary',
                                                                    )}
                                                                >
                                                                    {model}
                                                                </span>
                                                            </td>
                                                        </tr>
                                                    ))
                                                )}
                                            </tbody>
                                        </table>
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                )}
            </QueryState>
        </ObservatorySection>
    );
}
