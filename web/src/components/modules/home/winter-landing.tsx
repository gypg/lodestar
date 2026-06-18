'use client';

/*
GGZERO — 冬日风落地页（Winter Landing）

承接 `首页文件/home.html` 的报刊式冬日封面设计，移植为原生 React 组件：
刊头「雪 落 无 声」、飘雪、冷光氛围、活体时钟、左侧编号「章节目录」。
目录项通过 useNavStore 软路由切换到真实控制台 tab（无整页刷新）。

配色全部走主题 CSS 变量（--background/--foreground/--primary…），因此随用户所选
主题预设（冬日/玫瑰/…）与明暗模式自适应——与 GGZERO 的每用户主题系统连成一体。
*/

import { useEffect, useMemo, useState } from 'react';
import { useNavStore, type NavItem } from '@/components/modules/navbar';

const SNOW_SYMBOLS = ['❄', '❅', '❆', '✻', '✺', '*'];
const SNOW_COUNT = 72;

const TOC: { id: NavItem; label: string; meta: string }[] = [
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

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const SERIF = '"Songti SC","STSong","Noto Serif SC","SimSun",Georgia,serif';
const SANS = '"Helvetica Neue",Arial,sans-serif';

function pad2(n: number) {
  return String(n).padStart(2, '0');
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

export function WinterLanding({ onEnterDashboard }: { onEnterDashboard: () => void }) {
  const setActiveItem = useNavStore((s) => s.setActiveItem);
  const [now, setNow] = useState(() => new Date());

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
      {/* 0. 冷光氛围（CSS 径向渐变，随主题 primary/accent 着色） */}
      <div
        className="pointer-events-none absolute inset-0 z-0 opacity-70"
        style={{
          background:
            'radial-gradient(120% 80% at 80% 18%, color-mix(in oklch, var(--primary) 14%, transparent), transparent 60%),' +
            'radial-gradient(90% 70% at 15% 85%, color-mix(in oklch, var(--accent) 12%, transparent), transparent 55%)',
        }}
      />
      {/* 2. 左侧留白雾化，确保目录可读 */}
      <div
        className="pointer-events-none absolute inset-y-0 left-0 z-[2] w-[42%]"
        style={{
          background:
            'linear-gradient(to right, color-mix(in oklch, var(--background) 92%, transparent) 0%, color-mix(in oklch, var(--background) 60%, transparent) 65%, transparent 100%)',
        }}
      />
      {/* 3. 四角暗角 */}
      <div
        className="pointer-events-none absolute inset-0 z-[3]"
        style={{ background: 'radial-gradient(ellipse at center, transparent 55%, color-mix(in oklch, var(--foreground) 10%, transparent) 100%)' }}
      />

      {/* 5. 雪花 */}
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

      {/* 10. 内容层 */}
      <div className="relative z-10 h-full w-full">
        {/* 刊头 */}
        <header
          className="absolute left-[6vw] right-[6vw] top-[6vh] border-b pb-4"
          style={{ borderColor: 'var(--foreground)' }}
        >
          <div
            className="mb-3 flex justify-between text-[11px] uppercase tracking-[0.25em] text-muted-foreground"
            style={{ fontFamily: SANS }}
          >
            <span>Vol. 01</span>
            <span>{dateStr}</span>
            <span>GGZERO Daily</span>
          </div>
          <h1 className="mt-1.5 text-center font-normal leading-none tracking-[0.28em] text-foreground" style={{ fontSize: 'clamp(32px,5.5vw,76px)' }}>
            雪 落 无 声
          </h1>
          <div className="mt-3 text-center text-xs italic tracking-[0.4em] text-muted-foreground">
            a quiet afternoon, watching snow
          </div>
        </header>

        {/* 右上：进入数据概览 */}
        <button
          type="button"
          onClick={onEnterDashboard}
          className="absolute right-[6vw] top-[6vh] border-b pb-px text-[11px] uppercase tracking-[0.25em] text-muted-foreground transition-colors hover:text-foreground"
          style={{ fontFamily: SANS, borderColor: 'currentColor' }}
        >
          进入数据概览 →
        </button>

        {/* 左侧编号目录 */}
        <aside
          className="absolute left-[6vw] top-1/2 max-h-[66vh] -translate-y-1/2 overflow-y-auto"
          style={{ fontFamily: SANS, minWidth: 'min(320px, 60vw)' }}
        >
          <h2
            className="mb-3 border-t pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-muted-foreground"
            style={{ borderColor: 'var(--foreground)' }}
          >
            章 节 目 录
          </h2>
          <ul className="list-none">
            {TOC.map((item, idx) => (
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

        {/* 右下：活体时钟 + 引文 */}
        <div className="absolute bottom-[6vh] right-[6vw] text-right" style={{ fontFamily: SANS }}>
          <div className="text-[28px] font-light tracking-[0.12em] text-foreground tabular-nums">{clock}</div>
          <div className="mt-1 text-[11px] uppercase tracking-[0.25em] text-muted-foreground">{weekday} · GGZERO</div>
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
