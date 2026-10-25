/*
Lodestar — theme preset engine.

A "theme preset" is a set of CSS custom-property (design token) overrides applied
to <html> at runtime. The app's Tailwind v4 `@theme inline` layer maps utility
colors (bg-background, text-primary, …) onto these `--*` variables, so overriding
them on document.documentElement live-recolors the entire UI without touching any
component.

This is the foundation of Lodestar's "every user can have a different look" vision:
- built-in presets live here;
- a user's chosen preset is persisted (stores/theme-preset.ts);
- uploaded/custom presets (server-side) reuse the same token shape (ThemeTokens).

Each preset provides a `light` and `dark` token map. Tokens omitted by a preset
fall back to the base values defined in globals.css (:root / .dark), so a preset
may override just the accent hue or do a full reskin.
*/

// The full set of color tokens a preset may control. Keep in sync with globals.css.
export const THEME_TOKEN_KEYS = [
  'background',
  'foreground',
  'card',
  'card-foreground',
  'popover',
  'popover-foreground',
  'primary',
  'primary-foreground',
  'secondary',
  'secondary-foreground',
  'muted',
  'muted-foreground',
  'accent',
  'accent-foreground',
  'destructive',
  'destructive-foreground',
  'border',
  'input',
  'ring',
  'chart-1',
  'chart-2',
  'chart-3',
  'chart-4',
  'chart-5',
  'sidebar',
  'sidebar-foreground',
  'sidebar-primary',
  'sidebar-primary-foreground',
  'sidebar-accent',
  'sidebar-accent-foreground',
  'sidebar-border',
  'sidebar-ring',
  'radius',
] as const

export type ThemeTokenKey = (typeof THEME_TOKEN_KEYS)[number]
export type ThemeTokens = Partial<Record<ThemeTokenKey, string>>

export interface ThemePreset {
  id: string
  /** i18n-free display name; UI may translate via the `label` key if present. */
  name: string
  /** Short description shown under the name. */
  description?: string
  /** A representative color (any CSS color) used for the picker swatch. */
  swatch: string
  /** true for shipped presets; uploaded presets set this false. */
  builtin: boolean
  light: ThemeTokens
  dark: ThemeTokens
}

// Accent-only preset helper: recolor the hue-bearing tokens, keep neutral surfaces
// from the base theme. `lp`/`dp` are the light/dark primary; `la`/`da` the accents.
function accentPreset(
  id: string,
  name: string,
  swatch: string,
  description: string,
  lp: string,
  la: string,
  dp: string,
  da: string,
): ThemePreset {
  return {
    id,
    name,
    description,
    swatch,
    builtin: true,
    light: {
      primary: lp,
      'primary-foreground': 'oklch(0.99 0.002 90)',
      accent: la,
      ring: lp,
      'sidebar-primary': lp,
      'sidebar-ring': lp,
      'chart-1': lp,
      'chart-2': la,
    },
    dark: {
      primary: dp,
      'primary-foreground': 'oklch(0.18 0.02 90)',
      accent: da,
      ring: dp,
      'sidebar-primary': dp,
      'sidebar-ring': dp,
      'chart-1': dp,
      'chart-2': da,
    },
  }
}

// "default" carries no overrides — it simply lets globals.css (:root/.dark) rule.
const defaultPreset: ThemePreset = {
  id: 'default',
  name: 'Scandi',
  description: '内置默认 · 北欧灰绿',
  swatch: 'oklch(0.62 0.08 160)',
  builtin: true,
  light: {},
  dark: {},
}

