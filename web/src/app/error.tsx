'use client';

import { useEffect } from 'react';
import { reportError } from '@/lib/error-report';
import { runRecovery } from '@/lib/chunk-error';
import { motion } from 'motion/react';
import { AlertTriangle, RotateCcw, Home } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';

export default function ErrorBoundary({
    error,
    reset,
}: {
    error: Error & { digest?: string };
    reset: () => void;
}) {
    const t = useTranslations('common.errorBoundary');
    // WO-029 defect 2: after a deploy, a stale tab holds chunk URLs that no
    // longer exist on the server, so `reset` (re-render of the same JS) can
    // never fix a chunk-load failure — only a full reload that fetches fresh
    // HTML with the new manifest can. The chunk-vs-reset decision lives in
    // runRecovery (src/lib/chunk-error.ts, shared with global-error.tsx).
    // Non-chunk errors keep `reset` so unsaved form state survives the retry.
    const handleTryAgain = () => {
        runRecovery(error, reset, () => window.location.reload());
    };

    useEffect(() => {
        console.error('App Error Boundary caught:', error);
        // 渲染错误落库（后端 error_logs，source=frontend），排障不再只靠浏览器控制台。
        void reportError({
            level: 'error',
            message: error.message,
            stack: error.stack ?? '',
        });
    }, [error]);

    return (
        <div className="flex min-h-screen items-center justify-center p-6">
            <motion.div
                initial={{ opacity: 0, y: 20, scale: 0.97 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
                className="relative w-full max-w-md overflow-hidden rounded-xl border border-border/35 bg-card p-8 shadow-sm "
            >
                <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_16%_14%,color-mix(in_oklch,var(--primary)_20%,transparent)_0%,transparent_30%),radial-gradient(circle_at_84%_18%,color-mix(in_oklch,var(--primary)_14%,transparent)_0%,transparent_26%)]" />

                <div className="relative flex flex-col items-center text-center">
                    <div className="mb-6 flex h-16 w-16 items-center justify-center rounded-lg border border-destructive/20 bg-destructive/10 shadow-sm">
                        <AlertTriangle className="h-8 w-8 text-destructive" />
                    </div>

                    <h2 className="mb-2 text-xl font-semibold tracking-tight">{t('title')}</h2>
                    <p className="mb-6 text-sm text-muted-foreground">
                        {t('message')}
                    </p>

                    {error.message && (
                        <div className="mb-6 w-full rounded-xl border border-border/30 bg-muted/40 p-3">
                            <code className="block max-h-24 overflow-auto text-left text-xs text-destructive/90">
                                {error.message}
                            </code>
                            {error.digest && (
                                <span className="mt-1 block text-left text-[10px] text-muted-foreground">
                                    ID: {error.digest}
                                </span>
                            )}
                        </div>
                    )}

                    <div className="flex w-full flex-col gap-2 sm:flex-row">
                        <Button
                            onClick={handleTryAgain}
                            className="flex-1 gap-2 rounded-xl"
                            variant="default"
                        >
                            <RotateCcw className="h-4 w-4" />
                            {t('tryAgain')}
                        </Button>
                        <Button
                            onClick={() => window.location.reload()}
                            className="flex-1 gap-2 rounded-xl"
                            variant="outline"
                        >
                            <Home className="h-4 w-4" />
                            {t('reloadPage')}
                        </Button>
                    </div>
                </div>
            </motion.div>
        </div>
    );
}
