'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { KeyRound, Link2, Save, Check, Server, Globe } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { apiClient } from '@/api/client';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import {
    Select,
    SelectContent,
    SelectGroup,
    SelectItem,
    SelectLabel,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { useModelList, useModelChannelList } from '@/api/endpoints/model';
import { useChannelList, type Channel } from '@/api/endpoints/channel';
import { getModelIcon } from '@/lib/model-icons';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

type Mode = 'external' | 'local';

export function AIRouteConfig({ compact }: { compact?: boolean }) {
    const t = useTranslations('analytics');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const { data: models } = useModelList();
    const { data: channels } = useChannelList();
    const { data: modelChannels } = useModelChannelList();

    const [mode, setMode] = useState<Mode>('external');
    const [baseURL, setBaseURL] = useState('');
    const [apiKey, setAPIKey] = useState('');
    const [model, setModel] = useState('');

    const initialBaseURL = useRef('');
    const initialAPIKey = useRef('');
    const initialModel = useRef('');

    const [saving, setSaving] = useState(false);
    const [justSaved, setJustSaved] = useState(false);
    const [autoChannelName, setAutoChannelName] = useState<string | null>(null);
    const [channelLookupFailed, setChannelLookupFailed] = useState(false);

    // Group models by provider for the dropdown
    const modelsByProvider = useMemo(() => {
        const buckets: Record<string, string[]> = {};
        for (const m of models ?? []) {
            const { label } = getModelIcon(m.name);
            const key = label || 'Other';
            (buckets[key] ??= []).push(m.name);
        }
        return buckets;
    }, [models]);

    // Build a lookup: model name -> channel id
    const modelToChannel = useMemo(() => {
        const map = new Map<number, string>();
        for (const mc of modelChannels ?? []) {
            if (mc.enabled && mc.channel_id) {
                map.set(mc.channel_id, mc.name);
            }
        }
        return map;
    }, [modelChannels]);

    // Load saved settings on mount
    useEffect(() => {
        if (!settings) return;

        const baseURLSetting = settings.find((item) => item.key === SettingKey.AIRouteBaseURL);
        const apiKeySetting = settings.find((item) => item.key === SettingKey.AIRouteAPIKey);
        const modelSetting = settings.find((item) => item.key === SettingKey.AIRouteModel);

        if (baseURLSetting) {
            queueMicrotask(() => setBaseURL(baseURLSetting.value));
            initialBaseURL.current = baseURLSetting.value;
        }
        if (apiKeySetting) {
            queueMicrotask(() => setAPIKey(apiKeySetting.value));
            initialAPIKey.current = apiKeySetting.value;
        }
        if (modelSetting) {
            queueMicrotask(() => setModel(modelSetting.value));
            initialModel.current = modelSetting.value;
        }
    }, [settings]);

    const saveSingle = (key: string, value: string, initialRef: MutableRefObject<string>) => {
        if (value === initialRef.current) return;

        setSetting.mutate(
            { key, value },
            {
                onSuccess: () => {
                    toast.success(t('aiRoute.config.saved'));
                    initialRef.current = value;
                },
            },
        );
    };

    const hasChanges =
        baseURL !== initialBaseURL.current ||
        apiKey !== initialAPIKey.current ||
        model !== initialModel.current;

    const saveAll = () => {
        if (!hasChanges) return;

        setSaving(true);

        const updates: Array<{ key: string; value: string; ref: MutableRefObject<string> }> = [];
        if (baseURL !== initialBaseURL.current) {
            updates.push({ key: SettingKey.AIRouteBaseURL, value: baseURL, ref: initialBaseURL });
        }
        if (apiKey !== initialAPIKey.current) {
            updates.push({ key: SettingKey.AIRouteAPIKey, value: apiKey, ref: initialAPIKey });
        }
        if (model !== initialModel.current) {
            updates.push({ key: SettingKey.AIRouteModel, value: model, ref: initialModel });
        }

        let completed = 0;
        let failed = false;

        for (const update of updates) {
            setSetting.mutate(
                { key: update.key, value: update.value },
                {
                    onSuccess: () => {
                        update.ref.current = update.value;
                        completed++;
                        if (completed === updates.length && !failed) {
                            setSaving(false);
                            setJustSaved(true);
                            toast.success(t('aiRoute.config.saved'));
                            setTimeout(() => setJustSaved(false), 2000);
                        }
                    },
                    onError: () => {
                        failed = true;
                        setSaving(false);
                        toast.error(t('states.empty'));
                    },
                },
            );
        }
    };

    /**
     * Persist base_url, api_key, and model settings after resolving channel details.
     */
    const persistChannelSettings = useCallback(
        (resolvedBaseURL: string, resolvedAPIKey: string, modelName: string) => {
            setBaseURL(resolvedBaseURL);
            setAPIKey(resolvedAPIKey);

            const batchUpdates: Array<{ key: string; value: string; ref: MutableRefObject<string> }> = [];
            if (resolvedBaseURL !== initialBaseURL.current) {
                batchUpdates.push({ key: SettingKey.AIRouteBaseURL, value: resolvedBaseURL, ref: initialBaseURL });
            }
            if (resolvedAPIKey !== initialAPIKey.current) {
                batchUpdates.push({ key: SettingKey.AIRouteAPIKey, value: resolvedAPIKey, ref: initialAPIKey });
            }
            if (modelName !== initialModel.current) {
                batchUpdates.push({ key: SettingKey.AIRouteModel, value: modelName, ref: initialModel });
            }

            if (batchUpdates.length === 0) return;

            let completed = 0;
            let failed = false;

            for (const update of batchUpdates) {
                setSetting.mutate(
                    { key: update.key, value: update.value },
                    {
                        onSuccess: () => {
                            update.ref.current = update.value;
                            completed++;
                            if (completed === batchUpdates.length && !failed) {
                                toast.success(t('aiRoute.config.saved'));
                            }
                        },
                        onError: () => {
                            failed = true;
                            toast.error(t('states.empty'));
                        },
                    },
                );
            }
        },
        [setSetting, t],
    );

    /**
     * Find which channel serves the given model, then auto-fill base_url and api_key
     * and persist all three settings.
     */
    const handleLocalModelSelect = async (modelName: string) => {
        setModel(modelName);
        setChannelLookupFailed(false);

        // Find the channel that serves this model via modelChannels list
        const mc = (modelChannels ?? []).find(
            (item) => item.name === modelName && item.enabled,
        );

        if (!mc) {
            // No channel mapping found; just save the model name
            saveSingle(SettingKey.AIRouteModel, modelName, initialModel);
            setAutoChannelName(null);
            setChannelLookupFailed(true);
            toast.warning(t('aiRoute.config.noChannelFound') || '未找到该模型对应的渠道，base_url 和 api_key 需手动填写');
            return;
        }

        // First try: find the full channel record from the cached channel list
        const channelRecord = (channels ?? []).find(
            (ch) => ch.raw.id === mc.channel_id,
        );
        let channel: Channel | undefined = channelRecord?.raw;

        // Second try: if cached list lookup failed or returned empty base_urls/keys,
        // fetch the channel directly from the API
        if (!channel || (channel.base_urls.length === 0 && channel.keys.length === 0)) {
            try {
                const freshChannel = await apiClient.get<Channel>(`/api/v1/channel/${mc.channel_id}`);
                if (freshChannel) {
                    channel = {
                        ...freshChannel,
                        base_urls: freshChannel.base_urls ?? [],
                        custom_header: freshChannel.custom_header ?? [],
                        keys: freshChannel.keys ?? [],
                    };
                }
            } catch (e) { console.error(e);
                // Direct API fetch failed; will fall through to warning below
            }
        }

        if (!channel) {
            saveSingle(SettingKey.AIRouteModel, modelName, initialModel);
            setAutoChannelName(null);
            setChannelLookupFailed(true);
            toast.warning(t('aiRoute.config.noChannelFound') || '未找到该模型对应的渠道，base_url 和 api_key 需手动填写');
            return;
        }

        const resolvedBaseURL = channel.base_urls?.[0]?.url ?? '';
        const resolvedAPIKey = channel.keys?.[0]?.channel_key ?? '';
        const channelName = channel.name || mc.channel_name;

        setAutoChannelName(channelName);

        if (!resolvedBaseURL || !resolvedAPIKey) {
            // Channel found but base_url or api_key is empty -- still save what we have and warn
            saveSingle(SettingKey.AIRouteModel, modelName, initialModel);
            setChannelLookupFailed(true);
            toast.warning(
                t('aiRoute.config.channelIncomplete') || '渠道信息不完整，base_url 或 api_key 为空，请手动补充',
            );
            return;
        }

        persistChannelSettings(resolvedBaseURL, resolvedAPIKey, modelName);
    };

    const fieldClass = compact ? 'text-sm' : '';
    const labelClass = cn('text-xs font-medium text-muted-foreground', compact && 'text-[11px]');

    return (
        <div className={cn('space-y-3', compact ? 'space-y-2' : 'space-y-3')}>
            {/* Mode toggle */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    {mode === 'external' ? (
                        <Globe className={cn('text-muted-foreground', compact ? 'h-3.5 w-3.5' : 'h-4 w-4')} />
                    ) : (
                        <Server className={cn('text-muted-foreground', compact ? 'h-3.5 w-3.5' : 'h-4 w-4')} />
                    )}
                    <span className={cn('font-medium', compact ? 'text-xs' : 'text-sm')}>
                        {mode === 'external'
                            ? t('aiRoute.config.modeExternal')
                            : t('aiRoute.config.modeLocal')}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <span className={cn('text-muted-foreground', compact ? 'text-[10px]' : 'text-xs')}>
                        {t('aiRoute.config.modeExternal')}
                    </span>
                    <Switch
                        checked={mode === 'local'}
                        onCheckedChange={(checked) => setMode(checked ? 'local' : 'external')}
                        className={compact ? 'scale-75' : ''}
                    />
                    <span className={cn('text-muted-foreground', compact ? 'text-[10px]' : 'text-xs')}>
                        {t('aiRoute.config.modeLocal')}
                    </span>
                </div>
            </div>

            {/* Local mode: model dropdown only, auto-saves via channel lookup */}
            {mode === 'local' && (
                <>
                    <div className="space-y-1.5">
                        <label className={labelClass}>{t('aiRoute.config.model')}</label>
                        <Select value={model} onValueChange={handleLocalModelSelect}>
                            <SelectTrigger className={cn('rounded-lg', compact && 'h-8')}>
                                <SelectValue placeholder={t('aiRoute.config.modelPlaceholder') || 'Select a model'} />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(modelsByProvider).map(([provider, providerModels]) => (
                                    <SelectGroup key={provider}>
                                        <SelectLabel>{provider}</SelectLabel>
                                        {providerModels.map((m) => (
                                            <SelectItem key={m} value={m}>
                                                {m}
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    {autoChannelName && (
                        <p className={cn('text-muted-foreground', compact ? 'text-[10px]' : 'text-xs')}>
                            {t('aiRoute.config.autoChannelNote', { name: autoChannelName })}
                        </p>
                    )}
                    {channelLookupFailed && (
                        <p className={cn('text-amber-600 dark:text-amber-400', compact ? 'text-[10px]' : 'text-xs')}>
                            {t('aiRoute.config.switchToExternal') || '请切换到外部模式手动填写 base_url 和 api_key'}
                        </p>
                    )}
                </>
            )}

            {/* External mode: Base URL, API Key, Model dropdown + Save button */}
            {mode === 'external' && (
                <>
                    <div className="space-y-1.5">
                        <label className={labelClass}>{t('aiRoute.config.baseURL')}</label>
                        <div className="relative">
                            <Link2 className={cn('absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground', compact ? 'h-3.5 w-3.5' : 'h-4 w-4')} />
                            <Input
                                value={baseURL}
                                onChange={(e) => setBaseURL(e.target.value)}
                                onBlur={() => saveSingle(SettingKey.AIRouteBaseURL, baseURL, initialBaseURL)}
                                placeholder="https://api.openai.com/v1"
                                className={cn('rounded-lg pl-9', fieldClass, compact && 'h-8')}
                            />
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <label className={labelClass}>{t('aiRoute.config.apiKey')}</label>
                        <div className="relative">
                            <KeyRound className={cn('absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground', compact ? 'h-3.5 w-3.5' : 'h-4 w-4')} />
                            <Input
                                type="password"
                                value={apiKey}
                                onChange={(e) => setAPIKey(e.target.value)}
                                onBlur={() => saveSingle(SettingKey.AIRouteAPIKey, apiKey, initialAPIKey)}
                                placeholder="sk-..."
                                className={cn('rounded-lg pl-9', fieldClass, compact && 'h-8')}
                            />
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <label className={labelClass}>{t('aiRoute.config.model')}</label>
                        <Select
                            value={model}
                            onValueChange={(v) => {
                                setModel(v);
                                saveSingle(SettingKey.AIRouteModel, v, initialModel);
                            }}
                        >
                            <SelectTrigger className={cn('rounded-lg', compact && 'h-8')}>
                                <SelectValue placeholder={t('aiRoute.config.modelPlaceholder') || 'Select a model'} />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(modelsByProvider).map(([provider, providerModels]) => (
                                    <SelectGroup key={provider}>
                                        <SelectLabel>{provider}</SelectLabel>
                                        {providerModels.map((m) => (
                                            <SelectItem key={m} value={m}>
                                                {m}
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className={cn('flex items-center gap-2', compact ? 'pt-1' : 'pt-2')}>
                        <Button
                            size={compact ? 'sm' : 'default'}
                            onClick={saveAll}
                            disabled={!hasChanges || saving}
                            className={cn(compact && 'h-7 text-xs')}
                        >
                            {justSaved ? (
                                <Check className={cn('mr-1.5', compact ? 'h-3 w-3' : 'h-4 w-4')} />
                            ) : (
                                <Save className={cn('mr-1.5', compact ? 'h-3 w-3' : 'h-4 w-4')} />
                            )}
                            {justSaved ? t('aiRoute.config.saved') : t('aiRoute.config.save')}
                        </Button>
                    </div>
                </>
            )}
        </div>
    );
}
