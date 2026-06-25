'use client';

/*
Lodestar — 站内对话（消费级核心，思路源自 SAPI ChatSection，UI 用本栈重写）。

让登录用户在浏览器里直接和模型聊天：用自己的某个 API Key 调本站 OpenAI 兼容
`/v1/chat/completions`（SSE 流式）。会话持久化到服务端，侧栏可切换历史对话。
*/

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { MessageSquarePlus, Send, Square, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useAPIKeyList } from '@/api/endpoints/apikey';
import { usePublicOverview } from '@/api/endpoints/public';
import {
    useChatSession,
    useChatSessions,
    useCreateChatSession,
    useDeleteChatSession,
    useSaveChatSession,
    type ChatMessage,
} from '@/api/endpoints/chat';
import { Button } from '@/components/ui/button';
import { ModelSelector } from '@/components/ui/model-selector';
import { cn } from '@/lib/utils';
import { Markdown } from './Markdown';

const SAVE_DEBOUNCE_MS = 800;

export function Chat() {
    const t = useTranslations('chat');
    const { data: keys } = useAPIKeyList();
    const enabledKeys = useMemo(() => (keys ?? []).filter((k) => k.enabled && k.api_key), [keys]);
    const { data: overview } = usePublicOverview();
    const modelNames = useMemo(() => (overview?.models ?? []).map((m) => m.name).filter(Boolean), [overview]);

    const { data: sessions, isLoading: sessionsLoading } = useChatSessions();
    const createSession = useCreateChatSession();
    const saveSession = useSaveChatSession();
    const deleteSession = useDeleteChatSession();

    const [sessionId, setSessionId] = useState<number | null>(null);
    const { data: loadedSession, isFetching: sessionFetching } = useChatSession(sessionId);

    const [keyId, setKeyId] = useState<number | null>(null);
    const [model, setModel] = useState('gpt-4o-mini');
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [input, setInput] = useState('');
    const [streaming, setStreaming] = useState(false);
    const abortRef = useRef<AbortController | null>(null);
    const scrollRef = useRef<HTMLDivElement | null>(null);
    const hydratedIdRef = useRef<number | null>(null);
    const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const messagesRef = useRef(messages);
    messagesRef.current = messages;

    useEffect(() => {
        if (keyId === null && enabledKeys.length > 0) setKeyId(enabledKeys[0].id);
    }, [enabledKeys, keyId]);

    useEffect(() => {
        if (sessionId == null || !loadedSession || sessionFetching) return;
        if (hydratedIdRef.current === sessionId) return;
        hydratedIdRef.current = sessionId;
        setModel(loadedSession.model || 'gpt-4o-mini');
        if (loadedSession.api_key_id > 0) setKeyId(loadedSession.api_key_id);
        setMessages(loadedSession.messages ?? []);
    }, [sessionId, loadedSession, sessionFetching]);

    useEffect(() => {
        scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
    }, [messages]);

    const selectedKey = enabledKeys.find((k) => k.id === keyId);

    const persist = useCallback(
        (id: number, msgs: ChatMessage[], modelName: string, apiKeyId: number) => {
            if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
            saveTimerRef.current = setTimeout(() => {
                saveSession.mutate({ id, model: modelName, api_key_id: apiKeyId, messages: msgs });
            }, SAVE_DEBOUNCE_MS);
        },
        [saveSession],
    );

    useEffect(() => {
        if (sessionId == null || streaming) return;
        if (hydratedIdRef.current !== sessionId) return;
        const kid = keyId ?? 0;
        if (kid <= 0) return;
        persist(sessionId, messages, model, kid);
        return () => {
            if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
        };
    }, [sessionId, messages, model, keyId, streaming, persist]);

    const stop = () => {
        abortRef.current?.abort();
        setStreaming(false);
    };

    const onNewChat = () => {
        const kid = keyId ?? enabledKeys[0]?.id;
        if (!kid) return;
        createSession.mutate(
            { model, api_key_id: kid },
            {
                onSuccess: (d) => {
                    hydratedIdRef.current = d.id;
                    setSessionId(d.id);
                    setMessages([]);
                },
            },
        );
    };

    const onSelectSession = (id: number) => {
        if (id === sessionId) return;
        hydratedIdRef.current = null;
        setSessionId(id);
        setMessages([]);
    };

    const onDeleteSession = (id: number) => {
        deleteSession.mutate(id, {
            onSuccess: () => {
                if (sessionId === id) {
                    hydratedIdRef.current = null;
                    setSessionId(null);
                    setMessages([]);
                }
            },
        });
    };

    const send = async () => {
        const text = input.trim();
        if (!text || streaming) return;
        if (!selectedKey?.api_key) return;

        let activeId = sessionId;
        if (activeId == null) {
            const kid = keyId ?? enabledKeys[0]?.id;
            if (!kid) return;
            try {
                const d = await createSession.mutateAsync({ model, api_key_id: kid, title: text.slice(0, 32) });
                activeId = d.id;
                hydratedIdRef.current = d.id;
                setSessionId(d.id);
            } catch (e) { console.error(e);
                return;
            }
        }

        const next: ChatMessage[] = [...messagesRef.current, { role: 'user', content: text }, { role: 'assistant', content: '' }];
        setMessages(next);
        setInput('');
        setStreaming(true);
        const controller = new AbortController();
        abortRef.current = controller;
        const kid = keyId ?? selectedKey.id;
        try {
            const resp = await fetch(`${window.location.origin}/v1/chat/completions`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${selectedKey.api_key}` },
                body: JSON.stringify({
                    model,
                    stream: true,
                    messages: next.slice(0, -1).map((m) => ({ role: m.role, content: m.content })),
                }),
                signal: controller.signal,
            });
            if (!resp.ok || !resp.body) {
                const errText = await resp.text().catch(() => '');
                const failed: ChatMessage[] = [
                    ...next.slice(0, -1),
                    {
                        role: 'assistant',
                        content: `请求失败（${resp.status}）：${errText.slice(0, 300) || '请检查密钥/模型/余额'}`,
                    },
                ];
                setMessages(failed);
                setStreaming(false);
                if (activeId) saveSession.mutate({ id: activeId, model, api_key_id: kid, messages: failed });
                return;
            }
            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            let acc = '';
            for (;;) {
                const { done, value } = await reader.read();
                if (done) break;
                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';
                for (const line of lines) {
                    const t = line.trim();
                    if (!t.startsWith('data:')) continue;
                    const data = t.slice(5).trim();
                    if (data === '[DONE]') continue;
                    try {
                        const j = JSON.parse(data);
                        const delta = j.choices?.[0]?.delta?.content;
                        if (delta) {
                            acc += delta;
                            setMessages((m) => {
                                const c = [...m];
                                c[c.length - 1] = { role: 'assistant', content: acc };
                                return c;
                            });
                        }
                    } catch (e) { console.error(e);
                        /* ignore partial */
                    }
                }
            }
        } catch (e) {
            if (!(e instanceof DOMException && e.name === 'AbortError')) {
                setMessages((m) => {
                    const c = [...m];
                    c[c.length - 1] = { role: 'assistant', content: '连接中断或出错。' };
                    return c;
                });
            }
        } finally {
            setStreaming(false);
            abortRef.current = null;
            if (activeId) {
                if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
                saveSession.mutate({
                    id: activeId,
                    model,
                    api_key_id: kid,
                    messages: messagesRef.current,
                });
            }
        }
    };

    return (
        <div className="flex h-full min-h-0 gap-3">
            <aside className="hidden w-52 shrink-0 flex-col gap-2 rounded-xl border border-border bg-card p-2 md:flex">
                <Button type="button" variant="outline" size="sm" className="w-full justify-start gap-2" onClick={onNewChat} disabled={createSession.isPending || enabledKeys.length === 0}>
                    <MessageSquarePlus className="size-4" /> 新对话
                </Button>
                <div className="min-h-0 flex-1 space-y-1 overflow-y-auto">
                    {sessionsLoading && <p className="px-2 text-xs text-muted-foreground">加载…</p>}
                    {(sessions ?? []).map((s) => (
                        <div key={s.id} className="group flex items-center gap-1">
                            <button
                                type="button"
                                onClick={() => onSelectSession(s.id)}
                                className={cn(
                                    'min-w-0 flex-1 truncate rounded-lg px-2 py-1.5 text-left text-xs transition-colors',
                                    sessionId === s.id ? 'bg-primary/12 text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                                )}
                                title={s.title}
                            >
                                {s.title || t('newSession')}
                            </button>
                            <button
                                type="button"
                                className="rounded p-1 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-destructive/10"
                                aria-label={t('deleteSession')}
                                onClick={() => onDeleteSession(s.id)}
                            >
                                <Trash2 className="size-3.5 text-muted-foreground" />
                            </button>
                        </div>
                    ))}
                </div>
            </aside>

            <div className="flex h-full min-h-0 flex-1 flex-col gap-3 rounded-xl border border-border bg-card p-3 md:p-4">
                <div className="flex flex-wrap items-center gap-2 md:hidden">
                    <Button type="button" variant="outline" size="sm" onClick={onNewChat} disabled={createSession.isPending || enabledKeys.length === 0}>
                        <MessageSquarePlus className="size-4" /> {t('newSession')}
                    </Button>
                    <select
                        value={sessionId ?? ''}
                        onChange={(e) => {
                            const v = e.target.value;
                            if (v) onSelectSession(Number(v));
                        }}
                        className="h-9 min-w-0 flex-1 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        <option value="">{t('currentSession')}</option>
                        {(sessions ?? []).map((s) => (
                            <option key={s.id} value={s.id}>
                                {s.title}
                            </option>
                        ))}
                    </select>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                    <ModelSelector
                        models={modelNames}
                        value={model}
                        onChange={setModel}
                        placeholder="选择模型"
                        className="h-9 w-48 rounded-lg"
                    />
                    <select
                        value={keyId ?? ''}
                        onChange={(e) => setKeyId(Number(e.target.value))}
                        className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                    >
                        {enabledKeys.length === 0 && <option value="">无可用密钥（请先创建）</option>}
                        {enabledKeys.map((k) => (
                            <option key={k.id} value={k.id}>
                                {k.name}
                            </option>
                        ))}
                    </select>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                            setMessages([]);
                            hydratedIdRef.current = null;
                            setSessionId(null);
                        }}
                        className="ml-auto"
                    >
                        <Trash2 className="size-4" /> 清空
                    </Button>
                </div>

                <div ref={scrollRef} className="flex-1 min-h-0 space-y-3 overflow-y-auto rounded-lg bg-background/40 p-3">
                    {messages.length === 0 && (
                        <div className="grid h-full place-items-center text-sm text-muted-foreground">
                            在下方输入开始对话 · 使用你自己的密钥与余额 · 会话自动保存
                        </div>
                    )}
                    {messages.map((m, i) => (
                        <div key={i} className={m.role === 'user' ? 'flex justify-end' : 'flex justify-start'}>
                            {m.role === 'user' ? (
                                <div className="max-w-[85%] whitespace-pre-wrap rounded-2xl bg-primary px-3.5 py-2 text-sm leading-relaxed text-primary-foreground">
                                    {m.content}
                                </div>
                            ) : (
                                <div className="max-w-[85%] rounded-2xl border border-border/50 bg-card px-3.5 py-2 text-card-foreground">
                                    {m.content ? (
                                        <Markdown content={m.content} />
                                    ) : (
                                        streaming && i === messages.length - 1 && <span className="text-sm text-muted-foreground">…</span>
                                    )}
                                </div>
                            )}
                        </div>
                    ))}
                </div>

                <div className="flex items-end gap-2">
                    <textarea
                        value={input}
                        onChange={(e) => setInput(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter' && !e.shiftKey) {
                                e.preventDefault();
                                void send();
                            }
                        }}
                        rows={2}
                        placeholder="输入消息，Enter 发送，Shift+Enter 换行"
                        className="flex-1 resize-none rounded-lg border border-border/40 bg-background p-2.5 text-sm outline-none focus:border-primary/50"
                    />
                    {streaming ? (
                        <Button type="button" variant="outline" onClick={stop} className="h-11">
                            <Square className="size-4" /> 停止
                        </Button>
                    ) : (
                        <Button type="button" onClick={() => void send()} disabled={!input.trim() || !selectedKey} className="h-11">
                            <Send className="size-4" /> 发送
                        </Button>
                    )}
                </div>
            </div>
        </div>
    );
}