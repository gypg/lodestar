'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { SlidersHorizontal, Wand2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useSetSetting } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { GroupMode } from '@/api/endpoints/group';
import { MODE_LABELS } from '@/components/modules/group/utils';
import {
    STRATEGY_PRESETS,
    presetSettingWrites,
    keyRangeLabel,
    type StrategyPreset,
} from '@/lib/strategy-presets';

export function SettingStrategyPresets() {
    const t = useTranslations();
    const tg = useTranslations('group');
    const setSetting = useSetSetting();
    const [applyingId, setApplyingId] = useState<string | null>(null);
    const [appliedId, setAppliedId] = useState<string | null>(null);

    const handleApply = (preset: StrategyPreset) => {
        const writes = presetSettingWrites(preset);
        setApplyingId(preset.id);
        let failures = 0;
        let pending = writes.length;
        const finishOne = () => {
            pending -= 1;
            if (pending > 0) return;
            setApplyingId(null);
            if (failures === 0) {
                setAppliedId(preset.id);
                toast.success(t('setting.strategyPresets.applied', { name: t(preset.nameKey) }));
            } else {
                toast.error(t('setting.strategyPresets.applyFailed'));
            }
        };
        for (const write of writes) {
            setSetting.mutate(write, {
                onSuccess: finishOne,
                onError: () => {
                    failures += 1;
                    finishOne();
                },
            });
        }
    };

    return (
        <div className="space-y-5 rounded-xl border-border/35 bg-card p-6 text-card-foreground shadow-md">
            <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                <SlidersHorizontal className="h-5 w-5" />
                {t('setting.strategyPresets.title')}
            </h2>
            <p className="text-xs text-muted-foreground md:text-sm">
                {t('setting.strategyPresets.hint')}
            </p>

            <div className="space-y-4">
                {STRATEGY_PRESETS.map((preset) => {
                    const range = keyRangeLabel(preset);
                    const modeLabel = tg(`mode.${MODE_LABELS[preset.mode as GroupMode]}`);
                    return (
                        <div
                            key={preset.id}
                            className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm"
                        >
                            <div className="flex flex-wrap items-center gap-2 md:gap-3">
                                <span className="text-xl md:text-2xl" aria-hidden>{preset.icon}</span>
                                <span className="text-sm font-semibold">{t(preset.nameKey)}</span>
                                <span className="text-xs text-muted-foreground">{t(preset.characterKey)}</span>
                                {range && (
                                    <span className="rounded-full border border-border/25 bg-muted/40 px-2 py-0.5 text-[0.68rem] font-medium text-muted-foreground">
                                        {t('setting.strategyPresets.keyCount')} {range}
                                    </span>
                                )}
                                <span className="ml-auto">
                                    <Button
                                        size="sm"
                                        variant="outline"
                                        className="gap-1.5 rounded-xl"
                                        disabled={applyingId === preset.id}
                                        onClick={() => handleApply(preset)}
                                    >
                                        <Wand2 className="h-3.5 w-3.5" />
                                        {appliedId === preset.id
                                            ? t('setting.strategyPresets.appliedShort')
                                            : t('setting.strategyPresets.apply')}
                                    </Button>
                                </span>
                            </div>

                            <p className="text-xs leading-relaxed text-muted-foreground md:text-sm">
                                {t(preset.scenarioKey)}
                            </p>

                            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                                <span>
                                    {t('setting.strategyPresets.modeLabel')}：
                                    <span className="font-medium text-foreground">{modeLabel}</span>
                                </span>
                                <span>
                                    {t('setting.strategyPresets.knobsLabel')}：
                                    <span className="font-mono text-foreground">
                                        {presetSettingWrites(preset)
                                            .map((w) => `${w.key}=${w.value}`)
                                            .join('  ')}
                                    </span>
                                </span>
                            </div>

                            {preset.approximationKey && (
                                <p className="rounded-md border border-border/25 bg-muted/30 px-3 py-1.5 text-[0.7rem] leading-relaxed text-muted-foreground">
                                    {t(preset.approximationKey)}
                                </p>
                            )}
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
