'use client';

import { useTranslations } from 'next-intl';
import { Store } from 'lucide-react';
import { Switch } from '@/components/ui/switch';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { useState, useEffect } from 'react';

export function CommercialMode() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [enabled, setEnabled] = useState(false);
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const val = settings.find((s) => s.key === SettingKey.CommercialMode)?.value;
        setEnabled(val === 'true');
        setLoaded(true);
    }, [settings, loaded]);

    const toggle = (next: boolean) => {
        setEnabled(next);
        setSetting.mutate(
            { key: SettingKey.CommercialMode, value: next ? 'true' : 'false' },
            {
                onSuccess: () => toast.success(next ? t('commercialMode.enable') : t('commercialMode.disable')),
                onError: () => { setEnabled(!next); toast.error('Failed'); },
            },
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                        <Store className="h-5 w-5 text-primary" />
                    </div>
                    <div className="space-y-0.5">
                        <span className="text-sm font-semibold text-card-foreground">{t('commercialMode.title')}</span>
                        <p className="text-xs text-muted-foreground">{t('commercialMode.description')}</p>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={toggle} aria-label={t('commercialMode.title')} />
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span>{t('commercialMode.status')}:</span>
                <span className={`font-medium ${enabled ? 'text-primary' : 'text-muted-foreground'}`}>
                    {enabled ? t('commercialMode.statusCommercial') : t('commercialMode.statusSelfUse')}
                </span>
            </div>
        </div>
    );
}
