'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTheme } from 'next-themes';
import { SettingOrder } from './SettingOrder';
import { useTranslations } from 'next-intl';
import { Bell, Clock3, GripVertical, Languages, ListOrdered, Monitor, Moon, RotateCcw, Sun, Landmark, Palette, Store } from 'lucide-react';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DraggableProvided,
    type DropResult,
} from '@hello-pangea/dnd';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';
import {
    DEFAULT_NAV_ORDER,
    isFixedVisibleNavItem,
    MIN_VISIBLE_NAV_ITEMS,
    useNavStore,
    type NavItem,
} from '@/components/modules/navbar';
import { serializeNavOrder, serializeNavVisible } from '@/components/modules/navbar';
import { useSettingStore, type Locale } from '@/stores/setting';
import { useThemePresetStore } from '@/stores/theme-preset';
import { useSetUserPreferences, useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { SiteIdentity } from './SiteIdentity';
import { SettingWallet } from './SettingWallet';
import { ApiUsageGuide } from './ApiUsageGuide';
import { Feedback } from './Feedback';
import { PaymentSettings } from './PaymentSettings';
import { EmailSettings } from './EmailSettings';
import { BUILTIN_PRESETS } from '@/lib/theme-presets';
import { useCustomThemesStore, parseCustomThemes } from '@/stores/custom-themes';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

type AlertNotifyLanguage = Locale;
const TIME_ZONE_OPTIONS = [
    'Asia/Shanghai',
    'Asia/Tokyo',
    'Asia/Singapore',
    'UTC',
    'Europe/London',
    'Europe/Berlin',
    'America/New_York',
    'America/Chicago',
    'America/Denver',
    'America/Los_Angeles',
] as const;

function normalizeAlertNotifyLanguage(value: string | null | undefined): AlertNotifyLanguage {
    switch (value) {
        case 'zh-Hans':
        case 'zh-Hant':
        case 'en':
            return value;
        default:
            return 'en';
    }
}

function reorderList<T>(list: readonly T[], startIndex: number, endIndex: number): T[] {
    const result = [...list];
    const [removed] = result.splice(startIndex, 1);
    result.splice(endIndex, 0, removed);
    return result;
}

function NavigationPreferences() {
    const t = useTranslations('setting');
    const navT = useTranslations('navbar');
    const setSetting = useSetSetting();
    const orderedItems = useNavStore((state) => state.orderedItems);
    const visibleItems = useNavStore((state) => state.visibleItems);
    const setOrderedItems = useNavStore((state) => state.setOrderedItems);
    const setItemVisible = useNavStore((state) => state.setItemVisible);
    const resetPreferences = useNavStore((state) => state.resetPreferences);
    const visibleItemSet = useMemo(() => new Set(visibleItems), [visibleItems]);
    const visibleCount = visibleItems.length;

    const persistNavOrder = useCallback((items: readonly NavItem[], onSuccess?: () => void) => {
        setSetting.mutate(
            {
                key: SettingKey.NavOrder,
                value: serializeNavOrder(items),
            },
            {
                onSuccess,
                onError: () => {
                    toast.error(t('saveFailed'));
                },
            }
        );
    }, [setSetting, t]);

    const persistNavVisible = useCallback((items: readonly NavItem[], onSuccess?: () => void) => {
        setSetting.mutate(
            {
                key: SettingKey.NavVisible,
                value: serializeNavVisible(items),
            },
            {
                onSuccess,
                onError: () => {
                    toast.error(t('saveFailed'));
                },
            }
        );
    }, [setSetting, t]);

    const handleDragEnd = useCallback((result: DropResult) => {
        const { destination, source } = result;
        if (!destination || destination.index === source.index) {
            return;
        }

        const nextOrder = reorderList(orderedItems, source.index, destination.index);
        setOrderedItems(nextOrder);
        persistNavOrder(nextOrder);
    }, [orderedItems, persistNavOrder, setOrderedItems]);

    const handleVisibleChange = useCallback((item: NavItem, checked: boolean) => {
        if (!checked && isFixedVisibleNavItem(item)) {
            return;
        }
        if (!checked && visibleItemSet.has(item) && visibleCount <= MIN_VISIBLE_NAV_ITEMS) {
            toast.error(t('navOrder.minimumVisibleError', { count: MIN_VISIBLE_NAV_ITEMS }));
            return;
        }

        const nextVisible = checked
            ? Array.from(new Set([...visibleItems, item]))
            : visibleItems.filter((visibleItem) => visibleItem !== item);
        setItemVisible(item, checked);
        persistNavVisible(nextVisible);
    }, [persistNavVisible, setItemVisible, t, visibleCount, visibleItemSet, visibleItems]);

    const handleReset = useCallback(() => {
        resetPreferences();
        persistNavOrder(DEFAULT_NAV_ORDER, () => {
            toast.success(t('navOrder.resetSuccess'));
        });
        persistNavVisible(DEFAULT_NAV_ORDER);
    }, [persistNavOrder, persistNavVisible, resetPreferences, t]);

    return (
        <div className="space-y-4 rounded-lg border-border/30 bg-card p-4 shadow-sm ">
            <div className="flex items-start justify-between gap-3">
                <div className="space-y-1">
                    <div className="flex items-center gap-2">
                        <ListOrdered className="size-4 text-muted-foreground" />
                        <h3 className="text-sm font-semibold text-foreground">{t('navOrder.title')}</h3>
                    </div>
                    <p className="text-xs leading-5 text-muted-foreground">{t('navOrder.description')}</p>
                </div>

                <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleReset}
                    className="shrink-0 rounded-xl"
                >
                    <RotateCcw className="mr-1.5 size-3.5" />
                    {t('navOrder.reset')}
                </Button>
            </div>

            <div className="rounded-lg border border-border/30 bg-card p-1.5 shadow-sm ">
                <DragDropContext onDragEnd={handleDragEnd}>
                    <Droppable droppableId="setting-nav-order">
                        {(droppableProvided) => (
                            <div
                                ref={droppableProvided.innerRef}
                                {...droppableProvided.droppableProps}
                                className="max-h-[24rem] space-y-2 overflow-y-auto p-2 pr-3"
                            >
                                {orderedItems.map((item, index) => {
                                    const isVisible = visibleItemSet.has(item);
                                    const isFixed = isFixedVisibleNavItem(item);
                                    const disableToggle = isFixed || (isVisible && visibleCount <= MIN_VISIBLE_NAV_ITEMS);

                                    return (
                                        <Draggable key={item} draggableId={item} index={index}>
                                            {(draggableProvided, snapshot) => (
                                                <div
                                                    ref={draggableProvided.innerRef}
                                                    {...draggableProvided.draggableProps}
                                                    className={cn(
                                                        'flex items-center justify-between gap-3 rounded-lg border-border/30 bg-card px-3 py-3 shadow-sm transition-[transform,border-color,box-shadow]',
                                                        snapshot.isDragging && 'border-primary/40 shadow-md'
                                                    )}
                                                    style={draggableProvided.draggableProps.style}
                                                >
                                                    <div className="flex min-w-0 items-center gap-3">
                                                        <span className="grid size-7 shrink-0 place-items-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
                                                            {index + 1}
                                                        </span>
                                                        <div
                                                            className="rounded-lg p-1 text-muted-foreground"
                                                            {...(draggableProvided.dragHandleProps as DraggableProvided['dragHandleProps'])}
                                                        >
                                                            <GripVertical className="size-4" />
                                                        </div>
                                                        <div className="min-w-0">
                                                            <div className="truncate text-sm font-medium text-foreground">
                                                                {navT(item)}
                                                            </div>
                                                            <div className="text-xs text-muted-foreground">
                                                                {isVisible ? t('navOrder.visible') : t('navOrder.hidden')}
                                                            </div>
                                                        </div>
                                                    </div>

                                                    <div className="flex shrink-0 items-center gap-2">
                                                        {isFixed && (
                                                            <span className="rounded-full border border-border/60 bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                                                                {t('navOrder.fixed')}
                                                            </span>
                                                        )}
                                                        <Switch
                                                            checked={isVisible}
                                                            onCheckedChange={(checked) => handleVisibleChange(item, checked)}
                                                            disabled={disableToggle}
                                                            aria-label={t('navOrder.toggleAriaLabel', { page: navT(item) })}
                                                        />
                                                    </div>
                                                </div>
                                            )}
                                        </Draggable>
                                    );
                                })}
                                {droppableProvided.placeholder}
                            </div>
                        )}
                    </Droppable>
                </DragDropContext>
            </div>

            <p className="text-xs leading-5 text-muted-foreground">
                {t('navOrder.minimumVisibleHint', { count: MIN_VISIBLE_NAV_ITEMS })}
            </p>
        </div>
    );
}