// "winter" — a full reskin: papery cool surfaces + cold-blue accent + soft serif feel.
// Carries forward the 冬日风 aesthetic from the earlier new-api work.
const winterPreset: ThemePreset = {
  id: 'winter',
  name: '冬日 Winter',
  description: '纸感冷蓝 · 安静留白',
  swatch: 'oklch(0.66 0.075 240)',
  builtin: true,
  light: {
    background: 'oklch(0.965 0.006 250)',
    foreground: 'oklch(0.28 0.018 255)',
    card: 'oklch(0.99 0.004 250)',
    'card-foreground': 'oklch(0.28 0.018 255)',
    popover: 'oklch(0.99 0.004 250)',
    'popover-foreground': 'oklch(0.28 0.018 255)',
    primary: 'oklch(0.6 0.082 245)',
    'primary-foreground': 'oklch(0.99 0.004 250)',
    secondary: 'oklch(0.95 0.008 250)',
    'secondary-foreground': 'oklch(0.36 0.02 255)',
    muted: 'oklch(0.93 0.008 250)',
    'muted-foreground': 'oklch(0.48 0.02 255)',
    accent: 'oklch(0.66 0.075 240)',
    'accent-foreground': 'oklch(0.24 0.02 255)',
    border: 'oklch(0.89 0.01 250)',
    input: 'oklch(0.89 0.01 250)',
    ring: 'oklch(0.6 0.082 245)',
    'chart-1': 'oklch(0.6 0.082 245)',
    'chart-2': 'oklch(0.66 0.075 240)',
    sidebar: 'oklch(0.975 0.006 250)',
    'sidebar-foreground': 'oklch(0.28 0.018 255)',
    'sidebar-primary': 'oklch(0.6 0.082 245)',
    'sidebar-primary-foreground': 'oklch(0.99 0.004 250)',
    'sidebar-accent': 'oklch(0.93 0.008 250)',
    'sidebar-border': 'oklch(0.89 0.01 250)',
    'sidebar-ring': 'oklch(0.6 0.082 245)',
  },
  dark: {
    background: 'oklch(0.23 0.015 255)',
    foreground: 'oklch(0.91 0.008 250)',
    card: 'oklch(0.27 0.018 255)',
    'card-foreground': 'oklch(0.91 0.008 250)',
    popover: 'oklch(0.27 0.018 255)',
    'popover-foreground': 'oklch(0.91 0.008 250)',
    primary: 'oklch(0.7 0.085 245)',
    'primary-foreground': 'oklch(0.19 0.02 255)',
    secondary: 'oklch(0.29 0.018 255)',
    'secondary-foreground': 'oklch(0.91 0.008 250)',
    muted: 'oklch(0.29 0.018 255)',
    'muted-foreground': 'oklch(0.62 0.015 250)',
    accent: 'oklch(0.64 0.07 240)',
    'accent-foreground': 'oklch(0.19 0.02 255)',
    border: 'oklch(0.33 0.018 255)',
    input: 'oklch(0.33 0.018 255)',
    ring: 'oklch(0.7 0.085 245)',
    'chart-1': 'oklch(0.7 0.085 245)',
    'chart-2': 'oklch(0.64 0.07 240)',
    sidebar: 'oklch(0.25 0.016 255)',
    'sidebar-foreground': 'oklch(0.91 0.008 250)',
    'sidebar-primary': 'oklch(0.7 0.085 245)',
    'sidebar-primary-foreground': 'oklch(0.19 0.02 255)',
    'sidebar-accent': 'oklch(0.31 0.018 255)',
    'sidebar-border': 'oklch(0.33 0.018 255)',
    'sidebar-ring': 'oklch(0.7 0.085 245)',
  },
}

