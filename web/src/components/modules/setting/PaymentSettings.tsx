'use client';

/*
Lodestar commercial layer — 在线支付（易支付/Epay）管理配置。

管理员在此填入商户凭据（PID/密钥/网关）后即可启用在线充值；构建本功能不需要凭据，
凭据是运行时配置（对齐 new-api 的做法）。这些值经设置 API 持久化，供 op/payment 读取。
*/

import { useEffect, useState } from 'react';
import { CreditCard } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

export function PaymentSettings() {
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [enabled, setEnabled] = useState(false);
    const [addr, setAddr] = useState('');
    const [pid, setPid] = useState('');
    const [key, setKey] = useState('');
    const [rate, setRate] = useState('1');
    const [base, setBase] = useState('');
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const g = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setEnabled(g(SettingKey.EpayEnabled) === 'true');
        setAddr(g(SettingKey.PayAddress));
        setPid(g(SettingKey.EpayPID));
        setKey(g(SettingKey.EpayKey));
        setRate(g(SettingKey.TopupRate) || '1');
        setBase(g(SettingKey.PaymentCallbackBase));
        setLoaded(true);
    }, [settings, loaded]);

    const save = () => {
        Promise.all([
            setSetting.mutateAsync({ key: SettingKey.EpayEnabled, value: enabled ? 'true' : 'false' }),
            setSetting.mutateAsync({ key: SettingKey.PayAddress, value: addr }),
            setSetting.mutateAsync({ key: SettingKey.EpayPID, value: pid }),
            setSetting.mutateAsync({ key: SettingKey.EpayKey, value: key }),
            setSetting.mutateAsync({ key: SettingKey.TopupRate, value: rate }),
            setSetting.mutateAsync({ key: SettingKey.PaymentCallbackBase, value: base }),
        ])
            .then(() => toast.success('支付设置已保存'))
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
                        <span className="text-sm font-semibold text-card-foreground">在线支付 · 易支付</span>
                        <p className="text-xs text-muted-foreground">填入商户凭据并启用后，用户可在钱包在线充值（支付宝/微信）。</p>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="启用易支付" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">网关地址</label>
                    <Input value={addr} onChange={(e) => setAddr(e.target.value)} placeholder="https://pay.example.com" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">商户 PID</label>
                    <Input value={pid} onChange={(e) => setPid(e.target.value)} className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">商户密钥</label>
                    <Input value={key} onChange={(e) => setKey(e.target.value)} type="password" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">汇率（每 1 USD 收多少）</label>
                    <Input value={rate} onChange={(e) => setRate(e.target.value)} type="number" step="0.01" min="0" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5 sm:col-span-2">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">回调站点基址（公网，用于支付通知/跳回）</label>
                    <Input value={base} onChange={(e) => setBase(e.target.value)} placeholder="https://your-site.com" className="rounded-lg" />
                </div>
            </div>
            <div>
                <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>保存支付设置</Button>
            </div>
        </div>
    );
}
