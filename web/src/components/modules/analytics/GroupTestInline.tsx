'use client';

import { useState, useCallback, useMemo } from 'react';
import { Loader2, CircleCheck, CircleX, TestTubeDiagonal, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useGroupList, useTestGroup, useGroupTestProgress } from '@/api/endpoints/group';
import { cn } from '@/lib/utils';
import { Progress } from '@/components/ui/progress';

interface GroupTestEntry {
    groupId: number;
    groupName: string;
    progressId: string | null;
}

function GroupTestResultRow({ entry }: { entry: GroupTestEntry }) {
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
                <div className="truncate text-sm font-medium">{entry.groupName}</div>
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

function GroupChip({
    group,
    isSelected,
    isTesting,
    onToggle,
}: {
    group: { id: number; name: string; endpoint_type?: string };
    isSelected: boolean;
    isTesting: boolean;
    onToggle: () => void;
}) {
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
            <span className="truncate max-w-[10rem]">{group.name}</span>
            {group.endpoint_type && (
                <span className={cn(
                    'rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none',
                    isSelected
                        ? 'bg-primary/15 text-primary'
                        : 'bg-muted-foreground/10 text-muted-foreground/70',
                )}>
                    {group.endpoint_type}
                </span>
            )}
            {isSelected && <X className="size-3 shrink-0 opacity-60" />}
        </button>
    );
}

export function GroupTestInline() {
    const t = useTranslations('analytics');
    const { data: groups, isLoading } = useGroupList();
    const testGroup = useTestGroup();

    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
    const [testEntries, setTestEntries] = useState<GroupTestEntry[]>([]);
    const [isTesting, setIsTesting] = useState(false);

    const testableGroups = useMemo(
        () => (groups ?? []).filter((g) => g.id !== undefined && (g.items?.length ?? 0) > 0),
        [groups],
    );

    const allSelected = testableGroups.length > 0 && testableGroups.every((g) => selectedIds.has(g.id!));
    const hasSelection = selectedIds.size > 0;

    const toggleGroup = useCallback((id: number) => {
        setSelectedIds((prev) => {
            const next = new Set(prev);
            if (next.has(id)) {
                next.delete(id);
            } else {
                next.add(id);
            }
            return next;
        });
    }, []);

    const selectAll = useCallback(() => {
        setSelectedIds(new Set(testableGroups.map((g) => g.id!)));
    }, [testableGroups]);

    const deselectAll = useCallback(() => {
        setSelectedIds(new Set());
    }, []);

    const runTests = useCallback(async (groupIds: number[]) => {
        if (groupIds.length === 0 || isTesting) return;

        setIsTesting(true);
        const entries: GroupTestEntry[] = groupIds.map((id) => ({
            groupId: id,
            groupName: testableGroups.find((g) => g.id === id)?.name ?? `#${id}`,
            progressId: null,
        }));
        setTestEntries(entries);

        const results = await Promise.allSettled(
            groupIds.map(async (groupId) => {
                const progress = await testGroup.mutateAsync(groupId);
                return { groupId, progressId: progress.id };
            }),
        );

        setTestEntries((prev) => {
            const next = [...prev];
            for (const result of results) {
                if (result.status === 'fulfilled') {
                    const idx = next.findIndex((e) => e.groupId === result.value.groupId);
                    if (idx !== -1) {
                        next[idx] = { ...next[idx], progressId: result.value.progressId };
                    }
                } else {
                    const idx = next.findIndex((e) => e.groupId === (result as PromiseRejectedResult).reason?.groupId);
                    if (idx !== -1) {
                        // leave progressId null; mark done via message
                    }
                }
            }
            return next;
        });

        setIsTesting(false);
    }, [isTesting, testableGroups, testGroup]);

    const handleTestAll = useCallback(() => {
        runTests(testableGroups.map((g) => g.id!));
    }, [runTests, testableGroups]);

    const handleTestSelected = useCallback(() => {
        runTests(Array.from(selectedIds));
    }, [runTests, selectedIds]);

    if (isLoading) {
        return (
            <div className="flex items-center justify-center gap-2 rounded-lg border border-border/25 bg-card p-8 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                {t('states.loading')}
            </div>
        );
    }

    if (testableGroups.length === 0) {
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
                        {selectedIds.size}/{testableGroups.length}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={handleTestAll}
                        disabled={isTesting || testableGroups.length === 0}
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

            {/* Tag/chip selection area */}
            <div className="flex flex-wrap gap-2">
                {testableGroups.map((group) => (
                    <GroupChip
                        key={group.id!}
                        group={{ id: group.id!, name: group.name, endpoint_type: group.endpoint_type }}
                        isSelected={selectedIds.has(group.id!)}
                        isTesting={isTesting}
                        onToggle={() => toggleGroup(group.id!)}
                    />
                ))}
            </div>

            {/* Results area */}
            {testEntries.length > 0 && (
                <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
                    {testEntries.map((entry) => (
                        <GroupTestResultRow key={entry.groupId} entry={entry} />
                    ))}
                </div>
            )}
        </div>
    );
}
