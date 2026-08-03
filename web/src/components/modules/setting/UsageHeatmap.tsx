'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

export interface HeatmapDay {
    day: string;
    requests: number;
    tokens?: number;
}

const DAY_MS = 86400000;

function toKey(d: Date) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
}

function level(requests: number, max: number) {
    if (!requests) return 0;
    if (max <= 1) return 1;
    return Math.min(4, Math.max(1, Math.ceil((requests / max) * 4)));
}

const LEVEL_CLASS = [
    'bg-muted/40',
    'bg-emerald-500/20',
    'bg-emerald-500/40',
    'bg-emerald-500/60',
    'bg-emerald-500',
];

/** Compact GitHub-style usage heatmap (last N days from API). */
export function UsageHeatmap({ data, days = 30, className }: { data?: HeatmapDay[]; days?: number; className?: string }) {
    const t = useTranslations('setting.usageHeatmap');
    const { cells, max } = useMemo(() => {
        const map = new Map<string, number>();
        for (const row of data ?? []) {
            if (row?.day) map.set(row.day, Number(row.requests) || 0);
        }
        const today = new Date();
        today.setHours(0, 0, 0, 0);
        const start = new Date(today.getTime() - (days - 1) * DAY_MS);
        const gridStart = new Date(start);
        gridStart.setDate(gridStart.getDate() - gridStart.getDay());
        const items: { key: string; requests: number; inRange: boolean }[] = [];
        let maxR = 0;
        const totalDays = Math.ceil((today.getTime() - gridStart.getTime()) / DAY_MS) + 1;
        const weeks = Math.ceil(totalDays / 7);
        for (let i = 0; i < weeks * 7; i++) {
            const d = new Date(gridStart.getTime() + i * DAY_MS);
            const key = toKey(d);
            const inRange = d >= start && d <= today;
            const requests = inRange ? map.get(key) ?? 0 : 0;
            if (inRange) maxR = Math.max(maxR, requests);
            items.push({ key, requests, inRange });
        }
        return { cells: items, max: maxR };
    }, [data, days]);

    if (data === undefined) {
        return null;
    }

    if (!data.length && max === 0) {
        return <p className="text-xs text-muted-foreground">{t('noDailyData')}</p>;
    }

    return (
        <div className={cn('flex flex-wrap gap-0.5', className)} role="img" aria-label={t('ariaLabel')}>
            {cells.map((c) =>
                c.inRange ? (
                    <div
                        key={c.key}
                        title={t('tooltip', { date: c.key, count: c.requests })}
                        className={cn('size-2.5 rounded-sm sm:size-3', LEVEL_CLASS[level(c.requests, max)])}
                    />
                ) : (
                    <div key={c.key} className="size-2.5 sm:size-3" aria-hidden />
                ),
            )}
        </div>
    );
}