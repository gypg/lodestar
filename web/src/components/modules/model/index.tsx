'use client';

import { useMemo } from 'react';
import { MotionConfig } from 'motion/react';
import { useModelMarket } from '@/api/endpoints/model';
import { useTranslations } from 'next-intl';
import { ModelItem } from './Item';
import { MobileModelItem } from './MobileModelItem';
import { useIsMobile } from '@/hooks/use-mobile';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { sortModelMarketItems } from './sort';

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

    const sortedModels = useMemo(() => {
        const items = market?.items ?? [];
        return sortModelMarketItems(items, modelSortMode);
    }, [market, modelSortMode]);
    const hasAnyModel = (market?.items.length ?? 0) > 0;

    const visibleModels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        const byName = !term ? sortedModels : sortedModels.filter((m) => m.name.toLowerCase().includes(term));
        const hasPricing = (model: (typeof byName)[number]) =>
            model.input + model.output + model.cache_read + model.cache_write > 0;

        if (filter === 'priced') {
            return byName.filter(hasPricing);
        }
        if (filter === 'free') {
            return byName.filter((m) => !hasPricing(m));
        }

        return byName;
    }, [sortedModels, searchTerm, filter]);

    return (
        <section className="relative flex h-full min-h-0 flex-col gap-3 overflow-y-auto overscroll-contain rounded-t-xl pb-3 sm:gap-4 sm:pb-4 md:pb-4" aria-label={pageKey}>
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
