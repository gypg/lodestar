'use client';

import { useMemo } from 'react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import type { UsageDailyPoint } from '@/api/endpoints/wallet';

const chartConfig = {
    requests: { label: '请求', color: 'hsl(var(--primary))' },
};

function fmtDay(d: string) {
    if (/^\d{8}$/.test(d)) return `${d.slice(4, 6)}/${d.slice(6, 8)}`;
    return d;
}

export function WalletUsageChart({
    series,
    available,
}: {
    series?: UsageDailyPoint[];
    available?: boolean;
}) {
    const data = useMemo(
        () =>
            (series ?? []).map((p) => ({
                day: fmtDay(p.date),
                requests: p.requests,
            })),
        [series],
    );
    const hasData = data.some((d) => d.requests > 0);

    if (!available) {
        return (
            <p className="text-xs text-muted-foreground">
                按日曲线需在「系统 → 日志」中开启<strong>保留历史日志</strong>后才有数据。
            </p>
        );
    }
    if (!hasData) {
        return (
            <p className="text-xs text-muted-foreground">近 14 日暂无请求记录（开启日志保留后新请求会出现在此）。</p>
        );
    }

    return (
        <ChartContainer config={chartConfig} className="h-[140px] w-full">
            <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border/40" />
                <XAxis dataKey="day" tickLine={false} axisLine={false} tick={{ fontSize: 10 }} />
                <YAxis tickLine={false} axisLine={false} width={32} tick={{ fontSize: 10 }} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Area type="monotone" dataKey="requests" stroke="var(--color-requests)" fill="var(--color-requests)" fillOpacity={0.2} />
            </AreaChart>
        </ChartContainer>
    );
}