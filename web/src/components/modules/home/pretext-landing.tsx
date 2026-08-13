'use client';

/*
Lodestar — 报刊风落地页（Pretext Landing）

承接 `文档版本.html` 的三栏报纸排版：左栏（目录 + 引言）、中栏（编辑正文 + Drop Cap）、
右栏（实时数据）。纸感米白底 + 衬线体 + 虚线分割，与冬日风平行共存——由
`landing_ambient_mode === 'pretext'` 控制切换。
*/

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useNavStore } from '@/components/modules/navbar';
import { usePublicOverview } from '@/api/endpoints/public';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { useSettingStore } from '@/stores/setting';
import { HomeTocLabel, type HomeTocId } from './toc-label';

const SERIF = '"Songti SC","STSong","Noto Serif SC","SimSun",Georgia,serif';
const SANS = '"Helvetica Neue",Arial,sans-serif';

function pad2(n: number) {
    return String(n).padStart(2, '0');
}

function fmt(n: number | undefined) {
    return (n ?? 0).toLocaleString('en-US');
}

// 目录表只存「顺序 + 导航 id + 栏目缩写」，标签在渲染处用字面量 key 取，
// 与 winter-landing 共用 HomeTocLabel。见 toc-label.tsx 的说明。
const HOME_TOC: { id: HomeTocId; meta: string }[] = [
    { id: 'hub', meta: 'HUB' },
    { id: 'channel', meta: 'CHAN' },
    { id: 'group', meta: 'GROUP' },
    { id: 'model', meta: 'MODEL' },
    { id: 'analytics', meta: 'STATS' },
    { id: 'log', meta: 'LOG' },
    { id: 'alert', meta: 'ALERT' },
    { id: 'ops', meta: 'OPS' },
    { id: 'apikey', meta: 'KEYS' },
    { id: 'setting', meta: 'SET' },
    { id: 'user', meta: 'USER' },
];

type PublicPanel = 'announcement' | 'models' | 'usage' | 'about';
const PUBLIC_TOC: { key: PublicPanel | 'console'; meta: string }[] = [
    { key: 'announcement', meta: 'NEWS' },
    { key: 'models', meta: 'MODELS' },
    { key: 'usage', meta: 'STATS' },
    { key: 'about', meta: 'ABOUT' },
    { key: 'console', meta: 'LOGIN' },
];

