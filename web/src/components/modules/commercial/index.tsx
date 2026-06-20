'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import {
    Store, CreditCard, ToggleLeft, ToggleRight,
    ChevronDown, ChevronRight, Shield, Calculator,
    Package, Users, Settings,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import type { BootstrapStatusResponse } from '@/api/endpoints/bootstrap';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { PageWrapper } from '@/components/common/PageWrapper';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';

// ── Commercial Mode Toggle ──────────────────────────────────────────────────

function CommercialModeToggle() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const enabled = settings?.find((s) => s.key === SettingKey.CommercialMode)?.value === 'true';

    const toggle = (next: boolean) => {
        setSetting.mutate(
            { key: SettingKey.CommercialMode, value: next ? 'true' : 'false' },
            {
                onSuccess: () => toast.success(next ? t('commercialMode.enable') : t('commercialMode.disable')),
                onError: () => toast.error('Failed'),
            },
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-xl border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-5 shadow-sm">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/12">
                        <Store className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                        <h3 className="text-base font-semibold text-card-foreground">{t('commercialMode.title')}</h3>
                        <p className="text-xs text-muted-foreground mt-0.5">{t('commercialMode.description')}</p>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={toggle} />
            </div>
            <div className="flex items-center gap-2 pl-13">
                <span className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium border ${
                    enabled
                        ? 'bg-primary/10 text-primary border-primary/20'
                        : 'bg-muted text-muted-foreground border-border'
                }`}>
                    {enabled ? <ToggleRight className="size-3.5" /> : <ToggleLeft className="size-3.5" />}
                    {enabled ? t('commercialMode.statusCommercial') : t('commercialMode.statusSelfUse')}
                </span>
            </div>
        </div>
    );
}

// ── Stripe Settings (inline) ────────────────────────────────────────────────

function StripeSection() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [apiKey, setApiKey] = useState('');
    const [webhookSecret, setWebhookSecret] = useState('');
    const [currency, setCurrency] = useState('usd');
    const [minTopup, setMinTopup] = useState('5');
    const [loaded, setLoaded] = useState(false);
    const enabled = settings?.find((s) => s.key === SettingKey.StripeEnabled)?.value === 'true';

    useEffect(() => {
        if (!settings || loaded) return;
        const g = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setApiKey(g(SettingKey.StripeAPIKey));
        setWebhookSecret(g(SettingKey.StripeWebhookSecret));
        setCurrency(g(SettingKey.StripeCurrency) || 'usd');
        setMinTopup(g(SettingKey.StripeMinTopup) || '5');
        setLoaded(true);
    }, [settings, loaded]);

    const mask = (v: string) => v.length > 10 ? `${v.slice(0, 4)}${'*'.repeat(v.length - 8)}${v.slice(-4)}` : v;

    const save = () => {
        Promise.all([
            setSetting.mutateAsync({ key: SettingKey.StripeEnabled, value: enabled ? 'true' : 'false' }),
            setSetting.mutateAsync({ key: SettingKey.StripeAPIKey, value: apiKey }),
            setSetting.mutateAsync({ key: SettingKey.StripeWebhookSecret, value: webhookSecret }),
            setSetting.mutateAsync({ key: SettingKey.StripeCurrency, value: currency }),
            setSetting.mutateAsync({ key: SettingKey.StripeMinTopup, value: minTopup }),
        ]).then(() => toast.success(t('stripe.saved'))).catch(() => toast.error(t('stripe.saveFailed')));
    };

    return (
        <CollapsibleSection icon={<CreditCard className="h-4 w-4" />} title={t('stripe.title')}>
            <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                    <label className="text-xs font-medium text-muted-foreground">{t('stripe.apiKey')}</label>
                    <Input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={apiKey ? mask(apiKey) : 'sk_live_...'} type="password" className="rounded-lg text-xs" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="text-xs font-medium text-muted-foreground">{t('stripe.webhookSecret')}</label>
                    <Input value={webhookSecret} onChange={(e) => setWebhookSecret(e.target.value)} placeholder={webhookSecret ? mask(webhookSecret) : 'whsec_...'} type="password" className="rounded-lg text-xs" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="text-xs font-medium text-muted-foreground">{t('stripe.currency')}</label>
                    <Input value={currency} onChange={(e) => setCurrency(e.target.value)} placeholder="usd" className="rounded-lg text-xs" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="text-xs font-medium text-muted-foreground">{t('stripe.minTopup')}</label>
                    <Input value={minTopup} onChange={(e) => setMinTopup(e.target.value)} type="number" className="rounded-lg text-xs" />
                </div>
            </div>
            <div className="flex items-center justify-between mt-3">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span>{t('stripe.enable')}:</span>
                    <Switch checked={enabled} onCheckedChange={(v) => setSetting.mutate({ key: SettingKey.StripeEnabled, value: v ? 'true' : 'false' })} />
                </div>
                <Button size="sm" onClick={save} className="rounded-lg">{t('stripe.save')}</Button>
            </div>
        </CollapsibleSection>
    );
}

// ── BillingExpr Section ─────────────────────────────────────────────────────

function BillingExprSection() {
    const t = useTranslations('setting');
    return (
        <CollapsibleSection icon={<Calculator className="h-4 w-4" />} title={t('billingExpr.title')}>
            <p className="text-xs text-muted-foreground">{t('billingExpr.description')}</p>
            <Button variant="outline" size="sm" className="rounded-lg mt-2" onClick={() => {
                // Navigate to settings billing-expr by simulating click
                const el = document.querySelector('[data-setting-id="billing-expr"]') as HTMLElement;
                el?.click();
            }}>
                {t('billingExpr.title')} →
            </Button>
        </CollapsibleSection>
    );
}

// ── Collapsible Section ─────────────────────────────────────────────────────

function CollapsibleSection({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
    const [open, setOpen] = useState(false);
    return (
        <div className="rounded-xl border border-border/35 bg-card">
            <button type="button" onClick={() => setOpen(!open)} className="flex w-full items-center gap-2.5 px-4 py-3 text-left text-sm font-medium text-card-foreground hover:bg-muted/30 transition-colors">
                {open ? <ChevronDown className="size-4 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-4 shrink-0 text-muted-foreground" />}
                <span className="text-muted-foreground">{icon}</span>
                <span>{title}</span>
            </button>
            {open && <div className="border-t border-border/30 p-4 space-y-3">{children}</div>}
        </div>
    );
}

// ── Main Commercial Page ────────────────────────────────────────────────────

export function Commercial() {
    const t = useTranslations('setting');
    const { data: currentUser } = useCurrentUser();
    const isAdmin = currentUser !== undefined && isStaffRole(currentUser.role);
    const { data: settings } = useSettingList();
    const isCommercial = settings?.find((s) => s.key === SettingKey.CommercialMode)?.value === 'true';

    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl space-y-4 pb-6">
            {/* Commercial mode toggle — always visible for admin */}
            {isAdmin && <CommercialModeToggle />}

            {/* Commercial features — only shown when mode is ON */}
            {isCommercial ? (
                <div className="space-y-3">
                    <div className="flex items-center gap-2 px-1 text-xs text-muted-foreground">
                        <Settings className="size-3.5" />
                        <span>商业功能配置</span>
                    </div>
                    <StripeSection />
                    <BillingExprSection />
                    <CollapsibleSection icon={<Package className="size-4" />} title="订阅管理">
                        <p className="text-xs text-muted-foreground">管理订阅方案和用户订阅。</p>
                        <Button variant="outline" size="sm" className="rounded-lg mt-2" onClick={() => {
                            const store = (window as unknown as { __navSetActive?: (id: string) => void }).__navSetActive;
                            store?.('subscription');
                        }}>
                            前往订阅管理 →
                        </Button>
                    </CollapsibleSection>
                </div>
            ) : (
                <div className="flex flex-col items-center justify-center gap-3 py-12 text-center">
                    <div className="grid size-16 place-items-center rounded-2xl bg-muted/50">
                        <Store className="size-8 text-muted-foreground/50" />
                    </div>
                    <div>
                        <h3 className="text-sm font-medium text-muted-foreground">自用模式</h3>
                        <p className="text-xs text-muted-foreground/70 mt-1 max-w-sm">
                            开启商业模式后，此处将显示支付、订阅、计费等商业配置。
                        </p>
                    </div>
                </div>
            )}
        </PageWrapper>
    );
}
