'use client';

import { useEffect, useState, useRef, useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { ScrollText, Calendar, Hash, Trash2, Terminal, FolderX } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { useGroupList } from '@/api/endpoints/group';
import { useClearLogs } from '@/api/endpoints/log';
import { toast } from '@/components/common/Toast';

type KeepMode = 'count' | 'days';
type LogLevel = 'debug' | 'info' | 'warn' | 'error';

const LOG_LEVELS: LogLevel[] = ['debug', 'info', 'warn', 'error'];

export function SettingLog() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const { data: groups = [] } = useGroupList();
    const setSetting = useSetSetting();
    const clearLogs = useClearLogs();

    const [enabled, setEnabled] = useState(true);
    const [logLevel, setLogLevel] = useState<LogLevel>('info');
    const [mode, setMode] = useState<KeepMode>('count');
    const [keepCount, setKeepCount] = useState('1000');
    const [keepDays, setKeepDays] = useState('7');
    const [isClearing, setIsClearing] = useState(false);
    const [excludedGroups, setExcludedGroups] = useState<string[]>([]);

    // 去重的分组名称列表（同名分组只展示一次）
    const groupNames = useMemo(() => {
        const seen = new Set<string>();
        const names: string[] = [];
        for (const g of groups) {
            const name = g.name?.trim();
            if (!name || seen.has(name)) continue;
            seen.add(name);
            names.push(name);
        }
        names.sort((a, b) => a.localeCompare(b));
        return names;
    }, [groups]);

    const initialEnabled = useRef(true);
    const initialMode = useRef<KeepMode>('count');
    const initialKeepCount = useRef('1000');
    const initialKeepDays = useRef('7');

    useEffect(() => {
        if (settings) {
            const enabledSetting = settings.find(s => s.key === SettingKey.RelayLogKeepEnabled);
            const countSetting = settings.find(s => s.key === SettingKey.RelayLogKeepCount);
            const periodSetting = settings.find(s => s.key === SettingKey.RelayLogKeepPeriod);

            if (enabledSetting) {
                const isEnabled = enabledSetting.value === 'true';
                queueMicrotask(() => setEnabled(isEnabled));
                initialEnabled.current = isEnabled;
            }

            const logLevelSetting = settings.find(s => s.key === SettingKey.LogLevel);
            if (logLevelSetting && LOG_LEVELS.includes(logLevelSetting.value as LogLevel)) {
                queueMicrotask(() => setLogLevel(logLevelSetting.value as LogLevel));
            }

            // Determine mode: if keepCount > 0 → count mode, else days mode
            const countVal = countSetting?.value || '0';
            const daysVal = periodSetting?.value || '7';

            if (parseInt(countVal) > 0) {
                queueMicrotask(() => {
                    setMode('count');
                    setKeepCount(countVal);
                    setKeepDays(daysVal);
                });
                initialMode.current = 'count';
                initialKeepCount.current = countVal;
                initialKeepDays.current = daysVal;
            } else {
                queueMicrotask(() => {
                    setMode('days');
                    setKeepCount(countVal === '0' ? '1000' : countVal);
                    setKeepDays(daysVal);
                });
                initialMode.current = 'days';
                initialKeepCount.current = countVal === '0' ? '1000' : countVal;
                initialKeepDays.current = daysVal;
            }
        }
    }, [settings]);

    useEffect(() => {
        if (!settings) return;
        const raw = settings.find(s => s.key === SettingKey.LogExcludedGroups)?.value;
        if (raw === undefined) return;
        let parsed: string[] = [];
        try {
            const v = JSON.parse(raw);
            if (Array.isArray(v)) parsed = v.filter((x): x is string => typeof x === 'string');
        } catch (e) { console.error(e);
            parsed = [];
        }
        queueMicrotask(() => setExcludedGroups(parsed));
    }, [settings]);

    const saveExcludedGroups = (next: string[]) => {
        setExcludedGroups(next);
        setSetting.mutate(
            { key: SettingKey.LogExcludedGroups, value: JSON.stringify(next) },
            {
                onSuccess: () => { toast.success(t('saved')); },
            }
        );
    };

    const toggleExcludedGroup = (name: string) => {
        if (excludedGroups.includes(name)) {
            saveExcludedGroups(excludedGroups.filter(n => n !== name));
        } else {
            saveExcludedGroups([...excludedGroups, name]);
        }
    };

    const handleEnabledChange = (checked: boolean) => {
        setEnabled(checked);
        setSetting.mutate(
            { key: SettingKey.RelayLogKeepEnabled, value: checked ? 'true' : 'false' },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialEnabled.current = checked;
                }
            }
        );
    };

    const handleLogLevelChange = (level: LogLevel) => {
        setLogLevel(level);
        setSetting.mutate(
            { key: SettingKey.LogLevel, value: level },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                }
            }
        );
    };

    const handleModeChange = (newMode: KeepMode) => {
        setMode(newMode);
        initialMode.current = newMode;

        if (newMode === 'count') {
            // Switch to count mode: save current keepCount, clear period effect
            const val = keepCount || '1000';
            setSetting.mutate({ key: SettingKey.RelayLogKeepCount, value: val });
            initialKeepCount.current = val;
        } else {
            // Switch to days mode: set keepCount=0 to disable count-based cleanup
            setSetting.mutate({ key: SettingKey.RelayLogKeepCount, value: '0' });
            initialKeepCount.current = '0';
        }
        toast.success(t('saved'));
    };

    const handleKeepCountSave = () => {
        if (keepCount === initialKeepCount.current) return;
        const val = Math.max(1, parseInt(keepCount) || 1000).toString();
        setKeepCount(val);
        setSetting.mutate(
            { key: SettingKey.RelayLogKeepCount, value: val },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialKeepCount.current = val;
                }
            }
        );
    };

    const handleKeepDaysSave = () => {
        if (keepDays === initialKeepDays.current) return;
        const val = Math.max(1, parseInt(keepDays) || 7).toString();
        setKeepDays(val);
        setSetting.mutate(
            { key: SettingKey.RelayLogKeepPeriod, value: val },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialKeepDays.current = val;
                }
            }
        );
    };

    const handleClearLogs = () => {
        setIsClearing(true);
        clearLogs.mutate(undefined, {
            onSuccess: () => {
                toast.success(t('log.clearSuccess'));
                setIsClearing(false);
            },
            onError: () => {
                toast.error(t('log.clearFailed'));
                setIsClearing(false);
            }
        });
    };

    return (
        <div className="rounded-xl border-border/35 bg-card p-6 space-y-5 text-card-foreground shadow-md">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <ScrollText className="h-5 w-5" />
                {t('log.title')}
            </h2>

            {/* 是否启用历史日志 */}
            <div className="flex items-center justify-between gap-4 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                <div className="flex items-center gap-3">
                    <ScrollText className="h-5 w-5 text-muted-foreground" />
                    <div className="flex flex-col">
                        <span className="text-sm font-medium">{t('log.enabled.label')}</span>
                        <span className="text-xs text-muted-foreground">{t('log.enabled.description')}</span>
                    </div>
                </div>
                <Switch
                    checked={enabled}
                    onCheckedChange={handleEnabledChange}
                />
            </div>

            {/* 应用日志级别 */}
            <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                <div className="flex items-center gap-3">
                    <Terminal className="h-5 w-5 text-muted-foreground" />
                    <div className="flex flex-col">
                        <span className="text-sm font-medium">{t('log.logLevel.label')}</span>
                        <span className="text-xs text-muted-foreground">{t('log.logLevel.description')}</span>
                    </div>
                </div>
                <div className="flex gap-2 mt-1">
                    {LOG_LEVELS.map((level) => (
                        <Button
                            key={level}
                            variant={logLevel === level ? 'default' : 'outline'}
                            size="sm"
                            onClick={() => handleLogLevelChange(level)}
                            className="rounded-xl flex-1 md:flex-none"
                        >
                            {t(`log.logLevel.${level}`)}
                        </Button>
                    ))}
                </div>
            </div>

            {/* 保留模式切换 */}
            <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                <div className="flex items-center gap-3">
                    <Hash className="h-5 w-5 text-muted-foreground" />
                    <div className="flex flex-col">
                        <span className="text-sm font-medium">{t('log.mode.label')}</span>
                        <span className="text-xs text-muted-foreground">{t('log.mode.description')}</span>
                    </div>
                </div>
                <div className="flex gap-2 mt-1">
                    <Button
                        variant={mode === 'count' ? 'default' : 'outline'}
                        size="sm"
                        onClick={() => handleModeChange('count')}
                        disabled={!enabled}
                        className="rounded-xl flex-1 md:flex-none"
                    >
                        <Hash className="mr-1.5 h-3.5 w-3.5" />
                        {t('log.mode.count')}
                    </Button>
                    <Button
                        variant={mode === 'days' ? 'default' : 'outline'}
                        size="sm"
                        onClick={() => handleModeChange('days')}
                        disabled={!enabled}
                        className="rounded-xl flex-1 md:flex-none"
                    >
                        <Calendar className="mr-1.5 h-3.5 w-3.5" />
                        {t('log.mode.days')}
                    </Button>
                </div>
            </div>

            {/* 按条数保留设置 */}
            {mode === 'count' && (
                <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                    <div className="flex items-center gap-3">
                        <Hash className="h-5 w-5 text-muted-foreground" />
                        <div className="flex flex-col">
                            <span className="text-sm font-medium">{t('log.keepCount.label')}</span>
                            <span className="text-xs text-muted-foreground">{t('log.keepCount.description')}</span>
                        </div>
                    </div>
                    <Input
                        type="number"
                        value={keepCount}
                        onChange={(e) => setKeepCount(e.target.value)}
                        onBlur={handleKeepCountSave}
                        placeholder={t('log.keepCount.placeholder')}
                        className="w-48 rounded-xl"
                        disabled={!enabled}
                        min={1}
                    />
                </div>
            )}

            {/* 按天数保留设置 */}
            {mode === 'days' && (
                <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                    <div className="flex items-center gap-3">
                        <Calendar className="h-5 w-5 text-muted-foreground" />
                        <div className="flex flex-col">
                            <span className="text-sm font-medium">{t('log.keepPeriod.label')}</span>
                            <span className="text-xs text-muted-foreground">{t('log.keepPeriod.description')}</span>
                        </div>
                    </div>
                    <Input
                        type="number"
                        value={keepDays}
                        onChange={(e) => setKeepDays(e.target.value)}
                        onBlur={handleKeepDaysSave}
                        placeholder={t('log.keepPeriod.placeholder')}
                        className="w-48 rounded-xl"
                        disabled={!enabled}
                        min={1}
                    />
                </div>
            )}

            {/* 屏蔽分组：被选中的分组日志不在列表与实时流中显示或加载 */}
            <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                <div className="flex items-center gap-3">
                    <FolderX className="h-5 w-5 text-muted-foreground" />
                    <div className="flex flex-col">
                        <span className="text-sm font-medium">{t('log.excludedGroups.label')}</span>
                        <span className="text-xs text-muted-foreground">{t('log.excludedGroups.description')}</span>
                    </div>
                    {excludedGroups.length > 0 && (
                        <Badge variant="secondary" className="ml-auto text-xs">{excludedGroups.length}</Badge>
                    )}
                </div>
                {groupNames.length === 0 ? (
                    <p className="pl-8 text-xs text-muted-foreground">{t('log.excludedGroups.empty')}</p>
                ) : (
                    <div className="flex flex-wrap gap-2 pl-8">
                        {groupNames.map((name) => {
                            const active = excludedGroups.includes(name);
                            return (
                                <Badge
                                    key={name}
                                    variant={active ? 'default' : 'outline'}
                                    className="max-w-full cursor-pointer gap-1.5 rounded-lg px-2.5 py-1 text-xs transition-colors hover:bg-accent/60"
                                    onClick={() => toggleExcludedGroup(name)}
                                    title={active ? t('log.excludedGroups.removeHint') : t('log.excludedGroups.addHint')}
                                >
                                    <span className="truncate">{name}</span>
                                    {active && <span className="text-muted-foreground">×</span>}
                                </Badge>
                            );
                        })}
                    </div>
                )}
            </div>

            {/* 清空历史日志 */}
            <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                <div className="flex items-center gap-3">
                    <Trash2 className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('log.clear.label')}</span>
                </div>
                <Button
                    variant="destructive"
                    size="sm"
                    onClick={handleClearLogs}
                    disabled={isClearing}
                    className="rounded-xl"
                >
                    {isClearing ? t('log.clear.clearing') : t('log.clear.button')}
                </Button>
            </div>
        </div>
    );
}
