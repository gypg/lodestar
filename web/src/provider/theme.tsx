"use client"

import * as React from "react"
import { ThemeProvider as NextThemesProvider, useTheme } from "next-themes"
import { useThemePresetStore } from "@/stores/theme-preset"
import { useCustomThemesStore } from "@/stores/custom-themes"
import { getPreset, THEME_TOKEN_KEYS } from "@/lib/theme-presets"

function ThemeColorUpdater() {
    const { resolvedTheme } = useTheme()

    React.useEffect(() => {
        const metaThemeColor = document.querySelector('meta[name="theme-color"]')
        if (metaThemeColor) {
            metaThemeColor.setAttribute(
                'content',
                resolvedTheme === 'dark' ? '#2d2920' : '#faf8f5'
            )
        }
    }, [resolvedTheme])

    return null
}

// PresetApplier injects the active theme preset's design tokens onto <html> as
// inline CSS custom properties. Inline props override both :root and .dark, so
// we apply the light/dark variant matching the resolved mode. Tokens the preset
// does not define are removed, letting globals.css fall back cleanly. This is
// what makes switching presets live-recolor the whole UI.
function PresetApplier() {
    const { resolvedTheme } = useTheme()
    const presetId = useThemePresetStore((s) => s.presetId)
    const customThemes = useCustomThemesStore((s) => s.themes)

    React.useEffect(() => {
        const root = document.documentElement
        const preset = getPreset(presetId, customThemes)
        const tokens =
            preset && resolvedTheme === 'dark'
                ? preset.dark
                : preset?.light
        for (const key of THEME_TOKEN_KEYS) {
            const value = tokens?.[key]
            if (value) {
                root.style.setProperty(`--${key}`, value)
            } else {
                root.style.removeProperty(`--${key}`)
            }
        }
        root.setAttribute('data-theme-preset', presetId)
    }, [presetId, resolvedTheme, customThemes])

    return null
}

export function ThemeProvider({ children, ...props }: React.ComponentProps<typeof NextThemesProvider>) {
    return (
        <NextThemesProvider {...props}>
            <ThemeColorUpdater />
            <PresetApplier />
            {children}
        </NextThemesProvider>
    )
}
