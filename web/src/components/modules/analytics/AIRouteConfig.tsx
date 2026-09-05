'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { KeyRound, Link2, Save, Check, Server, Globe, Zap } from 'lucide-react';
import { useTranslations } from 'next-intl';
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
import { useModelList, useModelChannelList, useModelMarket } from '@/api/endpoints/model';
import { getModelIcon } from '@/lib/model-icons';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { resolveAIRouteSourceMode, isLocalSetupIncomplete, type AIRouteSourceMode } from './ai-route-source-mode';

type Mode = AIRouteSourceMode;

export function AIRouteConfig({ compact }: { compact?: boolean }) {
    const t = useTranslations('analytics');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const { data: models } = useModelList();
    const { data: modelChannels } = useModelChannelList();
    const { data: modelMarket } = useModelMarket();

    const [mode, setMode] = useState<Mode>('external');
    const [baseURL, setBaseURL] = useState('');
    const [apiKey, setAPIKey] = useState('');
    const [model, setModel] = useState('');

    const initialBaseURL = useRef('');
    const initialAPIKey = useRef('');
    const initialModel = useRef('');
    // Tracks the persisted mode so a reload of the settings query does not undo
    // a toggle the operator just made, and so the toggle is only written when it
    // actually changes.
    const initialMode = useRef<Mode | null>(null);

    const [saving, setSaving] = useState(false);
    const [justSaved, setJustSaved] = useState(false);
    const [channelLookupFailed, setChannelLookupFailed] = useState(false);
    // Local mode auto-fills the lowest-latency available model once per visit;
    // after that the operator's own choice (or the save button) is in charge.
    const autoPickedRef = useRef(false);

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
        // Restore the source toggle. Without this the switch reset to "external"
        // on every mount while the model name loaded back from settings, so a
        // local-mode setup rendered as an external one and read as the toggle
        // flipping itself. Only applied once: later refetches of the settings
        // list must not overwrite a toggle the operator just moved.
        if (initialMode.current === null) {
            const persisted = resolveAIRouteSourceMode(settings);
            initialMode.current = persisted;
            queueMicrotask(() => setMode(persisted));
        }
    }, [settings]);

    /**
     * Persist the source toggle. Kept separate from the credential fields
     * because it is written on interaction rather than on save, and because a
     * failure here has to surface: a toggle that silently fails to persist
     * reappears on the other setting after a reload.
     */
    const handleModeChange = (next: Mode) => {
        setMode(next);
        if (initialMode.current === next) return;
        setSetting.mutate(
            { key: SettingKey.AIRouteSourceMode, value: next },
            {
                onSuccess: () => {
                    initialMode.current = next;
                },
                onError: () => {
                    toast.error(t('states.empty'));
                },
            },
        );
    };

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
     * Whether an enabled channel serves the given model. In local mode the
     * backend derives base_url/api_key from exactly that channel, so this — not
     * hand-filled credentials — is what makes a picked model runnable.
     */
    const modelHasEnabledChannel = useCallback(
        (name: string) => {
            const trimmed = name.trim();
            if (!trimmed) return false;
            return (modelChannels ?? []).some((item) => item.name === trimmed && item.enabled);
        },
        [modelChannels],
    );

    /**
     * Pick the lowest-latency runnable model from the model market: served by an
     * enabled channel, not flagged by the scheduled probe, sorted by measured
     * latency. Models without latency data only back up the sort when nothing
     * has been measured yet.
     */
    const autoPickLocalModel = useCallback((): string | null => {
        const candidates = (modelMarket?.items ?? []).filter(
            (item) => !item.probe_failed_at && item.channels.some((c) => c.enabled),
        );
        if (candidates.length === 0) return null;
        const withLatency = candidates
            .filter((item) => item.average_latency_ms > 0)
            .sort((a, b) => a.average_latency_ms - b.average_latency_ms);
        const best = withLatency[0] ?? candidates[0];
        return best?.name ?? null;
    }, [modelMarket]);

    /** Apply a local-mode model choice: validate, but persist only on save. */
    const applyLocalModelChoice = (modelName: string) => {
        setModel(modelName);
        if (!modelHasEnabledChannel(modelName)) {
            setChannelLookupFailed(true);
            toast.warning(t('aiRoute.config.noChannelFound'));
            return;
        }
        setChannelLookupFailed(false);
    };

    /** Local-mode model dropdown selection. Persisted via the save button. */
    const handleLocalModelSelect = (modelName: string) => {
        autoPickedRef.current = true;
        applyLocalModelChoice(modelName);
    };

    /** Re-run the automatic lowest-latency pick on demand. */
    const handleAutoPick = () => {
        const picked = autoPickLocalModel();
        if (!picked) {
            toast.warning(t('aiRoute.config.noChannelFound'));
            return;
        }
        autoPickedRef.current = true;
        applyLocalModelChoice(picked);
        toast.success(t('aiRoute.config.autoPickedNote', { name: picked }));
    };

    // Entering local mode with no model chosen: fill in the lowest-latency
    // model automatically, once. Market data may still be loading on the first
    // render, so the effect re-runs when it arrives.
    useEffect(() => {
        if (mode !== 'local') {
            autoPickedRef.current = false;
            return;
        }
        if (autoPickedRef.current || model.trim() !== '') return;
        const picked = autoPickLocalModel();
        if (!picked) return;
        autoPickedRef.current = true;
        setModel(picked);
        setChannelLookupFailed(!modelHasEnabledChannel(picked));
        toast.success(t('aiRoute.config.autoPickedNote', { name: picked }));
    }, [mode, model, autoPickLocalModel, modelHasEnabledChannel, t]);

    const localSetupIncomplete = isLocalSetupIncomplete({
        mode,
        model,
        hasEnabledChannel: modelHasEnabledChannel(model),
    });

    // Green confirmation only when the chosen model is actually persisted.
    const localModelSaved =
        mode === 'local' && model.trim() !== '' && model === initialModel.current && !channelLookupFailed;

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
                        onCheckedChange={(checked) => handleModeChange(checked ? 'local' : 'external')}
                        className={compact ? 'scale-75' : ''}
                    />
                    <span className={cn('text-muted-foreground', compact ? 'text-[10px]' : 'text-xs')}>
                        {t('aiRoute.config.modeLocal')}
                    </span>
                </div>
            </div>

            {/* Local mode: model dropdown + auto lowest-latency pick + save button.
                Credentials are derived server-side from the serving channel. */}
            {mode === 'local' && (
                <>
                    <div className="space-y-1.5">
                        <div className="flex items-center justify-between">
                            <label className={labelClass}>{t('aiRoute.config.model')}</label>
                            <Button
                                variant="ghost"
                                size={compact ? 'sm' : 'default'}
                                onClick={handleAutoPick}
                                className={cn('h-6 gap-1 rounded-lg px-2 text-muted-foreground', compact && 'h-5 text-[10px]')}
                            >
                                <Zap className={cn(compact ? 'h-3 w-3' : 'h-3.5 w-3.5')} />
                                {t('aiRoute.config.autoPick')}
                            </Button>
                        </div>
                        <Select value={model} onValueChange={handleLocalModelSelect}>
                            <SelectTrigger className={cn('rounded-lg', compact && 'h-8')}>
                                <SelectValue placeholder={t('aiRoute.config.modelPlaceholder')} />
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
                    {localModelSaved && (
                        <div className={cn(
                            'flex items-center gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 px-3 py-2',
                            compact ? 'text-[10px]' : 'text-xs'
                        )}>
                            <Check className={cn('shrink-0 text-emerald-600', compact ? 'h-3 w-3' : 'h-3.5 w-3.5')} />
                            <span className="text-emerald-700 dark:text-emerald-300">
                                {t('aiRoute.config.autoSaved')} · {t('aiRoute.config.autoLocalNote')}
                            </span>
                        </div>
                    )}
                    {/* A model no enabled channel serves cannot run: the backend
                        would have nothing to derive credentials from. Keyed off the
                        values, not interaction flags, so a setup left broken is
                        recognised again on the next visit. Offer the one move that
                        can finish the setup, and actually perform it. */}
                    {(channelLookupFailed || localSetupIncomplete) && (
                        <div className={cn('space-y-2 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2')}>
                            <p className={cn('text-amber-700 dark:text-amber-400', compact ? 'text-[10px]' : 'text-xs')}>
                                {t('aiRoute.config.localModelUnavailableHint')}
                            </p>
                            <Button
                                variant="outline"
                                size={compact ? 'sm' : 'default'}
                                onClick={() => handleModeChange('external')}
                                className={cn('rounded-lg', compact && 'h-7 text-xs')}
                            >
                                <Globe className={cn('mr-1.5', compact ? 'h-3 w-3' : 'h-4 w-4')} />
                                {t('aiRoute.config.switchToExternal')}
                            </Button>
                        </div>
                    )}
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
                                <SelectValue placeholder={t('aiRoute.config.modelPlaceholder')} />
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
