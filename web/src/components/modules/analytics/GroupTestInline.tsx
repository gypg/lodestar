'use client';

import { useState, useCallback, useMemo } from 'react';
import {
    Loader2,
    CircleCheck,
    CircleX,
    TestTubeDiagonal,
    ChevronRight,
    ArrowLeft,
    Server,
    Hash,
    Cpu,
} from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useQueryClient } from '@tanstack/react-query';
import { useGroupList, useTestGroup, useGroupTestProgress } from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { useChannelList } from '@/api/endpoints/channel';
import { usePublicOverview, type PublicModel } from '@/api/endpoints/public';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';
import { Progress } from '@/components/ui/progress';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

interface ModelTestEntry {
    modelName: string;
    groupName: string;
    progressId: string | null;
}

/** A provider group within a channel */
interface ProviderInfo {
    provider: string;
    color: string;
    Avatar: ReturnType<typeof getModelIcon>['Avatar'];
    models: PublicModel[];
}

/** A channel with its providers */
interface ChannelInfo {
    channelId: number;
    channelName: string;
    providers: ProviderInfo[];
    totalModels: number;
    totalProviders: number;
}

/** Navigation level */
type NavLevel = 'channels' | 'providers' | 'models';

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
/*  Breadcrumb component                                               */
/* ------------------------------------------------------------------ */

function Breadcrumb({
    level,
    channelName,
    providerName,
    onNavigate,
    t,
}: {
    level: NavLevel;
    channelName?: string;
    providerName?: string;
    onNavigate: (target: NavLevel) => void;
    t: ReturnType<typeof useTranslations>;
}) {
    return (
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
            <button
                type="button"
                onClick={() => onNavigate('channels')}
                className={cn(
                    'transition-colors hover:text-foreground',
                    level === 'channels' && 'font-semibold text-foreground',
                )}
            >
                {t('groupTestInline.breadcrumbChannels')}
            </button>
            {level !== 'channels' && channelName && (
                <>
                    <ChevronRight className="size-3 shrink-0" />
                    <button
                        type="button"
                        onClick={() => onNavigate('providers')}
                        className={cn(
                            'transition-colors hover:text-foreground',
                            level === 'providers' && 'font-semibold text-foreground',
                        )}
                    >
                        {channelName}
                    </button>
                </>
            )}
            {level === 'models' && providerName && (
                <>
                    <ChevronRight className="size-3 shrink-0" />
                    <span className="font-semibold text-foreground">{providerName}</span>
                </>
            )}
        </div>
    );
}

/* ------------------------------------------------------------------ */
/*  Channel card                                                       */
/* ------------------------------------------------------------------ */

function ChannelCard({
    channel,
    isTesting,
    onDrill,
    onTestAll,
    t,
}: {
    channel: ChannelInfo;
    isTesting: boolean;
    onDrill: () => void;
    onTestAll: () => void;
    t: ReturnType<typeof useTranslations>;
}) {
    return (
        <div className="group flex items-center gap-3 rounded-lg border border-border/25 bg-card px-4 py-3 transition-colors hover:border-border/50">
            <button
                type="button"
                onClick={onDrill}
                className="flex min-w-0 flex-1 items-center gap-3 text-left"
            >
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border/20 bg-muted/50">
                    <Server className="size-4 text-muted-foreground" />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{channel.channelName}</div>
                    <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                        <span>{t('groupTestInline.modelsInChannel', { count: channel.totalModels, channel: '' }).replace(/^.*?(\d)/, '$1').replace(/\s*.*/, '')}</span>
                        <span className="text-muted-foreground/40">|</span>
                        <span>{channel.totalProviders} {channel.totalProviders === 1 ? 'provider' : 'providers'}</span>
                    </div>
                </div>
                <ChevronRight className="size-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
            </button>
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    onTestAll();
                }}
                disabled={isTesting}
                className={cn(
                    'inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 text-[11px] font-medium transition-colors',
                    isTesting
                        ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                        : 'border-primary/20 bg-primary/10 text-primary hover:bg-primary/20',
                )}
            >
                {isTesting ? <Loader2 className="size-3 animate-spin" /> : <TestTubeDiagonal className="size-3" />}
                {t('groupTestInline.testChannel')}
            </button>
        </div>
    );
}

/* ------------------------------------------------------------------ */
/*  Provider card                                                      */
/* ------------------------------------------------------------------ */

