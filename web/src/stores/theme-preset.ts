/*
GGZERO — active theme-preset store.

Persists the user's chosen theme preset id in localStorage so the look survives
reloads (per-browser today; a future increment syncs it to the account so the
preset follows the user across devices). Kept separate from the light/dark mode,
which remains owned by next-themes.
*/
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { DEFAULT_PRESET_ID } from '@/lib/theme-presets'

interface ThemePresetState {
  presetId: string
  setPreset: (id: string) => void
}

export const useThemePresetStore = create<ThemePresetState>()(
  persist(
    (set) => ({
      presetId: DEFAULT_PRESET_ID,
      setPreset: (presetId) => set({ presetId }),
    }),
    {
      name: 'ggzero-theme-preset',
      storage: createJSONStorage(() => localStorage),
    },
  ),
)
