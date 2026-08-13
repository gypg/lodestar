'use client';

/*
Lodestar — 意见反馈卡。任意登录用户可提交；管理员（staff）可展开查看收到的反馈。
*/

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { MessageSquareText } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { useSubmitFeedback, useFeedbackList } from '@/api/endpoints/feedback';

export function Feedback() {
    const t = useTranslations('setting.feedback');
    const { data: me } = useCurrentUser();
    const staff = isStaffRole(me?.role);
    const submit = useSubmitFeedback();
    const { data: list } = useFeedbackList(staff);
    const [content, setContent] = useState('');
    const [contact, setContact] = useState('');

    const onSubmit = () => {
        if (!content.trim()) return;
        submit.mutate(
            { content: content.trim(), contact: contact.trim() },
            {
                onSuccess: () => {
                    toast.success(t('thanks'));
                    setContent('');
                    setContact('');
                },
                onError: (e) => toast.error(e instanceof Error ? e.message : t('submitFailed')),
            }
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <MessageSquareText className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">{t('title')}</span>
                    <p className="text-xs text-muted-foreground">{t('description')}</p>
                </div>
            </div>
            <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={3}
                placeholder={t('placeholder')}
                className="w-full rounded-lg border border-border/40 bg-background p-3 text-sm outline-none focus:border-primary/50"
            />
            <div className="flex items-end gap-2">
                <div className="flex flex-1 flex-col gap-1.5">
                    <label className="ml-1 text-xs text-muted-foreground">{t('contactLabel')}</label>
                    <Input value={contact} onChange={(e) => setContact(e.target.value)} placeholder={t('contactPlaceholder')} className="rounded-lg" />
                </div>
                <Button type="button" size="sm" onClick={onSubmit} disabled={submit.isPending || !content.trim()}>{t('submit')}</Button>
            </div>

            {staff && (
                <details className="rounded-lg border border-border/30 bg-card p-3">
                    <summary className="cursor-pointer text-sm font-medium text-card-foreground">{t('adminReceived', { count: list?.length ?? 0 })}</summary>
                    <div className="mt-3 flex flex-col gap-2">
                        {(list ?? []).length === 0 && <p className="text-sm text-muted-foreground">{t('adminEmpty')}</p>}
                        {(list ?? []).map((f) => (
                            <div key={f.id} className="rounded-lg border border-border/40 p-2 text-xs">
                                <div className="whitespace-pre-wrap text-card-foreground">{f.content}</div>
                                <div className="mt-1 text-[10px] text-muted-foreground">{t('adminUser', { id: f.user_id })}{f.contact ? ` · ${f.contact}` : ''}</div>
                            </div>
                        ))}
                    </div>
                </details>
            )}
        </div>
    );
}
