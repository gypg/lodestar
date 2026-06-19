'use client';

/*
Lodestar — 报刊风落地页（布局源自 GGGZERO/文档版本.html，React + 冬日纸感 token）。
*/

import { useEffect, useState } from 'react';
import { usePublicOverview } from '@/api/endpoints/public';
import { useNavStore } from '@/components/modules/navbar';

const SERIF = '"Songti SC","STSong","Noto Serif SC","SimSun",Georgia,serif';
const SANS = '"Helvetica Neue",Arial,sans-serif';

function fmt(n: number | undefined) {
    return (n ?? 0).toLocaleString('en-US');
}

export function NewspaperLanding({
    variant = 'home',
    onEnterDashboard,
    onLogin,
}: {
    variant?: 'home' | 'public';
    onEnterDashboard?: () => void;
    onLogin?: () => void;
}) {
    const setActiveItem = useNavStore((s) => s.setActiveItem);
    const { data: overview } = usePublicOverview(true);
    const siteName = overview?.site_name?.trim() || 'Lodestar';
    const [now, setNow] = useState(() => new Date());
    const isPublic = variant === 'public';

    useEffect(() => {
        const t = setInterval(() => setNow(new Date()), 60_000);
        return () => clearInterval(t);
    }, []);

    const dateStr = `${now.getFullYear()}.${String(now.getMonth() + 1).padStart(2, '0')}.${String(now.getDate()).padStart(2, '0')}`;
    const desc = overview?.description?.trim() || `${siteName} — 高自定义 · 自用优先的个人 AI 中转站。`;
    const announce = overview?.announcement?.trim() || '暂无公告。';
    const models = (overview?.models ?? []).slice(0, 12);

    return (
        <div className="relative min-h-full w-full overflow-y-auto rounded-xl border border-[#1f1d1a]/15 bg-[#f4f1ec] text-[#1f1d1a]" style={{ fontFamily: SERIF }}>
            <header className="border-b border-[#1f1d1a] px-[6vw] pb-6 pt-10">
                <div className="mb-4 flex justify-between text-[11px] uppercase tracking-[0.25em] text-[#6b6862]" style={{ fontFamily: SANS }}>
                    <span>Vol. 02</span>
                    <span>{dateStr}</span>
                    <span className="max-w-[40%] truncate">{siteName} Gazette</span>
                </div>
                <h1 className="text-center text-[clamp(2rem,6vw,4.5rem)] font-normal tracking-[0.2em]">报 · 纸 · 版</h1>
                <p className="mt-3 text-center text-xs italic tracking-[0.35em] text-[#6b6862]">layout inspired by 文档版本 · Lodestar</p>
                <button
                    type="button"
                    onClick={isPublic ? onLogin : onEnterDashboard}
                    className="mx-auto mt-6 block border-b border-[#6f9ec2] pb-px text-[11px] uppercase tracking-[0.2em] text-[#6b6862] hover:text-[#1f1d1a]"
                    style={{ fontFamily: SANS }}
                >
                    {isPublic ? '登录 / 进入控制台 →' : '进入数据概览 →'}
                </button>
            </header>

            <div className="grid gap-10 px-[6vw] py-12 md:grid-cols-3 md:gap-14">
                <section>
                    <h2 className="mb-4 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]" style={{ fontFamily: SANS }}>
                        左栏 · 公告
                    </h2>
                    <p className="whitespace-pre-wrap text-justify text-[15px] leading-[1.9] text-[#2a2724]">{announce}</p>
                </section>
                <section>
                    <h2 className="mb-4 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]" style={{ fontFamily: SANS }}>
                        中栏 · 关于
                    </h2>
                    <p className="whitespace-pre-wrap text-justify text-[15px] leading-[1.9] text-[#2a2724]">{desc}</p>
                    <blockquote className="my-8 border-y border-[#1f1d1a] py-6 text-center text-xl italic leading-snug text-[#1f1d1a]">
                        雪落无声，心有所归。
                    </blockquote>
                    <div className="grid grid-cols-3 gap-2 text-center text-sm" style={{ fontFamily: SANS }}>
                        <div className="rounded border border-[#cdc7ba] p-2">
                            <div className="font-semibold tabular-nums text-[#5a86a8]">{fmt(overview?.total_requests)}</div>
                            <div className="text-[10px] text-[#6b6862]">请求</div>
                        </div>
                        <div className="rounded border border-[#cdc7ba] p-2">
                            <div className="font-semibold tabular-nums text-[#5a86a8]">{fmt(overview?.total_tokens)}</div>
                            <div className="text-[10px] text-[#6b6862]">Tokens</div>
                        </div>
                        <div className="rounded border border-[#cdc7ba] p-2">
                            <div className="font-semibold tabular-nums text-[#5a86a8]">{overview?.model_count ?? 0}</div>
                            <div className="text-[10px] text-[#6b6862]">模型</div>
                        </div>
                    </div>
                </section>
                <section>
                    <h2 className="mb-4 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]" style={{ fontFamily: SANS }}>
                        右栏 · 模型
                    </h2>
                    <ul className="list-none">
                        {models.map((m) => (
                            <li key={m.name} className="flex justify-between border-b border-dotted border-[#b8b3a8] py-2.5 text-[15px]">
                                <span className="truncate pr-2">{m.name}</span>
                                <span className="shrink-0 text-[11px] text-[#6b6862]" style={{ fontFamily: SANS }}>
                                    {m.input > 0 || m.output > 0 ? `${m.input}/${m.output}` : '—'}
                                </span>
                            </li>
                        ))}
                    </ul>
                    {!isPublic && (
                        <button
                            type="button"
                            className="mt-6 w-full border border-[#1f1d1a]/30 py-2 text-xs uppercase tracking-wider hover:bg-[#1f1d1a]/5"
                            style={{ fontFamily: SANS }}
                            onClick={() => setActiveItem('model')}
                        >
                            打开模型广场
                        </button>
                    )}
                </section>
            </div>
            {overview?.footer ? (
                <footer className="border-t border-[#cdc7ba] px-[6vw] py-6 text-center text-xs text-[#6b6862]" style={{ fontFamily: SANS }}>
                    {overview.footer}
                </footer>
            ) : null}
        </div>
    );
}