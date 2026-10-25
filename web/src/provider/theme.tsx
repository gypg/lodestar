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
    const firstRun = React.useRef(true)

    // 主题切换渐变：token 改写前给整个文档加一个颜色过渡 class，
    // 让 background/card/border/text 以 450ms 平滑滑入新配色；
    // 同时放一层从视口中心扩散的径向渐变覆盖（600ms 淡出），
    // 形成"空间跃迁"式的换肤感。首次加载不播（避免开屏闪一下）。
    const playTransition = React.useCallback((next: string) => {
        const root = document.documentElement
        root.classList.add('theme-switching')
        window.setTimeout(() => root.classList.remove('theme-switching'), 520)

        const veil = document.createElement('div')
        veil.className = 'theme-switch-veil'
        veil.setAttribute('data-next-preset', next)
        document.body.appendChild(veil)
        // 动画结束后自清理；以防万一同时挂 timeout 兜底。
        veil.addEventListener('animationend', () => veil.remove(), { once: true })
        window.setTimeout(() => veil.remove(), 1200)
    }, [])

    React.useEffect(() => {
        const root = document.documentElement
        if (firstRun.current) {
            firstRun.current = false
        } else {
            playTransition(presetId)
        }
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
    }, [presetId, resolvedTheme, customThemes, playTransition])

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
