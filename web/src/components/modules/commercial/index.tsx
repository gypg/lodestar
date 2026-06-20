'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import {
    Store, CreditCard, ToggleLeft, ToggleRight,
    ChevronDown, ChevronRight, Calculator, Package, Users, Mail, Shield, Globe2,
} from 'lucide-react';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { PageWrapper } from '@/components/common/PageWrapper';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { PaymentSettings } from '@/components/modules/setting/PaymentSettings';
import { EmailSettings } from '@/components/modules/setting/EmailSettings';

// ── Collapsible Section ─────────────────────────────────────────────────────

function Section({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
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

// ── Stripe Settings ─────────────────────────────────────────────────────────

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
        <Section icon={<CreditCard className="h-4 w-4" />} title={t('stripe.title')}>
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
        </Section>
    );
}

// ── Registration Settings ───────────────────────────────────────────────────

function RegistrationSection() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const inviteRequired = settings?.find((s) => s.key === SettingKey.RegisterInviteRequired)?.value === 'true';
    const emailRequired = settings?.find((s) => s.key === SettingKey.RegisterEmailRequired)?.value === 'true';

    return (
        <Section icon={<Users className="h-4 w-4" />} title="注册设置">
            <div className="space-y-3">
                <div className="flex items-center justify-between">
                    <div>
                        <span className="text-sm font-medium text-card-foreground">注册需邀请码</span>
                        <p className="text-xs text-muted-foreground">开启后新用户注册需要有效邀请码</p>
                    </div>
                    <Switch checked={inviteRequired} onCheckedChange={(v) => setSetting.mutate({ key: SettingKey.RegisterInviteRequired, value: v ? 'true' : 'false' })} />
                </div>
                <div className="flex items-center justify-between">
                    <div>
                        <span className="text-sm font-medium text-card-foreground">注册需邮箱验证</span>
                        <p className="text-xs text-muted-foreground">开启后新用户注册需要邮箱验证码（需配置 SMTP）</p>
                    </div>
                    <Switch checked={emailRequired} onCheckedChange={(v) => setSetting.mutate({ key: SettingKey.RegisterEmailRequired, value: v ? 'true' : 'false' })} />
                </div>
            </div>
        </Section>
    );
}

// ── Main Commercial Page ────────────────────────────────────────────────────

export function Commercial() {
    const t = useTranslations('setting');
    const { data: currentUser } = useCurrentUser();
    const isAdmin = currentUser !== undefined && isStaffRole(currentUser.role);
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const isCommercial = settings?.find((s) => s.key === SettingKey.CommercialMode)?.value === 'true';

    const toggleCommercial = (next: boolean) => {
        setSetting.mutate(
            { key: SettingKey.CommercialMode, value: next ? 'true' : 'false' },
            {
                onSuccess: () => toast.success(next ? t('commercialMode.enable') : t('commercialMode.disable')),
                onError: () => toast.error('Failed'),
            },
        );
    };

    if (!isAdmin) return null;

    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl space-y-4 pb-6">
            {/* Toggle — always at top */}
            <div className="rounded-xl border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-5 shadow-sm">
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
                    <Switch checked={isCommercial} onCheckedChange={toggleCommercial} />
                </div>
                <div className="flex items-center gap-2 pl-13 mt-3">
                    <span className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium border ${
                        isCommercial
                            ? 'bg-primary/10 text-primary border-primary/20'
                            : 'bg-muted text-muted-foreground border-border'
                    }`}>
                        {isCommercial ? <ToggleRight className="size-3.5" /> : <ToggleLeft className="size-3.5" />}
                        {isCommercial ? t('commercialMode.statusCommercial') : t('commercialMode.statusSelfUse')}
                    </span>
                </div>
            </div>

            {/* Commercial features — only when ON */}
            {isCommercial && (
                <>
                    {/* Payment gateways */}
                    <Section icon={<CreditCard className="h-4 w-4" />} title="支付网关">
                        <PaymentSettings />
                    </Section>
                    <StripeSection />

                    {/* Subscription */}
                    <Section icon={<Package className="size-4" />} title="订阅管理">
                        <p className="text-xs text-muted-foreground">管理订阅方案和用户订阅，请前往「订阅」标签。</p>
                    </Section>

                    {/* Billing */}
                    <Section icon={<Calculator className="h-4 w-4" />} title={t('billingExpr.title')}>
                        <p className="text-xs text-muted-foreground">{t('billingExpr.description')}</p>
                    </Section>

                    {/* Registration */}
                    <RegistrationSection />

                    {/* Email / SMTP */}
                    <Section icon={<Mail className="h-4 w-4" />} title="邮件设置">
                        <EmailSettings />
                    </Section>
                </>
            )}
        </PageWrapper>
    );
}
