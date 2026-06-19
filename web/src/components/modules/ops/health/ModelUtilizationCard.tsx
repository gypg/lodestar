'use client';

import { Boxes } from 'lucide-react';
import { useTranslations } from 'next-intl';
import type { AnalyticsModelBreakdownItem } from '@/api/endpoints/analytics';
import { formatPercent } from '@/components/modules/analytics/shared';
import { formatCount, formatMoney, cn } from '@/lib/utils';

function successRateClass(rate: number) {
    if (rate < 50) return 'text-destructive';
    if (rate < 90) return 'text-amber-600 dark:text-amber-400';
    if (rate >= 99.99) return 'text-emerald-600 dark:text-emerald-400';
    return 'text-foreground';
}

export function ModelUtilizationCard({ item }: { item: AnalyticsModelBreakdownItem }) {
    const t = useTranslations('ops');

    return (
        <article className="rounded-xl border border-border/50 bg-card p-3 shadow-sm">
            <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                    <div className="grid size-8 shrink-0 place-items-center rounded-md bg-muted/60 text-muted-foreground">
                        <Boxes className="size-3.5" />
                    </div>
                    <div className="min-w-0">
                        <h4
                            className="truncate font-mono text-xs font-semibold"
                            title={item.model_name}
                        >
                            {item.model_name}
                        </h4>
                        <p className="mt-0.5 text-[10px] text-muted-foreground">
                            {formatCount(item.request_count).formatted.value}
                            {formatCount(item.request_count).formatted.unit} {t('health.portal.requests')}
                        </p>
                    </div>
                </div>
                <span className={cn('shrink-0 text-sm font-semibold tabular-nums', successRateClass(item.success_rate))}>
                    {formatPercent(item.success_rate).formatted.value}%
                </span>
            </div>
            <div className="mt-2 flex justify-between text-[10px] text-muted-foreground">
                <span>
                    {formatCount(item.total_tokens).formatted.value}
                    {formatCount(item.total_tokens).formatted.unit} tokens
                </span>
                <span className="tabular-nums text-foreground">
                    {formatMoney(item.total_cost).formatted.value}
                    {formatMoney(item.total_cost).formatted.unit}
                </span>
            </div>
        </article>
    );
}