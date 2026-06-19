'use client';

import { useOpsHealthStatus } from '@/api/endpoints/ops';
import { StatusBadge } from '@/components/modules/analytics/shared';
import { Activity } from 'lucide-react';
import { useNavStore } from '@/components/modules/navbar';

/** SAPI-inspired compact health + link to full ops health tab (staff). */
export function PortalHealthStrip() {
    const { data, isLoading, error } = useOpsHealthStatus();
    const setActiveItem = useNavStore((s) => s.setActiveItem);

    if (isLoading || error || !data) return null;

    const issues =
        (data.warning_group_count ?? 0) +
        (data.degraded_group_count ?? 0) +
        (data.down_group_count ?? 0);
    const ok = data.database_ok && data.cache_ok && data.task_runtime_ok && issues === 0;
    const topFail = (data.failing_groups ?? []).slice(0, 3);

    return (
        <div className="flex flex-col gap-2 rounded-lg border border-border/40 bg-card/80 px-3 py-2 text-xs">
            <div className="flex flex-wrap items-center gap-2">
                <Activity className="size-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">平台健康</span>
                <StatusBadge label={ok ? '正常' : '需关注'} tone={ok ? 'success' : 'warning'} />
                {!ok && (
                    <span className="text-muted-foreground">
                        分组异常 {issues} · 近 24h 错误 {data.recent_error_count ?? 0}
                    </span>
                )}
                <button
                    type="button"
                    className="ml-auto text-primary underline-offset-2 hover:underline"
                    onClick={() => setActiveItem('ops')}
                >
                    运维详情
                </button>
            </div>
            {topFail.length > 0 && (
                <ul className="flex flex-col gap-1 border-t border-border/30 pt-2 text-muted-foreground">
                    {topFail.map((g) => (
                        <li key={`${g.group_id}-${g.endpoint_type}`} className="truncate">
                            <span className="font-medium text-card-foreground">{g.group_name}</span>
                            {' · '}
                            {g.endpoint_type} · {g.status} · 失败 {g.failure_count}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}