'use client';

import { useId, useMemo, useState } from 'react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { formatCount, formatMoney } from '@/lib/utils';
import type { UsageDailyPoint } from '@/api/endpoints/wallet';

export type WalletChartMetric = 'tokens' | 'cost' | 'requests';

function fmtDay(d: string) {
    if (/^\d{8}$/.test(d)) return `${d.slice(4, 6)}/${d.slice(6, 8)}`;
    return d;
}

function metricValue(p: UsageDailyPoint, metric: WalletChartMetric): number {
    if (metric === 'tokens') return p.tokens;
    if (metric === 'cost') return p.cost;
    return p.requests;
}

function metricLabel(metric: WalletChartMetric): string {
    if (metric === 'tokens') return 'Tokens';
    if (metric === 'cost') return '花费 (USD)';
    return '请求';
}

function formatAxis(metric: WalletChartMetric, value: number): string {
    if (metric === 'cost') {
        const f = formatMoney(value);
        return `${f.formatted.value}${f.formatted.unit}`;
    }
    if (metric === 'tokens' || metric === 'requests') {
        const f = formatCount(value);
        return `${f.formatted.value}${f.formatted.unit}`;
    }
    return String(value);
}

export function WalletUsageChart({
    series,
    available,
}: {
    series?: UsageDailyPoint[];
    available?: boolean;
}) {
    const [metric, setMetric] = useState<WalletChartMetric>('tokens');
    const gradientId = useId().replace(/:/g, '');

    const data = useMemo(
        () =>
            (series ?? []).map((p) => ({
                day: fmtDay(p.date),
                value: metricValue(p, metric),
            })),
        [series, metric],
    );

    const chartConfig = useMemo(
        () => ({
            value: { label: metricLabel(metric), color: metric === 'cost' ? 'var(--chart-1)' : 'var(--chart-3)' },
        }),
        [metric],
    );

    const hasData = data.some((d) => d.value > 0);

    if (!available) {
        return (
            <p className="text-xs text-muted-foreground">
                按日曲线需在「系统 → 日志」中开启<strong>保留历史日志</strong>后才有数据。
            </p>
        );
    }
    if (!hasData) {
        return (
            <p className="text-xs text-muted-foreground">近 14 日暂无记录（开启日志保留后新请求会出现在此）。</p>
        );
    }

    const stroke = metric === 'cost' ? 'var(--chart-1)' : 'var(--chart-3)';

    return (
        <div className="flex flex-col gap-2">
            <Tabs value={metric} onValueChange={(v) => setMetric(v as WalletChartMetric)}>
                <TabsList className="inline-flex h-8 w-full justify-start rounded-lg border border-border/40 bg-background p-0.5 sm:w-auto">
                    <TabsTrigger value="tokens" className="h-7 px-2.5 text-xs">
                        Tokens
                    </TabsTrigger>
                    <TabsTrigger value="cost" className="h-7 px-2.5 text-xs">
                        花费
                    </TabsTrigger>
                    <TabsTrigger value="requests" className="h-7 px-2.5 text-xs">
                        请求
                    </TabsTrigger>
                </TabsList>
            </Tabs>
            <ChartContainer config={chartConfig} className="h-[140px] w-full">
                <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                    <defs>
                        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor={stroke} stopOpacity={0.35} />
                            <stop offset="95%" stopColor={stroke} stopOpacity={0.05} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border/40" />
                    <XAxis dataKey="day" tickLine={false} axisLine={false} tick={{ fontSize: 10 }} />
                    <YAxis
                        tickLine={false}
                        axisLine={false}
                        width={40}
                        tick={{ fontSize: 10 }}
                        tickFormatter={(v) => formatAxis(metric, Number(v))}
                    />
                    <ChartTooltip
                        content={
                            <ChartTooltipContent
                                formatter={(value) => {
                                    const n = Number(value);
                                    if (metric === 'cost') return `$${n.toFixed(4)}`;
                                    return n.toLocaleString('en-US');
                                }}
                            />
                        }
                    />
                    <Area
                        type="monotone"
                        dataKey="value"
                        stroke={stroke}
                        fill={`url(#${gradientId})`}
                        fillOpacity={1}
                    />
                </AreaChart>
            </ChartContainer>
        </div>
    );
}