export function PretextLanding({
    variant = 'home',
    onEnterDashboard,
    onLogin,
}: {
    variant?: 'home' | 'public';
    onEnterDashboard?: () => void;
    onLogin?: () => void;
}) {
    const setActiveItem = useNavStore((s) => s.setActiveItem);
    const t = useTranslations();
    const locale = useSettingStore((s) => s.locale);
    const [now, setNow] = useState(() => new Date());
    const isPublic = variant === 'public';
    const [panel, setPanel] = useState<PublicPanel | null>(null);
    const { data: overview } = usePublicOverview(isPublic);
    const { data: me } = useCurrentUser();
    const siteName = overview?.site_name?.trim() || 'Lodestar';
    const portalOnly = !isPublic && me !== undefined && !isStaffRole(me.role);
    const homeItems = portalOnly
        ? HOME_TOC.filter((i) => i.id === 'model' || i.id === 'apikey' || i.id === 'setting')
        : HOME_TOC;

    useEffect(() => {
        const timer = setInterval(() => setNow(new Date()), 1000);
        return () => clearInterval(timer);
    }, []);

    const dateStr = `${now.getFullYear()}.${pad2(now.getMonth() + 1)}.${pad2(now.getDate())}`;
    const clock = `${pad2(now.getHours())}:${pad2(now.getMinutes())}`;
    // 星期名交给 Intl：三个 locale 的输出与原硬编码简体表逐字一致，繁体/英文自动正确。
    const weekday = new Intl.DateTimeFormat(locale, { weekday: 'short' }).format(now);

    const editorialText =
        overview?.description?.trim() || t('landing.pretext.defaultEditorial', { name: siteName });

    // 首字取第一行文本的第一个字符（用于 Drop Cap）
    const dropChar = editorialText.charAt(0);
    const editorialBody = editorialText.slice(1);

    return (
        <div
            className="relative h-full w-full overflow-hidden rounded-xl border border-[#1f1d1a]/15 bg-[#f4f1ec] text-[#1f1d1a]"
            style={{ fontFamily: SERIF }}
        >
            {/* 内容层 */}
            <div className="relative z-10 flex h-full w-full flex-col">
                {/* 刊头 Masthead */}
                <header className="border-b border-[#1f1d1a] px-[8vw] pb-4 pt-8 md:pt-12">
                    <div
                        className="mb-3 flex justify-between text-[11px] uppercase tracking-[0.25em] text-[#6b6862]"
                        style={{ fontFamily: SANS }}
                    >
                        <span>Vol. 01</span>
                        <span>
                            {dateStr} {weekday}
                        </span>
                        <span className="max-w-[40%] truncate">{siteName} Daily</span>
                    </div>
                    <h1
                        className="mt-2 text-center font-normal leading-none tracking-[0.28em] text-[#1f1d1a]"
                        style={{ fontSize: 'clamp(32px, 7vw, 96px)' }}
                    >
                        {siteName}
                    </h1>
                    <div className="mt-2 text-center text-xs italic tracking-[0.4em] text-[#6b6862]">
                        your fully-customizable AI gateway
                    </div>
                    <div className="relative mx-auto mt-4 w-[60%] border-t border-[#1f1d1a]">
                        <span
                            className="absolute left-1/2 -translate-x-1/2 -translate-y-1/2 bg-[#f4f1ec] px-3 text-sm text-[#1f1d1a]"
                            style={{ top: 0 }}
                        >
                            ❄
                        </span>
                    </div>
                </header>

                {/* 三栏内容 */}
                <main className="grid flex-1 grid-cols-1 gap-8 overflow-y-auto px-[8vw] py-8 md:grid-cols-[1fr_1.4fr_1fr] md:gap-14 md:py-10">
                    {/* 左栏：目录 + 引言 */}
                    <section className="col">
                        <h2
                            className="mb-5 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            {t('landing.toc.title')}
                        </h2>
                        <ul className="list-none">
                            {isPublic
                                ? PUBLIC_TOC.map((item, idx) => (
                                      <li
                                          key={item.key}
                                          className="flex items-baseline justify-between border-b border-dotted border-[#b8b3a8] py-2"
                                      >
                                          <span
                                              className="mr-2.5 text-[11px] tabular-nums text-[#6b6862]"
                                              style={{ fontFamily: SANS }}
                                          >
                                              {pad2(idx + 1)}
                                          </span>
                                          <button
                                              type="button"
                                              onClick={() =>
                                                  item.key === 'console'
                                                      ? onLogin?.()
                                                      : setPanel(item.key as PublicPanel)
                                              }
                                              className={`flex-1 border-b text-left text-sm text-[#1f1d1a] transition-colors hover:border-[#1f1d1a] ${
                                                  panel === item.key ? 'border-[#1f1d1a]' : 'border-transparent'
                                              }`}
                                          >
                                              {item.key === 'announcement' && t('landing.toc.announcement')}
                                              {item.key === 'models' && t('landing.toc.model')}
                                              {item.key === 'usage' && t('landing.toc.usage')}
                                              {item.key === 'about' && t('landing.toc.about')}
                                              {item.key === 'console' && t('landing.toc.console')}
                                          </button>
                                          <span
                                              className="ml-2 text-[10px] tracking-wider text-[#6b6862]"
                                              style={{ fontFamily: SANS }}
                                          >
                                              {item.meta}
                                          </span>
                                      </li>
                                  ))
                                : homeItems.map((item, idx) => (
                                      <li
                                          key={item.id}
                                          className="flex items-baseline justify-between border-b border-dotted border-[#b8b3a8] py-2"
                                      >
                                          <span
                                              className="mr-2.5 text-[11px] tabular-nums text-[#6b6862]"
                                              style={{ fontFamily: SANS }}
                                          >
                                              {pad2(idx + 1)}
                                          </span>
                                          <button
                                              type="button"
                                              onClick={() => setActiveItem(item.id)}
                                              className="flex-1 border-b border-transparent text-left text-sm text-[#1f1d1a] transition-colors hover:border-[#1f1d1a]"
                                          >
                                              <HomeTocLabel id={item.id} />
                                          </button>
                                          <span
                                              className="ml-2 text-[10px] tracking-wider text-[#6b6862]"
                                              style={{ fontFamily: SANS }}
                                          >
                                              {item.meta}
                                          </span>
                                      </li>
                                  ))}
                        </ul>
                        <p className="mt-6 text-sm italic leading-relaxed text-[#6b6862]">
                            {t('landing.pretext.tagline')}
                        </p>
                    </section>

                    {/* 中栏：编辑正文 + Drop Cap */}
                    <section className="col">
                        <h2
                            className="mb-5 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            {t('landing.pretext.featureTitle')}
                        </h2>
                        <p className="text-justify text-base leading-[1.9] text-[#2a2724]">
                            <span
                                className="float-left pr-2 pt-1 text-[56px] font-semibold leading-[0.9] text-[#1f1d1a]"
                                style={{ fontFamily: SERIF }}
                            >
                                {dropChar}
                            </span>
                            {editorialBody}
                        </p>
                        <p className="mt-3 text-justify text-base leading-[1.9] text-[#2a2724]">
                            {t('landing.pretext.editorialBody', { name: siteName })}
                        </p>

                        <div
                            className="my-6 border-y border-[#1f1d1a] py-4 text-center text-xl italic leading-relaxed text-[#1f1d1a] md:text-[26px]"
                            style={{ fontFamily: SERIF }}
                        >
                            <span className="mr-1 text-[40px] leading-none align-[-10px]">&ldquo;</span>
                            {t('landing.pretext.pullQuote')}
                            <span className="ml-1 text-[40px] leading-none align-[-10px]">&rdquo;</span>
                        </div>

                        <p className="text-justify text-base leading-[1.9] text-[#2a2724]">
                            {t('landing.pretext.closing')}
                        </p>

                        <div
                            className="mt-8 flex justify-between text-[11px] uppercase tracking-[0.2em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            <span>{t('landing.pretext.byline')}</span>
                            <span>
                                {clock} · {weekday}
                            </span>
                        </div>
                    </section>

                    {/* 右栏：实时数据 */}
                    <section className="col">
                        <h2
                            className="mb-5 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            {t('landing.pretext.numbersTitle')}
                        </h2>
                        <ul className="list-none">
                            <StatRow label={t('landing.pretext.stat.onlineModels')} value={fmt(overview?.model_count)} />
                            <StatRow label={t('landing.pretext.stat.todayRequests')} value={fmt(overview?.total_requests)} />
                            <StatRow label={t('landing.pretext.stat.totalTokens')} value={fmt(overview?.total_tokens)} />
                        </ul>
                        <p className="mt-6 text-base leading-[1.9] text-[#2a2724]">
                            {t('landing.pretext.numbersNote')}
                        </p>
                        <p className="mt-3 text-sm italic text-[#6b6862]">
                            {t('landing.pretext.numbersColophon')}
                        </p>

                        {/* 公开内容面板（访客点目录项浮出） */}
                        {isPublic && panel && (
                            <div
                                className="mt-6 rounded-lg border border-[#cdc7ba] bg-[#fbfaf7]/95 p-4 backdrop-blur"
                                role="dialog"
                                aria-label={panel === 'announcement' ? t('landing.announcement') : panel === 'models' ? t('landing.models') : panel === 'usage' ? t('landing.usage') : t('landing.about', { name: siteName })}
                                onKeyDown={(e) => { if (e.key === 'Escape') setPanel(null); }}
                                tabIndex={-1}
                            >
                                <div className="mb-2 flex items-center justify-between border-b border-[#cdc7ba] pb-2">
                                    <span
                                        className="text-sm font-semibold tracking-wide"
                                        style={{ fontFamily: SANS }}
                                    >
                                        {panel === 'announcement' && t('landing.announcement')}
                                        {panel === 'models' && t('landing.models')}
                                        {panel === 'usage' && t('landing.usage')}
                                        {panel === 'about' && t('landing.about', { name: siteName })}
                                    </span>
                                    <button
                                        type="button"
                                        onClick={() => setPanel(null)}
                                        className="text-[#6b6862] transition-colors hover:text-[#1f1d1a]"
                                        aria-label={t('landing.close')}
                                    >
                                        ✕
                                    </button>
                                </div>
                                {panel === 'announcement' && (
                                    <p className="whitespace-pre-wrap text-sm leading-relaxed">
                                        {overview?.announcement?.trim() || t('landing.noAnnouncement')}
                                    </p>
                                )}
                                {panel === 'models' && (
                                    <div className="flex flex-col gap-1.5">
                                        <p className="mb-1 text-xs text-[#6b6862]">
                                            {t('landing.modelCount', { count: overview?.model_count ?? 0 })}
                                        </p>
                                        {(overview?.models ?? []).length === 0 && (
                                            <p className="text-sm text-[#6b6862]">{t('landing.noPublicModels')}</p>
                                        )}
                                        {(overview?.models ?? []).map((m) => (
                                            <div
                                                key={m.name}
                                                className="flex items-baseline justify-between border-b border-dotted border-[#cdc7ba] py-1 text-sm"
                                            >
                                                <span className="mr-3 truncate">{m.name}</span>
                                                {(m.input > 0 || m.output > 0) && (
                                                    <span className="shrink-0 text-[11px] tabular-nums text-[#6b6862]">
                                                        {t('landing.modelPrice', { input: m.input, output: m.output })}
                                                    </span>
                                                )}
                                            </div>
                                        ))}
                                    </div>
                                )}
                                {panel === 'usage' && (
                                    <div className="grid grid-cols-3 gap-3 text-center">
                                        <div className="rounded-lg border border-[#cdc7ba] p-3">
                                            <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">
                                                {fmt(overview?.total_requests)}
                                            </div>
                                            <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">
                                                {t('landing.stat.requests')}
                                            </div>
                                        </div>
                                        <div className="rounded-lg border border-[#cdc7ba] p-3">
                                            <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">
                                                {fmt(overview?.total_tokens)}
                                            </div>
                                            <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">
                                                Tokens
                                            </div>
                                        </div>
                                        <div className="rounded-lg border border-[#cdc7ba] p-3">
                                            <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">
                                                {fmt(overview?.model_count)}
                                            </div>
                                            <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">
                                                {t('landing.stat.models')}
                                            </div>
                                        </div>
                                    </div>
                                )}
                                {panel === 'about' && (
                                    <p className="whitespace-pre-wrap text-sm leading-relaxed">
                                        {overview?.description?.trim() ||
                                            t('landing.defaultDescription', { name: siteName })}
                                    </p>
                                )}
                            </div>
                        )}
                    </section>
                </main>

                {/* 页脚 Colophon */}
                <footer
                    className="flex items-baseline justify-between border-t border-[#1f1d1a] px-[8vw] py-4 text-[11px] uppercase tracking-[0.25em] text-[#6b6862]"
                    style={{ fontFamily: SANS }}
                >
                    <span>© {now.getFullYear()} {siteName} {t('landing.pretext.footerEdition')}</span>
                    <span className="flex gap-3">
                        <button
                            type="button"
                            onClick={isPublic ? onLogin : onEnterDashboard}
                            className="border-b border-transparent transition-colors hover:border-[#1f1d1a] hover:text-[#1f1d1a]"
                        >
                            {isPublic ? t('landing.signIn') : t('landing.openDashboard')}
                        </button>
                    </span>
                    <span>Made with ❄</span>
                </footer>
            </div>
        </div>
    );
}

function StatRow({ label, value }: { label: string; value: string }) {
    return (
        <li className="flex items-baseline justify-between border-b border-dotted border-[#b8b3a8] py-2.5 text-[15px] text-[#2a2724]">
            <span>{label}</span>
            <span style={{ fontFamily: SANS }} className="text-[12px] tabular-nums text-[#6b6862]">
                {value}
            </span>
        </li>
    );
}
