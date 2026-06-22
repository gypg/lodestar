'use client';

import { useMemo, useState } from 'react';
import { LayoutGrid, List, Search, Timer, X, ArrowUpDown, Table as TableIcon } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useAnalyticsLatencyDistribution,
    useAnalyticsLatencyModels,
    useAnalyticsModelLatency,
    type AnalyticsRange,
    type ModelLatencyItem,
} from '@/api/endpoints/analytics';
import { ObservatorySection, QueryState } from './shared';
import { cn } from '@/lib/utils';
import { getModelIcon } from '@/lib/model-icons';

type SortOrder = 'asc' | 'desc';
type SortField = 'avg_ms' | 'model_name' | 'total_requests';
type ViewMode = 'table' | 'card' | 'list';

function LatencyMetricCard({ label, value, unit }: { label: string; value: number; unit: string }) {
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

function LatencyBar({ value, max }: { value: number; max: number }) {
    const percentage = max > 0 ? (value / max) * 100 : 0;
    return (
        <div className="h-2 w-full rounded-full bg-muted">
            <div
                className="h-full rounded-full bg-primary transition-all"
                style={{ width: `${percentage}%` }}
            />
        </div>
    );
}

function ModelIcon({ modelName }: { modelName: string }) {
    const { Avatar, color, label } = getModelIcon(modelName);
    return (
        <div className="flex items-center gap-2">
            <Avatar size={18} style={{ color }} />
            <span className="text-xs text-muted-foreground">{label}</span>
        </div>
    );
}

function SortableHeader({
    label,
    field,
    currentField,
    currentOrder,
    onSort,
}: {
    label: string;
    field: SortField;
    currentField: SortField;
    currentOrder: SortOrder;
    onSort: (field: SortField) => void;
}) {
    const isActive = currentField === field;
    return (
        <button
            type="button"
            onClick={() => onSort(field)}
            className="inline-flex items-center gap-1 text-left"
        >
            {label}
            <ArrowUpDown
                className={cn(
                    'h-3 w-3',
                    isActive ? 'text-primary' : 'text-muted-foreground/50'
                )}
            />
        </button>
    );
}

function ModelTableView({
    items,
    maxAvg,
    t,
    sortField,
    sortOrder,
    onSort,
}: {
    items: ModelLatencyItem[];
    maxAvg: number;
    t: ReturnType<typeof useTranslations>;
    sortField: SortField;
    sortOrder: SortOrder;
    onSort: (field: SortField) => void;
}) {
    return (
        <div className="max-h-80 overflow-y-auto rounded-lg border border-border/30">
            <table className="w-full text-sm">
                <thead className="sticky top-0 z-10">
                    <tr className="border-b border-border/30 bg-muted/30">
                        <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                            <SortableHeader
                                label={t('latency.sortByName')}
                                field="model_name"
                                currentField={sortField}
                                currentOrder={sortOrder}
                                onSort={onSort}
                            />
                        </th>
                        <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground">
                            <SortableHeader
                                label={t('latency.sortByRequests')}
                                field="total_requests"
                                currentField={sortField}
                                currentOrder={sortOrder}
                                onSort={onSort}
                            />
                        </th>
                        <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground">
                            <SortableHeader
                                label={t('latency.sortByAvg')}
                                field="avg_ms"
                                currentField={sortField}
                                currentOrder={sortOrder}
                                onSort={onSort}
                            />
                        </th>
                        <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground">P50</th>
                        <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground">P95</th>
                        <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground">P99</th>
                    </tr>
                </thead>
                <tbody>
                    {items.map((item) => (
                        <tr
                            key={item.model_name}
                            className="border-b border-border/20 transition-colors last:border-b-0 hover:bg-muted/30"
                        >
                            <td className="px-3 py-2">
                                <div className="flex items-center gap-2">
                                    <ModelIcon modelName={item.model_name} />
                                    <span className="truncate font-medium">{item.model_name}</span>
                                </div>
                            </td>
                            <td className="px-3 py-2 text-right tabular-nums">{item.total_requests.toLocaleString()}</td>
                            <td className="px-3 py-2 text-right tabular-nums">
                                <div className="flex items-center justify-end gap-2">
                                    <LatencyBar value={item.avg_ms} max={maxAvg} />
                                    <span className="w-16 text-right">{item.avg_ms.toFixed(1)}</span>
                                </div>
                            </td>
                            <td className="px-3 py-2 text-right tabular-nums">{item.p50_ms.toFixed(1)}</td>
                            <td className="px-3 py-2 text-right tabular-nums">{item.p95_ms.toFixed(1)}</td>
                            <td className="px-3 py-2 text-right tabular-nums">{item.p99_ms.toFixed(1)}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function ModelCardView({ items, maxAvg }: { items: ModelLatencyItem[]; maxAvg: number }) {
    return (
        <div className="max-h-80 overflow-y-auto rounded-lg border border-border/30 p-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {items.map((item) => (
                    <div
                        key={item.model_name}
                        className="rounded-lg border border-border/30 bg-card p-3 transition-colors hover:border-primary/20"
                    >
                        <div className="mb-2 flex items-center gap-2">
                            <ModelIcon modelName={item.model_name} />
                            <span className="truncate text-sm font-medium">{item.model_name}</span>
                        </div>
                        <div className="grid grid-cols-2 gap-2 text-xs">
                            <div>
                                <div className="text-muted-foreground">Requests</div>
                                <div className="font-semibold tabular-nums">{item.total_requests.toLocaleString()}</div>
                            </div>
                            <div>
                                <div className="text-muted-foreground">Avg</div>
                                <div className="font-semibold tabular-nums">{item.avg_ms.toFixed(1)} ms</div>
                            </div>
                            <div>
                                <div className="text-muted-foreground">P50</div>
                                <div className="font-semibold tabular-nums">{item.p50_ms.toFixed(1)} ms</div>
                            </div>
                            <div>
                                <div className="text-muted-foreground">P95</div>
                                <div className="font-semibold tabular-nums">{item.p95_ms.toFixed(1)} ms</div>
                            </div>
                        </div>
                        <div className="mt-2">
                            <LatencyBar value={item.avg_ms} max={maxAvg} />
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

function ModelListView({ items, maxAvg }: { items: ModelLatencyItem[]; maxAvg: number }) {
    return (
        <div className="max-h-80 space-y-1 overflow-y-auto rounded-lg border border-border/30 bg-muted/10 p-2">
            {items.map((item) => (
                <div
                    key={item.model_name}
                    className="flex items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-muted/50"
                >
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <ModelIcon modelName={item.model_name} />
                            <span className="truncate text-sm font-medium">{item.model_name}</span>
                        </div>
                    </div>
                    <div className="flex w-32 items-center gap-2">
                        <div className="flex-1">
                            <LatencyBar value={item.avg_ms} max={maxAvg} />
                        </div>
                        <span className="w-16 text-right text-xs tabular-nums">{item.avg_ms.toFixed(1)} ms</span>
                    </div>
                </div>
            ))}
        </div>
    );
}

export function LatencyDistribution({ range }: { range: AnalyticsRange }) {
    const t = useTranslations('analytics');
    const [selectedModel, setSelectedModel] = useState<string>('');
    const [searchQuery, setSearchQuery] = useState('');
    const [sortOrder, setSortOrder] = useState<SortOrder>('asc');
    const [sortField, setSortField] = useState<SortField>('avg_ms');
    const [viewMode, setViewMode] = useState<ViewMode>('list');
    const { data: models = [] } = useAnalyticsLatencyModels(range);
    const { data, isLoading, error } = useAnalyticsLatencyDistribution(range, selectedModel || undefined);
    const { data: modelLatencyData, isLoading: isModelLatencyLoading } = useAnalyticsModelLatency(range);

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

    const handleSort = (field: SortField) => {
        if (field === sortField) {
            setSortOrder((prev) => (prev === 'asc' ? 'desc' : 'asc'));
        } else {
            setSortField(field);
            setSortOrder(field === 'model_name' ? 'asc' : 'desc');
        }
    };

    const filteredModelLatencyItems = useMemo(() => {
        if (!modelLatencyData) return [];
        const query = searchQuery.toLowerCase();
        const filtered = query
            ? modelLatencyData.filter((item) => item.model_name.toLowerCase().includes(query))
            : modelLatencyData;
        return [...filtered].sort((a, b) => {
            const multiplier = sortOrder === 'asc' ? 1 : -1;
            if (sortField === 'model_name') {
                return multiplier * a.model_name.localeCompare(b.model_name);
            }
            return multiplier * (a[sortField] - b[sortField]);
        });
    }, [modelLatencyData, searchQuery, sortField, sortOrder]);

    const maxAvgMs = useMemo(() => {
        if (filteredModelLatencyItems.length === 0) return 1;
        return Math.max(...filteredModelLatencyItems.map((item) => item.avg_ms), 1);
    }, [filteredModelLatencyItems]);

    const hasModelLatencyData = modelLatencyData !== undefined && modelLatencyData.length > 0;

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
                        {/* Top section: Overall stats + histogram (preserved) */}
                        <div className="grid gap-4 md:grid-cols-2">
                            <div className="space-y-4">
                                <div>
                                    <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.useTime')}</h4>
                                    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                        <LatencyMetricCard label={t('latency.avg')} value={data.avg_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p50')} value={data.p50_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p95')} value={data.p95_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p99')} value={data.p99_ms} unit="ms" />
                                    </div>
                                </div>

                                <div>
                                    <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.ftut')}</h4>
                                    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                        <LatencyMetricCard label={t('latency.avg')} value={data.ftut_avg_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p50')} value={data.ftut_p50_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p95')} value={data.ftut_p95_ms} unit="ms" />
                                        <LatencyMetricCard label={t('latency.p99')} value={data.ftut_p99_ms} unit="ms" />
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

                        {/* Bottom section: Per-model latency data */}
                        <div className="space-y-3">
                            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                                <h4 className="text-sm font-medium text-muted-foreground">{t('latency.modelLatency')}</h4>
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
                                        value={`${sortField}:${sortOrder}`}
                                        onChange={(e) => {
                                            const [field, order] = e.target.value.split(':') as [SortField, SortOrder];
                                            setSortField(field);
                                            setSortOrder(order);
                                        }}
                                        className="h-7 rounded-md border border-border/50 bg-background px-2 text-xs outline-none focus:border-primary/30"
                                    >
                                        <option value="avg_ms:asc">{t('latency.sortByAvg')} ↑</option>
                                        <option value="avg_ms:desc">{t('latency.sortByAvg')} ↓</option>
                                        <option value="model_name:asc">{t('latency.sortByName')} A-Z</option>
                                        <option value="model_name:desc">{t('latency.sortByName')} Z-A</option>
                                        <option value="total_requests:desc">{t('latency.sortByRequests')} ↓</option>
                                        <option value="total_requests:asc">{t('latency.sortByRequests')} ↑</option>
                                    </select>

                                    {/* View toggle */}
                                    <div className="flex items-center rounded-md border border-border/50">
                                        <button
                                            type="button"
                                            onClick={() => setViewMode('table')}
                                            className={cn(
                                                'flex h-7 w-7 items-center justify-center rounded-l-md text-muted-foreground transition-colors',
                                                viewMode === 'table' && 'bg-primary/10 text-primary',
                                            )}
                                            title={t('latency.viewTable')}
                                        >
                                            <TableIcon className="h-3.5 w-3.5" />
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setViewMode('card')}
                                            className={cn(
                                                'flex h-7 w-7 items-center justify-center border-x border-border/50 text-muted-foreground transition-colors',
                                                viewMode === 'card' && 'bg-primary/10 text-primary',
                                            )}
                                            title={t('latency.viewCard')}
                                        >
                                            <LayoutGrid className="h-3.5 w-3.5" />
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setViewMode('list')}
                                            className={cn(
                                                'flex h-7 w-7 items-center justify-center rounded-r-md text-muted-foreground transition-colors',
                                                viewMode === 'list' && 'bg-primary/10 text-primary',
                                            )}
                                            title={t('latency.viewList')}
                                        >
                                            <List className="h-3.5 w-3.5" />
                                        </button>
                                    </div>
                                </div>
                            </div>

                            {/* Model latency content */}
                            {isModelLatencyLoading && !modelLatencyData ? (
                                <div className="flex min-h-24 items-center justify-center rounded-lg border border-border/30 bg-muted/10">
                                    <span className="text-xs text-muted-foreground">{t('states.loading')}</span>
                                </div>
                            ) : !hasModelLatencyData ? (
                                <div className="flex min-h-24 items-center justify-center rounded-lg border border-dashed border-border/30 bg-card">
                                    <span className="text-xs text-muted-foreground">{t('states.empty')}</span>
                                </div>
                            ) : filteredModelLatencyItems.length === 0 ? (
                                <div className="flex min-h-24 items-center justify-center rounded-lg border border-border/30 bg-muted/10">
                                    <span className="text-xs text-muted-foreground">{t('states.empty')}</span>
                                </div>
                            ) : (
                                <>
                                    {viewMode === 'table' && (
                                        <ModelTableView
                                            items={filteredModelLatencyItems}
                                            maxAvg={maxAvgMs}
                                            t={t}
                                            sortField={sortField}
                                            sortOrder={sortOrder}
                                            onSort={handleSort}
                                        />
                                    )}
                                    {viewMode === 'card' && (
                                        <ModelCardView items={filteredModelLatencyItems} maxAvg={maxAvgMs} />
                                    )}
                                    {viewMode === 'list' && (
                                        <ModelListView items={filteredModelLatencyItems} maxAvg={maxAvgMs} />
                                    )}
                                </>
                            )}
                        </div>

                        {/* Model Browser (preserved from original) */}
                        {models.length > 0 && (
                            <div className="space-y-3">
                                <h4 className="text-sm font-medium text-muted-foreground">{t('latency.modelBrowser')}</h4>
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
                            </div>
                        )}
                    </div>
                )}
            </QueryState>
        </ObservatorySection>
    );
}
