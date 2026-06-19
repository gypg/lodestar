'use client';

import { useOpsHealthStatus } from '@/api/endpoints/ops';
import { StatusBadge } from '@/components/modules/analytics/shared';
import { Activity } from 'lucide-react';

/** SAPI-inspired compact health strip for user portal (staff see full ops tab). */
export function PortalHealthStrip() {
    const { data, isLoading, error } = useOpsHealthStatus();

    if (isLoading || error || !data) return null;

    const issues =
        (data.warning_group_count ?? 0) +
        (data.degraded_group_count ?? 0) +
        (data.down_group_count ?? 0);
    const ok = data.database_ok && data.cache_ok && data.task_runtime_ok && issues === 0;

    return (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border/40 bg-card/80 px-3 py-2 text-xs">
            <Activity className="size-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">平台健康</span>
            <StatusBadge label={ok ? '正常' : '需关注'} tone={ok ? 'success' : 'warning'} />
            {!ok && (
                <span className="text-muted-foreground">
                    分组异常 {issues} · 近 24h 错误 {data.recent_error_count ?? 0}
                </span>
            )}
        </div>
    );
}