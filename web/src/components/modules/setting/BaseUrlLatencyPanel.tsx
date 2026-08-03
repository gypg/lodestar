'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, Copy, Gauge, Server } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import {
    LATENCY_SAMPLE_COUNT,
    latencyBadgeClass,
    measureLatency,
    publicPingURL,
    type LatencySample,
} from '@/lib/base-url-latency';

export function BaseUrlLatencyPanel() {
    const t = useTranslations('setting.latency');
    const tCommon = useTranslations('common');
    const [origin, setOrigin] = useState('');
    const [samples, setSamples] = useState<LatencySample[]>([]);
    const [testing, setTesting] = useState(false);
    const [lastError, setLastError] = useState('');

    useEffect(() => {
        if (typeof window !== 'undefined') setOrigin(window.location.origin);
    }, []);

    const pingUrl = useMemo(() => publicPingURL(origin), [origin]);

    const successSamples = samples.filter((s) => s.ok);
    const best = successSamples.length ? Math.min(...successSamples.map((s) => s.ms)) : 0;
    const avg = successSamples.length
        ? Math.round(successSamples.reduce((sum, s) => sum + s.ms, 0) / successSamples.length)
        : 0;
    const latest = samples[samples.length - 1];

    const runTest = useCallback(async () => {
        const o = origin || (typeof window !== 'undefined' ? window.location.origin : '');
        const target = publicPingURL(o);
        setTesting(true);
        setLastError('');
        setSamples([]);
        const next: LatencySample[] = [];
        for (let i = 0; i < LATENCY_SAMPLE_COUNT; i += 1) {
            const sample = await measureLatency(target);
            next.push(sample);
            setSamples([...next]);
            if (!sample.ok) setLastError(sample.error || t('failed'));
        }
        setTesting(false);
    }, [origin, t]);

    const copyPing = () => {
        void navigator.clipboard?.writeText(pingUrl);
    };

    return (
        <div className="flex flex-col gap-3 rounded-lg border border-border/50 bg-background/60 p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm font-medium text-card-foreground">
                    <Gauge className="size-4 text-primary" />
                    {t('title')}
                </div>
                <Button type="button" size="sm" variant="outline" onClick={runTest} disabled={testing} className="h-8 gap-1.5">
                    <Activity className={`size-3.5 ${testing ? 'animate-pulse' : ''}`} />
                    {testing ? t('testing') : t('test')}
                </Button>
            </div>
            <p className="text-xs text-muted-foreground">
                {t('description')}
            </p>
            <div className="flex items-center gap-2 rounded-md border border-border/40 bg-muted/30 px-2 py-1.5 font-mono text-xs">
                <Server className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate" title={pingUrl}>
                    {pingUrl}
                </span>
                <button type="button" onClick={copyPing} className="text-muted-foreground hover:text-foreground" aria-label={tCommon('copyLatencyUrl')}>
                    <Copy className="size-3.5" />
                </button>
            </div>
            <div className="grid grid-cols-3 gap-2 text-center text-xs">
                <div className="rounded-md border border-border/40 p-2">
                    <div className="text-muted-foreground">{t('best')}</div>
                    <div className="font-semibold tabular-nums">{best ? `${best} ms` : '—'}</div>
                </div>
                <div className="rounded-md border border-border/40 p-2">
                    <div className="text-muted-foreground">{t('average')}</div>
                    <div className="font-semibold tabular-nums">{avg ? `${avg} ms` : '—'}</div>
                </div>
                <div className="rounded-md border border-border/40 p-2">
                    <div className="text-muted-foreground">{t('recent')}</div>
                    <div className="font-semibold">{latest ? (latest.ok ? `HTTP ${latest.status}` : t('failed')) : '—'}</div>
                </div>
            </div>
            {samples.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                    {samples.map((sample, index) => (
                        <span
                            key={`${sample.ms}-${index}`}
                            className={`rounded-full border px-2 py-0.5 text-xs font-medium tabular-nums ${latencyBadgeClass(sample.ms, sample.ok)}`}
                        >
                            #{index + 1} {sample.ok ? `${sample.ms} ms` : t('failed')}
                        </span>
                    ))}
                </div>
            )}
            {lastError ? <p className="text-xs text-destructive break-all">{lastError}</p> : null}
        </div>
    );
}