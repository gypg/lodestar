"use client"
import * as React from "react"

const MOBILE_BREAKPOINT = 768

// 返回视口宽度是否小于给定断点（px）。用于在 JS 侧根据断点切换布局，
// 例如宽表格 ↔ 折叠卡片。SSR/静态导出时首帧返回 false，挂载后再校正。
export function useIsBelowBreakpoint(breakpoint: number) {
  const getMatches = React.useCallback(() => {
    if (typeof window === "undefined") {
      return false
    }
    return window.innerWidth < breakpoint
  }, [breakpoint])

  const [matches, setMatches] = React.useState<boolean>(() => getMatches())

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`)
    const onChange = () => {
      setMatches(getMatches())
    }
    mql.addEventListener("change", onChange)
    setMatches(getMatches())
    return () => mql.removeEventListener("change", onChange)
  }, [breakpoint, getMatches])

  return matches
}

export function useIsMobile() {
  return useIsBelowBreakpoint(MOBILE_BREAKPOINT)
}
