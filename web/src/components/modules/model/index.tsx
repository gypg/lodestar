'use client';

import { useMemo, useState } from 'react';
import { MotionConfig } from 'motion/react';
import { Eye } from 'lucide-react';
import { useModelMarket } from '@/api/endpoints/model';
import { useTranslations } from 'next-intl';
import { ModelItem } from './Item';
import { MobileModelItem } from './MobileModelItem';
import { useIsMobile } from '@/hooks/use-mobile';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { sortModelMarketItems } from './sort';
import { isProbeFailed } from './probe-state';
import { ModelMappingPanel } from './ModelMapping';
import { getModelIcon } from '@/lib/model-icons';

export function Model() {
    const t = useTranslations('model');
    const { data: market } = useModelMarket();
    const isMobile = useIsMobile();
    const pageKey = 'model' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.modelFilter);
    const modelSortMode = useToolbarViewOptionsStore((s) => s.modelSortMode);
    const modelLatencyUnit = useToolbarViewOptionsStore((s) => s.modelLatencyUnit);
    const modelProviderFilter = useToolbarViewOptionsStore((s) => s.modelProviderFilter);

    const sortedModels = useMemo(() => {
        const items = market?.items ?? [];
        return sortModelMarketItems(items, modelSortMode);
    }, [market, modelSortMode]);
    const hasAnyModel = (market?.items.length ?? 0) > 0;

    // WO-028: models that failed the scheduled probe threshold are hidden from
    // the default view; the toggle reveals them with a visible badge.
    const [showAllProbed, setShowAllProbed] = useState(false);
    // isProbeFailed，不是 !!m.probe_failed_at：后者把 Go 零值时刻读成"失败"，
    // 曾导致整个广场默认视图为空 + 横幅谎报（详见 probe-state.ts 的注释）。
    const probedBadCount = useMemo(
        () => sortedModels.filter((m) => isProbeFailed(m)).length,
        [sortedModels],
    );

    const visibleModels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        const byName = !term ? sortedModels : sortedModels.filter((m) => m.name.toLowerCase().includes(term));
        const hasPricing = (model: (typeof byName)[number]) =>
            model.input + model.output + model.cache_read + model.cache_write > 0;

        let filtered = byName;
        if (filter === 'priced') {
            filtered = byName.filter(hasPricing);
        } else if (filter === 'free') {
            filtered = byName.filter((m) => !hasPricing(m));
        }

        if (modelProviderFilter !== 'all') {
            filtered = filtered.filter((m) => getModelIcon(m.name).label === modelProviderFilter);
        }

        if (!showAllProbed) {
            filtered = filtered.filter((m) => !isProbeFailed(m));
        }

        return filtered;
    }, [sortedModels, searchTerm, filter, modelProviderFilter, showAllProbed]);

    return (
        <section className="relative flex h-full min-h-0 flex-col gap-3 overflow-y-auto overscroll-contain rounded-t-xl pb-3 sm:gap-4 sm:pb-4 md:pb-4" aria-label={pageKey}>
            <ModelMappingPanel />
            {probedBadCount > 0 ? (
                <div className="flex items-center justify-between gap-3 rounded-xl border border-border/35 bg-card px-3 py-2 text-card-foreground md:px-4">
                    <span className="text-xs text-muted-foreground">
                        {t('probe.hiddenCount', { count: probedBadCount })}
                    </span>
                    <button
                        type="button"
                        onClick={() => setShowAllProbed((prev) => !prev)}
                        aria-pressed={showAllProbed}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border/40 px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
                    >
                        <Eye className="size-3.5" />
                        {showAllProbed ? t('probe.showHealthyOnly') : t('probe.showAll')}
                    </button>
                </div>
            ) : null}
            {visibleModels.length > 0 ? (
                <section className="relative flex min-h-0 flex-1 flex-col rounded-xl border border-border/35 bg-card p-3 text-card-foreground md:p-4">
                    <div className="relative min-h-0 flex-1">
                        {isMobile ? (
                            <VirtualizedGrid
                                items={visibleModels}
                                layout="list"
                                columns={{ default: 1 }}
                                estimateItemHeight={132}
                                getItemKey={(model) => `m-model-${model.name}`}
                                renderItem={(model) => <MobileModelItem model={model} latencyUnit={modelLatencyUnit} />}
                                bottomPaddingClassName="pb-3 md:pb-4"
                            />
                        ) : (
                            <MotionConfig transition={{ layout: { duration: 0 } }}>
                                <VirtualizedGrid
                                    items={visibleModels}
                                    layout={layout}
                                    columns={{ default: 1, sm: 2, md: 2, lg: 3 }}
                                    estimateItemHeight={228}
                                    getItemKey={(model) => `model-${model.name}`}
                                    renderItem={(model) => <ModelItem model={model} layout={layout} latencyUnit={modelLatencyUnit} />}
                                    bottomPaddingClassName="pb-3 md:pb-4"
                                />
                            </MotionConfig>
                        )}
                    </div>
                </section>
            ) : (
                <section className="rounded-xl border border-border/35 bg-card p-3 text-card-foreground md:p-4">
                    <div className="relative flex min-h-[18rem] items-center justify-center overflow-hidden rounded-xl border border-dashed border-border/35 bg-card py-6">
                        <div className="relative flex flex-col items-center gap-4 px-6 text-center">
                            <div className="flex items-end gap-3">
                                <span className="h-24 w-16 rounded-lg border border-border/30 bg-card" />
                                <span className="h-28 w-20 rounded-xl border border-primary/18 bg-card" />
                                <span className="h-20 w-14 rounded-lg border border-border/30 bg-card" />
                            </div>
                            <p className="text-sm text-muted-foreground">
                                {hasAnyModel ? t('empty') : t('emptyAll')}
                            </p>
                        </div>
                    </div>
                </section>
            )}
        </section>
    );
}
