'use client';

import { Radio } from 'lucide-react';
import { useTranslations } from 'next-intl';
import type { OpsTelemetryProviderItem } from '@/api/endpoints/ops';
import { StatusBadge } from '@/components/modules/analytics/shared';
import { formatTelemetryPercent } from '../telemetry-format';
import { SuccessSparkline } from './SuccessSparkline';
import { AvailabilityBar } from './AvailabilityBar';

function providerTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
    if (status === 'healthy') return 'success';
    if (status === 'warning' || status === 'degraded') return 'warning';
    if (status === 'down') return 'danger';
    return 'neutral';
}

export function ProviderChannelCard({ provider }: { provider: OpsTelemetryProviderItem }) {
    const t = useTranslations('ops');
    const tone = providerTone(provider.health_status);
    const statusLabel =
        provider.health_status === 'disabled'
            ? t('system.fields.disabled')
            : t(`health.groupStatuses.${provider.health_status}`);

    return (
        <article className="flex flex-col gap-3 rounded-xl border border-border/50 bg-card p-4 shadow-sm">
            <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                    <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                        <Radio className="size-4" />
                    </div>
                    <div className="min-w-0">
                        <h4 className="truncate text-sm font-semibold">{provider.channel_name}</h4>
                        <p className="mt-0.5 truncate text-[11px] text-muted-foreground" title={provider.base_url}>
                            {provider.health_hint || provider.base_url || '—'}
                        </p>
                    </div>
                </div>
                <StatusBadge label={statusLabel} tone={tone} />
            </div>

            <div className="grid grid-cols-2 gap-2">
                <div className="rounded-lg border border-border/40 bg-emerald-500/5 px-2.5 py-2">
                    <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                        {t('health.portal.latency')}
                    </div>
                    <div className="mt-1 text-base font-semibold tabular-nums">
                        {provider.average_latency_ms.toFixed(0)}
                        <span className="ml-0.5 text-xs font-normal text-muted-foreground">ms</span>
                    </div>
                </div>
                <div className="rounded-lg border border-border/40 bg-primary/5 px-2.5 py-2">
                    <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                        {t('health.portal.successRate')}
                    </div>
                    <div className="mt-1 text-base font-semibold tabular-nums">
                        {formatTelemetryPercent(provider.success_rate)}
                    </div>
                </div>
            </div>

            <div className="flex items-center justify-between border-t border-border/30 pt-2 text-xs text-muted-foreground">
                <span>{t('health.portal.requests')}</span>
                <span className="font-medium tabular-nums text-foreground">
                    {provider.request_count.toLocaleString('en-US')}
                </span>
            </div>
            {provider.sparkline_7d && provider.sparkline_7d.length > 0 ? (
                <div className="flex items-center justify-between gap-2 border-t border-border/20 pt-2">
                    <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                        {t('health.portal.sparkline7d')}
                    </span>
                    <SuccessSparkline values={provider.sparkline_7d} />
                </div>
            ) : null}
            {provider.sparkline_30d && provider.sparkline_30d.length > 0 ? (
                <div className="space-y-1 border-t border-border/20 pt-2">
                    <div className="flex items-center justify-between text-[10px] uppercase tracking-wide text-muted-foreground">
                        <span>{t('health.portal.availability30d') || '30d 可用性'}</span>
                        <span className="tabular-nums">
                            {(provider.sparkline_30d.reduce((a, b) => a + b, 0) / provider.sparkline_30d.length).toFixed(0)}%
                        </span>
                    </div>
                    <AvailabilityBar values={provider.sparkline_30d} />
                </div>
            ) : null}
        </article>
    );
}