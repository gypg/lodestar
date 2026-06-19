'use client';

import { useEffect, useRef } from 'react';

declare global {
    interface Window {
        AmbientLightBg?: new (el: HTMLElement, opts?: Record<string, unknown>) => { destroy?: () => void };
    }
}

/** Optional color4bg-style ambient layer (GGGZERO AmbientLightBg). Fails silently. */
export function Color4bgAmbient({ active }: { active: boolean }) {
    const ref = useRef<HTMLDivElement>(null);
    const inst = useRef<{ destroy?: () => void } | null>(null);

    useEffect(() => {
        if (!active || !ref.current) return;
        let cancelled = false;
        const el = ref.current;
        const script = document.createElement('script');
        script.src = '/AmbientLightBg.min.js';
        script.async = true;
        script.onload = () => {
            if (cancelled || !window.AmbientLightBg) return;
            try {
                inst.current = new window.AmbientLightBg(el, { resize: true });
            } catch {
                /* keep photo background */
            }
        };
        document.body.appendChild(script);
        return () => {
            cancelled = true;
            inst.current?.destroy?.();
            inst.current = null;
            script.remove();
        };
    }, [active]);

    if (!active) return null;
    return (
        <div
            ref={ref}
            className="pointer-events-none absolute inset-0 z-[0] opacity-70"
            aria-hidden
        />
    );
}