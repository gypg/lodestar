'use client';

import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

/** Mini 7-day success-rate bars (0–100), SAPI-inspired. */
export function SuccessSparkline({
    values,
    className,
    title,
}: {
    values?: number[];
    className?: string;
    title?: string;
}) {
    const t = useTranslations('ops.health.portal');
    const resolvedTitle = title ?? t('sparkline7d');
    const pts = values?.length ? values : [];
    if (pts.length === 0) return null;
    const max = 100;
    return (
        <div className={cn('flex items-end gap-0.5', className)} role="img" aria-label={resolvedTitle}>
            {pts.map((v, i) => {
                const h = Math.max(2, Math.round((Math.min(max, Math.max(0, v)) / max) * 100));
                const tone =
                    v >= 95 ? 'bg-emerald-500/70' : v >= 80 ? 'bg-emerald-500/40' : v > 0 ? 'bg-amber-500/50' : 'bg-muted/50';
                return (
                    <div
                        key={i}
                        title={`${v.toFixed(0)}%`}
                        className={cn('w-1.5 rounded-sm sm:w-2', tone)}
                        style={{ height: `${Math.max(8, h * 0.22)}px` }}
                    />
                );
            })}
        </div>
    );
}