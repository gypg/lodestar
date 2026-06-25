'use client';

/*
Lodestar — 报刊风落地页（Pretext Landing）

承接 `文档版本.html` 的三栏报纸排版：左栏（目录 + 引言）、中栏（编辑正文 + Drop Cap）、
右栏（实时数据）。纸感米白底 + 衬线体 + 虚线分割，与冬日风平行共存——由
`landing_ambient_mode === 'pretext'` 控制切换。
*/

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useNavStore, type NavItem } from '@/components/modules/navbar';
import { usePublicOverview } from '@/api/endpoints/public';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const SERIF = '"Songti SC","STSong","Noto Serif SC","SimSun",Georgia,serif';
const SANS = '"Helvetica Neue",Arial,sans-serif';

function pad2(n: number) {
    return String(n).padStart(2, '0');
}

function fmt(n: number | undefined) {
    return (n ?? 0).toLocaleString('en-US');
}

const HOME_TOC: { id: NavItem; label: string; meta: string }[] = [
    { id: 'hub', label: '多站 · 聚合', meta: 'HUB' },
    { id: 'channel', label: '渠道 · 上游', meta: 'CHAN' },
    { id: 'group', label: '分组 · 路由', meta: 'GROUP' },
    { id: 'model', label: '模型 · 广场', meta: 'MODEL' },
    { id: 'analytics', label: '数据 · 分析', meta: 'STATS' },
    { id: 'log', label: '调用 · 日志', meta: 'LOG' },
    { id: 'alert', label: '告警 · 监控', meta: 'ALERT' },
    { id: 'ops', label: '运维 · 健康', meta: 'OPS' },
    { id: 'apikey', label: 'API · 密钥', meta: 'KEYS' },
    { id: 'setting', label: '系统 · 设置', meta: 'SET' },
    { id: 'user', label: '用户 · 账户', meta: 'USER' },
];

type PublicPanel = 'announcement' | 'models' | 'usage' | 'about';
const PUBLIC_TOC: { key: PublicPanel | 'console'; label: string; meta: string }[] = [
    { key: 'announcement', label: '站点 · 公告', meta: 'NEWS' },
    { key: 'models', label: '模型 · 广场', meta: 'MODELS' },
    { key: 'usage', label: '用量 · 概览', meta: 'STATS' },
    { key: 'about', label: '关于 · 本站', meta: 'ABOUT' },
    { key: 'console', label: '进入 · 控制台', meta: 'LOGIN' },
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
    const weekday = WEEKDAYS[now.getDay()];

    const editorialText =
        overview?.description?.trim() ||
        `${siteName} —— 高自定义 · 自用优先 · 可聚合的个人 AI 中转站。每一个 token、每一次请求，都像铅字一样落在纸上。`;

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
                            章节目录
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
                                              {item.label}
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
                                              {item.label}
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
                            每一项都是通往数字世界的一扇窗。今日的风落在哪一扇窗前，由你决定。
                        </p>
                    </section>

                    {/* 中栏：编辑正文 + Drop Cap */}
                    <section className="col">
                        <h2
                            className="mb-5 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            今日特写
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
                            我们做 {siteName} 的初衷，是让它像铅字一样沉稳可靠。每一个 token、每一次请求、每一份账单，都像排版上的铅字，一一落位。重要的不是它有多显眼，而是它是否落在对的地方。
                        </p>

                        <div
                            className="my-6 border-y border-[#1f1d1a] py-4 text-center text-xl italic leading-relaxed text-[#1f1d1a] md:text-[26px]"
                            style={{ fontFamily: SERIF }}
                        >
                            <span className="mr-1 text-[40px] leading-none align-[-10px]">&ldquo;</span>
                            落笔无声，心有所归
                            <span className="ml-1 text-[40px] leading-none align-[-10px]">&rdquo;</span>
                        </div>

                        <p className="text-justify text-base leading-[1.9] text-[#2a2724]">
                            愿你今天打开这个页面的时候，也能保持安静、专注、带着一点好奇。剩下的，就交给时间。
                        </p>

                        <div
                            className="mt-8 flex justify-between text-[11px] uppercase tracking-[0.2em] text-[#6b6862]"
                            style={{ fontFamily: SANS }}
                        >
                            <span>— 编辑部</span>
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
                            此刻的数字
                        </h2>
                        <ul className="list-none">
                            <StatRow label="在线模型" value={fmt(overview?.model_count)} />
                            <StatRow label="今日请求" value={fmt(overview?.total_requests)} />
                            <StatRow label="总 Tokens" value={fmt(overview?.total_tokens)} />
                        </ul>
                        <p className="mt-6 text-base leading-[1.9] text-[#2a2724]">
                            这些数字不断跳动，像铅字从排版架上一一落下。它们构成了你与这个系统之间最安静的对话。
                        </p>
                        <p className="mt-3 text-sm italic text-[#6b6862]">
                            —— 铅字一直在排，我们一直在听。
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
                                            <p className="text-sm text-[#6b6862]">暂无公开模型。</p>
                                        )}
                                        {(overview?.models ?? []).map((m) => (
                                            <div
                                                key={m.name}
                                                className="flex items-baseline justify-between border-b border-dotted border-[#cdc7ba] py-1 text-sm"
                                            >
                                                <span className="mr-3 truncate">{m.name}</span>
                                                {(m.input > 0 || m.output > 0) && (
                                                    <span className="shrink-0 text-[11px] tabular-nums text-[#6b6862]">
                                                        入 {m.input} / 出 {m.output}
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
                                                请求
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
                                                模型
                                            </div>
                                        </div>
                                    </div>
                                )}
                                {panel === 'about' && (
                                    <p className="whitespace-pre-wrap text-sm leading-relaxed">
                                        {overview?.description?.trim() ||
                                            `${siteName} —— 高自定义 · 自用优先 · 可聚合的个人 AI 中转站。`}
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
                    <span>© {now.getFullYear()} {siteName} · 自用版本</span>
                    <span className="flex gap-3">
                        <button
                            type="button"
                            onClick={isPublic ? onLogin : onEnterDashboard}
                            className="border-b border-transparent transition-colors hover:border-[#1f1d1a] hover:text-[#1f1d1a]"
                        >
                            {isPublic ? '登入' : '进入数据概览'}
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
