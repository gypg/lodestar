'use client';

import { useEffect } from 'react';
import { initErrorReporting } from '@/lib/error-report';

/**
 * 挂载前端崩溃上报监听（window.onerror / unhandledrejection）。
 * React 渲染错误由 app/error.tsx 与 global-error.tsx 边界单独上报。
 */
export function ErrorReportInit() {
    useEffect(() => {
        initErrorReporting();
    }, []);

    return null;
}
