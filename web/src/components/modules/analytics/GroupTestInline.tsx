'use client';

import { useState, useCallback, useMemo } from 'react';
import { Loader2, CircleCheck, CircleX, TestTubeDiagonal, X, Search, Server, Layers, Cpu } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useQueryClient } from '@tanstack/react-query';
import { useGroupList, useTestGroup, useGroupTestProgress } from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { useChannelList } from '@/api/endpoints/channel';
import { usePublicOverview, type PublicModel } from '@/api/endpoints/public';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';
import { Progress } from '@/components/ui/progress';

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

type GroupByMode = 'channel' | 'provider' | 'type';

const IMAGE_HINT = /dall-e|gpt-image|flux|stable-diffusion|sdxl|midjourney|imagen|kolors|cogview|wanx|ideogram|recraft/i;
const AUDIO_HINT = /whisper|tts|audio|speech|voice/i;
const EMBEDDING_HINT = /embedding|embed/i;

function classifyModelType(name: string): string {
    const lower = name.toLowerCase();
    if (IMAGE_HINT.test(lower)) return 'Image';
    if (AUDIO_HINT.test(lower)) return 'Audio';
    if (EMBEDDING_HINT.test(lower)) return 'Embedding';
    return 'Chat';
}

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

/** A section in the hierarchical model list */
interface ModelSection {
    key: string;
    label: string;
    icon: typeof Server;
    subGroups: Array<{
        key: string;
        label: string;
        color?: string;
        Avatar?: ReturnType<typeof getModelIcon>['Avatar'];
        models: PublicModel[];
    }>;
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
/*  Section renderers                                                  */
/* ------------------------------------------------------------------ */

function ModelSectionBlock({
    section,
    selectedModels,
    isTesting,
    onToggle,
}: {
    section: ModelSection;
    selectedModels: Set<string>;
    isTesting: boolean;
    onToggle: (name: string) => void;
}) {
    const SectionIcon = section.icon;
    const totalCount = section.subGroups.reduce((acc, sg) => acc + sg.models.length, 0);

    return (
        <div>
            <div className="mb-2 flex items-center gap-1.5">
                <SectionIcon className="size-3.5 text-muted-foreground/60" />
                <span className="text-[11px] font-bold uppercase tracking-wide text-muted-foreground/80">
                    {section.label}
                </span>
                <span className="text-[10px] text-muted-foreground/40">({totalCount})</span>
            </div>
            <div className="ml-3 space-y-2">
                {section.subGroups.map((sg) => (
                    <div key={sg.key}>
                        <div className="mb-1 flex items-center gap-1.5">
                            {sg.Avatar ? (
                                <span className="flex h-3.5 w-3.5 items-center justify-center">
                                    <sg.Avatar size={12} />
                                </span>
                            ) : null}
                            <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground/60">
                                {sg.label}
                            </span>
                            <span className="text-[10px] text-muted-foreground/30">({sg.models.length})</span>
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                            {sg.models.map((model) => {
                                const icon = getModelIcon(model.name);
                                return (
                                    <ModelChip
                                        key={model.name}
                                        model={model}
                                        icon={icon}
                                        isSelected={selectedModels.has(model.name)}
                                        isTesting={isTesting}
                                        onToggle={() => onToggle(model.name)}
                                    />
                                );
                            })}
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

/* ------------------------------------------------------------------ */
/*  Main component                                                     */
/* ------------------------------------------------------------------ */

export function GroupTestInline() {
    const t = useTranslations('analytics');
    const queryClient = useQueryClient();
    const { data: groups, isLoading: groupsLoading } = useGroupList();
    const { data: overview, isLoading: overviewLoading } = usePublicOverview();
    const { data: modelChannels } = useModelChannelList();
    const { data: channels } = useChannelList();
    const testGroup = useTestGroup();

    const [selectedModels, setSelectedModels] = useState<Set<string>>(new Set());
    const [searchQuery, setSearchQuery] = useState('');
    const [testEntries, setTestEntries] = useState<ModelTestEntry[]>([]);
    const [isTesting, setIsTesting] = useState(false);
    const [groupBy, setGroupBy] = useState<GroupByMode>('provider');

    const isLoading = groupsLoading || overviewLoading;
    const models = useMemo(() => overview?.models ?? [], [overview]);
    const testableGroups = useMemo(
        () => (groups ?? []).filter((g) => g.id !== undefined && (g.items?.length ?? 0) > 0),
        [groups],
    );

    /* ---- Filter models by search ---- */
    const filteredModels = useMemo(() => {
        if (!searchQuery) return models;
        const q = searchQuery.toLowerCase();
        return models.filter((m) => m.name.toLowerCase().includes(q));
    }, [models, searchQuery]);

    /* ---- Build model -> channel mapping ---- */
    const modelChannelMap = useMemo(() => {
        const map = new Map<string, { channelId: number; channelName: string }[]>();
        for (const mc of modelChannels ?? []) {
            if (!mc.enabled) continue;
            const existing = map.get(mc.name) ?? [];
            existing.push({ channelId: mc.channel_id, channelName: mc.channel_name });
            map.set(mc.name, existing);
        }
        return map;
    }, [modelChannels]);

    /* ---- Build channel id -> channel display name mapping ---- */
    const channelNameMap = useMemo(() => {
        const map = new Map<number, string>();
        for (const ch of channels ?? []) {
            map.set(ch.raw.id, ch.raw.name);
        }
        return map;
    }, [channels]);

    /* ---- Group models by provider (flat, used by "provider" mode) ---- */
    const providerGroups = useMemo((): ModelProviderGroup[] => {
        const map = new Map<string, ModelProviderGroup>();
        for (const model of filteredModels) {
            const icon = getModelIcon(model.name);
            const key = icon.label;
            if (!map.has(key)) {
                map.set(key, { provider: key, color: icon.color, Avatar: icon.Avatar, models: [] });
            }
            map.get(key)!.models.push(model);
        }
        return Array.from(map.values());
    }, [filteredModels]);

    /* ---- Build hierarchical sections based on groupBy mode ---- */
    const sections = useMemo((): ModelSection[] => {
        if (groupBy === 'provider') {
            // Flat provider grouping (same as before, wrapped in section format)
            return providerGroups.map((pg) => ({
                key: pg.provider,
                label: pg.provider,
                icon: Cpu,
                subGroups: [{
                    key: pg.provider,
                    label: pg.provider,
                    color: pg.color,
                    Avatar: pg.Avatar,
                    models: pg.models,
                }],
            }));
        }

        if (groupBy === 'channel') {
            // Group by channel, then by provider within each channel
            const channelSections = new Map<string, Map<string, { color: string; Avatar: ReturnType<typeof getModelIcon>['Avatar']; models: PublicModel[] }>>();

            for (const model of filteredModels) {
                const channelEntries = modelChannelMap.get(model.name);
                const icon = getModelIcon(model.name);

                if (!channelEntries || channelEntries.length === 0) {
                    // No channel mapping -- put in "Unassigned"
                    const unassigned = channelSections.get('__unassigned__') ?? new Map();
                    const providerGroup = unassigned.get(icon.label) ?? { color: icon.color, Avatar: icon.Avatar, models: [] };
                    providerGroup.models.push(model);
                    unassigned.set(icon.label, providerGroup);
                    channelSections.set('__unassigned__', unassigned);
                } else {
                    for (const entry of channelEntries) {
                        const chName = channelNameMap.get(entry.channelId) ?? entry.channelName ?? `Channel ${entry.channelId}`;
                        const providerMap = channelSections.get(chName) ?? new Map();
                        const providerGroup = providerMap.get(icon.label) ?? { color: icon.color, Avatar: icon.Avatar, models: [] };
                        // Deduplicate: don't add the same model twice to the same provider group
                        if (!providerGroup.models.some((m: PublicModel) => m.name === model.name)) {
                            providerGroup.models.push(model);
                        }
                        providerMap.set(icon.label, providerGroup);
                        channelSections.set(chName, providerMap);
                    }
                }
            }

            const result: ModelSection[] = [];
            // Put unassigned last
            const sortedKeys = Array.from(channelSections.keys()).sort((a, b) => {
                if (a === '__unassigned__') return 1;
                if (b === '__unassigned__') return -1;
                return a.localeCompare(b);
            });

            for (const chName of sortedKeys) {
                const providerMap = channelSections.get(chName)!;
                const subGroups = Array.from(providerMap.entries()).map(([providerLabel, data]) => ({
                    key: providerLabel,
                    label: providerLabel,
                    color: data.color,
                    Avatar: data.Avatar,
                    models: data.models,
                }));
                result.push({
                    key: chName,
                    label: chName === '__unassigned__' ? 'Unassigned' : chName,
                    icon: Server,
                    subGroups,
                });
            }
            return result;
        }

        if (groupBy === 'type') {
            // Group by model type (Chat/Image/Audio/Embedding), then by provider
            const typeSections = new Map<string, Map<string, { color: string; Avatar: ReturnType<typeof getModelIcon>['Avatar']; models: PublicModel[] }>>();

            for (const model of filteredModels) {
                const modelType = classifyModelType(model.name);
                const icon = getModelIcon(model.name);

                const providerMap = typeSections.get(modelType) ?? new Map();
                const providerGroup = providerMap.get(icon.label) ?? { color: icon.color, Avatar: icon.Avatar, models: [] };
                providerGroup.models.push(model);
                providerMap.set(icon.label, providerGroup);
                typeSections.set(modelType, providerMap);
            }

            // Sort types: Chat first, then alphabetical
            const typeOrder = ['Chat', 'Image', 'Audio', 'Embedding'];
            const sortedTypes = Array.from(typeSections.keys()).sort((a, b) => {
                const ai = typeOrder.indexOf(a);
                const bi = typeOrder.indexOf(b);
                if (ai !== -1 && bi !== -1) return ai - bi;
                if (ai !== -1) return -1;
                if (bi !== -1) return 1;
                return a.localeCompare(b);
            });

            return sortedTypes.map((modelType) => {
                const providerMap = typeSections.get(modelType)!;
                return {
                    key: modelType,
                    label: modelType,
                    icon: Layers,
                    subGroups: Array.from(providerMap.entries()).map(([providerLabel, data]) => ({
                        key: providerLabel,
                        label: providerLabel,
                        color: data.color,
                        Avatar: data.Avatar,
                        models: data.models,
                    })),
                };
            });
        }

        return [];
    }, [groupBy, providerGroups, filteredModels, modelChannelMap, channelNameMap]);

    const filteredModelCount = filteredModels.length;

    /* ---- Selection helpers ---- */
    const filteredModelNames = useMemo(
        () => filteredModels.map((m) => m.name),
        [filteredModels],
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

        // Invalidate the test-history query so the Evaluation page picks up new results
        queryClient.invalidateQueries({ queryKey: ['groups', 'test-history'] });
        queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
    }, [isTesting, testableGroups, testGroup, queryClient]);

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

            {/* Search input + group-by toggle */}
            <div className="flex items-center gap-2">
                <div className="relative flex-1">
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
                <div className="inline-flex rounded-lg border border-border/25 bg-card p-0.5">
                    {([
                        { value: 'channel' as const, label: 'Channel', Icon: Server },
                        { value: 'provider' as const, label: 'Provider', Icon: Cpu },
                        { value: 'type' as const, label: 'Type', Icon: Layers },
                    ]).map(({ value, label, Icon }) => (
                        <button
                            key={value}
                            type="button"
                            onClick={() => setGroupBy(value)}
                            className={cn(
                                'inline-flex h-7 items-center gap-1 rounded-md px-2 text-[11px] font-medium transition-colors',
                                groupBy === value
                                    ? 'bg-primary/10 text-primary'
                                    : 'text-muted-foreground hover:bg-accent',
                            )}
                        >
                            <Icon className="size-3" />
                            {label}
                        </button>
                    ))}
                </div>
            </div>

            {/* Model tags grouped hierarchically */}
            <div className="max-h-72 space-y-4 overflow-y-auto pr-1">
                {sections.map((section) => (
                    <ModelSectionBlock
                        key={section.key}
                        section={section}
                        selectedModels={selectedModels}
                        isTesting={isTesting}
                        onToggle={toggleModel}
                    />
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