// "selora" — deep-space silver (WO-055). Full reskin in dark mode:
// near-black #070709-ish surfaces, silver-white accents, and the status
// palette green/amber/red that the reference console used. Light mode keeps
// the identity in a papery daylight variant (the preset is primarily a dark
// skin, but light mode must still look deliberate, not accidental).
const seloraPreset: ThemePreset = {
  id: 'selora',
  name: '深空 Selora',
  description: '深空银白 · 星点静谧',
  swatch: 'oklch(0.22 0.012 270)',
  builtin: true,
  light: {
    background: 'oklch(0.975 0.004 270)',
    foreground: 'oklch(0.22 0.012 270)',
    card: 'oklch(0.995 0.002 270)',
    'card-foreground': 'oklch(0.22 0.012 270)',
    popover: 'oklch(0.995 0.002 270)',
    'popover-foreground': 'oklch(0.22 0.012 270)',
    primary: 'oklch(0.32 0.012 270)',
    'primary-foreground': 'oklch(0.97 0.004 270)',
    secondary: 'oklch(0.945 0.006 270)',
    'secondary-foreground': 'oklch(0.3 0.014 270)',
    muted: 'oklch(0.93 0.006 270)',
    'muted-foreground': 'oklch(0.5 0.014 270)',
    accent: 'oklch(0.94 0.006 270)',
    'accent-foreground': 'oklch(0.24 0.012 270)',
    border: 'oklch(0.9 0.008 270)',
    input: 'oklch(0.9 0.008 270)',
    ring: 'oklch(0.32 0.012 270)',
    'chart-1': 'oklch(0.32 0.012 270)',
    'chart-2': 'oklch(0.62 0.13 155)',
    'chart-3': 'oklch(0.72 0.13 75)',
    'chart-4': 'oklch(0.6 0.18 25)',
    'chart-5': 'oklch(0.45 0.02 270)',
    sidebar: 'oklch(0.965 0.005 270)',
    'sidebar-foreground': 'oklch(0.24 0.012 270)',
    'sidebar-primary': 'oklch(0.32 0.012 270)',
    'sidebar-primary-foreground': 'oklch(0.97 0.004 270)',
    'sidebar-accent': 'oklch(0.93 0.006 270)',
    'sidebar-border': 'oklch(0.9 0.008 270)',
    'sidebar-ring': 'oklch(0.32 0.012 270)',
  },
  dark: {
    // Near-black space surfaces; card/popover lift one step, never grey-blue.
    background: 'oklch(0.145 0.008 270)',
    foreground: 'oklch(0.955 0.004 270)',
    card: 'oklch(0.185 0.01 270)',
    'card-foreground': 'oklch(0.955 0.004 270)',
    popover: 'oklch(0.205 0.01 270)',
    'popover-foreground': 'oklch(0.955 0.004 270)',
    // Silver-white accent system (the reference console's accent-400/500).
    primary: 'oklch(0.93 0.004 270)',
    'primary-foreground': 'oklch(0.185 0.01 270)',
    secondary: 'oklch(0.235 0.01 270)',
    'secondary-foreground': 'oklch(0.9 0.005 270)',
    muted: 'oklch(0.235 0.01 270)',
    'muted-foreground': 'oklch(0.68 0.01 270)',
    accent: 'oklch(0.255 0.012 270)',
    'accent-foreground': 'oklch(0.955 0.004 270)',
    border: 'oklch(0.28 0.012 270)',
    input: 'oklch(0.28 0.012 270)',
    ring: 'oklch(0.93 0.004 270)',
    // Status palette from the reference: ok-green, warn-amber, danger-red.
    'chart-1': 'oklch(0.93 0.004 270)',
    'chart-2': 'oklch(0.72 0.14 158)',
    'chart-3': 'oklch(0.8 0.14 85)',
    'chart-4': 'oklch(0.62 0.19 25)',
    'chart-5': 'oklch(0.45 0.02 270)',
    sidebar: 'oklch(0.165 0.009 270)',
    'sidebar-foreground': 'oklch(0.9 0.005 270)',
    'sidebar-primary': 'oklch(0.93 0.004 270)',
    'sidebar-primary-foreground': 'oklch(0.185 0.01 270)',
    'sidebar-accent': 'oklch(0.235 0.01 270)',
    'sidebar-border': 'oklch(0.28 0.012 270)',
    'sidebar-ring': 'oklch(0.93 0.004 270)',
  },
}

export const BUILTIN_PRESETS: ThemePreset[] = [
  defaultPreset,
  winterPreset,
  seloraPreset,
  accentPreset('rose', '玫瑰 Rose', 'oklch(0.62 0.18 18)', '暖玫红 · 柔和', 'oklch(0.6 0.17 18)', 'oklch(0.68 0.12 30)', 'oklch(0.68 0.16 18)', 'oklch(0.7 0.11 30)'),
  accentPreset('violet', '紫罗兰 Violet', 'oklch(0.58 0.2 295)', '静谧紫', 'oklch(0.56 0.19 295)', 'oklch(0.64 0.14 280)', 'oklch(0.66 0.17 295)', 'oklch(0.7 0.13 280)'),
  accentPreset('amber', '琥珀 Amber', 'oklch(0.7 0.15 70)', '温暖琥珀', 'oklch(0.66 0.14 70)', 'oklch(0.72 0.12 55)', 'oklch(0.74 0.13 70)', 'oklch(0.76 0.11 55)'),
  accentPreset('forest', '森林 Forest', 'oklch(0.55 0.12 150)', '沉静林绿', 'oklch(0.52 0.12 150)', 'oklch(0.62 0.1 165)', 'oklch(0.64 0.12 150)', 'oklch(0.7 0.1 165)'),
  accentPreset('ocean', '海洋 Ocean', 'oklch(0.6 0.11 215)', '清澈海青', 'oklch(0.58 0.11 215)', 'oklch(0.66 0.09 195)', 'oklch(0.68 0.11 215)', 'oklch(0.72 0.09 195)'),
  accentPreset('sakura', '樱花 Sakura', 'oklch(0.74 0.11 350)', '柔粉樱色', 'oklch(0.66 0.13 350)', 'oklch(0.74 0.09 340)', 'oklch(0.74 0.12 350)', 'oklch(0.78 0.09 340)'),
  accentPreset('mono', '墨色 Mono', 'oklch(0.5 0.01 250)', '极简灰墨', 'oklch(0.45 0.01 250)', 'oklch(0.6 0.015 250)', 'oklch(0.7 0.01 250)', 'oklch(0.62 0.015 250)'),
]

export const DEFAULT_PRESET_ID = 'winter'

export function getPreset(id: string, extra: ThemePreset[] = []): ThemePreset | undefined {
  return [...BUILTIN_PRESETS, ...extra].find((p) => p.id === id)
}
