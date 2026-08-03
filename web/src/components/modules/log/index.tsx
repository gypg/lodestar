'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useChannelList } from '@/api/endpoints/channel';
import { useAPIKeyList } from '@/api/endpoints/apikey';
import { useLogs, type LogFilter } from '@/api/endpoints/log';
import { LogCard } from './Item';
import { Loader2, X, Columns3 } from 'lucide-react';
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover';
import { useLogFieldVisibilityStore, useLogModelSearchStore, type LogFieldName } from './ui-store';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useNavHandoff } from '@/lib/nav-handoff';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { ENDPOINT_TYPE_OPTIONS } from '@/components/modules/group/utils';

const EMPTY_FILTER: LogFilter = {};

/**
 * 日志筛选栏
 */
function LogFilterBar({
    filter,
    onChange,
}: {
    filter: LogFilter;
    onChange: (f: LogFilter) => void;
}) {
    const t = useTranslations('log.filter');
    const tGroup = useTranslations('group');
    const tView = useTranslations('log.viewOptions');
    const { data: channels = [] } = useChannelList();
    const { data: apiKeys = [] } = useAPIKeyList();
    const visibility = useLogFieldVisibilityStore((s) => s.visibility);

    const hasFilter = !!(
        filter.channel_id != null ||
        filter.api_key_id != null ||
        filter.endpoint_type ||
        filter.status ||
        filter.is_test != null
    );

    const setModelSearch = useLogModelSearchStore((s) => s.setModelSearch);
    const handleClear = useCallback(() => {
        onChange(EMPTY_FILTER);
        setModelSearch('');
    }, [onChange, setModelSearch]);

    return (
        <div className="flex flex-nowrap items-center gap-2 overflow-x-auto py-1">
            <Select
                value={filter.channel_id != null ? String(filter.channel_id) : ''}
                onValueChange={(v) => {
                    const next = { ...filter };
                    if (v && v !== '' && v !== '__all__') {
                        next.channel_id = Number(v);
                    } else {
                        delete next.channel_id;
                    }
                    onChange(next);
                }}
            >
                <SelectTrigger size="sm" className="h-7 text-xs min-w-[7rem]">
                    <SelectValue placeholder={t('allChannels')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="__all__">{t('allChannels')}</SelectItem>
                    {channels.map((ch) => (
                        <SelectItem key={ch.raw.id} value={String(ch.raw.id)}>
                            {ch.raw.name}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            <Select
                value={filter.api_key_id != null ? String(filter.api_key_id) : ''}
                onValueChange={(v) => {
                    const next = { ...filter };
                    if (v && v !== '' && v !== '__all__') {
                        next.api_key_id = Number(v);
                    } else {
                        delete next.api_key_id;
                    }
                    onChange(next);
                }}
            >
                <SelectTrigger size="sm" className="h-7 text-xs min-w-[7rem]">
                    <SelectValue placeholder={t('allKeys')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="__all__">{t('allKeys')}</SelectItem>
                    {apiKeys.map((key) => (
                        <SelectItem key={key.id} value={String(key.id)}>
                            {key.name}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            <Select
                value={filter.endpoint_type ?? ''}
                onValueChange={(v) => {
                    const next = { ...filter };
                    if (v && v !== '' && v !== '__all__') {
                        next.endpoint_type = v;
                    } else {
                        delete next.endpoint_type;
                    }
                    onChange(next);
                }}
            >
                <SelectTrigger size="sm" className="h-7 text-xs min-w-[7rem]">
                    <SelectValue placeholder={t('allTypes')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="__all__">{t('allTypes')}</SelectItem>
                    {ENDPOINT_TYPE_OPTIONS.filter((o) => o.value !== '*').map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                            {tGroup(opt.labelKey)}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            <Select
                value={filter.status ?? ''}
                onValueChange={(v) => {
                    const next = { ...filter };
                    if (v && v !== '' && v !== '__all__') {
                        next.status = v as 'success' | 'error';
                    } else {
                        delete next.status;
                    }
                    onChange(next);
                }}
            >
                <SelectTrigger size="sm" className="h-7 text-xs min-w-[6rem]">
                    <SelectValue placeholder={t('allStatuses')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="__all__">{t('allStatuses')}</SelectItem>
                    <SelectItem value="success">{t('statusSuccess')}</SelectItem>
                    <SelectItem value="error">{t('statusError')}</SelectItem>
                </SelectContent>
            </Select>

            <Select
                value={filter.is_test != null ? String(filter.is_test) : ''}
                onValueChange={(v) => {
                    const next = { ...filter };
                    if (v === 'true') {
                        next.is_test = true;
                    } else if (v === 'false') {
                        next.is_test = false;
                    } else {
                        delete next.is_test;
                    }
                    onChange(next);
                }}
            >
                <SelectTrigger size="sm" className="h-7 text-xs min-w-[6rem]">
                    <SelectValue placeholder={t('allLogs')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="__all__">{t('allLogs')}</SelectItem>
                    <SelectItem value="true">{t('testOnly')}</SelectItem>
                    <SelectItem value="false">{t('nonTest')}</SelectItem>
                </SelectContent>
            </Select>

            {hasFilter && (
                <button
                    type="button"
                    onClick={handleClear}
                    className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                >
                    <X className="size-3" />
                    {t('clear')}
                </button>
            )}

            <Popover>
                <PopoverTrigger asChild>
                    <button
                        type="button"
                        className="flex items-center gap-1 rounded-md p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                        title={tView('title')}
                    >
                        <Columns3 className="size-3.5" />
                    </button>
                </PopoverTrigger>
                <PopoverContent align="end" className="w-52 p-3">
                    <p className="text-xs font-medium text-muted-foreground mb-2">{tView('title')}</p>
                    <div className="flex flex-col gap-1">
                        {(['endpointType', 'channelName', 'actualModel', 'apiKeyName', 'clientIP', 'cost'] as LogFieldName[]).map((field) => (
                            <label key={field} className="flex items-center gap-2 cursor-pointer rounded px-1.5 py-1 text-xs hover:bg-muted transition-colors">
                                <input
                                    type="checkbox"
                                    checked={visibility[field]}
                                    onChange={() => useLogFieldVisibilityStore.getState().toggleField(field)}
                                    className="size-3 rounded"
                                />
                                {tView(field)}
                            </label>
                        ))}
                    </div>
                    <button
                        type="button"
                        onClick={() => useLogFieldVisibilityStore.getState().resetFields()}
                        className="mt-2 w-full rounded-md border border-border/50 px-2 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                    >
                        {tView('reset')}
                    </button>
                </PopoverContent>
            </Popover>
        </div>
    );
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志
 * - 滚动自动加载更多
 * - 筛选（模型、渠道、密钥、端点类型、状态）
 */
export function Log() {
    const t = useTranslations('log');
    const [filter, setFilter] = useState<LogFilter>(EMPTY_FILTER);
    const modelSearch = useLogModelSearchStore((s) => s.modelSearch);
    const setModelSearch = useLogModelSearchStore((s) => s.setModelSearch);
    const combinedFilter = useMemo<LogFilter>(
        () => (modelSearch ? { ...filter, model: modelSearch } : filter),
        [filter, modelSearch],
    );
    const { logs, hasMore, isLoading, isLoadingMore, loadMore } = useLogs({ filter: combinedFilter });
    const { data: channels = [] } = useChannelList();

    // 消费来自其它模块（分析/分组健康）的待处理筛选，实现"点击失败渠道 → 跳转日志并预填"。
    const pendingLogFilter = useNavHandoff((s) => s.pendingLogFilter);
    const consumePendingLogFilter = useNavHandoff((s) => s.consumePendingLogFilter);
    useEffect(() => {
        const pending = consumePendingLogFilter();
        if (pending) {
            const { model, ...rest } = pending;
            setFilter(rest);
            if (model) setModelSearch(model);
        }
    }, [pendingLogFilter, consumePendingLogFilter, setModelSearch]);

    const channelNameById = useMemo(() => {
        const map = new Map<number, string>();
        for (const item of channels) {
            map.set(item.raw.id, item.raw.name);
        }
        return map;
    }, [channels]);

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && logs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const footer = useMemo(() => {
        if (hasMore && (isLoading || isLoadingMore)) {
            return (
                <div className="flex justify-center py-6">
                    <div className="flex items-center gap-2 rounded-full border border-border/50 bg-card/80 px-4 py-2 shadow-sm backdrop-blur">
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        <span className="text-xs text-muted-foreground">{t('list.loadingMore')}</span>
                    </div>
                </div>
            );
        }
        if (!hasMore && logs.length > 0) {
            return (
                <div className="flex justify-center py-6">
                    <span className="text-xs text-muted-foreground/60">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, isLoading, isLoadingMore, logs.length, t]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-2 overflow-hidden">
            <LogFilterBar filter={filter} onChange={setFilter} />
            {isLoading && logs.length === 0 ? (
                <div className="flex min-h-[18rem] items-center justify-center rounded-xl border border-border/35 bg-card">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            ) : logs.length === 0 ? (
                <div className="flex min-h-[18rem] items-center justify-center rounded-xl border border-dashed border-border/35 bg-card px-6 py-6 text-center">
                    <p className="text-sm text-muted-foreground">{t('list.empty')}</p>
                </div>
            ) : (
                <div className="min-h-0 flex-1">
                    <VirtualizedGrid
                        items={logs}
                        layout="list"
                        columns={{ default: 1 }}
                        estimateItemHeight={180}
                        overscan={8}
                        getItemKey={(log) => `log-${log.id}`}
                        renderItem={(log) => <LogCard log={log} channelNameById={channelNameById} />}
                        footer={footer}
                        onReachEnd={handleReachEnd}
                        reachEndEnabled={canLoadMore}
                        reachEndOffset={2}
                        bottomPaddingClassName="pb-16 md:pb-4"
                    />
                </div>
            )}
        </div>
    );
}
