'use client';

/*
GGZERO — SMTP 邮件管理配置。管理员填 SMTP 凭据后可启用邮箱验证注册 + 发测试邮件。
凭据为运行时设置（构建无需凭据，对齐易支付）。
*/

import { useEffect, useState } from 'react';
import { Mail } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { useTestEmail } from '@/api/endpoints/wallet';
import { toast } from '@/components/common/Toast';

export function EmailSettings() {
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const testEmail = useTestEmail();
    const [enabled, setEnabled] = useState(false);
    const [host, setHost] = useState('');
    const [port, setPort] = useState('587');
    const [user, setUser] = useState('');
    const [pass, setPass] = useState('');
    const [from, setFrom] = useState('');
    const [emailRequired, setEmailRequired] = useState(false);
    const [testTo, setTestTo] = useState('');
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const g = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setEnabled(g(SettingKey.SMTPEnabled) === 'true');
        setHost(g(SettingKey.SMTPHost));
        setPort(g(SettingKey.SMTPPort) || '587');
        setUser(g(SettingKey.SMTPUser));
        setPass(g(SettingKey.SMTPPass));
        setFrom(g(SettingKey.SMTPFrom));
        setEmailRequired(g(SettingKey.RegisterEmailRequired) === 'true');
        setLoaded(true);
    }, [settings, loaded]);

    const save = () => {
        Promise.all([
            setSetting.mutateAsync({ key: SettingKey.SMTPEnabled, value: enabled ? 'true' : 'false' }),
            setSetting.mutateAsync({ key: SettingKey.SMTPHost, value: host }),
            setSetting.mutateAsync({ key: SettingKey.SMTPPort, value: port }),
            setSetting.mutateAsync({ key: SettingKey.SMTPUser, value: user }),
            setSetting.mutateAsync({ key: SettingKey.SMTPPass, value: pass }),
            setSetting.mutateAsync({ key: SettingKey.SMTPFrom, value: from }),
            setSetting.mutateAsync({ key: SettingKey.RegisterEmailRequired, value: emailRequired ? 'true' : 'false' }),
        ])
            .then(() => toast.success('邮件设置已保存'))
            .catch(() => toast.error('保存失败'));
    };

    const onTest = () => {
        if (!testTo.trim()) return;
        testEmail.mutate(testTo.trim(), {
            onSuccess: () => toast.success('测试邮件已发送'),
            onError: (e) => toast.error(e instanceof Error ? e.message : '发送失败（先保存并启用 SMTP）'),
        });
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                        <Mail className="h-5 w-5 text-primary" />
                    </div>
                    <div className="space-y-0.5">
                        <span className="text-sm font-semibold text-card-foreground">邮件 · SMTP</span>
                        <p className="text-xs text-muted-foreground">配置后可启用注册邮箱验证、发送通知（587 STARTTLS）。</p>
                    </div>
                </div>
                <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="启用 SMTP" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">SMTP 服务器</label>
                    <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="smtp.example.com" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">端口</label>
                    <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="587" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">用户名</label>
                    <Input value={user} onChange={(e) => setUser(e.target.value)} className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">密码 / 授权码</label>
                    <Input value={pass} onChange={(e) => setPass(e.target.value)} type="password" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5 sm:col-span-2">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">发件人地址</label>
                    <Input value={from} onChange={(e) => setFrom(e.target.value)} placeholder="noreply@example.com" className="rounded-lg" />
                </div>
            </div>
            <div className="flex items-center justify-between rounded-lg border border-border/30 bg-card p-3">
                <div className="space-y-0.5">
                    <span className="text-sm font-medium text-card-foreground">注册需邮箱验证</span>
                    <p className="text-xs text-muted-foreground">仅商业模式生效：注册需邮箱收到的验证码。</p>
                </div>
                <Switch checked={emailRequired} onCheckedChange={setEmailRequired} aria-label="注册需邮箱验证" />
            </div>
            <div className="flex items-end gap-2">
                <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>保存邮件设置</Button>
                <Input value={testTo} onChange={(e) => setTestTo(e.target.value)} placeholder="测试收件邮箱" className="h-9 w-48 rounded-lg" />
                <Button type="button" variant="outline" size="sm" onClick={onTest} disabled={testEmail.isPending || !testTo.trim()}>发测试邮件</Button>
            </div>
        </div>
    );
}
