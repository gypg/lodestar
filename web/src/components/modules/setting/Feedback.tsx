'use client';

/*
GGZERO — 意见反馈卡。任意登录用户可提交；管理员（staff）可展开查看收到的反馈。
*/

import { useState } from 'react';
import { MessageSquareText } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { useSubmitFeedback, useFeedbackList } from '@/api/endpoints/feedback';

export function Feedback() {
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
                    toast.success('感谢反馈！');
                    setContent('');
                    setContact('');
                },
                onError: (e) => toast.error(e instanceof Error ? e.message : '提交失败'),
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
                    <span className="text-sm font-semibold text-card-foreground">意见反馈</span>
                    <p className="text-xs text-muted-foreground">使用中有问题或建议？告诉我们。</p>
                </div>
            </div>
            <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={3}
                placeholder="写下你的问题或建议…"
                className="w-full rounded-lg border border-border/40 bg-background p-3 text-sm outline-none focus:border-primary/50"
            />
            <div className="flex items-end gap-2">
                <div className="flex flex-1 flex-col gap-1.5">
                    <label className="ml-1 text-xs text-muted-foreground">联系方式（选填）</label>
                    <Input value={contact} onChange={(e) => setContact(e.target.value)} placeholder="邮箱 / TG / …" className="rounded-lg" />
                </div>
                <Button type="button" size="sm" onClick={onSubmit} disabled={submit.isPending || !content.trim()}>提交</Button>
            </div>

            {staff && (
                <details className="rounded-lg border border-border/30 bg-card p-3">
                    <summary className="cursor-pointer text-sm font-medium text-card-foreground">管理员 · 收到的反馈（{list?.length ?? 0}）</summary>
                    <div className="mt-3 flex flex-col gap-2">
                        {(list ?? []).length === 0 && <p className="text-sm text-muted-foreground">暂无反馈。</p>}
                        {(list ?? []).map((f) => (
                            <div key={f.id} className="rounded-lg border border-border/40 p-2 text-xs">
                                <div className="whitespace-pre-wrap text-card-foreground">{f.content}</div>
                                <div className="mt-1 text-[10px] text-muted-foreground">用户 #{f.user_id}{f.contact ? ` · ${f.contact}` : ''}</div>
                            </div>
                        ))}
                    </div>
                </details>
            )}
        </div>
    );
}
