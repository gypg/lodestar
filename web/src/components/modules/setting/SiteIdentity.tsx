'use client';

/*
GGZERO — 站点信息设置。

让管理员配置平台对外身份：站点名称 / 简介 / 公告 / 页脚。这些值经公开端点
(/api/v1/public/overview) 展示在落地页（封面刊头、关于、公告），是 octopus 没有、
让本站"属于自己"的平台层配置。
*/

import { useEffect, useState } from 'react';
import { Globe } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

export function SiteIdentity() {
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [name, setName] = useState('');
    const [desc, setDesc] = useState('');
    const [announce, setAnnounce] = useState('');
    const [footer, setFooter] = useState('');
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const get = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setName(get(SettingKey.SiteName));
        setDesc(get(SettingKey.SiteDescription));
        setAnnounce(get(SettingKey.SiteAnnouncement));
        setFooter(get(SettingKey.SiteFooter));
        setLoaded(true);
    }, [settings, loaded]);

    const save = () => {
        const entries = [
            { key: SettingKey.SiteName, value: name },
            { key: SettingKey.SiteDescription, value: desc },
            { key: SettingKey.SiteAnnouncement, value: announce },
            { key: SettingKey.SiteFooter, value: footer },
        ];
        Promise.all(entries.map((e) => setSetting.mutateAsync(e)))
            .then(() => toast.success('站点信息已保存'))
            .catch(() => toast.error('保存失败'));
    };

    const textareaCls =
        'w-full rounded-lg border border-border/40 bg-background p-3 text-sm leading-6 outline-none focus:border-primary/50';

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <Globe className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">站点信息</span>
                    <p className="text-xs text-muted-foreground">对外展示的平台身份：访客在首页可见（封面刊头 / 公告 / 关于）。</p>
                </div>
            </div>
            <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">站点名称</label>
                    <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="GGZERO" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">站点简介（关于本站）</label>
                    <textarea value={desc} onChange={(e) => setDesc(e.target.value)} rows={2} className={textareaCls} placeholder="一句话介绍你的站点" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">站点公告（首页公开展示）</label>
                    <textarea value={announce} onChange={(e) => setAnnounce(e.target.value)} rows={3} className={textareaCls} placeholder="留空则首页显示「暂无公告」" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">页脚文案</label>
                    <Input value={footer} onChange={(e) => setFooter(e.target.value)} placeholder="© 2026 ..." className="rounded-lg" />
                </div>
                <div>
                    <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>保存站点信息</Button>
                </div>
            </div>
        </div>
    );
}
