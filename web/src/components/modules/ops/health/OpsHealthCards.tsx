'use client';

import { useTranslations } from 'next-intl';
import { useOpsTelemetrySummary } from '@/api/endpoints/ops';
import {
    useAnalyticsAutoStrategy,
    useAnalyticsGroupHealth,
    useAnalyticsUtilization,
} from '@/api/endpoints/analytics';
import { GroupHealthCard } from '@/components/modules/analytics/GroupHealth';
import { QueryState } from '@/components/modules/analytics/shared';
import { useNavStore } from '@/components/modules/navbar/nav-store';
import type { AutoStrategySnapshotItem } from '@/api/endpoints/analytics';
import { ProviderChannelCard } from './ProviderChannelCard';
import { ModelUtilizationCard } from './ModelUtilizationCard';

function autoItemsForGroup(
    item: import('@/api/endpoints/analytics').AnalyticsGroupHealthItem,
    autoByChannel: Map<number, AutoStrategySnapshotItem[]>,
): AutoStrategySnapshotItem[] {
    if (item.mode !== 5) return [];
    const channelIds = new Set<number>(item.channel_ids ?? []);
    if (channelIds.size === 0) return [];
    const result: AutoStrategySnapshotItem[] = [];
    for (const [channelId, list] of autoByChannel) {
        if (channelIds.has(channelId)) result.push(...list);
    }
    return result.slice(0, 12);
}

/** Wave B: SAPI-inspired provider + route group + model cards on Ops → Health */
export function OpsHealthCards() {
    const t = useTranslations('ops');
    const setActiveItem = useNavStore((s) => s.setActiveItem);
    const { data: telemetry, isLoading: telLoading, error: telError } = useOpsTelemetrySummary();
    const { data: groups, isLoading: ghLoading, error: ghError } = useAnalyticsGroupHealth();
    const { data: util, isLoading: utilLoading, error: utilError } = useAnalyticsUtilization('7d');
    const { data: autoData } = useAnalyticsAutoStrategy();

    const providers = telemetry?.provider_health.providers ?? [];
    const models = (util?.model_breakdown ?? []).slice(0, 24);

    const autoByChannel = new Map<number, AutoStrategySnapshotItem[]>();
    for (const ai of autoData ?? []) {
        const list = autoByChannel.get(ai.channel_id) ?? [];
        list.push(ai);
        autoByChannel.set(ai.channel_id, list);
    }

    const sortedGroups = [...(groups ?? [])].sort((a, b) => {
        const rank = (s: string) => (s === 'down' ? 0 : s === 'degraded' ? 1 : s === 'warning' ? 2 : 3);
        return rank(a.status) - rank(b.status);
    });

    return (
        <div className="space-y-6">
            <section className="space-y-3">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <div>
                        <h4 className="text-sm font-semibold">{t('health.portal.providersTitle')}</h4>
                        <p className="text-xs text-muted-foreground">{t('health.portal.providersHint')}</p>
                    </div>
                    <p className="text-[11px] text-muted-foreground">{t('health.portal.telemetryHint')}</p>
                </div>
                <QueryState
                    loading={telLoading}
                    error={telError}
                    empty={providers.length === 0}
                    emptyLabel={t('telemetry.provider_health.empty')}
                >
                    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                        {providers.map((p) => (
                            <ProviderChannelCard key={p.channel_id} provider={p} />
                        ))}
                    </div>
                </QueryState>
            </section>

            <section className="space-y-3">
                <div>
                    <h4 className="text-sm font-semibold">{t('health.portal.routesTitle')}</h4>
                    <p className="text-xs text-muted-foreground">{t('health.portal.routesHint')}</p>
                </div>
                <QueryState
                    loading={ghLoading}
                    error={ghError}
                    empty={sortedGroups.length === 0}
                    emptyLabel={t('health.portal.routesEmpty')}
                >
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        {sortedGroups.map((item) => (
                            <GroupHealthCard
                                key={`${item.group_id}-${item.endpoint_type}`}
                                item={item}
                                autoItems={autoItemsForGroup(item, autoByChannel)}
                            />
                        ))}
                    </div>
                </QueryState>
            </section>

            <section className="space-y-3">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <div>
                        <h4 className="text-sm font-semibold">{t('health.portal.modelsTitle')}</h4>
                        <p className="text-xs text-muted-foreground">{t('health.portal.modelsHint')}</p>
                    </div>
                    <button
                        type="button"
                        className="text-xs text-primary underline-offset-2 hover:underline"
                        onClick={() => setActiveItem('analytics')}
                    >
                        {t('health.portal.openAnalytics')}
                    </button>
                </div>
                <QueryState
                    loading={utilLoading}
                    error={utilError}
                    empty={models.length === 0}
                    emptyLabel={t('health.portal.modelsEmpty')}
                >
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                        {models.map((m) => (
                            <ModelUtilizationCard key={m.model_name} item={m} />
                        ))}
                    </div>
                </QueryState>
            </section>
        </div>
    );
}