export function SettingAppearance() {
    const t = useTranslations('setting');
    const { theme, setTheme } = useTheme();
    const { presetId, setPreset } = useThemePresetStore();
    const { data: meUser } = useCurrentUser();
    const showAdmin = isStaffRole(meUser?.role);
    const setUserPreferences = useSetUserPreferences();
    const applyPreset = (id: string) => {
        setPreset(id);
        // 绑账户：把选择持久化到当前用户，跨设备一致（失败静默，本地仍生效）。
        setUserPreferences.mutate({ themePreset: id });
    };
    const { themes: customThemes, setThemes: setCustomThemes } = useCustomThemesStore();
    const [themeUploadOpen, setThemeUploadOpen] = useState(false);
    const [themeUploadText, setThemeUploadText] = useState('');
    const { locale, setLocale, timeZone, setTimeZone, chinaMode, setChinaMode, exchangeRate, setExchangeRate } = useSettingStore();
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [alertNotifyLanguage, setAlertNotifyLanguage] = useState<AlertNotifyLanguage>('en');
    const initialAlertNotifyLanguage = useRef<AlertNotifyLanguage>('en');
    const [localExchangeRate, setLocalExchangeRate] = useState(exchangeRate.toString());
    const initialExchangeRate = useRef(exchangeRate);

    useEffect(() => {
        if (!settings) return;
        const alertNotifyLanguageSetting = settings.find((item) => item.key === SettingKey.AlertNotifyLanguage);
        if (!alertNotifyLanguageSetting) return;

        const nextValue = normalizeAlertNotifyLanguage(alertNotifyLanguageSetting.value);
        queueMicrotask(() => setAlertNotifyLanguage(nextValue));
        initialAlertNotifyLanguage.current = nextValue;
    }, [settings]);

    // Sync server-side uploaded themes (custom_themes setting) into the client store
    // so both the picker and the live applier see them.
    useEffect(() => {
        if (!settings) return;
        const customThemesSetting = settings.find((item) => item.key === SettingKey.CustomThemes);
        setCustomThemes(parseCustomThemes(customThemesSetting?.value));
    }, [settings, setCustomThemes]);

    const handleAddTheme = useCallback(() => {
        let parsed: unknown;
        try {
            parsed = JSON.parse(themeUploadText);
        } catch {
            toast.error('JSON 解析失败');
            return;
        }
        const incoming = parseCustomThemes(JSON.stringify(Array.isArray(parsed) ? parsed : [parsed]));
        if (incoming.length === 0) {
            toast.error('主题格式无效：需包含 id、name 以及 light/dark 颜色 token');
            return;
        }
        const byId = new Map(customThemes.map((th) => [th.id, th]));
        for (const th of incoming) byId.set(th.id, th);
        const merged = Array.from(byId.values());
        setSetting.mutate(
            { key: SettingKey.CustomThemes, value: JSON.stringify(merged) },
            {
                onSuccess: () => {
                    setCustomThemes(merged);
                    setThemeUploadText('');
                    setThemeUploadOpen(false);
                    toast.success(`已添加 ${incoming.length} 个主题`);
                },
                onError: () => toast.error(t('saveFailed')),
            }
        );
    }, [themeUploadText, customThemes, setSetting, setCustomThemes, t]);

    const handleAlertNotifyLanguageChange = (value: string) => {
        const nextValue = normalizeAlertNotifyLanguage(value);
        setAlertNotifyLanguage(nextValue);

        setSetting.mutate(
            { key: SettingKey.AlertNotifyLanguage, value: nextValue },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialAlertNotifyLanguage.current = nextValue;
                },
                onError: () => {
                    setAlertNotifyLanguage(initialAlertNotifyLanguage.current);
                    toast.error(t('saveFailed'));
                },
            }
        );
    };

    return (
        <div className="relative overflow-visible rounded-xl border-border/35 bg-card p-4 sm:p-6 text-card-foreground shadow-none ">
            <div className="space-y-5">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                    <div className="space-y-1.5">
                        <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                            <Sun className="h-5 w-5" />
                            {t('appearance')}
                        </h2>
                        <p className="text-sm text-muted-foreground">{t('navOrder.description')}</p>
                    </div>
                    <div className="w-fit rounded-full border-border/25 bg-card px-3 py-1.5 text-xs font-medium text-muted-foreground shadow-sm">
                        Lodestar
                    </div>
                </div>

                <div className="grid gap-4">
                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                        <div className="flex items-center gap-3">
                            {theme === 'dark' ? (
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Moon className="h-5 w-5 text-primary" />
                                </div>
                            ) : (
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Sun className="h-5 w-5 text-primary" />
                                </div>
                            )}
                            <div className="space-y-0.5">
                                <span className="text-sm font-semibold text-card-foreground">{t('theme.label')}</span>
                                <p className="text-xs text-muted-foreground">{theme === 'dark' ? t('theme.dark') : theme === 'light' ? t('theme.light') : t('theme.system')}</p>
                            </div>
                        </div>
                        <Select value={theme} onValueChange={setTheme}>
                            <SelectTrigger className="w-full rounded-lg md:w-44">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent className="rounded-lg">
                                <SelectItem value="light" className="rounded-xl">
                                    <Sun className="size-4" />
                                    {t('theme.light')}
                                </SelectItem>
                                <SelectItem value="dark" className="rounded-xl">
                                    <Moon className="size-4" />
                                    {t('theme.dark')}
                                </SelectItem>
                                <SelectItem value="system" className="rounded-xl">
                                    <Monitor className="size-4" />
                                    {t('theme.system')}
                                </SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                <Palette className="h-5 w-5 text-primary" />
                            </div>
                            <div className="space-y-0.5">
                                <span className="text-sm font-semibold text-card-foreground">主题配色 · Theme</span>
                                <p className="text-xs text-muted-foreground">选择配色预设，整站实时换肤（每个浏览器/账户独立）</p>
                            </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                            {[...BUILTIN_PRESETS, ...customThemes].map((p) => (
                                <button
                                    key={p.id}
                                    type="button"
                                    onClick={() => applyPreset(p.id)}
                                    aria-pressed={presetId === p.id}
                                    title={p.description}
                                    className={cn(
                                        'group flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition',
                                        presetId === p.id
                                            ? 'border-primary ring-2 ring-primary/30 bg-primary/5'
                                            : 'border-border/40 hover:border-primary/40 hover:bg-muted/40'
                                    )}
                                >
                                    <span
                                        className="size-5 shrink-0 rounded-full border border-border/40"
                                        style={{ background: p.swatch }}
                                    />
                                    <span className="font-medium text-card-foreground">{p.name}</span>
                                </button>
                            ))}
                        </div>
                    </div>

                    <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <button
                            type="button"
                            onClick={() => setThemeUploadOpen((v) => !v)}
                            className="flex items-center gap-2 text-sm font-medium text-muted-foreground transition hover:text-foreground"
                        >
                            <Palette className="h-4 w-4" />
                            上传 / 自定义主题（粘贴 JSON）
                        </button>
                        {themeUploadOpen && (
                            <div className="flex flex-col gap-3">
                                <textarea
                                    value={themeUploadText}
                                    onChange={(e) => setThemeUploadText(e.target.value)}
                                    rows={8}
                                    spellCheck={false}
                                    placeholder={'{\n  "id": "mytheme",\n  "name": "我的主题",\n  "swatch": "oklch(0.6 0.15 320)",\n  "light": { "primary": "oklch(0.6 0.15 320)", "accent": "oklch(0.7 0.1 320)", "ring": "oklch(0.6 0.15 320)" },\n  "dark": { "primary": "oklch(0.68 0.15 320)" }\n}'}
                                    className="w-full rounded-lg border border-border/40 bg-background p-3 font-mono text-xs leading-5 outline-none focus:border-primary/50"
                                />
                                <div className="flex items-center gap-2">
                                    <Button
                                        type="button"
                                        size="sm"
                                        onClick={handleAddTheme}
                                        disabled={setSetting.isPending || !themeUploadText.trim()}
                                    >
                                        添加主题
                                    </Button>
                                    <p className="text-xs text-muted-foreground">
                                        支持单个对象或数组；颜色 token 用 OKLCH 或任意 CSS 颜色。等价于 PUT /api/v1/setting key=custom_themes。
                                    </p>
                                </div>
                            </div>
                        )}
                    </div>

                    <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
                        <div className="flex flex-col gap-4 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                            <div className="flex items-center gap-3">
                                <Languages className="h-5 w-5 text-muted-foreground" />
                                <span className="text-sm font-medium">{t('language.label')}</span>
                            </div>
                            <Select value={locale} onValueChange={(value) => setLocale(value as Locale)}>
                                <SelectTrigger className="w-full rounded-lg">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-lg">
                                    <SelectItem value="zh-Hans" className="rounded-xl">{t('language.zh_hans')}</SelectItem>
                                    <SelectItem value="zh-Hant" className="rounded-xl">{t('language.zh_hant')}</SelectItem>
                                    <SelectItem value="en" className="rounded-xl">{t('language.en')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex flex-col gap-4 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                            <div className="flex items-center gap-3">
                                <Clock3 className="h-5 w-5 text-muted-foreground" />
                                <span className="text-sm font-medium">{t('timeZone.label')}</span>
                            </div>
                            <Select value={timeZone} onValueChange={setTimeZone}>
                                <SelectTrigger className="w-full rounded-lg">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-lg">
                                    {TIME_ZONE_OPTIONS.map((option) => (
                                        <SelectItem key={option} value={option} className="rounded-xl">
                                            {t(`timeZone.options.${option}`)}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <p className="text-xs leading-5 text-muted-foreground">{t('timeZone.description')}</p>
                        </div>

                        <div className="flex flex-col gap-4 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                            <div className="flex items-center gap-3">
                                <Bell className="h-5 w-5 text-muted-foreground" />
                                <span className="text-sm font-medium">{t('alertLanguage.label')}</span>
                            </div>
                            <Select value={alertNotifyLanguage} onValueChange={handleAlertNotifyLanguageChange}>
                                <SelectTrigger className="w-full rounded-lg">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-lg">
                                    <SelectItem value="zh-Hans" className="rounded-xl">{t('alertLanguage.zh_hans')}</SelectItem>
                                    <SelectItem value="zh-Hant" className="rounded-xl">{t('alertLanguage.zh_hant')}</SelectItem>
                                    <SelectItem value="en" className="rounded-xl">{t('alertLanguage.en')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <SettingWallet />

                    <ApiUsageGuide />

                    <Feedback />

                    {showAdmin && <PaymentSettings />}

                    {showAdmin && <EmailSettings />}

                    {showAdmin && <SiteIdentity />}

                    {/* 商业模式（Lodestar 一键开关：开放公开注册 = 释放商业潜力的第一步） */}
                    {showAdmin && (
                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Store className="h-5 w-5 text-primary" />
                                </div>
                                <div className="space-y-0.5">
                                    <span className="text-sm font-semibold text-card-foreground">商业模式</span>
                                    <p className="text-xs text-muted-foreground">开启=开放公开注册（访客自助注册）；关闭=自用模式（仅管理员建号）。这是「一键释放商业潜力」的总开关。</p>
                                </div>
                            </div>
                            <Switch
                                checked={settings?.find((s) => s.key === SettingKey.CommercialMode)?.value === 'true'}
                                onCheckedChange={(checked) =>
                                    setSetting.mutate(
                                        { key: SettingKey.CommercialMode, value: checked ? 'true' : 'false' },
                                        { onError: () => toast.error(t('saveFailed')) }
                                    )
                                }
                                aria-label="商业模式"
                            />
                        </div>
                    </div>
                    )}

                    {/* 维护模式 */}
                    {showAdmin && (
                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Store className="h-5 w-5 text-primary" />
                                </div>
                                <div className="space-y-0.5">
                                    <span className="text-sm font-semibold text-card-foreground">维护模式</span>
                                    <p className="text-xs text-muted-foreground">开启后对非管理员显示「站点维护中」；管理员仍可正常使用并关闭。</p>
                                </div>
                            </div>
                            <Switch
                                checked={settings?.find((s) => s.key === SettingKey.MaintenanceMode)?.value === 'true'}
                                onCheckedChange={(checked) =>
                                    setSetting.mutate(
                                        { key: SettingKey.MaintenanceMode, value: checked ? 'true' : 'false' },
                                        { onError: () => toast.error(t('saveFailed')) }
                                    )
                                }
                                aria-label="维护模式"
                            />
                        </div>
                    </div>
                    )}

                    {/* 注册需邀请码 */}
                    {showAdmin && (
                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Store className="h-5 w-5 text-primary" />
                                </div>
                                <div className="space-y-0.5">
                                    <span className="text-sm font-semibold text-card-foreground">注册需邀请码</span>
                                    <p className="text-xs text-muted-foreground">仅商业模式下生效：开启后注册需填有效邀请码（在「钱包」生成）。控制谁能注册。</p>
                                </div>
                            </div>
                            <Switch
                                checked={settings?.find((s) => s.key === SettingKey.RegisterInviteRequired)?.value === 'true'}
                                onCheckedChange={(checked) =>
                                    setSetting.mutate(
                                        { key: SettingKey.RegisterInviteRequired, value: checked ? 'true' : 'false' },
                                        { onError: () => toast.error(t('saveFailed')) }
                                    )
                                }
                                aria-label="注册需邀请码"
                            />
                        </div>
                    </div>
                    )}

                    {/* 中国化模式 */}
                    <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                                    <Landmark className="h-5 w-5 text-primary" />
                                </div>
                                <div className="space-y-0.5">
                                    <span className="text-sm font-semibold text-card-foreground">{t('chinaMode.label')}</span>
                                    <p className="text-xs text-muted-foreground">{t('chinaMode.description')}</p>
                                </div>
                            </div>
                            <Switch
                                checked={chinaMode}
                                onCheckedChange={setChinaMode}
                                aria-label={t('chinaMode.label')}
                            />
                        </div>
                        {chinaMode && (
                            <div className="flex flex-col gap-3 rounded-lg border-border/30 bg-card p-4 shadow-sm md:flex-row md:items-center md:justify-between">
                                <div className="flex items-center gap-3">
                                    <span className="text-sm font-medium">{t('chinaMode.exchangeRate.label')}</span>
                                    <span className="text-xs text-muted-foreground">{t('chinaMode.exchangeRate.hint')}</span>
                                </div>
                                <Input
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={localExchangeRate}
                                    onChange={(e) => setLocalExchangeRate(e.target.value)}
                                    onBlur={() => {
                                        const parsed = parseFloat(localExchangeRate);
                                        if (!isNaN(parsed) && parsed > 0) {
                                            setExchangeRate(parsed);
                                            initialExchangeRate.current = parsed;
                                        } else {
                                            setLocalExchangeRate(initialExchangeRate.current.toString());
                                        }
                                    }}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            (e.target as HTMLInputElement).blur();
                                        }
                                    }}
                                    className="w-48 rounded-xl"
                                    placeholder="7.2"
                                />
                            </div>
                        )}
                    </div>
                    <div className="grid items-start gap-4 xl:grid-cols-2">
                        <NavigationPreferences />
                        <SettingOrder />
                    </div>
                </div>
            </div>
        </div>
    );
}
