'use client';

import { X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

export type SiteBannerTone = 'info' | 'warning' | 'success';

export function SiteBannerStrip({
    text,
    tone = 'info',
    onDismiss,
}: {
    text: string;
    tone?: SiteBannerTone;
    onDismiss?: () => void;
}) {
    const t = useTranslations('banner');
    const trimmed = text.trim();
    if (!trimmed) return null;
    const toneClass =
        tone === 'warning'
            ? 'border-amber-500/30 bg-amber-500/10 text-amber-950 dark:text-amber-100'
            : tone === 'success'
              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-950 dark:text-emerald-100'
              : 'border-primary/25 bg-primary/8 text-foreground';

    return (
        <div className={cn('flex items-center gap-2 border-b px-3 py-2 text-sm', toneClass)}>
            <p className="min-w-0 flex-1 text-center sm:text-left">{trimmed}</p>
            {onDismiss ? (
                <button
                    type="button"
                    className="shrink-0 rounded-md p-1 opacity-70 hover:opacity-100"
                    onClick={onDismiss}
                    aria-label={t('closeAnnouncement')}
                >
                    <X className="size-4" />
                </button>
            ) : null}
        </div>
    );
}