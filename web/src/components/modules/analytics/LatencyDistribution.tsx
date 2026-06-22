'use client';

import { useState } from 'react';
import { Timer } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useAnalyticsLatencyDistribution,
    useAnalyticsLatencyModels,
    type AnalyticsRange,
} from '@/api/endpoints/analytics';
import { ObservatorySection, QueryState } from './shared';

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
    const { data: models = [] } = useAnalyticsLatencyModels(range);
    const { data, isLoading, error } = useAnalyticsLatencyDistribution(range, selectedModel || undefined);

    const maxBucketCount = data ? Math.max(...data.buckets.map((b) => b.count), 1) : 1;

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
                )}
            </QueryState>
        </ObservatorySection>
    );
}
