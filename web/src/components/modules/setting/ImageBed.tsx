'use client';

import { useEffect, useRef, useState } from 'react';
import { Cloud, Globe2, KeyRound, ToggleLeft, Zap } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { SettingKey, useSetSetting, useSettingList, useTestImageBedConnection } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';

const defaultSettingValues: Record<string, string> = {
    [SettingKey.ImageBedEnabled]: 'false',
    [SettingKey.ImageBedEndpoint]: '',
    [SettingKey.ImageBedToken]: '',
};

export function SettingImageBed() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const testConnection = useTestImageBedConnection();

    const [enabled, setEnabled] = useState(false);
    const [endpoint, setEndpoint] = useState('');
    const [token, setToken] = useState('');

    const intendedValuesRef = useRef<Record<string, string>>({ ...defaultSettingValues });
    const loadedKeysRef = useRef<Set<string>>(new Set());
    const hasLocalIntentRef = useRef<Record<string, boolean>>({});
    const inFlightValuesRef = useRef<Record<string, string | undefined>>({});

    useEffect(() => {
        if (!settings) return;

        const nextValues: Record<string, string> = {
            [SettingKey.ImageBedEnabled]:
                settings.find((item) => item.key === SettingKey.ImageBedEnabled)?.value || 'false',
            [SettingKey.ImageBedEndpoint]:
                settings.find((item) => item.key === SettingKey.ImageBedEndpoint)?.value || '',
            [SettingKey.ImageBedToken]:
                settings.find((item) => item.key === SettingKey.ImageBedToken)?.value || '',
        };

        const shouldApplyServerValue = (key: string, nextValue: string) => {
            if (!loadedKeysRef.current.has(key)) {
                loadedKeysRef.current.add(key);
                return true;
            }
            if (hasLocalIntentRef.current[key] && intendedValuesRef.current[key] !== nextValue) {
                return false;
            }
            hasLocalIntentRef.current[key] = false;
            return true;
        };

        queueMicrotask(() => {
            if (shouldApplyServerValue(SettingKey.ImageBedEnabled, nextValues[SettingKey.ImageBedEnabled])) {
                intendedValuesRef.current[SettingKey.ImageBedEnabled] = nextValues[SettingKey.ImageBedEnabled];
                setEnabled(nextValues[SettingKey.ImageBedEnabled] === 'true');
            }
            if (shouldApplyServerValue(SettingKey.ImageBedEndpoint, nextValues[SettingKey.ImageBedEndpoint])) {
                intendedValuesRef.current[SettingKey.ImageBedEndpoint] = nextValues[SettingKey.ImageBedEndpoint];
                setEndpoint(nextValues[SettingKey.ImageBedEndpoint]);
            }
            if (shouldApplyServerValue(SettingKey.ImageBedToken, nextValues[SettingKey.ImageBedToken])) {
                intendedValuesRef.current[SettingKey.ImageBedToken] = nextValues[SettingKey.ImageBedToken];
                setToken(nextValues[SettingKey.ImageBedToken]);
            }
        });
    }, [settings]);

    const flushSettingSave = (key: string) => {
        if (inFlightValuesRef.current[key] !== undefined || !hasLocalIntentRef.current[key]) {
            return;
        }
        const value = intendedValuesRef.current[key];
        inFlightValuesRef.current[key] = value;

        setSetting.mutate(
            { key, value },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                },
                onError: () => {
                    if (intendedValuesRef.current[key] === value) {
                        hasLocalIntentRef.current[key] = false;
                    }
                },
                onSettled: () => {
                    delete inFlightValuesRef.current[key];
                    if (hasLocalIntentRef.current[key] && intendedValuesRef.current[key] !== value) {
                        flushSettingSave(key);
                    }
                },
            },
        );
    };

    const saveTextSetting = (key: string, value: string) => {
        if (value === intendedValuesRef.current[key]) return;
        intendedValuesRef.current[key] = value;
        hasLocalIntentRef.current[key] = true;
        flushSettingSave(key);
    };

    const saveBooleanSetting = (checked: boolean) => {
        const value = checked ? 'true' : 'false';
        setEnabled(checked);
        if (value === intendedValuesRef.current[SettingKey.ImageBedEnabled]) return;
        intendedValuesRef.current[SettingKey.ImageBedEnabled] = value;
        hasLocalIntentRef.current[SettingKey.ImageBedEnabled] = true;
        flushSettingSave(SettingKey.ImageBedEnabled);
    };

    return (
        <div className="min-w-0 space-y-5 rounded-xl border border-border/35 bg-card p-6 text-card-foreground">
            <div className="space-y-1">
                <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                    <Cloud className="h-5 w-5" />
                    {t('imageBed.title')}
                </h2>
            </div>

            <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border/30 bg-card p-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0 flex items-center gap-3">
                    <ToggleLeft className="h-5 w-5 text-muted-foreground" />
                    <div className="flex flex-col">
                        <span className="text-sm font-medium">{t('imageBed.enabled')}</span>
                        <span className="text-xs text-muted-foreground">{t('imageBed.enabledDescription')}</span>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={saveBooleanSetting} />
            </div>

            <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border/30 bg-card p-4 md:flex-row md:items-center md:justify-between">
                <div className="min-w-0 flex items-center gap-3">
                    <Globe2 className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('imageBed.endpoint')}</span>
                </div>
                <Input
                    value={endpoint}
                    onChange={(event) => setEndpoint(event.target.value)}
                    onBlur={() => saveTextSetting(SettingKey.ImageBedEndpoint, endpoint)}
                    placeholder={t('imageBed.endpointPlaceholder')}
                    className="w-full rounded-xl md:w-80"
                />
            </div>

            <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border/30 bg-card p-4 md:flex-row md:items-center md:justify-between">
                <div className="min-w-0 flex items-center gap-3">
                    <KeyRound className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('imageBed.token')}</span>
                </div>
                <Input
                    type="password"
                    value={token}
                    onChange={(event) => setToken(event.target.value)}
                    onBlur={() => saveTextSetting(SettingKey.ImageBedToken, token)}
                    placeholder={t('imageBed.tokenPlaceholder')}
                    className="w-full rounded-xl md:w-80"
                />
            </div>

            <div className="flex justify-end">
                <Button
                    variant="outline"
                    size="sm"
                    className="rounded-xl"
                    disabled={testConnection.isPending || !endpoint}
                    onClick={() => {
                        testConnection.mutate(undefined, {
                            onSuccess: (data) => {
                                toast.success(t('imageBed.testSuccess') || 'Connection successful');
                            },
                            onError: (error: Error) => {
                                toast.error(error.message || 'Connection failed');
                            },
                        });
                    }}
                >
                    <Zap className="size-4" />
                    {testConnection.isPending ? (t('imageBed.testing') || 'Testing...') : (t('imageBed.testConnection') || 'Test Connection')}
                </Button>
            </div>
        </div>
    );
}
