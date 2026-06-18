'use client';

/*
GGZERO commercial layer — wallet UI.

- Everyone: see balance, redeem a top-up code.
- Admin: generate top-up codes (calls users:write endpoint; non-admins get 403).

Balance is USD; consumed per-request when commercial_mode is on.
*/

import { useState } from 'react';
import { Wallet } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import { useWallet, useRedeemCode, useGenerateCodes, useTopup } from '@/api/endpoints/wallet';

export function SettingWallet() {
    const { data: balance } = useWallet();
    const redeem = useRedeemCode();
    const genCodes = useGenerateCodes();
    const topup = useTopup();
    const [code, setCode] = useState('');
    const [amount, setAmount] = useState('5');
    const [method, setMethod] = useState('alipay');
    const [count, setCount] = useState('10');
    const [quota, setQuota] = useState('1');
    const [generated, setGenerated] = useState<string[]>([]);

    const onRedeem = () => {
        const c = code.trim();
        if (!c) return;
        redeem.mutate(c, {
            onSuccess: (d) => {
                toast.success(`充值成功，+$${d.credited}`);
                setCode('');
            },
            onError: (e) => toast.error(e instanceof Error ? e.message : '兑换失败'),
        });
    };

    const onTopup = () => {
        const amt = parseFloat(amount);
        if (!amt || amt <= 0) {
            toast.error('请输入有效金额');
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
                onError: (e) => toast.error(e instanceof Error ? e.message : '发起支付失败（需管理员配置支付）'),
            }
        );
    };

    const onGenerate = () => {
        genCodes.mutate(
            { count: parseInt(count, 10) || 0, quota: parseFloat(quota) || 0 },
            {
                onSuccess: (codes) => {
                    setGenerated(codes.map((c) => c.code));
                    toast.success(`已生成 ${codes.length} 个兑换码`);
                },
                onError: (e) => toast.error(e instanceof Error ? e.message : '生成失败（需管理员权限）'),
            }
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <Wallet className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">钱包 · 余额</span>
                    <p className="text-xs text-muted-foreground">商业模式开启时，每次请求按成本（USD）从余额扣减。</p>
                </div>
            </div>

            <div className="flex items-center gap-4 rounded-lg border border-border/30 bg-card p-3">
                <div>
                    <div className="text-lg font-semibold tabular-nums text-primary">${(balance?.quota ?? 0).toFixed(4)}</div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">余额</div>
                </div>
                <div>
                    <div className="text-lg font-semibold tabular-nums text-muted-foreground">${(balance?.used_quota ?? 0).toFixed(4)}</div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">已用</div>
                </div>
            </div>

            <div className="flex items-end gap-2">
                <div className="flex flex-1 flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">兑换码充值</label>
                    <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="gz-..." className="rounded-lg" />
                </div>
                <Button type="button" size="sm" onClick={onRedeem} disabled={redeem.isPending || !code.trim()}>兑换</Button>
            </div>

            {balance?.epay_configured && (
                <div className="flex items-end gap-2">
                    <div className="flex flex-1 flex-col gap-1.5">
                        <label className="ml-1 text-xs font-medium text-muted-foreground">在线充值 (USD)</label>
                        <Input value={amount} onChange={(e) => setAmount(e.target.value)} type="number" step="0.01" min="0" className="rounded-lg" />
                    </div>
                    <select
                        value={method}
                        onChange={(e) => setMethod(e.target.value)}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        <option value="alipay">支付宝</option>
                        <option value="wxpay">微信</option>
                    </select>
                    <Button type="button" size="sm" onClick={onTopup} disabled={topup.isPending}>去支付</Button>
                </div>
            )}

            <details className="rounded-lg border border-border/30 bg-card p-3">
                <summary className="cursor-pointer text-sm font-medium text-card-foreground">管理员 · 生成兑换码</summary>
                <div className="mt-3 flex flex-col gap-3">
                    <div className="flex items-end gap-2">
                        <div className="flex flex-col gap-1">
                            <label className="ml-1 text-xs text-muted-foreground">数量</label>
                            <Input value={count} onChange={(e) => setCount(e.target.value)} type="number" min="1" className="w-24 rounded-lg" />
                        </div>
                        <div className="flex flex-col gap-1">
                            <label className="ml-1 text-xs text-muted-foreground">每个面值 (USD)</label>
                            <Input value={quota} onChange={(e) => setQuota(e.target.value)} type="number" step="0.01" min="0" className="w-32 rounded-lg" />
                        </div>
                        <Button type="button" size="sm" onClick={onGenerate} disabled={genCodes.isPending}>生成</Button>
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
        </div>
    );
}
