'use client';

/*
Lodestar commercial layer — Stripe payment settings.

Admin fills in Stripe API key, webhook secret, currency, and minimum topup amount.
Follows the same pattern as PaymentSettings.tsx (Epay).
Sensitive fields (API key, webhook secret) are masked in the display.
*/

import { useEffect, useState } from 'react';
import { CreditCard } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

/** Mask a secret: show first 4 + last 4 chars, replace middle with asterisks. */
function maskSecret(value: string): string {
    if (!value || value.length <= 10) return value;
    return `${value.slice(0, 4)}${'*'.repeat(value.length - 8)}${value.slice(-4)}`;
}

export function StripeSettings() {
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [enabled, setEnabled] = useState(false);
    const [apiKey, setApiKey] = useState('');
    const [webhookSecret, setWebhookSecret] = useState('');
    const [currency, setCurrency] = useState('usd');
    const [minTopup, setMinTopup] = useState('5');
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const g = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setEnabled(g(SettingKey.StripeEnabled) === 'true');
        setApiKey(g(SettingKey.StripeAPIKey));
        setWebhookSecret(g(SettingKey.StripeWebhookSecret));
        setCurrency(g(SettingKey.StripeCurrency) || 'usd');
        setMinTopup(g(SettingKey.StripeMinTopup) || '5');
        setLoaded(true);
    }, [settings, loaded]);

    const save = () => {
        Promise.all([
            setSetting.mutateAsync({ key: SettingKey.StripeEnabled, value: enabled ? 'true' : 'false' }),
            setSetting.mutateAsync({ key: SettingKey.StripeAPIKey, value: apiKey }),
            setSetting.mutateAsync({ key: SettingKey.StripeWebhookSecret, value: webhookSecret }),
            setSetting.mutateAsync({ key: SettingKey.StripeCurrency, value: currency }),
            setSetting.mutateAsync({ key: SettingKey.StripeMinTopup, value: minTopup }),
        ])
            .then(() => toast.success('Stripe 设置已保存'))
            .catch(() => toast.error('保存失败'));
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                        <CreditCard className="h-5 w-5 text-primary" />
                    </div>
                    <div className="space-y-0.5">
                        <span className="text-sm font-semibold text-card-foreground">在线支付 · Stripe</span>
                        <p className="text-xs text-muted-foreground">填入 Stripe 凭据并启用后，用户可通过 Stripe Checkout 在线充值。</p>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="启用 Stripe" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">Stripe API Key</label>
                    <Input
                        value={apiKey}
                        onChange={(e) => setApiKey(e.target.value)}
                        placeholder={apiKey ? maskSecret(apiKey) : 'sk_live_...'}
                        type="password"
                        className="rounded-lg"
                    />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">Webhook Secret</label>
                    <Input
                        value={webhookSecret}
                        onChange={(e) => setWebhookSecret(e.target.value)}
                        placeholder={webhookSecret ? maskSecret(webhookSecret) : 'whsec_...'}
                        type="password"
                        className="rounded-lg"
                    />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">货币（三字母代码）</label>
                    <Input value={currency} onChange={(e) => setCurrency(e.target.value)} placeholder="usd" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">最低充值金额</label>
                    <Input value={minTopup} onChange={(e) => setMinTopup(e.target.value)} type="number" step="0.01" min="0" className="rounded-lg" />
                </div>
            </div>
            <div>
                <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>保存 Stripe 设置</Button>
            </div>
        </div>
    );
}
