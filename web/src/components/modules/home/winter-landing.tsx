'use client';

/*
GGZERO — 冬日风落地页（Winter Landing）

承接 `首页文件/home.html` 的报刊式冬日封面：刊头、飘雪、冷光氛围、活体时钟、左侧目录。
两种形态：
- public（访客，未登录）：左侧目录的「公开项」点击后在右侧浮出站点内容（公告/模型广场/
  用量概览/关于本站），无需登录即可看；「私密项/进入控制台」点击 → 唤出登录。
- home（已登录）：目录项经 useNavStore 软路由切到控制台 tab；右上「进入数据概览」切仪表盘。

配色全部走主题 CSS 变量，随主题预设与明暗自适配——与 GGZERO 主题系统连成一体。
*/

import { useEffect, useMemo, useState } from 'react';
import { useNavStore, type NavItem } from '@/components/modules/navbar';
import { usePublicOverview } from '@/api/endpoints/public';

const SNOW_SYMBOLS = ['❄', '❅', '❆', '✻', '✺', '*'];
const SNOW_COUNT = 72;

// 已登录目录：软路由到控制台 tab
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
// 访客目录：公开项浮出内容；最后一项进控制台 → 登录
const PUBLIC_TOC: { key: PublicPanel | 'console'; label: string; meta: string }[] = [
  { key: 'announcement', label: '站点 · 公告', meta: 'NEWS' },
  { key: 'models', label: '模型 · 广场', meta: 'MODELS' },
  { key: 'usage', label: '用量 · 概览', meta: 'STATS' },
  { key: 'about', label: '关于 · 本站', meta: 'ABOUT' },
  { key: 'console', label: '进入 · 控制台', meta: 'LOGIN' },
];

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const SERIF = '"Songti SC","STSong","Noto Serif SC","SimSun",Georgia,serif';
const SANS = '"Helvetica Neue",Arial,sans-serif';

function pad2(n: number) {
  return String(n).padStart(2, '0');
}
function fmt(n: number | undefined) {
  return (n ?? 0).toLocaleString('en-US');
}

interface Flake {
  left: number;
  size: number;
  dur: number;
  delay: number;
  drift: number;
  opacity: number;
  sym: string;
}

