'use client';

import { useState, useCallback, useMemo } from 'react';
import { Loader2, CircleCheck, CircleX, TestTubeDiagonal, X, Search } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useGroupList, useTestGroup, useGroupTestProgress } from '@/api/endpoints/group';
import { usePublicOverview, type PublicModel } from '@/api/endpoints/public';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';
import { Progress } from '@/components/ui/progress';

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

interface ModelProviderGroup {
    provider: string;
    color: string;
    Avatar: ReturnType<typeof getModelIcon>['Avatar'];
    models: PublicModel[];
}

interface ModelTestEntry {
    modelName: string;
    groupName: string;
    progressId: string | null;
}

/* ------------------------------------------------------------------ */
/*  Result row -- one row per group that tested a model                 */
/* ------------------------------------------------------------------ */

function ModelTestResultRow({ entry }: { entry: ModelTestEntry }) {
    const { data: progress } = useGroupTestProgress(entry.progressId);

    const isDone = progress?.done ?? false;
    const passed = progress?.passed ?? false;
    const completed = progress?.completed ?? 0;
    const total = progress?.total ?? 0;
    const progressValue = total > 0 ? (completed / total) * 100 : 0;

    return (
        <div className="flex items-center gap-3 rounded-lg border border-border/25 bg-card px-3 py-2">
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border/20">
                {!entry.progressId ? (
                    <div className="size-2 rounded-full bg-muted-foreground/30" />
                ) : !isDone ? (
                    <Loader2 className="size-3.5 animate-spin text-primary" />
                ) : passed ? (
                    <CircleCheck className="size-3.5 text-emerald-500" />
                ) : (
                    <CircleX className="size-3.5 text-destructive" />
                )}
            </div>
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{entry.modelName}</span>
                    <span className="shrink-0 text-[11px] text-muted-foreground">via {entry.groupName}</span>
                </div>
                {entry.progressId && !isDone && total > 0 ? (
                    <div className="mt-1 flex items-center gap-2">
                        <Progress value={progressValue} className="h-1.5 flex-1" />
                        <span className="shrink-0 text-[11px] text-muted-foreground">{completed}/{total}</span>
                    </div>
                ) : null}
                {isDone && progress?.message ? (
                    <div className="mt-0.5 text-[11px] text-destructive">{progress.message}</div>
                ) : null}
            </div>
            {isDone && (
                <span className={cn(
                    'shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium',
                    passed
                        ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600'
                        : 'border-destructive/20 bg-destructive/10 text-destructive'
                )}>
                    {passed ? 'PASS' : 'FAIL'}
                </span>
            )}
        </div>
    );
}

/* ------------------------------------------------------------------ */
/*  Model chip                                                         */
/* ------------------------------------------------------------------ */

function ModelChip({
    model,
    icon,
    isSelected,
    isTesting,
    onToggle,
}: {
    model: PublicModel;
    icon: ReturnType<typeof getModelIcon>;
    isSelected: boolean;
    isTesting: boolean;
    onToggle: () => void;
}) {
    const IconAvatar = icon.Avatar;

    return (
        <button
            type="button"
            onClick={onToggle}
            disabled={isTesting}
            className={cn(
                'inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors',
                isTesting && 'cursor-not-allowed opacity-60',
                isSelected
                    ? 'border-primary/30 bg-primary/10 text-primary'
                    : 'border-border/30 bg-card text-muted-foreground hover:bg-accent',
            )}
        >
            <span className="flex h-4 w-4 shrink-0 items-center justify-center">
                <IconAvatar size={14} />
            </span>
            <span className="truncate max-w-[12rem]">{model.name}</span>
            {isSelected && <X className="size-3 shrink-0 opacity-60" />}
        </button>
    );
}

/* ------------------------------------------------------------------ */
/*  Group helper -- find groups that serve a given model name           */
/* ------------------------------------------------------------------ */

function findGroupsForModel(
    modelName: string,
    groups: { id?: number; name: string; items?: { model_name: string; channel_id: number }[] }[],
) {
    return groups.filter(
        (g) => g.items?.some((item) => item.model_name === modelName),
    );
}

/* ------------------------------------------------------------------ */
/*  Main component                                                     */
/* ------------------------------------------------------------------ */