function ProviderCard({
    provider,
    isTesting,
    onDrill,
    onTestAll,
    t,
}: {
    provider: ProviderInfo;
    isTesting: boolean;
    onDrill: () => void;
    onTestAll: () => void;
    t: ReturnType<typeof useTranslations>;
}) {
    const ProviderAvatar = provider.Avatar;
    return (
        <div className="group flex items-center gap-3 rounded-lg border border-border/25 bg-card px-4 py-3 transition-colors hover:border-border/50">
            <button
                type="button"
                onClick={onDrill}
                className="flex min-w-0 flex-1 items-center gap-3 text-left"
            >
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border/20 bg-muted/50">
                    <ProviderAvatar size={18} />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{provider.provider}</div>
                    <div className="text-[11px] text-muted-foreground">
                        {t('groupTestInline.modelsInProvider', { count: provider.models.length })}
                    </div>
                </div>
                <ChevronRight className="size-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
            </button>
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    onTestAll();
                }}
                disabled={isTesting}
                className={cn(
                    'inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 text-[11px] font-medium transition-colors',
                    isTesting
                        ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                        : 'border-primary/20 bg-primary/10 text-primary hover:bg-primary/20',
                )}
            >
                {isTesting ? <Loader2 className="size-3 animate-spin" /> : <TestTubeDiagonal className="size-3" />}
                {t('groupTestInline.testProvider')}
            </button>
        </div>
    );
}

/* ------------------------------------------------------------------ */
/*  Model chip                                                         */
/* ------------------------------------------------------------------ */

