'use client';

/*
Lodestar commercial layer — wallet UI.

- Everyone: see balance, redeem a top-up code.
- Admin: generate top-up codes (calls users:write endpoint; non-admins get 403).

Balance is USD; consumed per-request when commercial_mode is on.
*/

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Wallet } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import { useWallet, useRedeemCode, useGenerateCodes, useTopup, useStripeTopup, useUsage, useGenerateInvites } from '@/api/endpoints/wallet';
import { SettingKey, useSettingList } from '@/api/endpoints/setting';
import { WalletUsageChart } from './WalletUsageChart';
import { UsageHeatmap } from './UsageHeatmap';

export function SettingWallet() {
    const t = useTranslations();
    const { data: balance } = useWallet();
    const { data: usage } = useUsage();
    const redeem = useRedeemCode();
    const genCodes = useGenerateCodes();
    const genInvites = useGenerateInvites();
    const topup = useTopup();
    const stripeTopup = useStripeTopup();
    const { data: settings } = useSettingList();
    const [code, setCode] = useState('');
    const [amount, setAmount] = useState('5');
    const [stripeAmount, setStripeAmount] = useState('5');
    const [method, setMethod] = useState('alipay');
    const [count, setCount] = useState('10');
    const [quota, setQuota] = useState('1');
    const [generated, setGenerated] = useState<string[]>([]);
    const [inviteCount, setInviteCount] = useState('10');
    const [invites, setInvites] = useState<string[]>([]);

    const onRedeem = () => {
        const c = code.trim();
        if (!c) return;
        redeem.mutate(c, {
            onSuccess: (d) => {
                toast.success(t('setting.wallet.toast.redeemed', { credited: d.credited }));
                setCode('');
            },
            onError: (e) =>
                toast.error(e instanceof Error ? e.message : t('setting.wallet.toast.redeemFailed')),
        });
    };

    const onTopup = () => {
        const amt = parseFloat(amount);
        if (!amt || amt <= 0) {
            toast.error(t('setting.wallet.toast.invalidAmount'));
            return;
        }
        topup.mutate(
            { amount: amt, method },
            {
                onSuccess: (d) => {
                    // 构造表单提交到易支付网关，跳转用户去付款
                    const form = document.createElement('form');
                    form.method = 'POST';
                    form.action = d.url;
                    Object.entries(d.params || {}).forEach(([k, v]) => {
                        const input = document.createElement('input');
                        input.type = 'hidden';
                        input.name = k;
                        input.value = String(v);
                        form.appendChild(input);
                    });
                    document.body.appendChild(form);
                    form.submit();
                },
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : t('setting.wallet.toast.topupFailed')),
            }
        );
    };

    const onStripeTopup = () => {
        const amt = parseFloat(stripeAmount);
        if (!amt || amt <= 0) {
            toast.error(t('setting.wallet.toast.invalidAmount'));
            return;
        }
        stripeTopup.mutate(
            { amount: amt },
            {
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : t('setting.wallet.toast.stripeFailed')),
            }
        );
    };

    const onGenerate = () => {
        genCodes.mutate(
            { count: parseInt(count, 10) || 0, quota: parseFloat(quota) || 0 },
            {
                onSuccess: (codes) => {
                    setGenerated(codes.map((c) => c.code));
                    toast.success(t('setting.wallet.toast.codesGenerated', { count: codes.length }));
                },
                onError: (e) =>
                    toast.error(e instanceof Error ? e.message : t('setting.wallet.toast.generateFailed')),
            }
        );
    };

    const onGenInvites = () => {
        genInvites.mutate(parseInt(inviteCount, 10) || 0, {
            onSuccess: (codes) => {
                setInvites(codes.map((c) => c.code));
                toast.success(t('setting.wallet.toast.invitesGenerated', { count: codes.length }));
            },
            onError: (e) =>
                toast.error(e instanceof Error ? e.message : t('setting.wallet.toast.generateFailed')),
        });
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <Wallet className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">{t('setting.wallet.title')}</span>
                    <p className="text-xs text-muted-foreground">{t('setting.wallet.description')}</p>
                </div>
            </div>

            <div className="flex items-center gap-4 rounded-lg border border-border/30 bg-card p-3">
                <div>
                    <div className="text-lg font-semibold tabular-nums text-primary">${(balance?.quota ?? 0).toFixed(4)}</div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{t('setting.wallet.balance')}</div>
                </div>
                <div>
                    <div className="text-lg font-semibold tabular-nums text-muted-foreground">${(balance?.used_quota ?? 0).toFixed(4)}</div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{t('setting.wallet.used')}</div>
                </div>
            </div>

            {/* 我的用量（聚合自己名下各 key） */}
            <div className="flex flex-col gap-2 rounded-lg border border-border/30 bg-card p-3">
                <div className="grid grid-cols-3 gap-2 text-center">
                    <div>
                        <div className="text-base font-semibold tabular-nums text-card-foreground">{(usage?.total_requests ?? 0).toLocaleString('en-US')}</div>
                        <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{t('setting.wallet.requests')}</div>
                    </div>
                    <div>
                        <div className="text-base font-semibold tabular-nums text-card-foreground">{(usage?.total_tokens ?? 0).toLocaleString('en-US')}</div>
                        <div className="text-[10px] uppercase tracking-wider text-muted-foreground">Tokens</div>
                    </div>
                    <div>
                        <div className="text-base font-semibold tabular-nums text-card-foreground">${(usage?.total_cost ?? 0).toFixed(4)}</div>
                        <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{t('setting.wallet.cost')}</div>
                    </div>
                </div>
                {usage && usage.per_key.length > 0 && (
                    <div className="mt-1 flex flex-col gap-1 border-t border-border/40 pt-2">
                        {usage.per_key.map((k) => (
                            <div key={k.name} className="flex items-baseline justify-between text-xs">
                                <span className="mr-3 truncate text-card-foreground">{k.name}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground">
                                    {t('setting.wallet.requestsAndCost', {
                                        requests: k.requests.toLocaleString('en-US'),
                                        cost: k.cost.toFixed(4),
                                    })}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
                {usage && (usage.per_model?.length ?? 0) > 0 && (
                    <div className="mt-1 flex flex-col gap-1 border-t border-border/40 pt-2">
                        <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{t('setting.wallet.byModel')}</div>
                        {usage.per_model!.map((m) => (
                            <div key={m.model} className="flex items-baseline justify-between text-xs">
                                <span className="mr-3 truncate font-mono text-card-foreground">{m.model}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground">
                                    {t('setting.wallet.requestsAndCost', {
                                        requests: m.requests.toLocaleString('en-US'),
                                        cost: m.cost.toFixed(4),
                                    })}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
                <div className="mt-2 border-t border-border/40 pt-2">
                    <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{t('setting.wallet.last14Days')}</div>
                    <WalletUsageChart series={usage?.daily_series} available={usage?.usage_chart_available} />
                </div>
                <div className="mt-2 border-t border-border/40 pt-2">
                    <div className="mb-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{t('setting.wallet.last30DaysHeatmap')}</div>
                    <UsageHeatmap data={usage?.heatmap_by_day} days={30} />
                </div>
            </div>

            <div className="flex items-end gap-2">
                <div className="flex flex-1 flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('setting.wallet.redeemLabel')}</label>
                    <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder={t('setting.wallet.redeemPlaceholder')} className="rounded-lg" />
                </div>
                <Button type="button" size="sm" onClick={onRedeem} disabled={redeem.isPending || !code.trim()}>{t('setting.wallet.redeemAction')}</Button>
            </div>

            {balance?.epay_configured && (
                <div className="flex items-end gap-2">
                    <div className="flex flex-1 flex-col gap-1.5">
                        <label className="ml-1 text-xs font-medium text-muted-foreground">{t('setting.wallet.onlineTopupLabel')}</label>
                        <Input value={amount} onChange={(e) => setAmount(e.target.value)} type="number" step="0.01" min="0" className="rounded-lg" />
                    </div>
                    <select
                        value={method}
                        onChange={(e) => setMethod(e.target.value)}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        <option value="alipay">{t('setting.wallet.alipay')}</option>
                        <option value="wxpay">{t('setting.wallet.wechatPay')}</option>
                    </select>
                    <Button type="button" size="sm" onClick={onTopup} disabled={topup.isPending}>{t('setting.wallet.payNow')}</Button>
                </div>
            )}

            {settings?.find((s) => s.key === SettingKey.StripeEnabled)?.value === 'true' && (
                <div className="flex items-end gap-2">
                    <div className="flex flex-1 flex-col gap-1.5">
                        <label className="ml-1 text-xs font-medium text-muted-foreground">{t('setting.wallet.stripeTopupLabel')}</label>
                        <Input value={stripeAmount} onChange={(e) => setStripeAmount(e.target.value)} type="number" step="0.01" min="0" className="rounded-lg" />
                    </div>
                    <Button type="button" size="sm" onClick={onStripeTopup} disabled={stripeTopup.isPending}>Pay with Stripe</Button>
                </div>
            )}

            <details className="rounded-lg border border-border/30 bg-card p-3">
                <summary className="cursor-pointer text-sm font-medium text-card-foreground">{t('setting.wallet.adminGenerateCodes')}</summary>
                <div className="mt-3 flex flex-col gap-3">
                    <div className="flex items-end gap-2">
                        <div className="flex flex-col gap-1">
                            <label className="ml-1 text-xs text-muted-foreground">{t('setting.wallet.countLabel')}</label>
                            <Input value={count} onChange={(e) => setCount(e.target.value)} type="number" min="1" className="w-24 rounded-lg" />
                        </div>
                        <div className="flex flex-col gap-1">
                            <label className="ml-1 text-xs text-muted-foreground">{t('setting.wallet.codeValueLabel')}</label>
                            <Input value={quota} onChange={(e) => setQuota(e.target.value)} type="number" step="0.01" min="0" className="w-32 rounded-lg" />
                        </div>
                        <Button type="button" size="sm" onClick={onGenerate} disabled={genCodes.isPending}>{t('setting.wallet.generate')}</Button>
                    </div>
                    {generated.length > 0 && (
                        <textarea
                            readOnly
                            value={generated.join('\n')}
                            rows={Math.min(generated.length, 8)}
                            className="w-full rounded-lg border border-border/40 bg-background p-2 font-mono text-xs"
                            onFocus={(e) => e.currentTarget.select()}
                        />
                    )}
                </div>
            </details>

            <details className="rounded-lg border border-border/30 bg-card p-3">
                <summary className="cursor-pointer text-sm font-medium text-card-foreground">{t('setting.wallet.adminGenerateInvites')}</summary>
                <div className="mt-3 flex flex-col gap-3">
                    <div className="flex items-end gap-2">
                        <div className="flex flex-col gap-1">
                            <label className="ml-1 text-xs text-muted-foreground">{t('setting.wallet.countLabel')}</label>
                            <Input value={inviteCount} onChange={(e) => setInviteCount(e.target.value)} type="number" min="1" className="w-24 rounded-lg" />
                        </div>
                        <Button type="button" size="sm" onClick={onGenInvites} disabled={genInvites.isPending}>{t('setting.wallet.generateInvites')}</Button>
                        <p className="text-xs text-muted-foreground">{t('setting.wallet.inviteHint')}</p>
                    </div>
                    {invites.length > 0 && (
                        <textarea
                            readOnly
                            value={invites.join('\n')}
                            rows={Math.min(invites.length, 8)}
                            className="w-full rounded-lg border border-border/40 bg-background p-2 font-mono text-xs"
                            onFocus={(e) => e.currentTarget.select()}
                        />
                    )}
                </div>
            </details>
        </div>
    );
}
