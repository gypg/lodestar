'use client';

/*
GGZERO — 站内对话（消费级核心，思路源自 SAPI ChatSection，UI 用本栈重写）。

让登录用户在浏览器里直接和模型聊天：用自己的某个 API Key 调本站 OpenAI 兼容
`/v1/chat/completions`（SSE 流式）。无需写代码即可用——把"带计费的网关"变成"人人能用的平台"。
*/

import { useEffect, useMemo, useRef, useState } from 'react';
import { Send, Square, Trash2 } from 'lucide-react';
import { useAPIKeyList } from '@/api/endpoints/apikey';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

interface Msg {
    role: 'user' | 'assistant';
    content: string;
}

export function Chat() {
    const { data: keys } = useAPIKeyList();
    const enabledKeys = useMemo(() => (keys ?? []).filter((k) => k.enabled && k.api_key), [keys]);
    const [keyId, setKeyId] = useState<number | null>(null);
    const [model, setModel] = useState('gpt-4o-mini');
    const [messages, setMessages] = useState<Msg[]>([]);
    const [input, setInput] = useState('');
    const [streaming, setStreaming] = useState(false);
    const abortRef = useRef<AbortController | null>(null);
    const scrollRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        if (keyId === null && enabledKeys.length > 0) setKeyId(enabledKeys[0].id);
    }, [enabledKeys, keyId]);

    useEffect(() => {
        scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
    }, [messages]);

    const selectedKey = enabledKeys.find((k) => k.id === keyId);

    const stop = () => {
        abortRef.current?.abort();
        setStreaming(false);
    };

    const send = async () => {
        const text = input.trim();
        if (!text || streaming) return;
        if (!selectedKey?.api_key) return;
        const next: Msg[] = [...messages, { role: 'user', content: text }, { role: 'assistant', content: '' }];
        setMessages(next);
        setInput('');
        setStreaming(true);
        const controller = new AbortController();
        abortRef.current = controller;
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
                setMessages((m) => {
                    const c = [...m];
                    c[c.length - 1] = { role: 'assistant', content: `请求失败（${resp.status}）：${errText.slice(0, 300) || '请检查密钥/模型/余额'}` };
                    return c;
                });
                setStreaming(false);
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
                    } catch {
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
        }
    };

    return (
        <div className="flex h-full min-h-0 flex-col gap-3 rounded-xl border border-border bg-card p-3 md:p-4">
            {/* 工具条：模型 + 密钥 */}
            <div className="flex flex-wrap items-center gap-2">
                <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="模型，如 gpt-4o-mini" className="h-9 w-48 rounded-lg" />
                <select
                    value={keyId ?? ''}
                    onChange={(e) => setKeyId(Number(e.target.value))}
                    className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm"
                >
                    {enabledKeys.length === 0 && <option value="">无可用密钥（请先创建）</option>}
                    {enabledKeys.map((k) => (
                        <option key={k.id} value={k.id}>{k.name}</option>
                    ))}
                </select>
                <Button type="button" variant="outline" size="sm" onClick={() => setMessages([])} className="ml-auto">
                    <Trash2 className="size-4" /> 清空
                </Button>
            </div>

            {/* 消息区 */}
            <div ref={scrollRef} className="flex-1 min-h-0 space-y-3 overflow-y-auto rounded-lg bg-background/40 p-3">
                {messages.length === 0 && (
                    <div className="grid h-full place-items-center text-sm text-muted-foreground">在下方输入开始对话 · 使用你自己的密钥与余额</div>
                )}
                {messages.map((m, i) => (
                    <div key={i} className={m.role === 'user' ? 'flex justify-end' : 'flex justify-start'}>
                        <div
                            className={
                                'max-w-[85%] whitespace-pre-wrap rounded-2xl px-3.5 py-2 text-sm leading-relaxed ' +
                                (m.role === 'user' ? 'bg-primary text-primary-foreground' : 'border border-border/50 bg-card text-card-foreground')
                            }
                        >
                            {m.content || (streaming && i === messages.length - 1 ? '…' : '')}
                        </div>
                    </div>
                ))}
            </div>

            {/* 输入区 */}
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
                    <Button type="button" variant="outline" onClick={stop} className="h-11"><Square className="size-4" /> 停止</Button>
                ) : (
                    <Button type="button" onClick={() => void send()} disabled={!input.trim() || !selectedKey} className="h-11"><Send className="size-4" /> 发送</Button>
                )}
            </div>
        </div>
    );
}