function ModelChip({
    model,
    icon,
    isTesting,
    onTest,
}: {
    model: PublicModel;
    icon: ReturnType<typeof getModelIcon>;
    isTesting: boolean;
    onTest: () => void;
}) {
    const IconAvatar = icon.Avatar;
    return (
        <button
            type="button"
            onClick={onTest}
            disabled={isTesting}
            className={cn(
                'inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors',
                isTesting
                    ? 'cursor-not-allowed opacity-60'
                    : 'border-border/30 bg-card text-muted-foreground hover:border-primary/30 hover:bg-primary/5 hover:text-primary',
            )}
        >
            <span className="flex h-4 w-4 shrink-0 items-center justify-center">
                <IconAvatar size={14} />
            </span>
            <span className="truncate max-w-[12rem]">{model.name}</span>
            <TestTubeDiagonal className="size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-60" />
        </button>
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

    const [open, setOpen] = useState(false);
    const [level, setLevel] = useState<NavLevel>('channels');
    const [selectedChannelId, setSelectedChannelId] = useState<number | null>(null);
    const [selectedProviderKey, setSelectedProviderKey] = useState<string | null>(null);
    const [testEntries, setTestEntries] = useState<ModelTestEntry[]>([]);
    const [isTesting, setIsTesting] = useState(false);

    const isLoading = groupsLoading || overviewLoading;
    const models = useMemo(() => overview?.models ?? [], [overview]);
    const testableGroups = useMemo(
        () => (groups ?? []).filter((g) => g.id !== undefined && (g.items?.length ?? 0) > 0),
        [groups],
    );

    /* ---- Build channel id -> channel display name mapping ---- */
    const channelNameMap = useMemo(() => {
        const map = new Map<number, string>();
        for (const ch of channels ?? []) {
            map.set(ch.raw.id, ch.raw.name);
        }
        return map;
    }, [channels]);

    /* ---- Build channel list with providers ---- */
    const channelList = useMemo((): ChannelInfo[] => {
        const channelMap = new Map<number, Map<string, ProviderInfo>>();

        for (const model of models) {
            const channelEntries = modelChannels?.filter(
                (mc) => mc.name === model.name && mc.enabled
            ) ?? [];

            for (const entry of channelEntries) {
                const icon = getModelIcon(model.name);
                let providerMap = channelMap.get(entry.channel_id);
                if (!providerMap) {
                    providerMap = new Map();
                    channelMap.set(entry.channel_id, providerMap);
                }
                let provider = providerMap.get(icon.label);
                if (!provider) {
                    provider = { provider: icon.label, color: icon.color, Avatar: icon.Avatar, models: [] };
                    providerMap.set(icon.label, provider);
                }
                if (!provider.models.some((m) => m.name === model.name)) {
                    provider.models.push(model);
                }
            }
        }

        const result: ChannelInfo[] = [];
        for (const [channelId, providerMap] of channelMap) {
            const providers = Array.from(providerMap.values());
            const channelName = channelNameMap.get(channelId) ?? `Channel ${channelId}`;
            result.push({
                channelId,
                channelName,
                providers,
                totalModels: providers.reduce((sum, p) => sum + p.models.length, 0),
                totalProviders: providers.length,
            });
        }

        return result.sort((a, b) => a.channelName.localeCompare(b.channelName));
    }, [models, modelChannels, channelNameMap]);

    /* ---- Selected channel's providers ---- */
    const selectedChannel = useMemo(
        () => channelList.find((c) => c.channelId === selectedChannelId),
        [channelList, selectedChannelId],
    );

    /* ---- Selected provider's models ---- */
    const selectedProvider = useMemo(
        () => selectedChannel?.providers.find((p) => p.provider === selectedProviderKey),
        [selectedChannel, selectedProviderKey],
    );

    /* ---- Navigation ---- */
    const navigateTo = useCallback((target: NavLevel) => {
        setLevel(target);
        if (target === 'channels') {
            setSelectedChannelId(null);
            setSelectedProviderKey(null);
        } else if (target === 'providers') {
            setSelectedProviderKey(null);
        }
    }, []);

    const drillIntoChannel = useCallback((channelId: number) => {
        setSelectedChannelId(channelId);
        setSelectedProviderKey(null);
        setLevel('providers');
    }, []);

    const drillIntoProvider = useCallback((providerKey: string) => {
        setSelectedProviderKey(providerKey);
        setLevel('models');
    }, []);

    /* ---- Test execution ---- */
    const runTests = useCallback(async (modelNames: string[]) => {
        if (modelNames.length === 0 || isTesting) return;

        setIsTesting(true);

        const modelGroupMap = new Map<string, { groupId: number; groupName: string }[]>();
        for (const modelName of modelNames) {
            const matchingGroups = findGroupsForModel(modelName, testableGroups);
            if (matchingGroups.length > 0) {
                modelGroupMap.set(modelName, matchingGroups.map((g) => ({ groupId: g.id!, groupName: g.name })));
            }
        }

        const entries: ModelTestEntry[] = [];
        for (const [modelName, groupList] of modelGroupMap) {
            for (const { groupName } of groupList) {
                entries.push({ modelName, groupName, progressId: null });
            }
        }
        setTestEntries((prev) => [...prev, ...entries]);

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

        const results = await Promise.allSettled(
            groupIdsToTest.map(async (groupId) => {
                const progress = await testGroup.mutateAsync(groupId);
                return { groupId, progressId: progress.id };
            }),
        );

        const groupProgressMap = new Map<number, string>();
        for (const result of results) {
            if (result.status === 'fulfilled') {
                groupProgressMap.set(result.value.groupId, result.value.progressId);
            }
        }

        setTestEntries((prev) =>
            prev.map((entry) => {
                if (entry.progressId) return entry;
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

        queryClient.invalidateQueries({ queryKey: ['groups', 'test-history'] });
        queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
    }, [isTesting, testableGroups, testGroup, queryClient]);

    const handleTestChannel = useCallback((channel: ChannelInfo) => {
        const modelNames = channel.providers.flatMap((p) => p.models.map((m) => m.name));
        const unique = [...new Set(modelNames)];
        runTests(unique);
    }, [runTests]);

    const handleTestProvider = useCallback((provider: ProviderInfo) => {
        runTests(provider.models.map((m) => m.name));
    }, [runTests]);

    const handleTestModel = useCallback((modelName: string) => {
        runTests([modelName]);
    }, [runTests]);

    /* ---- Render content based on level ---- */
    const renderContent = () => {
        if (level === 'channels') {
            return (
                <div className="space-y-2">
                    {channelList.length === 0 ? (
                        <div className="flex flex-col items-center justify-center gap-3 py-12">
                            <Server className="size-8 text-muted-foreground/30" />
                            <span className="text-sm text-muted-foreground">{t('groupTestInline.noChannels')}</span>
                        </div>
                    ) : (
                        <div className="space-y-2">
                            {channelList.map((channel) => (
                                <ChannelCard
                                    key={channel.channelId}
                                    channel={channel}
                                    isTesting={isTesting}
                                    onDrill={() => drillIntoChannel(channel.channelId)}
                                    onTestAll={() => handleTestChannel(channel)}
                                    t={t}
                                />
                            ))}
                        </div>
                    )}
                </div>
            );
        }

        if (level === 'providers' && selectedChannel) {
            return (
                <div className="space-y-2">
                    <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                        <Hash className="size-3" />
                        <span>
                            {t('groupTestInline.modelsInChannel', {
                                count: selectedChannel.totalModels,
                                channel: selectedChannel.channelName,
                            })}
                        </span>
                    </div>
                    {selectedChannel.providers.map((provider) => (
                        <ProviderCard
                            key={provider.provider}
                            provider={provider}
                            isTesting={isTesting}
                            onDrill={() => drillIntoProvider(provider.provider)}
                            onTestAll={() => handleTestProvider(provider)}
                            t={t}
                        />
                    ))}
                </div>
            );
        }

        if (level === 'models' && selectedProvider) {
            return (
                <div className="space-y-3">
                    <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                        <Cpu className="size-3" />
                        <span>
                            {t('groupTestInline.modelsInProvider', { count: selectedProvider.models.length })}
                        </span>
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                        {selectedProvider.models.map((model) => {
                            const icon = getModelIcon(model.name);
                            return (
                                <ModelChip
                                    key={model.name}
                                    model={model}
                                    icon={icon}
                                    isTesting={isTesting}
                                    onTest={() => handleTestModel(model.name)}
                                />
                            );
                        })}
                    </div>
                    <button
                        type="button"
                        onClick={() => handleTestProvider(selectedProvider)}
                        disabled={isTesting}
                        className={cn(
                            'inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition-colors',
                            isTesting
                                ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                                : 'border-primary/20 bg-primary text-primary-foreground hover:opacity-90',
                        )}
                    >
                        {isTesting ? <Loader2 className="size-3.5 animate-spin" /> : <TestTubeDiagonal className="size-3.5" />}
                        {isTesting ? t('groupTestInline.testing') : t('groupTestInline.testAll')}
                    </button>
                </div>
            );
        }

        return null;
    };

    /* ---- Main render ---- */
    return (
        <>
            <button
                type="button"
                onClick={() => setOpen(true)}
                disabled={isLoading}
                className={cn(
                    'inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition-colors',
                    isLoading
                        ? 'cursor-not-allowed border-border/25 bg-muted text-muted-foreground'
                        : 'border-primary/20 bg-primary text-primary-foreground hover:opacity-90',
                )}
            >
                {isLoading ? <Loader2 className="size-3.5 animate-spin" /> : <TestTubeDiagonal className="size-3.5" />}
                {t('groupTestInline.startTest')}
            </button>

            <Dialog open={open} onOpenChange={setOpen}>
                <DialogContent className="sm:max-w-2xl">
                    <DialogTitle className="flex items-center gap-2">
                        <TestTubeDiagonal className="size-4" />
                        {t('groupTestInline.startTest')}
                    </DialogTitle>

                    {/* Breadcrumb navigation */}
                    <div className="flex items-center justify-between">
                        <Breadcrumb
                            level={level}
                            channelName={selectedChannel?.channelName}
                            providerName={selectedProvider?.provider}
                            onNavigate={navigateTo}
                            t={t}
                        />
                        {level !== 'channels' && (
                            <button
                                type="button"
                                onClick={() => navigateTo(level === 'models' ? 'providers' : 'channels')}
                                className="inline-flex h-7 items-center gap-1 rounded-md border border-border/25 bg-card px-2 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                            >
                                <ArrowLeft className="size-3" />
                                {t('groupTestInline.back')}
                            </button>
                        )}
                    </div>

                    {/* Main content area */}
                    <div className="max-h-80 overflow-y-auto pr-1">
                        {isLoading ? (
                            <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
                                <Loader2 className="size-4 animate-spin" />
                                {t('states.loading')}
                            </div>
                        ) : models.length === 0 ? (
                            <div className="flex flex-col items-center justify-center gap-3 py-12">
                                <TestTubeDiagonal className="size-6 text-muted-foreground/40" />
                                <span className="text-sm text-muted-foreground">{t('groupTestInline.noGroups')}</span>
                            </div>
                        ) : (
                            renderContent()
                        )}
                    </div>

                    {/* Results area */}
                    {testEntries.length > 0 && (
                        <div className="border-t border-border/25 pt-3">
                            <div className="mb-2 flex items-center justify-between">
                                <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground/80">
                                    {t('groupTestInline.results')}
                                </span>
                                <span className="text-[11px] text-muted-foreground">
                                    {testEntries.filter((e) => e.progressId).length}/{testEntries.length}
                                </span>
                            </div>
                            <div className="max-h-48 space-y-1.5 overflow-y-auto pr-1">
                                {testEntries.map((entry, idx) => (
                                    <ModelTestResultRow key={`${entry.modelName}-${entry.groupName}-${idx}`} entry={entry} />
                                ))}
                            </div>
                        </div>
                    )}
                </DialogContent>
            </Dialog>
        </>
    );
}
