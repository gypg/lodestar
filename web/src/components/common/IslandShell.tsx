'use client';

import { cn } from '@/lib/utils';
import type { ReactNode } from 'react';

interface IslandShellProps {
    children: ReactNode;
    className?: string;
    showDecorations?: boolean;
}

export function IslandShell({ children, className }: IslandShellProps) {
    return (
        <div
            className={cn(
                'relative flex h-full min-h-0 flex-col rounded-xl border border-border bg-card text-card-foreground',
                className
            )}
        >
            {children}
        </div>
    );
}
