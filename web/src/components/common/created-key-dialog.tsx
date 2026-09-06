'use client';

/*
 * WO-040 ⑥：API Key 创建后的一次性完整展示。
 *
 * 服务端只在创建响应里返回完整 key（列表/详情均为掩码），客户错过这一次就再
 * 也拿不到可用的凭据。本弹窗完整展示 + 复制，并明示"仅此一次"。
 */

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Check, Copy, TriangleAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';

export function CreatedKeyDialog({ secret, onClose }: { secret: string | null; onClose: () => void }) {
    const t = useTranslations('apiKey.createdOnce');
    const [copied, setCopied] = useState(false);

    if (!secret) {
        return null;
    }

    const handleCopy = async () => {
        try {
            await navigator.clipboard.writeText(secret);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch {
            // 剪贴板不可用（权限/非安全上下文）时用户仍可手动选中复制。
        }
    };

    return (
        <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{t('title')}</DialogTitle>
                    <DialogDescription>{t('body')}</DialogDescription>
                </DialogHeader>
                <div className="flex items-center gap-2 rounded-lg border border-border/40 bg-muted/40 p-2.5">
                    <code className="min-w-0 flex-1 break-all font-mono text-xs text-foreground">{secret}</code>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="shrink-0 gap-1.5"
                        onClick={() => void handleCopy()}
                    >
                        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                        {copied ? t('copied') : t('copy')}
                    </Button>
                </div>
                <div className="flex items-start gap-2 rounded-lg border border-amber-500/25 bg-amber-500/5 p-2.5 text-xs text-amber-700 dark:text-amber-300">
                    <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    <span>{t('onceWarning')}</span>
                </div>
                <DialogFooter>
                    <Button type="button" onClick={onClose}>{t('done')}</Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
