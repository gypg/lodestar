'use client';

/*
Lodestar — 站点信息设置。

让管理员配置平台对外身份：站点名称 / 简介 / 公告 / 页脚。这些值经公开端点
(/api/v1/public/overview) 展示在落地页（封面刊头、关于、公告），是上游没有、
让本站"属于自己"的平台层配置。
*/

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Globe } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { useQueryClient } from '@tanstack/react-query';
import { toast } from '@/components/common/Toast';

/** Landing cover modes, in dropdown order. Must stay in sync with the Go
 *  SettingKeyLandingAmbientMode comment and public overview payload type. */
const AMBIENT_MODES = ['photo', 'classic', 'color4bg', 'pretext'] as const;
type LandingAmbientMode = (typeof AMBIENT_MODES)[number];

function parseAmbientMode(value: string): LandingAmbientMode {
    return (AMBIENT_MODES as readonly string[]).includes(value)
        ? (value as LandingAmbientMode)
        : 'photo';
}

export function SiteIdentity() {
    const t = useTranslations('setting.siteIdentity');
    const queryClient = useQueryClient();
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [name, setName] = useState('');
    const [desc, setDesc] = useState('');
    const [announce, setAnnounce] = useState('');
    const [footer, setFooter] = useState('');
    const [ambient, setAmbient] = useState<LandingAmbientMode>('photo');
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
        setAmbient(parseAmbientMode(get(SettingKey.LandingAmbientMode)));
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
                toast.success(t('toast.saved'));
                void queryClient.invalidateQueries({ queryKey: ['public', 'overview'] });
                void queryClient.invalidateQueries({ queryKey: ['bootstrap', 'status'] });
            })
            .catch(() => toast.error(t('toast.saveFailed')));
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
                    <span className="text-sm font-semibold text-card-foreground">{t('title')}</span>
                    <p className="text-xs text-muted-foreground">{t('description')}</p>
                </div>
            </div>
            <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('nameLabel')}</label>
                    <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Lodestar" className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('descriptionLabel')}</label>
                    <textarea value={desc} onChange={(e) => setDesc(e.target.value)} rows={2} className={textareaCls} placeholder={t('descriptionPlaceholder')} />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('announcementLabel')}</label>
                    <textarea value={announce} onChange={(e) => setAnnounce(e.target.value)} rows={3} className={textareaCls} placeholder={t('announcementPlaceholder')} />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('footerLabel')}</label>
                    <Input value={footer} onChange={(e) => setFooter(e.target.value)} placeholder="© 2026 ..." className="rounded-lg" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <label className="ml-1 text-xs font-medium text-muted-foreground">{t('ambientLabel')}</label>
                    <select
                        value={ambient}
                        onChange={(e) => setAmbient(parseAmbientMode(e.target.value))}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        <option value="photo">{t('ambient.photo')}</option>
                        <option value="classic">{t('ambient.classic')}</option>
                        <option value="color4bg">{t('ambient.color4bg')}</option>
                        <option value="pretext">{t('ambient.pretext')}</option>
                    </select>
                </div>
                <div className="flex flex-col gap-2 rounded-lg border border-border/30 bg-background/50 p-3">
                    <label className="flex cursor-pointer items-center gap-2 text-sm font-medium text-card-foreground">
                        <input type="checkbox" checked={bannerOn} onChange={(e) => setBannerOn(e.target.checked)} className="rounded border-border" />
                        {t('bannerToggle')}
                    </label>
                    <p className="text-xs text-muted-foreground">{t('bannerHint')}</p>
                    <textarea
                        value={bannerText}
                        onChange={(e) => setBannerText(e.target.value)}
                        rows={2}
                        className={textareaCls}
                        placeholder={t('bannerPlaceholder')}
                        disabled={!bannerOn}
                    />
                    <select
                        value={bannerTone}
                        onChange={(e) => setBannerTone(e.target.value === 'warning' || e.target.value === 'success' ? e.target.value : 'info')}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                        disabled={!bannerOn}
                    >
                        <option value="info">{t('bannerTone.info')}</option>
                        <option value="warning">{t('bannerTone.warning')}</option>
                        <option value="success">{t('bannerTone.success')}</option>
                    </select>
                </div>
                <div>
                    <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>{t('save')}</Button>
                </div>
            </div>
        </div>
    );
}
