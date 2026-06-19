'use client';

/*
Lodestar — 站点信息设置。

让管理员配置平台对外身份：站点名称 / 简介 / 公告 / 页脚。这些值经公开端点
(/api/v1/public/overview) 展示在落地页（封面刊头、关于、公告），是 octopus 没有、
让本站"属于自己"的平台层配置。
*/

import { useEffect, useState } from 'react';
import { Globe } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { useQueryClient } from '@tanstack/react-query';
import { toast } from '@/components/common/Toast';

export function SiteIdentity() {
    const queryClient = useQueryClient();
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [name, setName] = useState('');
    const [desc, setDesc] = useState('');
    const [announce, setAnnounce] = useState('');
    const [footer, setFooter] = useState('');
    const [ambient, setAmbient] = useState<'photo' | 'classic' | 'color4bg'>('photo');
    const [bannerOn, setBannerOn] = useState(false);
    const [bannerText, setBannerText] = useState('');
    const [bannerTone, setBannerTone] = useState<'info' | 'warning' | 'success'>('info');
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        if (!settings || loaded) return;
        const get = (k: string) => settings.find((s) => s.key === k)?.value ?? '';
        setName(get(SettingKey.SiteName));
        setDesc(get(SettingKey.SiteDescription));
        setAnnounce(get(SettingKey.SiteAnnouncement));
        setFooter(get(SettingKey.SiteFooter));
        const am = get(SettingKey.LandingAmbientMode);
        setAmbient(am === 'color4bg' ? 'color4bg' : am === 'classic' ? 'classic' : 'photo');
        setBannerOn(get(SettingKey.SiteBannerEnabled) === 'true');
        setBannerText(get(SettingKey.SiteBannerText));
        const tone = get(SettingKey.SiteBannerTone);
        setBannerTone(tone === 'warning' || tone === 'success' ? tone : 'info');
        setLoaded(true);
    }, [settings, loaded]);

    const save = () => {
        const entries = [
            { key: SettingKey.SiteName, value: name },
            { key: SettingKey.SiteDescription, value: desc },
            { key: SettingKey.SiteAnnouncement, value: announce },
            { key: SettingKey.SiteFooter, value: footer },
            { key: SettingKey.LandingAmbientMode, value: ambient },
            { key: SettingKey.SiteBannerEnabled, value: bannerOn ? 'true' : 'false' },
            { key: SettingKey.SiteBannerText, value: bannerText },
            { key: SettingKey.SiteBannerTone, value: bannerTone },
        ];
        Promise.all(entries.map((e) => setSetting.mutateAsync(e)))
            .then(() => {
                toast.success('站点信息已保存');
                void queryClient.invalidateQueries({ queryKey: ['public', 'overview'] });
                void queryClient.invalidateQueries({ queryKey: ['bootstrap', 'status'] });
            })
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
                    <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Lodestar" className="rounded-lg" />
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
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">封面氛围光</label>
                    <select
                        value={ambient}
                        onChange={(e) => setAmbient(e.target.value === 'color4bg' ? 'color4bg' : e.target.value === 'classic' ? 'classic' : 'photo')}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        <option value="photo">冬日实景照片（默认）</option>
                        <option value="classic">经典大图（newapi 风格）</option>
                        <option value="color4bg">动态氛围光（color4bg，失败则回退照片）</option>
                    </select>
                </div>
                <div className="flex flex-col gap-2 rounded-lg border border-border/30 bg-background/50 p-3">
                    <label className="flex cursor-pointer items-center gap-2 text-sm font-medium text-card-foreground">
                        <input type="checkbox" checked={bannerOn} onChange={(e) => setBannerOn(e.target.checked)} className="rounded border-border" />
                        全站顶栏公告条
                    </label>
                    <p className="text-xs text-muted-foreground">登录后与访客入口顶部展示；可关闭（仅当次浏览）。</p>
                    <textarea
                        value={bannerText}
                        onChange={(e) => setBannerText(e.target.value)}
                        rows={2}
                        className={textareaCls}
                        placeholder="例如：今晚 22:00–24:00 维护，期间可能短暂不可用"
                        disabled={!bannerOn}
                    />
                    <select
                        value={bannerTone}
                        onChange={(e) => setBannerTone(e.target.value === 'warning' || e.target.value === 'success' ? e.target.value : 'info')}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                        disabled={!bannerOn}
                    >
                        <option value="info">信息（默认）</option>
                        <option value="warning">警告</option>
                        <option value="success">成功/通知</option>
                    </select>
                </div>
                <div>
                    <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>保存站点信息</Button>
                </div>
            </div>
        </div>
    );
}
