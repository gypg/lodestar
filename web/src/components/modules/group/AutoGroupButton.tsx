'use client';

import { useCallback, useMemo, useState } from 'react';
import { RotateCcw, Sparkles, Waves } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useAutoGroupModels } from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { Button, buttonVariants } from '@/components/ui/button';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
    AlertDialogTrigger,
} from '@/components/ui/alert-dialog';
import { cn } from '@/lib/utils';

type AutoGroupButtonProps = {
    variant?: 'ghost' | 'default';
    className?: string;
    /** When true, always runs in force-rebuild mode (deletes all auto-created groups first). */
    forceMode?: boolean;
};

export function AutoGroupButton({ variant = 'ghost', className, forceMode = false }: AutoGroupButtonProps) {
    const t = useTranslations('group');
    const autoGroup = useAutoGroupModels();
    const [open, setOpen] = useState(false);

    const isForce = forceMode;
    const icon = isForce ? <RotateCcw className="size-4" /> : <Sparkles className="size-4" />;
    const label = isForce ? t('actions.forceRegroup') : t('actions.autoGroup');

    const summary = useMemo(() => {
        const result = autoGroup.data;
        if (!result) return '';
        const parts = [t('toast.autoGroupSuccess', {
            created: result.created_groups,
            skipped: result.skipped_existing_groups,
        })];
        if (result.deleted_groups > 0) {
            parts.push(t('toast.autoGroupDeleted', { deleted: result.deleted_groups }));
        }
        return parts.join(' ');
    }, [autoGroup.data, t]);

    const details = useMemo(() => {
        const result = autoGroup.data;
        if (!result) return '';
        return t('toast.autoGroupSuccessDescription', {
            models: result.total_models_seen,
            candidates: result.total_candidates,
            created: result.created_groups,
            skippedExisting: result.skipped_existing_groups,
            skippedCovered: result.skipped_covered_models,
        });
    }, [autoGroup.data, t]);

    const handleConfirm = useCallback(() => {
        autoGroup.mutate(isForce || undefined, {
            onSuccess: () => {
                setOpen(false);
                toast.success(summary, { description: details });
            },
            onError: (error) => {
                toast.error(t('toast.autoGroupFailed'), { description: error.message });
            },
        });
    }, [autoGroup, isForce, summary, details, t]);

    return (
        <AlertDialog open={open} onOpenChange={setOpen}>
            <AlertDialogTrigger asChild>
                {variant === 'default' ? (
                    <Button type="button" className={cn('rounded-lg', className)}>
                        {icon}
                        {label}
                    </Button>
                ) : (
                    <button
                        type="button"
                        className={cn(
                            buttonVariants({
                                variant: isForce ? 'outline' : 'ghost',
                                size: 'default',
                                className: cn(
                                    'rounded-lg border border-border/25 bg-card px-3 text-muted-foreground transition-[transform,border-color,background-color] duration-300 hover:-translate-y-0.5 hover:bg-card hover:text-foreground',
                                    isForce && 'border-orange-500/30 text-orange-600 hover:text-orange-700',
                                ),
                            }),
                            className,
                        )}
                    >
                        {icon}
                        <span>{label}</span>
                    </button>
                )}
            </AlertDialogTrigger>
            <AlertDialogContent className="rounded-xl">
                <AlertDialogHeader>
                    <div className={cn(
                        'inline-flex w-fit items-center gap-2 rounded-full border px-3 py-1 text-[0.68rem] font-semibold',
                        isForce
                            ? 'border-orange-500/20 bg-orange-500/5 text-orange-600'
                            : 'border-primary/15 bg-card text-primary',
                    )}>
                        {isForce ? <RotateCcw className="size-3.5" /> : <Waves className="size-3.5" />}
                        {label}
                    </div>
                    <AlertDialogTitle>
                        {isForce ? t('autoGroup.forceConfirmTitle') : t('autoGroup.confirmTitle')}
                    </AlertDialogTitle>
                    <AlertDialogDescription className="whitespace-pre-line">
                        {isForce ? t('autoGroup.forceConfirmDescription') : t('autoGroup.confirmDescription')}
                    </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel disabled={autoGroup.isPending}>{t('detail.actions.cancel')}</AlertDialogCancel>
                    <AlertDialogAction
                        disabled={autoGroup.isPending}
                        onClick={(event) => {
                            event.preventDefault();
                            handleConfirm();
                        }}
                    >
                        {autoGroup.isPending ? t('autoGroup.submitting') : (isForce ? t('autoGroup.forceSubmit') : t('autoGroup.submit'))}
                    </AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    );
}
