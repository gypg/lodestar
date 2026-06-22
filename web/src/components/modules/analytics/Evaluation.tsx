'use client';

import { useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Activity, AlertTriangle, Clock, Orbit, Radar, Route, Settings } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { apiClient } from '@/api/client';
import { useAnalyticsEvaluationRuntime } from '@/api/endpoints/analytics';
import { useAIRouteHistory, useGroupList } from '@/api/endpoints/group';
import { useSettingList, SettingKey } from '@/api/endpoints/setting';
import { useNavStore } from '@/components/modules/navbar';
import { Button } from '@/components/ui/button';
import { ObservatorySection, StatusBadge } from './shared';
import { AIRouteConfig } from './AIRouteConfig';
import { GroupTestInline } from './GroupTestInline';

function getStatusTone(status?: string) {
    switch (status) {
        case 'completed':
            return 'success' as const;
        case 'unavailable':
            return 'warning' as const;
        case 'failed':
        case 'timeout':
            return 'danger' as const;
        case 'running':
            return 'warning' as const;
        default:
            return 'neutral' as const;
    }
}

function SummaryStat({ label, value }: { label: string; value: string }) {
    return (
        <div className="rounded-lg border border-border/25 bg-card p-3 shadow-sm">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="mt-2 text-sm font-semibold">{value}</div>
        </div>
    );
}

function ConfigWarning({ message, onNavigate }: { message: string; onNavigate: () => void }) {
    return (
        <div className="mt-3 flex items-center gap-2 rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2">
            <AlertTriangle className="size-4 shrink-0 text-amber-500" />
            <span className="min-w-0 flex-1 text-xs text-amber-700 dark:text-amber-300">{message}</span>
            <Button variant="ghost" size="sm" className="shrink-0 text-xs" onClick={onNavigate}>
                <Settings className="size-3" />
                设置
            </Button>
        </div>
    );
}

function EntryCard({
    icon: Icon,
    title,
    description,
    hint,
    status,
    stats,
    warning,
    action,
}: {
    icon: typeof Activity;
    title: string;
    description: string;
    hint: string;
    status?: { label: string; tone: 'success' | 'warning' | 'danger' | 'neutral' };
    stats?: Array<{ label: string; value: string }>;
    warning?: { message: string; onNavigate: () => void };
    action: ReactNode;
}) {
    return (
        <article className="rounded-lg border border-border/30 bg-card p-4">
            <div className="flex items-start justify-between gap-3">
                <div className="grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-border/25 bg-card text-primary">
                    <Icon className="h-4 w-4" />
                </div>
                {status ? <StatusBadge label={status.label} tone={status.tone} /> : null}
            </div>
            <div className="mt-4 space-y-2">
                <h4 className="text-sm font-semibold">{title}</h4>
                <p className="text-sm leading-6 text-muted-foreground">{description}</p>
                <div className="rounded-lg border border-border/20 bg-card px-3 py-2 text-sm text-muted-foreground">
                    {hint}
                </div>
            </div>
            {stats && stats.length > 0 ? (
                <div className="mt-3 grid grid-cols-2 gap-2">
                    {stats.map((s) => (
                        <SummaryStat key={s.label} label={s.label} value={s.value} />
                    ))}
                </div>
            ) : null}
            {warning ? <ConfigWarning message={warning.message} onNavigate={warning.onNavigate} /> : null}
            <div className="mt-4">{action}</div>
        </article>
    );
}

