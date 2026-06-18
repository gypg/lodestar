'use client';

/*
GGZERO — 冬日风落地页（Winter Landing）

忠实承接 `首页文件/home.html`：蓝色少女雪景照片背景（靠右，左侧自然留白）+ 纸感冷调 +
飘雪 + 左侧目录 + 活体时钟。封面采用**固定的冬日纸感配色**（纸 #f4f1ec / 墨 #1f1d1a /
冷蓝 #6f9ec2），不随应用明暗模式变化——保证始终是记忆里那张唯美的蓝色冬日封面。
（主题系统仍管控登录后的控制台各页；此封面是站点招牌门面，固定其美术。）

两种形态：
- public（访客）：左侧目录点「公告/模型广场/用量概览/关于」→ 右侧浮出站点内容（无需登录）；
  「进入控制台」→ 唤出登录。
- home（已登录）：目录软路由到控制台 tab；右上「进入数据概览」切仪表盘。
*/

import { useEffect, useMemo, useState } from 'react';
import { useNavStore, type NavItem } from '@/components/modules/navbar';
import { usePublicOverview } from '@/api/endpoints/public';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';

const SNOW_SYMBOLS = ['❄', '❅', '❆', '✻', '✺', '*'];
const SNOW_COUNT = 72;

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
  const { data: me } = useCurrentUser();
  const siteName = overview?.site_name?.trim() || 'GGZERO';
  // 非 staff（商业注册用户）只在目录里看到用户自助项
  const portalOnly = !isPublic && me !== undefined && !isStaffRole(me.role);
  const homeItems = portalOnly
    ? HOME_TOC.filter((i) => i.id === 'model' || i.id === 'apikey' || i.id === 'setting')
    : HOME_TOC;

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
      className="relative h-full w-full overflow-hidden rounded-xl border border-[#1f1d1a]/15 bg-[#f4f1ec] text-[#1f1d1a]"
      style={{ fontFamily: SERIF }}
    >
      {/* 1. 蓝色少女雪景照片背景（靠右，左侧留白给导航） */}
      <div
        className="pointer-events-none absolute inset-0 z-[1]"
        style={{
          backgroundImage: "url('/winter-bg.jpg')",
          backgroundSize: 'cover',
          backgroundPosition: 'right center',
          backgroundRepeat: 'no-repeat',
        }}
      />
      {/* 2. 左侧雾化（纸感冷白），让左侧文字可读 */}
      <div
        className="pointer-events-none absolute inset-y-0 left-0 z-[2] w-[42%]"
        style={{
          background:
            'linear-gradient(to right, rgba(244,249,253,0.92) 0%, rgba(244,249,253,0.55) 65%, rgba(244,249,253,0) 100%)',
        }}
      />
      {/* 3. 四角暗角（老照片感） */}
      <div
        className="pointer-events-none absolute inset-0 z-[3]"
        style={{ background: 'radial-gradient(ellipse at center, transparent 55%, rgba(20,30,50,0.18) 100%)' }}
      />

      {/* 5. 雪花 */}
      <div className="pointer-events-none absolute inset-0 z-[5] overflow-hidden" aria-hidden>
        {flakes.map((f, i) => (
          <span
            key={i}
            className="ggzero-snowflake absolute top-0 select-none"
            style={{
              left: `${f.left}%`,
              fontSize: `${f.size}px`,
              opacity: f.opacity,
              color: '#ffffff',
              textShadow: '0 0 3px rgba(120,140,160,0.5)',
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
        <header className="absolute left-[6vw] right-[6vw] top-[6vh] border-b border-[#1f1d1a] pb-4">
          <div className="mb-3 flex justify-between text-[11px] uppercase tracking-[0.25em] text-[#6b6862]" style={{ fontFamily: SANS }}>
            <span>Vol. 01</span>
            <span>{dateStr}</span>
            <span className="max-w-[40%] truncate">{siteName} Daily</span>
          </div>
          <h1 className="mt-1.5 text-center font-normal leading-none tracking-[0.28em] text-[#1f1d1a]" style={{ fontSize: 'clamp(32px,5.5vw,76px)' }}>
            雪 落 无 声
          </h1>
          <div className="mt-3 text-center text-xs italic tracking-[0.4em] text-[#6b6862]">a quiet afternoon, watching snow</div>
        </header>

        {/* 右上：登录 / 进入数据概览 */}
        <button
          type="button"
          onClick={isPublic ? onLogin : onEnterDashboard}
          className="absolute right-[6vw] top-[16vh] border-b border-current pb-px text-[11px] uppercase tracking-[0.25em] text-[#6b6862] transition-colors hover:text-[#1f1d1a]"
          style={{ fontFamily: SANS }}
        >
          {isPublic ? '登录 / 进入 →' : '进入数据概览 →'}
        </button>

        {/* 左侧目录 */}
        <aside className="absolute left-[6vw] top-1/2 max-h-[66vh] -translate-y-1/2 overflow-y-auto" style={{ fontFamily: SANS, minWidth: 'min(320px, 60vw)' }}>
          <h2 className="mb-3 border-t border-[#1f1d1a] pt-2 text-[11px] font-medium uppercase tracking-[0.3em] text-[#6b6862]">章 节 目 录</h2>
          <ul className="list-none">
            {isPublic
              ? PUBLIC_TOC.map((item, idx) => (
                  <li key={item.key} className="flex items-baseline justify-between border-b border-dotted border-[#b8b3a8] py-2">
                    <span className="mr-2.5 text-[11px] tabular-nums text-[#6b6862]">{pad2(idx + 1)}</span>
                    <button
                      type="button"
                      onClick={() => (item.key === 'console' ? onLogin?.() : setPanel(item.key as PublicPanel))}
                      className={`flex-1 border-b text-left text-sm text-[#1f1d1a] transition-colors hover:border-[#6f9ec2] ${panel === item.key ? 'border-[#6f9ec2]' : 'border-transparent'}`}
                    >
                      {item.label}
                    </button>
                    <span className="ml-2 text-[10px] tracking-wider text-[#6b6862]">{item.meta}</span>
                  </li>
                ))
              : homeItems.map((item, idx) => (
                  <li key={item.id} className="flex items-baseline justify-between border-b border-dotted border-[#b8b3a8] py-2">
                    <span className="mr-2.5 text-[11px] tabular-nums text-[#6b6862]">{pad2(idx + 1)}</span>
                    <button
                      type="button"
                      onClick={() => setActiveItem(item.id)}
                      className="flex-1 border-b border-transparent text-left text-sm text-[#1f1d1a] transition-colors hover:border-[#6f9ec2]"
                    >
                      {item.label}
                    </button>
                    <span className="ml-2 text-[10px] tracking-wider text-[#6b6862]">{item.meta}</span>
                  </li>
                ))}
          </ul>
        </aside>

        {/* 公开内容面板（访客点目录项浮出，纸感卡片） */}
        {isPublic && panel && (
          <div
            className="absolute right-[5vw] top-[20vh] z-20 w-[min(440px,88vw)] max-h-[62vh] overflow-y-auto rounded-xl border border-[#cdc7ba] bg-[#fbfaf7]/95 p-5 shadow-lg backdrop-blur"
            style={{ fontFamily: SANS, color: '#1f1d1a' }}
          >
            <div className="mb-3 flex items-center justify-between border-b border-[#cdc7ba] pb-2">
              <span className="text-sm font-semibold tracking-wide">
                {panel === 'announcement' && '站点公告'}
                {panel === 'models' && '模型广场'}
                {panel === 'usage' && '用量概览'}
                {panel === 'about' && `关于 ${siteName}`}
              </span>
              <button type="button" onClick={() => setPanel(null)} className="text-[#6b6862] transition-colors hover:text-[#1f1d1a]" aria-label="关闭">✕</button>
            </div>

            {panel === 'announcement' && (
              <p className="whitespace-pre-wrap text-sm leading-relaxed">{overview?.announcement?.trim() || '暂无公告。'}</p>
            )}

            {panel === 'models' && (
              <div className="flex flex-col gap-1.5">
                <p className="mb-1 text-xs text-[#6b6862]">共 {overview?.model_count ?? 0} 个模型</p>
                {(overview?.models ?? []).length === 0 && <p className="text-sm text-[#6b6862]">暂无公开模型。</p>}
                {(overview?.models ?? []).map((m) => (
                  <div key={m.name} className="flex items-baseline justify-between border-b border-dotted border-[#cdc7ba] py-1 text-sm">
                    <span className="mr-3 truncate">{m.name}</span>
                    {(m.input > 0 || m.output > 0) && (
                      <span className="shrink-0 text-[11px] tabular-nums text-[#6b6862]">入 {m.input} / 出 {m.output}</span>
                    )}
                  </div>
                ))}
              </div>
            )}

            {panel === 'usage' && (
              <div className="grid grid-cols-3 gap-3 text-center">
                <div className="rounded-lg border border-[#cdc7ba] p-3">
                  <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">{fmt(overview?.total_requests)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">请求</div>
                </div>
                <div className="rounded-lg border border-[#cdc7ba] p-3">
                  <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">{fmt(overview?.total_tokens)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">Tokens</div>
                </div>
                <div className="rounded-lg border border-[#cdc7ba] p-3">
                  <div className="text-lg font-semibold tabular-nums text-[#5a86a8]">{fmt(overview?.model_count)}</div>
                  <div className="mt-1 text-[10px] uppercase tracking-wider text-[#6b6862]">模型</div>
                </div>
              </div>
            )}

            {panel === 'about' && (
              <p className="whitespace-pre-wrap text-sm leading-relaxed">
                {overview?.description?.trim() || `${siteName} —— 高自定义 · 自用优先 · 可聚合的个人 AI 中转站。`}
              </p>
            )}
          </div>
        )}

        {/* 右下：活体时钟 + 引文 */}
        <div className="absolute bottom-[6vh] right-[6vw] text-right" style={{ fontFamily: SANS }}>
          <div className="text-[28px] font-light tracking-[0.12em] tabular-nums text-[#1f1d1a]">{clock}</div>
          <div className="mt-1 text-[11px] uppercase tracking-[0.25em] text-[#6b6862]">{weekday} · {siteName}</div>
          <div className="mt-3 max-w-[280px] text-xs italic leading-relaxed text-[#6b6862]" style={{ fontFamily: SERIF }}>
            雪落无声，心有所归。<br />
            每一行代码，每一个请求，<br />
            都像雪花一样，悄悄落下。
          </div>
        </div>
      </div>
    </div>
  );
}
