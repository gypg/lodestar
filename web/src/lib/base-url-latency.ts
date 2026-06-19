/**
 * Lodestar — browser-side Base URL / site reachability latency (SAPI-inspired).
 * Uses the public ping endpoint so results match the user's real network path.
 */

export const LATENCY_SAMPLE_COUNT = 3;
export const LATENCY_TIMEOUT_MS = 8000;

export interface LatencySample {
    ok: boolean;
    status: number;
    ms: number;
    error: string;
}

export function publicPingURL(origin?: string): string {
    const base = origin ?? (typeof window !== 'undefined' ? window.location.origin : '');
    try {
        const url = new URL('/api/v1/public/ping', base || 'http://localhost');
        return url.toString();
    } catch {
        return `${base}/api/v1/public/ping`;
    }
}

export async function measureLatency(url: string): Promise<LatencySample> {
    const target = new URL(url);
    target.searchParams.set('_lodestar_latency', `${Date.now()}-${Math.random().toString(36).slice(2)}`);
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), LATENCY_TIMEOUT_MS);
    const started = performance.now();
    try {
        const response = await fetch(target.toString(), {
            method: 'GET',
            cache: 'no-store',
            headers: { 'Cache-Control': 'no-store', Pragma: 'no-cache' },
            signal: controller.signal,
        });
        const ms = Math.max(1, Math.round(performance.now() - started));
        return {
            ok: response.ok,
            status: response.status,
            ms,
            error: response.ok ? '' : `HTTP ${response.status}`,
        };
    } catch (err) {
        const ms = Math.max(1, Math.round(performance.now() - started));
        const e = err as Error & { name?: string };
        return {
            ok: false,
            status: 0,
            ms,
            error: e?.name === 'AbortError' ? '测速超时' : e?.message || '网络错误',
        };
    } finally {
        window.clearTimeout(timeout);
    }
}

export function latencyBadgeClass(ms: number, ok: boolean): string {
    if (!ok) return 'border-destructive/50 bg-destructive/10 text-destructive';
    if (ms <= 300) return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400';
    if (ms <= 1000) return 'border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-400';
    return 'border-orange-500/40 bg-orange-500/10 text-orange-800 dark:text-orange-400';
}