export function Evaluation() {
    const t = useTranslations('analytics');
    const { setActiveItem } = useNavStore();
    const sectionDescription = t('evaluation.description');
    const runtime = useAnalyticsEvaluationRuntime();
    const { data: groups } = useGroupList();
    const { data: settings } = useSettingList();
    const { data: aiRouteHistory } = useAIRouteHistory();

    // Check if /api/v1/group/test/history exists
    const { data: groupTestHistory } = useQuery({
        queryKey: ['groups', 'test-history'],
        queryFn: async () => apiClient.get('/api/v1/group/test/history'),
        staleTime: 30_000,
        retry: false,
    });

    const [showAiConfig, setShowAiConfig] = useState(false);
    const aiRoute = runtime.aiRouteProgress;
    const groupTest = runtime.groupTestProgress;
    const passedCount = (groupTest?.results ?? []).filter((result) => result.passed).length;
    const failedCount = (groupTest?.results ?? []).filter((result) => !result.passed).length;
    const hasAiRouteUnavailable = Boolean(runtime.aiRouteTask && runtime.aiRouteError && !aiRoute);
    const hasGroupTestUnavailable = Boolean(runtime.groupTestTask && runtime.groupTestError && !groupTest);
    const groupTestHasFailures = failedCount > 0 || Boolean(groupTest?.message);
    const aiRouteStatus = hasAiRouteUnavailable ? 'unavailable' : (aiRoute?.status ?? 'idle');
    const aiRouteStep = aiRoute?.current_step ?? 'idle';
    const groupTestStatus = groupTest
        ? (groupTest.done ? (groupTestHasFailures ? 'failed' : 'completed') : 'running')
        : hasGroupTestUnavailable
            ? 'unavailable'
            : 'idle';
    const groupTestResultLabel = !groupTest
        ? t('evaluation.summary.empty')
        : !groupTest.done
            ? t('evaluation.runtime.status.running')
            : groupTestHasFailures
                ? t('evaluation.summary.partialFailed')
                : t('evaluation.summary.allPassed');
    // Group statistics
    const groupStats = useMemo(() => {
        if (!groups) return null;
        const total = groups.length;
        const withItems = groups.filter((g) => (g.items?.length ?? 0) > 0).length;
        const empty = total - withItems;
        const endpointTypes = new Set(groups.map((g) => g.endpoint_type).filter(Boolean));
        return { total, withItems, empty, endpointTypes: endpointTypes.size };
    }, [groups]);

    // AI route configuration check
    const aiRouteConfigured = useMemo(() => {
        if (!settings) return false;
        const baseURL = settings.find((s) => s.key === SettingKey.AIRouteBaseURL)?.value?.trim();
        const apiKey = settings.find((s) => s.key === SettingKey.AIRouteAPIKey)?.value?.trim();
        const model = settings.find((s) => s.key === SettingKey.AIRouteModel)?.value?.trim();
        return Boolean(baseURL && apiKey && model);
    }, [settings]);

    const lastAiRouteTask = aiRouteHistory?.[0];

    return (
        <ObservatorySection
            eyebrow={t('evaluation.title')}
            title={t('evaluation.title')}
            description={sectionDescription}
            icon={Radar}
        >
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {/* Group Detection Card */}
                <EntryCard
                    icon={Activity}
                    title={t('evaluation.availability.title')}
                    description={t('evaluation.availability.description')}
                    hint={runtime.isLoading
                        ? t('states.loading')
                        : runtime.hasGroups
                            ? t('evaluation.availability.hint', { count: runtime.groupCount })
                            : t('evaluation.availability.empty')}
                    stats={groupStats ? [
                        { label: t('evaluation.groupStats.total') || '总分组', value: String(groupStats.total) },
                        { label: t('evaluation.groupStats.active') || '有成员', value: String(groupStats.withItems) },
                        { label: t('evaluation.groupStats.empty') || '空分组', value: String(groupStats.empty) },
                        { label: t('evaluation.groupStats.endpointTypes') || '端点类型', value: String(groupStats.endpointTypes) },
                    ] : undefined}
                    status={groupTest
                        ? { label: groupTestResultLabel, tone: getStatusTone(groupTest.done ? (groupTestHasFailures ? 'failed' : 'completed') : 'running') }
                        : undefined}
                    action={<GroupTestInline />}
                />

                {/* AI Route Card */}
                <EntryCard
                    icon={Route}
                    title={t('evaluation.aiRoute.title')}
                    description={t('evaluation.aiRoute.description')}
                    hint={aiRoute
                        ? t('evaluation.aiRoute.hint', { step: t(`evaluation.runtime.step.${aiRouteStep}`) })
                        : !aiRouteConfigured
                            ? t('evaluation.aiRoute.notConfigured') || 'AI 路由分析需要先配置分析模型（设置 → AI 路由）'
                            : hasAiRouteUnavailable
                                ? t('evaluation.aiRoute.unavailable')
                                : t('evaluation.aiRoute.empty')}
                    status={{
                        label: !aiRouteConfigured
                            ? (t('evaluation.aiRoute.needConfig') || '需配置')
                            : t(`evaluation.runtime.status.${aiRouteStatus}`),
                        tone: !aiRouteConfigured ? 'warning' : getStatusTone(aiRouteStatus),
                    }}
                    stats={lastAiRouteTask?.result ? [
                        { label: t('evaluation.summary.groups') || '分组', value: String(lastAiRouteTask.result.group_count ?? 0) },
                        { label: t('evaluation.summary.routes') || '路由', value: `${lastAiRouteTask.result.route_count ?? 0} / ${lastAiRouteTask.result.item_count ?? 0}` },
                        { label: t('evaluation.summary.lastRun') || '上次运行', value: lastAiRouteTask.finished_at ? new Date(lastAiRouteTask.finished_at).toLocaleString() : '-' },
                        { label: t('evaluation.summary.status') || '状态', value: lastAiRouteTask.status ?? '-' },
                    ] : undefined}
                    action={
                        !aiRouteConfigured ? (
                            <AIRouteConfig compact />
                        ) : (
                            <div className="space-y-2">
                                {lastAiRouteTask?.result ? (
                                    <div className="rounded-lg border border-border/20 bg-card px-3 py-2 text-sm text-muted-foreground">
                                        {t('evaluation.summary.groups') || '分组'}: {lastAiRouteTask.result.group_count ?? 0} | {t('evaluation.summary.routes') || '路由'}: {lastAiRouteTask.result.route_count ?? 0} / {lastAiRouteTask.result.item_count ?? 0}
                                    </div>
                                ) : null}
                                {showAiConfig ? <AIRouteConfig compact /> : null}
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="rounded-lg"
                                    onClick={() => setShowAiConfig((prev) => !prev)}
                                >
                                    <Settings className="size-3" />
                                    {showAiConfig
                                        ? (t('evaluation.actions.closeConfig') || '收起配置')
                                        : (t('evaluation.actions.editConfig') || '编辑配置')}
                                </Button>
                            </div>
                        )
                    }
                />
            </div>

            <div className="mt-4 space-y-4">
                <div className="rounded-lg border border-dashed border-border/30 bg-card p-4">
                    <div className="mb-2 inline-flex items-center gap-2 rounded-full border border-primary/10 bg-card px-3 py-1 text-[0.68rem] font-semibold text-primary">
                        <Orbit className="h-3.5 w-3.5" />
                        {t('evaluation.summary.title')}
                    </div>
                    <p className="mt-1 text-sm leading-6 text-muted-foreground">{t('evaluation.summary.description')}</p>
                </div>

                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <article className="rounded-lg border border-border/30 bg-card p-4">
                        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <Route className="h-4 w-4" />
                        </div>
                        <div className="mt-4 flex items-center justify-between gap-3">
                            <h4 className="text-sm font-semibold">{t('evaluation.summary.aiRoute')}</h4>
                            <StatusBadge
                                label={!aiRouteConfigured ? (t('evaluation.aiRoute.needConfig') || '需配置') : t(`evaluation.runtime.status.${aiRouteStatus}`)}
                                tone={!aiRouteConfigured ? 'warning' : getStatusTone(aiRouteStatus)}
                            />
                        </div>
                        {aiRoute ? (
                            <div className="mt-4 grid grid-cols-2 gap-3">
                                <SummaryStat
                                    label={t('evaluation.summary.status')}
                                    value={t(`evaluation.runtime.step.${aiRouteStep}`)}
                                />
                                <SummaryStat
                                    label={t('evaluation.summary.progress')}
                                    value={`${aiRoute.completed_batches} / ${aiRoute.total_batches}`}
                                />
                                <SummaryStat
                                    label={t('evaluation.summary.groups')}
                                    value={String(aiRoute.result?.group_count ?? 0)}
                                />
                                <SummaryStat
                                    label={t('evaluation.summary.routes')}
                                    value={`${aiRoute.result?.route_count ?? 0} / ${aiRoute.result?.item_count ?? 0}`}
                                />
                            </div>
                        ) : !aiRouteConfigured ? (
                            <div className="mt-4 space-y-3">
                                <div className="rounded-lg border border-amber-500/25 bg-amber-500/5 px-4 py-3 text-sm text-amber-700 dark:text-amber-300">
                                    {t('evaluation.aiRoute.configWarning') || '请先在设置页面配置 AI 路由的 Base URL、API Key 和分析模型，否则无法执行分析。'}
                                </div>
                                <Button variant="outline" size="sm" className="rounded-lg" onClick={() => setActiveItem('setting')}>
                                    <Settings className="size-4" />
                                    {t('evaluation.actions.goToSettings') || '前往设置'}
                                </Button>
                            </div>
                        ) : (
                            <div className="mt-4 rounded-lg border border-border/20 bg-card px-4 py-3 text-sm leading-6 text-muted-foreground">
                                {t('evaluation.aiRoute.empty')}
                            </div>
                        )}
                    </article>

                    <article className="rounded-lg border border-border/30 bg-card p-4">
                        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <Activity className="h-4 w-4" />
                        </div>
                        <div className="mt-4 flex items-center justify-between gap-3">
                            <h4 className="text-sm font-semibold">{t('evaluation.summary.groupTest')}</h4>
                            <StatusBadge
                                label={t(`evaluation.runtime.status.${groupTestStatus}`)}
                                tone={getStatusTone(groupTestStatus)}
                            />
                        </div>
                        {groupTest ? (
                            <>
                                <div className="mt-4 grid grid-cols-2 gap-3">
                                    <SummaryStat
                                        label={t('evaluation.summary.progress')}
                                        value={`${groupTest.completed} / ${groupTest.total}`}
                                    />
                                    <SummaryStat
                                        label={t('evaluation.summary.result')}
                                        value={groupTestResultLabel}
                                    />
                                    <SummaryStat
                                        label={t('evaluation.summary.passed')}
                                        value={String(passedCount)}
                                    />
                                    <SummaryStat
                                        label={t('evaluation.summary.failed')}
                                        value={String(failedCount)}
                                    />
                                </div>
                                {groupTest.message ? (
                                    <p className="mt-3 text-sm leading-6 text-destructive">{groupTest.message}</p>
                                ) : null}
                            </>
                        ) : (
                            <div className="mt-4 rounded-lg border border-border/20 bg-card px-4 py-3 text-sm leading-6 text-muted-foreground">
                                {hasGroupTestUnavailable
                                    ? t('evaluation.summary.unavailable')
                                    : t('evaluation.summary.empty')}
                            </div>
                        )}
                    </article>
                </div>
            </div>

            {(aiRouteHistory && aiRouteHistory.length > 0) || (groupTestHistory && Array.isArray(groupTestHistory) && groupTestHistory.length > 0) || groupTest ? (
                <div className="mt-4 space-y-3">
                    <div className="inline-flex items-center gap-2 rounded-full border border-primary/10 bg-card px-3 py-1 text-[0.68rem] font-semibold text-primary">
                        <Clock className="h-3.5 w-3.5" />
                        {t('evaluation.history.title') || 'Recent History'}
                    </div>
                    <div className="space-y-2">
                        {/* Current runtime group test result */}
                        {groupTest && (
                            <div className="flex items-center justify-between rounded-lg border border-border/30 bg-card p-3">
                                <div className="flex items-center gap-3">
                                    <StatusBadge
                                        label={groupTest.done ? (groupTestHasFailures ? 'failed' : 'completed') : 'running'}
                                        tone={getStatusTone(groupTest.done ? (groupTestHasFailures ? 'failed' : 'completed') : 'running')}
                                    />
                                    <span className="text-sm text-muted-foreground">
                                        {t('evaluation.summary.groupTest') || 'Group Test'}
                                    </span>
                                    <span className="text-xs text-muted-foreground">
                                        {passedCount} passed / {failedCount} failed ({groupTest.completed}/{groupTest.total})
                                    </span>
                                </div>
                                {groupTest.message ? (
                                    <span className="text-xs text-destructive">{groupTest.message}</span>
                                ) : null}
                            </div>
                        )}
                        {aiRouteHistory?.map((task) => {
                            const date = task.finished_at ? new Date(task.finished_at).toLocaleString() : '';
                            return (
                                <div key={`ai-${task.id}`} className="flex items-center justify-between rounded-lg border border-border/30 bg-card p-3">
                                    <div className="flex items-center gap-3">
                                        <StatusBadge
                                            label={t(`evaluation.runtime.status.${task.status}`)}
                                            tone={getStatusTone(task.status)}
                                        />
                                        <span className="text-sm text-muted-foreground">{date}</span>
                                        {task.result ? (
                                            <span className="text-xs text-muted-foreground">
                                                {task.result.group_count ?? 0} groups / {task.result.route_count ?? 0} routes
                                            </span>
                                        ) : null}
                                    </div>
                                    {task.error_reason ? (
                                        <span className="text-xs text-destructive">{task.error_reason}</span>
                                    ) : null}
                                </div>
                            );
                        })}
                        {Array.isArray(groupTestHistory) && groupTestHistory.map((record: Record<string, unknown>, idx: number) => {
                            const date = record.finished_at ? new Date(record.finished_at as string).toLocaleString() : '';
                            return (
                                <div key={`gt-${String(record.id ?? idx)}`} className="flex items-center justify-between rounded-lg border border-border/30 bg-card p-3">
                                    <div className="flex items-center gap-3">
                                        <StatusBadge
                                            label={String(record.status ?? 'unknown')}
                                            tone={getStatusTone(record.status as string)}
                                        />
                                        <span className="text-sm text-muted-foreground">{date}</span>
                                        {record.passed !== undefined ? (
                                            <span className="text-xs text-muted-foreground">
                                                {String(record.passed)} passed
                                            </span>
                                        ) : null}
                                    </div>
                                    {record.error_reason ? (
                                        <span className="text-xs text-destructive">{String(record.error_reason)}</span>
                                    ) : null}
                                </div>
                            );
                        })}
                    </div>
                </div>
            ) : null}
        </ObservatorySection>
    );
}