export function WinterLanding({
  variant = 'home',
  onEnterDashboard,
  onLogin,
}: {
  variant?: 'home' | 'public';
  onEnterDashboard?: () => void;
  onLogin?: () => void;
}) {
  const setActiveItem = useNavStore((s) => s.setActiveItem);
  const [now, setNow] = useState(() => new Date());
  const isPublic = variant === 'public';
  const [panel, setPanel] = useState<PublicPanel | null>(null);
  const { data: overview } = usePublicOverview(isPublic);
  const siteName = overview?.site_name?.trim() || 'GGZERO';

  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(timer);
  }, []);

  const flakes = useMemo<Flake[]>(() => {
    return Array.from({ length: SNOW_COUNT }, () => ({
      left: Math.random() * 100,
      size: Math.random() * 14 + 6,
      dur: Math.random() * 9 + 8,
      delay: Math.random() * -15,
      drift: (Math.random() - 0.5) * 180,
      opacity: Math.random() * 0.5 + 0.4,
      sym: SNOW_SYMBOLS[Math.floor(Math.random() * SNOW_SYMBOLS.length)],
    }));
  }, []);

  const dateStr = `${now.getFullYear()}.${pad2(now.getMonth() + 1)}.${pad2(now.getDate())}`;
  const clock = `${pad2(now.getHours())}:${pad2(now.getMinutes())}`;
  const weekday = WEEKDAYS[now.getDay()];

  return (
    <div
      className="relative h-full w-full overflow-hidden rounded-xl border border-border bg-background text-foreground"
      style={{ fontFamily: SERIF }}
    >
      {/* 冷光氛围（动态极光，随主题着色，缓慢呼吸） */}
      <div
        className="ggzero-aurora pointer-events-none absolute inset-0 z-0"
        style={{
          background:
            'radial-gradient(120% 80% at 80% 18%, color-mix(in oklch, var(--primary) 16%, transparent), transparent 60%),' +
            'radial-gradient(90% 70% at 15% 85%, color-mix(in oklch, var(--accent) 14%, transparent), transparent 55%),' +
            'radial-gradient(70% 60% at 50% 50%, color-mix(in oklch, var(--primary) 7%, transparent), transparent 70%)',
        }}
      />
      {/* 左侧留白雾化 */}
      <div
        className="pointer-events-none absolute inset-y-0 left-0 z-[2] w-[42%]"
        style={{
          background:
            'linear-gradient(to right, color-mix(in oklch, var(--background) 92%, transparent) 0%, color-mix(in oklch, var(--background) 60%, transparent) 65%, transparent 100%)',
        }}
      />
      {/* 四角暗角 */}
      <div
        className="pointer-events-none absolute inset-0 z-[3]"
        style={{ background: 'radial-gradient(ellipse at center, transparent 55%, color-mix(in oklch, var(--foreground) 10%, transparent) 100%)' }}
      />

      {/* 雪花 */}
      <div className="pointer-events-none absolute inset-0 z-[5] overflow-hidden" aria-hidden>
        {flakes.map((f, i) => (
          <span
            key={i}
            className="ggzero-snowflake absolute top-0 select-none text-foreground"
            style={{
              left: `${f.left}%`,
              fontSize: `${f.size}px`,
              opacity: f.opacity,
              textShadow: '0 0 3px color-mix(in oklch, var(--primary) 50%, transparent)',
              animation: `ggzero-snow-fall ${f.dur}s linear ${f.delay}s infinite`,
              ['--drift' as string]: `${f.drift}px`,
            }}
          >
            {f.sym}
          </span>
        ))}
      </div>

      {/* 内容层 */}
      <div className="relative z-10 h-full w-full">
        {/* 刊头 */}
        <header className="absolute left-[6vw] right-[6vw] top-[6vh] border-b pb-4" style={{ borderColor: 'var(--foreground)' }}>
          <div className="mb-3 flex justify-between text-[11px] uppercase tracking-[0.25em] text-muted-foreground" style={{ fontFamily: SANS }}>
            <span>Vol. 01</span>
            <span>{dateStr}</span>
            <span className="max-w-[40%] truncate">{siteName} Daily</span>
          </div>
          <h1 className="mt-1.5 text-center font-normal leading-none tracking-[0.28em] text-foreground" style={{ fontSize: 'clamp(32px,5.5vw,76px)' }}>
            雪 落 无 声
          </h1>
          <div className="mt-3 text-center text-xs italic tracking-[0.4em] text-muted-foreground">a quiet afternoon, watching snow</div>
        </header>

        {/* 右上：登录 / 进入数据概览 */}
        <button
          type="button"
          onClick={isPublic ? onLogin : onEnterDashboard}
          className="absolute right-[6vw] top-[16vh] border-b pb-px text-[11px] uppercase tracking-[0.25em] text-muted-foreground transition-colors hover:text-foreground"
          style={{ fontFamily: SANS, borderColor: 'currentColor' }}
        >
          {isPublic ? '登录 / 进入 →' : '进入数据概览 →'}
        </button>

        {/* 左侧目录 */}
        <aside className="absolute left-[6vw] top-1/2 max-h-[66vh] -translate-y-1/2 overflow-y-auto" style={{ fontFamily: SANS, minWidth: 'min(320px, 60vw)' }}>
          <h2 className="mb-3 border-t pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-muted-foreground" style={{ borderColor: 'var(--foreground)' }}>
            章 节 目 录
          </h2>
          <ul className="list-none">
            {isPublic
              ? PUBLIC_TOC.map((item, idx) => (
                  <li key={item.key} className="flex items-baseline justify-between border-b border-dotted border-border py-2">
                    <span className="mr-2.5 text-[11px] tabular-nums text-muted-foreground">{pad2(idx + 1)}</span>
                    <button
                      type="button"
                      onClick={() => (item.key === 'console' ? onLogin?.() : setPanel(item.key as PublicPanel))}
                      className={`flex-1 border-b text-left text-sm text-foreground transition-colors hover:border-primary ${panel === item.key ? 'border-primary' : 'border-transparent'}`}
                    >
                      {item.label}
                    </button>
                    <span className="ml-2 text-[10px] tracking-wider text-muted-foreground">{item.meta}</span>
                  </li>
                ))
              : HOME_TOC.map((item, idx) => (
                  <li key={item.id} className="flex items-baseline justify-between border-b border-dotted border-border py-2">
                    <span className="mr-2.5 text-[11px] tabular-nums text-muted-foreground">{pad2(idx + 1)}</span>
                    <button
                      type="button"
                      onClick={() => setActiveItem(item.id)}
                      className="flex-1 border-b border-transparent text-left text-sm text-foreground transition-colors hover:border-primary"
                    >
                      {item.label}
                    </button>
                    <span className="ml-2 text-[10px] tracking-wider text-muted-foreground">{item.meta}</span>
                  </li>
                ))}
          </ul>
        </aside>

        {/* 公开内容面板（访客点目录项浮出） */}
        {isPublic && panel && (
          <div
            className="absolute right-[5vw] top-[20vh] z-20 w-[min(440px,88vw)] max-h-[62vh] overflow-y-auto rounded-xl border border-border bg-card/95 p-5 shadow-lg backdrop-blur"
            style={{ fontFamily: SANS }}
          >
            <div className="mb-3 flex items-center justify-between border-b border-border/60 pb-2">
              <span className="text-sm font-semibold tracking-wide text-card-foreground">
                {panel === 'announcement' && '站点公告'}
                {panel === 'models' && '模型广场'}
                {panel === 'usage' && '用量概览'}
                {panel === 'about' && `关于 ${siteName}`}
              </span>
              <button type="button" onClick={() => setPanel(null)} className="text-muted-foreground transition-colors hover:text-foreground" aria-label="关闭">✕</button>
            </div>

            {panel === 'announcement' && (
              <p className="whitespace-pre-wrap text-sm leading-relaxed text-card-foreground/90">
                {overview?.announcement?.trim() || '暂无公告。'}
              </p>
            )}

            {panel === 'models' && (
              <div className="flex flex-col gap-1.5">
                <p className="mb-1 text-xs text-muted-foreground">共 {overview?.model_count ?? 0} 个模型</p>
                {(overview?.models ?? []).length === 0 && <p className="text-sm text-muted-foreground">暂无公开模型。</p>}
                {(overview?.models ?? []).map((m) => (
                  <div key={m.name} className="flex items-baseline justify-between border-b border-dotted border-border/50 py-1 text-sm">
                    <span className="mr-3 truncate text-card-foreground">{m.name}</span>
                    {(m.input > 0 || m.output > 0) && (
                      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">入 {m.input} / 出 {m.output}</span>
                    )}
                  </div>
                ))}
              </div>
            )}

            {panel === 'usage' && (
              <div className="grid grid-cols-3 gap-3 text-center">
                <div className="rounded-lg border border-border/40 p-3">
                  <div className="text-lg font-semibold tabular-nums text-primary">{fmt(overview?.total_requests)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-muted-foreground">请求</div>
                </div>
                <div className="rounded-lg border border-border/40 p-3">
                  <div className="text-lg font-semibold tabular-nums text-primary">{fmt(overview?.total_tokens)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-muted-foreground">Tokens</div>
                </div>
                <div className="rounded-lg border border-border/40 p-3">
                  <div className="text-lg font-semibold tabular-nums text-primary">{fmt(overview?.model_count)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-muted-foreground">模型</div>
                </div>
              </div>
            )}

            {panel === 'about' && (
              <p className="whitespace-pre-wrap text-sm leading-relaxed text-card-foreground/90">
                {overview?.description?.trim() || `${siteName} —— 高自定义 · 自用优先 · 可聚合的个人 AI 中转站。`}
              </p>
            )}
          </div>
        )}

        {/* 右下：活体时钟 + 引文 */}
        <div className="absolute bottom-[6vh] right-[6vw] text-right" style={{ fontFamily: SANS }}>
          <div className="text-[28px] font-light tracking-[0.12em] text-foreground tabular-nums">{clock}</div>
          <div className="mt-1 text-[11px] uppercase tracking-[0.25em] text-muted-foreground">{weekday} · {siteName}</div>
          <div className="mt-3 max-w-[280px] text-xs italic leading-relaxed text-muted-foreground" style={{ fontFamily: SERIF }}>
            雪落无声，心有所归。<br />
            每一行代码，每一个请求，<br />
            都像雪花一样，悄悄落下。
          </div>
        </div>
      </div>
    </div>
  );
}
