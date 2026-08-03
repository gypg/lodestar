'use client';

import { useCallback, useRef, useState, type ReactNode } from 'react';
import { useTranslations } from 'next-intl';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';

interface ConfirmOptions {
    title?: string;
    description?: string | ReactNode;
    confirmText?: string;
    cancelText?: string;
}

/**
 * Promise-based confirm dialog that replaces window.confirm().
 *
 * Usage:
 *   const confirm = useConfirm();
 *   const ok = await confirm({ title: 'Sure?', description: 'This cannot be undone.' });
 *   if (!ok) return;
 */
export function useConfirm() {
    const t = useTranslations('common');
    const [open, setOpen] = useState(false);
    const [options, setOptions] = useState<ConfirmOptions>({});
    const resolveRef = useRef<((value: boolean) => void) | null>(null);

    const confirm = useCallback((opts?: ConfirmOptions) => {
        setOptions(opts ?? {});
        setOpen(true);
        return new Promise<boolean>((resolve) => {
            resolveRef.current = resolve;
        });
    }, []);

    const handleConfirm = useCallback(() => {
        setOpen(false);
        resolveRef.current?.(true);
        resolveRef.current = null;
    }, []);

    const handleCancel = useCallback(() => {
        setOpen(false);
        resolveRef.current?.(false);
        resolveRef.current = null;
    }, []);

    const dialog = (
        <AlertDialog open={open} onOpenChange={(v) => { if (!v) handleCancel(); }}>
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>{options.title ?? t('confirm')}</AlertDialogTitle>
                    {options.description && (
                        <AlertDialogDescription>{options.description}</AlertDialogDescription>
                    )}
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel onClick={handleCancel}>{options.cancelText ?? t('cancel')}</AlertDialogCancel>
                    <AlertDialogAction onClick={handleConfirm}>{options.confirmText ?? t('confirm')}</AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    );

    return { confirm, dialog } as const;
}
