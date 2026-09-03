'use client';

import { useEffect } from 'react';
import { reportError } from '@/lib/error-report';
import { runRecovery } from '@/lib/chunk-error';
import { resolveRuntimeI18nMessage } from '@/lib/i18n-runtime';

export default function GlobalError({
    error,
    reset,
}: {
    error: Error & { digest?: string };
    reset: () => void;
}) {
    useEffect(() => {
        console.error('Global Error caught:', error);
        // 根层崩溃也要上报：global-error 期间 React 已卸载，这是最后的机会。
        void reportError({
            level: 'error',
            message: error.message,
            stack: error.stack ?? '',
        });
    }, [error]);

    // WO-029 defect 2: this boundary has a single recovery button, so a chunk
    // load failure (stale chunk URL after a deploy) previously left the user
    // with a reset() that can never succeed and no fallback at all. The
    // decision lives in runRecovery (src/lib/chunk-error.ts, shared with
    // app/error.tsx).
    const handleRecover = () => {
        runRecovery(error, reset, () => window.location.reload());
    };

    const title = resolveRuntimeI18nMessage('common.globalErrorBoundary.title', undefined, 'Critical Error');
    const message = resolveRuntimeI18nMessage('common.globalErrorBoundary.message', undefined, 'The application encountered a critical error and cannot recover automatically.');
    const tryToRecover = resolveRuntimeI18nMessage('common.globalErrorBoundary.tryToRecover', undefined, 'Try to Recover');

    return (
        <html>
            <body>
                <div
                    style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        minHeight: '100vh',
                        padding: '24px',
                        fontFamily: 'system-ui, -apple-system, sans-serif',
                        background: 'linear-gradient(180deg, #eae9e3 0%, #e1e6dd 100%)',
                    }}
                >
                    <div
                        style={{
                            maxWidth: '420px',
                            width: '100%',
                            padding: '32px',
                            borderRadius: '24px',
                            border: '1px solid rgba(0,0,0,0.08)',
                            background: 'rgba(255,255,255,0.6)',
                            backdropFilter: 'blur(12px)',
                            boxShadow: '0 4px 24px rgba(0,0,0,0.06)',
                            textAlign: 'center',
                        }}
                    >
                        <div
                            style={{
                                width: '56px',
                                height: '56px',
                                margin: '0 auto 20px',
                                borderRadius: '16px',
                                background: 'rgba(239,68,68,0.1)',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                fontSize: '28px',
                            }}
                        >
                            ⚠️
                        </div>
                        <h1 style={{ fontSize: '20px', fontWeight: 600, marginBottom: '8px', color: '#1a1a1a' }}>
                            {title}
                        </h1>
                        <p style={{ fontSize: '14px', color: '#666', marginBottom: '20px', lineHeight: 1.6 }}>
                            {message}
                        </p>
                        {error.message && (
                            <pre
                                style={{
                                    textAlign: 'left',
                                    fontSize: '12px',
                                    padding: '12px',
                                    borderRadius: '12px',
                                    background: 'rgba(0,0,0,0.04)',
                                    color: '#dc2626',
                                    overflow: 'auto',
                                    maxHeight: '120px',
                                    marginBottom: '20px',
                                }}
                            >
                                {error.message}
                            </pre>
                        )}
                        <button
                            onClick={handleRecover}
                            style={{
                                padding: '10px 24px',
                                borderRadius: '12px',
                                border: 'none',
                                background: '#1a1a1a',
                                color: '#fff',
                                fontSize: '14px',
                                fontWeight: 500,
                                cursor: 'pointer',
                                width: '100%',
                            }}
                        >
                            {tryToRecover}
                        </button>
                    </div>
                </div>
            </body>
        </html>
    );
}
