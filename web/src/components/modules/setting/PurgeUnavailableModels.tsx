'use client';

import { useState } from 'react';
import { Trash2, Stethoscope } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { usePurgeUnavailableGroupItems } from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
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

export function SettingPurgeUnavailableModels() {
    const t = useTranslations('setting');
    const purge = usePurgeUnavailableGroupItems();
    const [open, setOpen] = useState(false);

    const handlePurge = () => {
        purge.mutate(undefined, {
            onSuccess: (result) => {
                setOpen(false);
                if (result.deleted_count === 0) {
                    toast.success(t('purgeUnavailable.successNone'));
                    return;
                }
                toast.success(t('purgeUnavailable.success', { count: result.deleted_count }), {
                    description: t('purgeUnavailable.summary', {
                        disabled: result.channel_disabled,
                        missing: result.model_missing,
                        removed: result.channel_missing,
                    }),
                });
            },
            onError: (error: Error) => {
                toast.error(t('purgeUnavailable.failed'), { description: error.message });
            },
        });
    };

    return (
        <>
            <div className="rounded-xl border border-amber-500/25 bg-card p-6 space-y-5 text-card-foreground shadow-md">
                <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                    <Stethoscope className="h-5 w-5 text-amber-600 dark:text-amber-400" />
                    {t('purgeUnavailable.title')}
                </h2>

                <div className="space-y-2 rounded-lg border border-amber-500/15 bg-amber-500/6 p-4 shadow-sm">
                    <p className="text-sm text-muted-foreground">{t('purgeUnavailable.description')}</p>
                </div>

                <Button
                    type="button"
                    variant="outline"
                    className="w-full rounded-xl border-amber-500/30 text-amber-700 hover:bg-amber-500/10 hover:text-amber-800 dark:text-amber-300 dark:hover:text-amber-200"
                    disabled={purge.isPending}
                    onClick={() => setOpen(true)}
                >
                    <Trash2 className="size-4" />
                    {purge.isPending ? t('purgeUnavailable.purging') : t('purgeUnavailable.button')}
                </Button>
            </div>

            <AlertDialog open={open} onOpenChange={setOpen}>
                <AlertDialogContent className="rounded-xl">
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('purgeUnavailable.confirmTitle')}</AlertDialogTitle>
                        <AlertDialogDescription className="whitespace-pre-line">
                            {t('purgeUnavailable.confirmDescription')}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={purge.isPending}>
                            {t('purgeUnavailable.cancel')}
                        </AlertDialogCancel>
                        <AlertDialogAction
                            disabled={purge.isPending}
                            onClick={(event) => {
                                event.preventDefault();
                                handlePurge();
                            }}
                        >
                            {purge.isPending ? t('purgeUnavailable.purging') : t('purgeUnavailable.confirm')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    );
}
