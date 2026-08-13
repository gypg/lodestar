'use client';

import { useCallback, useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import { Plus, X, XIcon } from 'lucide-react';
import {
    Dialog,
    DialogContent,
    DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from '@/components/ui/accordion';
import { ProxySelector } from '@/components/modules/proxy-pool/ProxySelector';
import { toast } from '@/components/common/Toast';
import { useSettingStore } from '@/stores/setting';
import {
    Site as SiteRecord,
    SitePlatform,
    type CustomHeader,
    useCreateSite,
    useDetectSitePlatform,
    useUpdateSite,
} from '@/api/endpoints/site';
import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { translateSiteMessage } from './site-message';

type SiteFormState = {
    name: string;
    platform: SitePlatform | '';
    base_url: string;
    enabled: boolean;
    proxy_mode: Exclude<ProxyMode, 'inherit'>;
    proxy_config_id: number | null;
    external_checkin_url: string;
    is_pinned: boolean;
    sort_order: number;
    global_weight: number;
    custom_header: CustomHeader[];
};

const AUTO_DETECT_VALUE = '__auto__';

const PLATFORM_LABELS: Record<SitePlatform, string> = {
    [SitePlatform.NewAPI]: 'New API',
    [SitePlatform.AnyRouter]: 'AnyRouter',
    [SitePlatform.OneAPI]: 'One API',
    [SitePlatform.OneHub]: 'One Hub',
    [SitePlatform.DoneHub]: 'Done Hub',
    [SitePlatform.Sub2API]: 'Sub2API',
    [SitePlatform.OpenAI]: 'OpenAI',
    [SitePlatform.Claude]: 'Claude',
    [SitePlatform.Gemini]: 'Gemini',
    [SitePlatform.SAPI]: 'SAPI',
};

function createEmptySiteForm(): SiteFormState {
    return {
        name: '',
        platform: '',
        base_url: '',
        enabled: true,
        proxy_mode: 'direct',
        proxy_config_id: null,
        external_checkin_url: '',
        is_pinned: false,
        sort_order: 0,
        global_weight: 1,
        custom_header: [{ header_key: '', header_value: '' }],
    };
}

function createSiteForm(site: SiteRecord): SiteFormState {
    return {
        name: site.name,
        platform: site.platform,
        base_url: site.base_url,
        enabled: site.enabled,
        proxy_mode: site.proxy_mode ?? 'direct',
        proxy_config_id: site.proxy_config_id ?? null,
        external_checkin_url: site.external_checkin_url ?? '',
        is_pinned: site.is_pinned,
        sort_order: site.sort_order,
        global_weight: site.global_weight,
        custom_header: site.custom_header.length > 0
            ? site.custom_header.map((item) => ({ ...item }))
            : [{ header_key: '', header_value: '' }],
    };
}

function normalizeSiteRecord(site: SiteRecord): SiteRecord {
    return {
        ...site,
        custom_header: site.custom_header ?? [],
        proxy_mode: site.proxy_mode ?? 'direct',
        proxy_config_id: site.proxy_config_id ?? null,
        external_checkin_url: site.external_checkin_url ?? null,
        is_pinned: site.is_pinned ?? false,
        sort_order: typeof site.sort_order === 'number' ? site.sort_order : 0,
        global_weight:
            typeof site.global_weight === 'number' && site.global_weight > 0
                ? site.global_weight
                : 1,
        accounts: (site.accounts ?? []).map((account) => ({
            ...account,
            proxy_mode: account.proxy_mode ?? 'inherit',
            proxy_config_id: account.proxy_config_id ?? null,
        })),
    };
}

function trimHeaders(items: CustomHeader[]) {
    return items
        .map((item) => ({
            header_key: item.header_key.trim(),
            header_value: item.header_value.trim(),
        }))
        .filter((item) => item.header_key || item.header_value);
}

/**
 * Extracts an upstream error message. Returns null when the error carries no
 * usable text, so the caller can supply the localized fallback — a module-level
 * helper must not hold copy of its own (the i18n gate cannot see it there).
 */
function getErrorMessage(error: unknown): string | null {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return null;
}

interface SiteEditDialogProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    site: SiteRecord | null;
    onCreated?: (site: SiteRecord) => void;
}

/**
 * 站点编辑/创建弹窗。视觉风格与 Channel/Group 卡片编辑面板（MorphingDialog）保持一致：
 * bg-card / rounded-3xl / text-2xl 标题 / 自定义 close 按钮 / 整体 flex 布局并对长表单
 * 提供独立滚动区域，避免视口高度较小时底部按钮被裁切。
 */
export function SiteEditDialog({ open, onOpenChange, site, onCreated }: SiteEditDialogProps) {
    const t = useTranslations();
    const tForm = useTranslations('site.siteForm');
    const tProxy = useTranslations('proxyPool');
    const locale = useSettingStore((state) => state.locale);
    const createSite = useCreateSite();
    const updateSite = useUpdateSite();
    const detectPlatform = useDetectSitePlatform();
    const [siteForm, setSiteForm] = useState<SiteFormState>(() =>
        site ? createSiteForm(site) : createEmptySiteForm(),
    );

    const handleSubmit = useCallback(
        async (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();

            if (!siteForm.name.trim()) {
                toast.error(tForm('nameRequired'));
                return;
            }
            if (!siteForm.base_url.trim()) {
                toast.error(tForm('baseUrlRequired'));
                return;
            }

            let platform = siteForm.platform;
            if (!platform && !site) {
                try {
                    const detected = await detectPlatform.mutateAsync(
                        siteForm.base_url.trim(),
                    );
                    platform = detected.platform as SitePlatform;
                    toast.success(
                        tForm('platformDetected', {
                            platform: PLATFORM_LABELS[platform] ?? platform,
                        }),
                    );
                } catch (e) { console.error(e);
                    toast.error(tForm('platformDetectFailed'));
                    return;
                }
            }
            if (!platform) {
                toast.error(tForm('platformRequired'));
                return;
            }

            const customHeader = trimHeaders(siteForm.custom_header);
            const invalidHeader = customHeader.find(
                (item) => !item.header_key || !item.header_value,
            );
            if (invalidHeader) {
                toast.error(tForm('headerIncomplete'));
                return;
            }

            if (siteForm.proxy_mode === 'pool' && !siteForm.proxy_config_id) {
                toast.error(tProxy('selectRequired'));
                return;
            }

            const payload = {
                name: siteForm.name.trim(),
                platform: platform as SitePlatform,
                base_url: siteForm.base_url.trim(),
                enabled: siteForm.enabled,
                proxy_mode: siteForm.proxy_mode,
                proxy_config_id:
                    siteForm.proxy_mode === 'pool' ? siteForm.proxy_config_id : null,
                external_checkin_url: siteForm.external_checkin_url.trim() || null,
                is_pinned: siteForm.is_pinned,
                sort_order: siteForm.sort_order,
                global_weight: siteForm.global_weight,
                custom_header: customHeader,
            };

            try {
                if (site) {
                    await updateSite.mutateAsync({ id: site.id, ...payload });
                    toast.success(tForm('updated'));
                    onOpenChange(false);
                } else {
                    const createdSite = normalizeSiteRecord(
                        await createSite.mutateAsync(payload),
                    );
                    toast.success(tForm('created'));
                    onOpenChange(false);
                    onCreated?.(createdSite);
                }
            } catch (submitError) {
                const raw = getErrorMessage(submitError) ?? tForm('actionFailed');
                toast.error(translateSiteMessage(locale, raw, t));
            }
        },
        [
            siteForm,
            site,
            detectPlatform,
            tForm,
            tProxy,
            updateSite,
            createSite,
            onOpenChange,
            onCreated,
            locale,
            t,
        ],
    );

    const isPending = createSite.isPending || updateSite.isPending || detectPlatform.isPending;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                aria-describedby={undefined}
                showCloseButton={false}
                className="w-screen max-w-full md:max-w-xl bg-card text-card-foreground px-6 py-4 rounded-3xl flex flex-col gap-0 border-0 sm:max-w-xl max-h-[min(calc(100vh-2rem),52rem)] overflow-hidden"
            >
                <header className="mb-4 flex items-start justify-between gap-4 shrink-0">
                    <div className="min-w-0 flex-1">
                        <DialogTitle className="text-2xl font-bold text-card-foreground truncate">
                            {site ? t('site.editSite') : t('site.addSite')}
                        </DialogTitle>
                    </div>
                    <button
                        type="button"
                        onClick={() => onOpenChange(false)}
                        aria-label={t('site.closeDialog')}
                        className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                    >
                        <XIcon className="size-5" />
                    </button>
                </header>

                <form className="flex flex-1 min-h-0 flex-col" onSubmit={handleSubmit}>
                    <div className="flex-1 min-h-0 space-y-5 overflow-y-auto px-1">
                        <div className="grid gap-4 md:grid-cols-2">
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">{tForm('nameLabel')}</span>
                                <Input
                                    value={siteForm.name}
                                    onChange={(event) =>
                                        setSiteForm((current) => ({
                                            ...current,
                                            name: event.target.value,
                                        }))
                                    }
                                    placeholder={tForm('namePlaceholder')}
                                    className="rounded-xl"
                                />
                            </label>

                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">{tForm('platformLabel')}</span>
                                <Select
                                    value={siteForm.platform || AUTO_DETECT_VALUE}
                                    onValueChange={(value) =>
                                        setSiteForm((current) => ({
                                            ...current,
                                            platform:
                                                value === AUTO_DETECT_VALUE
                                                    ? ''
                                                    : (value as SitePlatform),
                                        }))
                                    }
                                >
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue placeholder={tForm('platformAuto')} />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {!site && (
                                            <SelectItem className="rounded-xl" value={AUTO_DETECT_VALUE}>{tForm('platformAuto')}</SelectItem>
                                        )}
                                        {Object.entries(PLATFORM_LABELS).map(([value, label]) => (
                                            <SelectItem className="rounded-xl" key={value} value={value}>
                                                {label}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </label>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">{tForm('baseUrlLabel')}</span>
                            <Input
                                value={siteForm.base_url}
                                onChange={(event) =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        base_url: event.target.value,
                                    }))
                                }
                                placeholder="https://example.com"
                                className="rounded-xl"
                            />
                        </label>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">{tForm('checkinUrlLabel')}</span>
                            <Input
                                value={siteForm.external_checkin_url}
                                onChange={(event) =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        external_checkin_url: event.target.value,
                                    }))
                                }
                                placeholder={tForm('checkinUrlPlaceholder')}
                                className="rounded-xl"
                            />
                            <span className="text-xs text-muted-foreground">
                                {tForm('checkinUrlHint')}
                            </span>
                        </label>

                        <ProxySelector
                            value={{ proxy_mode: siteForm.proxy_mode, proxy_config_id: siteForm.proxy_config_id }}
                            onChange={(next) => setSiteForm((current) => ({
                                ...current,
                                proxy_mode: next.proxy_mode as Exclude<ProxyMode, 'inherit'>,
                                proxy_config_id: next.proxy_config_id ?? null,
                            }))}
                        />

                        <div className="flex items-center justify-between rounded-xl border border-border/60 bg-muted/20 px-4 py-3">
                            <div>
                                <div className="text-sm font-medium">{tForm('enabledLabel')}</div>
                                <div className="text-xs text-muted-foreground">
                                    {tForm('enabledHint')}
                                </div>
                            </div>
                            <Switch
                                checked={siteForm.enabled}
                                onCheckedChange={(checked) =>
                                    setSiteForm((current) => ({ ...current, enabled: checked }))
                                }
                            />
                        </div>

                        <Accordion type="single" collapsible className="w-full rounded-xl border bg-card">
                            <AccordionItem value="advanced" className="border-none">
                                <AccordionTrigger className="rounded-xl px-4 py-3 text-sm font-medium text-card-foreground transition-colors hover:bg-muted/30 hover:no-underline">
                                    {tForm('advanced')}
                                </AccordionTrigger>
                                <AccordionContent className="space-y-4 border-t px-4 pb-4 pt-4">
                                    <div className="space-y-2">
                                        <div className="flex items-center justify-between">
                                            <label className="text-sm font-medium text-card-foreground">
                                                {tForm('customHeader')} {siteForm.custom_header.length > 0 ? `(${siteForm.custom_header.length})` : ''}
                                            </label>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() =>
                                                    setSiteForm((current) => ({
                                                        ...current,
                                                        custom_header: [
                                                            ...current.custom_header,
                                                            { header_key: '', header_value: '' },
                                                        ],
                                                    }))
                                                }
                                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                                            >
                                                <Plus className="mr-1 h-3 w-3" />
                                                {tForm('addHeader')}
                                            </Button>
                                        </div>
                                        <div className="space-y-2">
                                            {siteForm.custom_header.map((item, index) => (
                                                <div key={`site-hdr-${index}`} className="flex items-center gap-2">
                                                    <Input
                                                        value={item.header_key}
                                                        onChange={(event) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.map(
                                                                    (header, headerIndex) =>
                                                                        headerIndex === index
                                                                            ? { ...header, header_key: event.target.value }
                                                                            : header,
                                                                ),
                                                            }))
                                                        }
                                                        placeholder="Header Key"
                                                        className="flex-1 rounded-xl"
                                                    />
                                                    <Input
                                                        value={item.header_value}
                                                        onChange={(event) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.map(
                                                                    (header, headerIndex) =>
                                                                        headerIndex === index
                                                                            ? {
                                                                                  ...header,
                                                                                  header_value: event.target.value,
                                                                              }
                                                                            : header,
                                                                ),
                                                            }))
                                                        }
                                                        placeholder="Header Value"
                                                        className="flex-1 rounded-xl"
                                                    />
                                                    <Button
                                                        type="button"
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={() =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.filter(
                                                                    (_, headerIndex) => headerIndex !== index,
                                                                ),
                                                            }))
                                                        }
                                                        disabled={siteForm.custom_header.length <= 1}
                                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                                        title="Remove"
                                                    >
                                                        <X className="h-4 w-4" />
                                                    </Button>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                </AccordionContent>
                            </AccordionItem>
                        </Accordion>
                    </div>

                    <footer className="mt-5 flex shrink-0 flex-col gap-3 px-1 pt-2 sm:flex-row">
                        <Button
                            type="button"
                            variant="secondary"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            onClick={() => onOpenChange(false)}
                        >
                            {tForm('cancel')}
                        </Button>
                        <Button
                            type="submit"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            disabled={isPending}
                        >
                            {isPending ? tForm('saving') : site ? tForm('saveChanges') : tForm('createSite')}
                        </Button>
                    </footer>
                </form>
            </DialogContent>
        </Dialog>
    );
}
