'use client';

import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { KeyRound, Link2, Save, Check } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
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
import { useModelList } from '@/api/endpoints/model';
import { getModelIcon } from '@/lib/model-icons';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

export function AIRouteConfig({ compact }: { compact?: boolean }) {
    const t = useTranslations('analytics');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const { data: models } = useModelList();

    const modelsByProvider = useMemo(() => {
        const buckets: Record<string, string[]> = {};
        for (const m of models ?? []) {
            const { label } = getModelIcon(m.name);
            const key = label || 'Other';
            (buckets[key] ??= []).push(m.name);
        }
        return buckets;
    }, [models]);

    const [baseURL, setBaseURL] = useState('');
    const [apiKey, setAPIKey] = useState('');
    const [model, setModel] = useState('');

    const initialBaseURL = useRef('');
    const initialAPIKey = useRef('');
    const initialModel = useRef('');

    const [saving, setSaving] = useState(false);
    const [justSaved, setJustSaved] = useState(false);

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

    const fieldClass = compact ? 'text-sm' : '';
    const labelClass = cn('text-xs font-medium text-muted-foreground', compact && 'text-[11px]');

    return (
        <div className={cn('space-y-3', compact ? 'space-y-2' : 'space-y-3')}>
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
                <Select value={model} onValueChange={(v) => { setModel(v); saveSingle(SettingKey.AIRouteModel, v, initialModel); }}>
                    <SelectTrigger className={cn("rounded-lg", compact && "h-8")}>
                        <SelectValue placeholder={t('aiRoute.config.modelPlaceholder') || "Select a model"} />
                    </SelectTrigger>
                    <SelectContent>
                        {Object.entries(modelsByProvider).map(([provider, providerModels]) => (
                            <SelectGroup key={provider}>
                                <SelectLabel>{provider}</SelectLabel>
                                {providerModels.map((m) => (
                                    <SelectItem key={m} value={m}>{m}</SelectItem>
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
        </div>
    );
}
