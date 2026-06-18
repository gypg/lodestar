/*
GGZERO — theme preset engine.

A "theme preset" is a set of CSS custom-property (design token) overrides applied
to <html> at runtime. The app's Tailwind v4 `@theme inline` layer maps utility
colors (bg-background, text-primary, …) onto these `--*` variables, so overriding
them on document.documentElement live-recolors the entire UI without touching any
component.

This is the foundation of GGZERO's "every user can have a different look" vision:
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

export const BUILTIN_PRESETS: ThemePreset[] = [
  defaultPreset,
  winterPreset,
  accentPreset('rose', '玫瑰 Rose', 'oklch(0.62 0.18 18)', '暖玫红 · 柔和', 'oklch(0.6 0.17 18)', 'oklch(0.68 0.12 30)', 'oklch(0.68 0.16 18)', 'oklch(0.7 0.11 30)'),
  accentPreset('violet', '紫罗兰 Violet', 'oklch(0.58 0.2 295)', '静谧紫', 'oklch(0.56 0.19 295)', 'oklch(0.64 0.14 280)', 'oklch(0.66 0.17 295)', 'oklch(0.7 0.13 280)'),
  accentPreset('amber', '琥珀 Amber', 'oklch(0.7 0.15 70)', '温暖琥珀', 'oklch(0.66 0.14 70)', 'oklch(0.72 0.12 55)', 'oklch(0.74 0.13 70)', 'oklch(0.76 0.11 55)'),
]

export const DEFAULT_PRESET_ID = 'winter'

export function getPreset(id: string, extra: ThemePreset[] = []): ThemePreset | undefined {
  return [...BUILTIN_PRESETS, ...extra].find((p) => p.id === id)
}
