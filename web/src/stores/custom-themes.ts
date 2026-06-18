/*
Lodestar — custom (uploaded) theme registry, client side.

Themes uploaded via the API are stored server-side as the `custom_themes`
setting (a JSON array of ThemePreset). This store mirrors that list on the
client so both the theme picker (inside the query provider) and the early
PresetApplier (outside it, in ThemeProvider) can read the same set without a
prop bridge. It persists to localStorage so an uploaded theme applies instantly
on reload, before the settings request resolves.
*/
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { THEME_TOKEN_KEYS, type ThemePreset, type ThemeTokens } from '@/lib/theme-presets'

interface CustomThemesState {
  themes: ThemePreset[]
  setThemes: (themes: ThemePreset[]) => void
}

export const useCustomThemesStore = create<CustomThemesState>()(
  persist(
    (set) => ({
      themes: [],
      setThemes: (themes) => set({ themes }),
    }),
    {
      name: 'Lodestar-custom-themes',
      storage: createJSONStorage(() => localStorage),
    },
  ),
)

function sanitizeTokens(input: unknown): ThemeTokens {
  const out: ThemeTokens = {}
  if (!input || typeof input !== 'object') return out
  const obj = input as Record<string, unknown>
  for (const key of THEME_TOKEN_KEYS) {
    const v = obj[key]
    if (typeof v === 'string' && v.trim()) out[key] = v.trim()
  }
  return out
}

/**
 * Parse the raw `custom_themes` setting value (a JSON array) into validated
 * ThemePreset objects. Invalid entries are skipped rather than throwing, so one
 * bad upload never breaks the whole picker.
 */
export function parseCustomThemes(raw: string | undefined | null): ThemePreset[] {
  if (!raw) return []
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    return []
  }
  if (!Array.isArray(data)) return []
  const result: ThemePreset[] = []
  for (const item of data) {
    if (!item || typeof item !== 'object') continue
    const o = item as Record<string, unknown>
    const id = typeof o.id === 'string' ? o.id.trim() : ''
    const name = typeof o.name === 'string' ? o.name.trim() : ''
    if (!id || !name) continue
    const light = sanitizeTokens(o.light)
    const dark = sanitizeTokens(o.dark)
    if (Object.keys(light).length === 0 && Object.keys(dark).length === 0) continue
    result.push({
      id,
      name,
      description: typeof o.description === 'string' ? o.description : undefined,
      swatch: typeof o.swatch === 'string' && o.swatch.trim() ? o.swatch : (light.primary || dark.primary || '#888'),
      builtin: false,
      light,
      dark,
    })
  }
  return result
}