export function GroupTestInline() {
    const t = useTranslations('analytics');
    const { data: groups, isLoading: groupsLoading } = useGroupList();
    const { data: overview, isLoading: overviewLoading } = usePublicOverview();
    const testGroup = useTestGroup();

    const [selectedModels, setSelectedModels] = useState<Set<string>>(new Set());
    const [searchQuery, setSearchQuery] = useState('');
    const [testEntries, setTestEntries] = useState<ModelTestEntry[]>([]);
    const [isTesting, setIsTesting] = useState(false);

    const isLoading = groupsLoading || overviewLoading;
    const models = useMemo(() => overview?.models ?? [], [overview]);
    const testableGroups = useMemo(
        () => (groups ?? []).filter((g) => g.id !== undefined && (g.items?.length ?? 0) > 0),
        [groups],
    );

    /* ---- Group models by provider ---- */
    const providerGroups = useMemo((): ModelProviderGroup[] => {
        const filtered = searchQuery
            ? models.filter((m) => m.name.toLowerCase().includes(searchQuery.toLowerCase()))
            : models;

        const map = new Map<string, ModelProviderGroup>();
        for (const model of filtered) {
            const icon = getModelIcon(model.name);
            const key = icon.label;
            if (!map.has(key)) {
                map.set(key, { provider: key, color: icon.color, Avatar: icon.Avatar, models: [] });
            }
            map.get(key)!.models.push(model);
        }
        return Array.from(map.values());
    }, [models, searchQuery]);

    const filteredModelCount = providerGroups.reduce((acc, g) => acc + g.models.length, 0);

    /* ---- Selection helpers ---- */
    const filteredModelNames = useMemo(
        () => providerGroups.flatMap((g) => g.models.map((m) => m.name)),
        [providerGroups],
    );

    const allSelected = filteredModelNames.length > 0 && filteredModelNames.every((n) => selectedModels.has(n));
    const hasSelection = selectedModels.size > 0;

    const toggleModel = useCallback((name: string) => {
        setSelectedModels((prev) => {
            const next = new Set(prev);
            if (next.has(name)) {
                next.delete(name);
            } else {
                next.add(name);
            }
            return next;
        });
    }, []);

    const selectAll = useCallback(() => {
        setSelectedModels(new Set(filteredModelNames));
    }, [filteredModelNames]);

    const deselectAll = useCallback(() => {
        setSelectedModels(new Set());
    }, []);

    /* ---- Test execution ---- */
    const runTests = useCallback(async (modelNames: string[]) => {
        if (modelNames.length === 0 || isTesting) return;

        setIsTesting(true);

        // For each model, find groups that serve it, deduplicate by group id
        const modelGroupMap = new Map<string, { groupId: number; groupName: string }[]>();
        for (const modelName of modelNames) {
            const matchingGroups = findGroupsForModel(modelName, testableGroups);
            if (matchingGroups.length > 0) {
                modelGroupMap.set(modelName, matchingGroups.map((g) => ({ groupId: g.id!, groupName: g.name })));
            }
        }

        // Build test entries: one entry per (model, group) pair
        const entries: ModelTestEntry[] = [];
        for (const [modelName, groupList] of modelGroupMap) {
            for (const { groupName } of groupList) {
                entries.push({ modelName, groupName, progressId: null });
            }
        }
        setTestEntries(entries);

        // Collect unique group ids to test
        const seenGroupIds = new Set<number>();
        const groupIdsToTest: number[] = [];
        for (const [, groupList] of modelGroupMap) {
            for (const { groupId } of groupList) {
                if (!seenGroupIds.has(groupId)) {
                    seenGroupIds.add(groupId);
                    groupIdsToTest.push(groupId);
                }
            }
        }

        // Fire tests for each unique group
        const results = await Promise.allSettled(
            groupIdsToTest.map(async (groupId) => {
                const progress = await testGroup.mutateAsync(groupId);
                return { groupId, progressId: progress.id };
            }),
        );

        // Map group id -> progress id from results
        const groupProgressMap = new Map<number, string>();
        for (const result of results) {
            if (result.status === 'fulfilled') {
                groupProgressMap.set(result.value.groupId, result.value.progressId);
            }
        }

        // Update entries with progress ids
        setTestEntries((prev) =>
            prev.map((entry) => {
                const matchingGroups = findGroupsForModel(entry.modelName, testableGroups);
                const matchingGroup = matchingGroups.find((g) => g.name === entry.groupName);
                if (matchingGroup) {
                    const progressId = groupProgressMap.get(matchingGroup.id!) ?? null;
                    return { ...entry, progressId };
                }
                return entry;
            }),
        );

        setIsTesting(false);
    }, [isTesting, testableGroups, testGroup]);

    const handleTestAll = useCallback(() => {
        const allModelNames = models.map((m) => m.name);
        runTests(allModelNames);
    }, [runTests, models]);

    const handleTestSelected = useCallback(() => {
        runTests(Array.from(selectedModels));
    }, [runTests, selectedModels]);

    /* ---- Render ---- */

    if (isLoading) {
        return (
            <div className="flex items-center justify-center gap-2 rounded-lg border border-border/25 bg-card p-8 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                {t('states.loading')}
            </div>
        );
    }

    if (models.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-border/25 bg-card p-8">
                <TestTubeDiagonal className="size-6 text-muted-foreground/40" />
                <span className="text-sm text-muted-foreground">{t('groupTestInline.noGroups')}</span>
            </div>
        );
    }

    return (
        <div className="space-y-3">
            {/* Action bar */}
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={allSelected ? deselectAll : selectAll}
                        className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border/25 bg-card px-2.5 text-xs font-medium transition-colors hover:bg-accent"
                    >
                        {allSelected ? t('groupTestInline.deselectAll') : t('groupTestInline.selectAll')}
                    </button>
                    <span className="text-[11px] text-muted-foreground">
                        {selectedModels.size}/{models.length}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={handleTestAll}
                        disabled={isTesting || models.length === 0}
                        className={cn(
                            'inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-xs font-medium transition-colors',
                            isTesting
                                ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                                : 'border-primary/20 bg-primary text-primary-foreground hover:opacity-90'
                        )}
                    >
                        {isTesting ? <Loader2 className="size-3.5 animate-spin" /> : <TestTubeDiagonal className="size-3.5" />}
                        {isTesting ? t('groupTestInline.testing') : t('groupTestInline.testAll')}
                    </button>
                    <button
                        type="button"
                        onClick={handleTestSelected}
                        disabled={isTesting || !hasSelection}
                        className={cn(
                            'inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-xs font-medium transition-colors',
                            isTesting || !hasSelection
                                ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                                : 'border-primary/20 bg-primary/80 text-primary hover:bg-primary/12'
                        )}
                    >
                        {isTesting ? <Loader2 className="size-3.5 animate-spin" /> : <TestTubeDiagonal className="size-3.5" />}
                        {isTesting ? t('groupTestInline.testing') : t('groupTestInline.testSelected')}
                    </button>
                </div>
            </div>

            {/* Search input */}
            <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
                <input
                    type="text"
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    placeholder={t('groupTestInline.searchModels')}
                    className="h-8 w-full rounded-lg border border-border/25 bg-card pl-8 pr-3 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary/30 focus:outline-none focus:ring-1 focus:ring-primary/20"
                />
                {searchQuery && (
                    <button
                        type="button"
                        onClick={() => setSearchQuery('')}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
                    >
                        <X className="size-3.5" />
                    </button>
                )}
            </div>

            {/* Model tags grouped by provider */}
            <div className="max-h-72 space-y-3 overflow-y-auto pr-1">
                {providerGroups.map((group) => (
                    <div key={group.provider}>
                        <div className="mb-1.5 flex items-center gap-1.5">
                            <span className="flex h-4 w-4 items-center justify-center">
                                <group.Avatar size={14} />
                            </span>
                            <span className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/70">
                                {group.provider}
                            </span>
                            <span className="text-[10px] text-muted-foreground/40">({group.models.length})</span>
                        </div>
                        <div className="flex flex-wrap gap-2">
                            {group.models.map((model) => {
                                const icon = getModelIcon(model.name);
                                return (
                                    <ModelChip
                                        key={model.name}
                                        model={model}
                                        icon={icon}
                                        isSelected={selectedModels.has(model.name)}
                                        isTesting={isTesting}
                                        onToggle={() => toggleModel(model.name)}
                                    />
                                );
                            })}
                        </div>
                    </div>
                ))}

                {filteredModelCount === 0 && searchQuery && (
                    <div className="flex items-center justify-center py-6 text-xs text-muted-foreground">
                        {t('groupTestInline.noMatch')}
                    </div>
                )}
            </div>

            {/* Results area */}
            {testEntries.length > 0 && (
                <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
                    {testEntries.map((entry, idx) => (
                        <ModelTestResultRow key={`${entry.modelName}-${entry.groupName}-${idx}`} entry={entry} />
                    ))}
                </div>
            )}
        </div>
    );
}
