'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Activity } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { useSettingList, useSetSetting } from '@/api/endpoints/setting';
import { useModelProbeStates, useModelMarket } from '@/api/endpoints/model';
import { toast } from '@/components/common/Toast';

type ProbeSettingKey =
    | 'model_probe_enabled'
    | 'model_probe_interval_hours'
    | 'model_probe_fail_threshold'
    | 'model_probe_skip_groups';

function parseSkipGroups(raw: string): string[] {
    try {
        const parsed = JSON.parse(raw || '[]');
        return Array.isArray(parsed) ? parsed.filter((x) => typeof x === 'string') : [];
    } catch {
        return [];
    }
}

export function SettingModelProbe() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const { data: probeStates } = useModelProbeStates();
    const { data: market } = useModelMarket();

    const [enabled, setEnabled] = useState(false);
    const [intervalHours, setIntervalHours] = useState('2');
    const [failThreshold, setFailThreshold] = useState('3');
    const [skipInput, setSkipInput] = useState('');
    const [skipGroups, setSkipGroups] = useState<string[]>([]);

    const initialValues = useRef({
        enabled: false,
        intervalHours: '2',
        failThreshold: '3',
        skipGroups: [] as string[],
    });

    useEffect(() => {
        if (!settings) return;
        const get = (key: ProbeSettingKey) =>
            settings.find((item) => item.key === key)?.value ?? '';
        const nextEnabled = get('model_probe_enabled') === 'true';
        const nextInterval = get('model_probe_interval_hours') || '2';
        const nextThreshold = get('model_probe_fail_threshold') || '3';
        const nextSkip = parseSkipGroups(get('model_probe_skip_groups'));

        queueMicrotask(() => {
            setEnabled(nextEnabled);
            setIntervalHours(nextInterval);
            setFailThreshold(nextThreshold);
            setSkipGroups(nextSkip);
        });
        initialValues.current = {
            enabled: nextEnabled,
            intervalHours: nextInterval,
            failThreshold: nextThreshold,
            skipGroups: nextSkip,
        };
    }, [settings]);

    const saveSetting = (key: ProbeSettingKey, value: string) => {
        setSetting.mutate(
            { key, value },
            { onSuccess: () => toast.success(t('saved')) }
        );
    };

    const handleEnabledChange = (checked: boolean) => {
        setEnabled(checked);
        if (checked === initialValues.current.enabled) return;
        saveSetting('model_probe_enabled', checked ? 'true' : 'false');
        initialValues.current.enabled = checked;
    };

    const commitInterval = () => {
        const v = parseInt(intervalHours, 10);
        if (Number.isNaN(v) || v < 1 || v > 168) {
            toast.error(t('modelProbe.interval.invalid'));
            setIntervalHours(initialValues.current.intervalHours);
            return;
        }
        if (String(v) === initialValues.current.intervalHours) return;
        saveSetting('model_probe_interval_hours', String(v));
        initialValues.current.intervalHours = String(v);
    };

    const commitThreshold = () => {
        const v = parseInt(failThreshold, 10);
        if (Number.isNaN(v) || v < 1 || v > 10) {
            toast.error(t('modelProbe.threshold.invalid'));
            setFailThreshold(initialValues.current.failThreshold);
            return;
        }
        if (String(v) === initialValues.current.failThreshold) return;
        saveSetting('model_probe_fail_threshold', String(v));
        initialValues.current.failThreshold = String(v);
    };

    const commitSkipGroups = (next: string[]) => {
        const value = JSON.stringify(next);
        if (value === JSON.stringify(initialValues.current.skipGroups)) return;
        setSkipGroups(next);
        saveSetting('model_probe_skip_groups', value);
        initialValues.current.skipGroups = next;
    };

    const addSkip = () => {
        const name = skipInput.trim();
        if (!name) return;
        if (skipGroups.some((g) => g.toLowerCase() === name.toLowerCase())) {
            setSkipInput('');
            return;
        }
        commitSkipGroups([...skipGroups, name]);
        setSkipInput('');
    };

    const removeSkip = (name: string) => {
        commitSkipGroups(skipGroups.filter((g) => g !== name));
    };

    const stateByGroup = useMemo(() => {
        const map = new Map<string, { failures: number; lastProbedAt: string }>();
        for (const row of probeStates ?? []) {
            map.set(row.group_name, {
                failures: row.consecutive_failures,
                lastProbedAt: row.last_probed_at,
            });
        }
        return map;
    }, [probeStates]);

    const modelRows = useMemo(() => {
        const rows = (market?.items ?? []).map((item) => ({
            name: item.display_name || item.name,
            channelCount: item.channel_count,
        }));
        rows.sort((a, b) => a.name.localeCompare(b.name));
        return rows;
    }, [market]);

    const threshold = parseInt(failThreshold, 10) || 3;

    return (
        <div className="space-y-5 rounded-xl border-border/35 bg-card p-6 text-card-foreground shadow-md">
            <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                <Activity className="h-5 w-5" />
                {t('modelProbe.title')}
            </h2>

            <div className="flex min-w-0 flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                <div className="min-w-0 space-y-1">
                    <div className="text-sm font-medium">{t('modelProbe.enabled.label')}</div>
                    <div className="text-xs text-muted-foreground">{t('modelProbe.enabled.hint')}</div>
                </div>
                <Switch checked={enabled} onCheckedChange={handleEnabledChange} />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                    <label className="text-sm font-medium" htmlFor="model-probe-interval">
                        {t('modelProbe.interval.label')}
                    </label>
                    <Input
                        id="model-probe-interval"
                        type="number"
                        min={1}
                        max={168}
                        value={intervalHours}
                        onChange={(e) => setIntervalHours(e.target.value)}
                        onBlur={commitInterval}
                    />
                    <p className="text-xs text-muted-foreground">{t('modelProbe.interval.hint')}</p>
                </div>
                <div className="space-y-2">
                    <label className="text-sm font-medium" htmlFor="model-probe-threshold">
                        {t('modelProbe.threshold.label')}
                    </label>
                    <Input
                        id="model-probe-threshold"
                        type="number"
                        min={1}
                        max={10}
                        value={failThreshold}
                        onChange={(e) => setFailThreshold(e.target.value)}
                        onBlur={commitThreshold}
                    />
                    <p className="text-xs text-muted-foreground">{t('modelProbe.threshold.hint')}</p>
                </div>
            </div>

            <div className="space-y-2">
                <label className="text-sm font-medium" htmlFor="model-probe-skip">
                    {t('modelProbe.skip.label')}
                </label>
                <p className="text-xs text-muted-foreground">{t('modelProbe.skip.hint')}</p>
                <div className="flex gap-2">
                    <Input
                        id="model-probe-skip"
                        placeholder={t('modelProbe.skip.placeholder')}
                        value={skipInput}
                        onChange={(e) => setSkipInput(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                e.preventDefault();
                                addSkip();
                            }
                        }}
                    />
                    <Button variant="outline" onClick={addSkip}>
                        {t('modelProbe.skip.add')}
                    </Button>
                </div>
                {skipGroups.length > 0 && (
                    <div className="flex flex-wrap gap-2 pt-1">
                        {skipGroups.map((name) => (
                            <button
                                key={name}
                                type="button"
                                onClick={() => removeSkip(name)}
                                className="inline-flex items-center gap-1 rounded-md border-border/40 bg-muted px-2 py-1 text-xs hover:border-destructive/50 hover:text-destructive"
                            >
                                {name}
                                <span aria-hidden>×</span>
                            </button>
                        ))}
                    </div>
                )}
            </div>

            <div className="space-y-2">
                <div className="text-sm font-medium">{t('modelProbe.status.title')}</div>
                <p className="text-xs text-muted-foreground">{t('modelProbe.status.hint')}</p>
                {modelRows.length === 0 ? (
                    <div className="rounded-lg border-border/30 bg-muted/30 p-4 text-xs text-muted-foreground">
                        {t('modelProbe.status.empty')}
                    </div>
                ) : (
                    <div className="max-h-72 overflow-y-auto rounded-lg border-border/30">
                        <table className="w-full text-xs">
                            <thead className="sticky top-0 bg-muted/60">
                                <tr className="text-left">
                                    <th className="px-3 py-2 font-medium">{t('modelProbe.status.model')}</th>
                                    <th className="px-3 py-2 font-medium">{t('modelProbe.status.channels')}</th>
                                    <th className="px-3 py-2 font-medium">{t('modelProbe.status.failures')}</th>
                                    <th className="px-3 py-2 font-medium">{t('modelProbe.status.lastProbed')}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {modelRows.map((row) => {
                                    const state = stateByGroup.get(row.name);
                                    const hidden = (state?.failures ?? 0) >= threshold;
                                    return (
                                        <tr key={row.name} className="border-t border-border/20">
                                            <td className="max-w-[220px] truncate px-3 py-2">{row.name}</td>
                                            <td className="px-3 py-2">{row.channelCount}</td>
                                            <td className={`px-3 py-2 ${hidden ? 'font-semibold text-destructive' : ''}`}>
                                                {state?.failures ?? 0}
                                                {hidden ? ` · ${t('modelProbe.status.hidden')}` : ''}
                                            </td>
                                            <td className="px-3 py-2 text-muted-foreground">
                                                {state?.lastProbedAt
                                                    ? new Date(state.lastProbedAt).toLocaleString()
                                                    : t('modelProbe.status.never')}
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    );
}
