'use client';

/*
Lodestar — 生图工坊（消费级，思路源自 SAPI ImagePlayground，UI 用本栈重写）。

登录用户用自己的 API Key 调本站 OpenAI 兼容 `/v1/images/generations` 生图，站内预览/下载。
*/

import { useEffect, useMemo, useState } from 'react';
import { ImageIcon, Download, Loader2 } from 'lucide-react';
import { useAPIKeyList } from '@/api/endpoints/apikey';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

const SIZES = ['1024x1024', '1024x1792', '1792x1024', '512x512'];

export function ImageStudio() {
    const { data: keys } = useAPIKeyList();
    const enabledKeys = useMemo(() => (keys ?? []).filter((k) => k.enabled && k.api_key), [keys]);
    const [keyId, setKeyId] = useState<number | null>(null);
    const [model, setModel] = useState('dall-e-3');
    const [size, setSize] = useState('1024x1024');
    const [prompt, setPrompt] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [images, setImages] = useState<string[]>([]);

    useEffect(() => {
        if (keyId === null && enabledKeys.length > 0) setKeyId(enabledKeys[0].id);
    }, [enabledKeys, keyId]);

    const selectedKey = enabledKeys.find((k) => k.id === keyId);

    const generate = async () => {
        const p = prompt.trim();
        if (!p || loading || !selectedKey?.api_key) return;
        setLoading(true);
        setError(null);
        try {
            const resp = await fetch(`${window.location.origin}/v1/images/generations`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${selectedKey.api_key}` },
                body: JSON.stringify({ model, prompt: p, n: 1, size }),
            });
            const text = await resp.text();
            if (!resp.ok) {
                setError(`生成失败（${resp.status}）：${text.slice(0, 300)}`);
                return;
            }
            const j = JSON.parse(text);
            const urls: string[] = (j.data ?? [])
                .map((d: { url?: string; b64_json?: string }) => (d.url ? d.url : d.b64_json ? `data:image/png;base64,${d.b64_json}` : ''))
                .filter(Boolean);
            if (urls.length === 0) {
                setError('未返回图片，请检查模型是否支持生图。');
                return;
            }
            setImages((prev) => [...urls, ...prev]);
        } catch (e) {
            setError(e instanceof Error ? e.message : '请求出错');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="flex h-full min-h-0 flex-col gap-3 overflow-y-auto rounded-xl border border-border bg-card p-3 md:p-4">
            <div className="flex flex-wrap items-center gap-2">
                <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="模型，如 dall-e-3" className="h-9 w-44 rounded-lg" />
                <select value={size} onChange={(e) => setSize(e.target.value)} className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm">
                    {SIZES.map((s) => (<option key={s} value={s}>{s}</option>))}
                </select>
                <select value={keyId ?? ''} onChange={(e) => setKeyId(Number(e.target.value))} className="h-9 rounded-lg border border-border/40 bg-background px-2 text-sm">
                    {enabledKeys.length === 0 && <option value="">无可用密钥（请先创建）</option>}
                    {enabledKeys.map((k) => (<option key={k.id} value={k.id}>{k.name}</option>))}
                </select>
            </div>

            <div className="flex items-end gap-2">
                <textarea
                    value={prompt}
                    onChange={(e) => setPrompt(e.target.value)}
                    rows={2}
                    placeholder="描述你想要的图像…"
                    className="flex-1 resize-none rounded-lg border border-border/40 bg-background p-2.5 text-sm outline-none focus:border-primary/50"
                />
                <Button type="button" onClick={() => void generate()} disabled={loading || !prompt.trim() || !selectedKey} className="h-11">
                    {loading ? <Loader2 className="size-4 animate-spin" /> : <ImageIcon className="size-4" />} 生成
                </Button>
            </div>

            {error && <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-2 text-xs text-destructive">{error}</div>}

            <div className="grid flex-1 grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                {images.length === 0 && !loading && (
                    <div className="col-span-full grid place-items-center py-10 text-sm text-muted-foreground">输入描述并生成 · 使用你自己的密钥与余额</div>
                )}
                {images.map((src, i) => (
                    <div key={i} className="group relative overflow-hidden rounded-lg border border-border/40">
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img src={src} alt={`generated-${i}`} className="aspect-square w-full object-cover" />
                        <a
                            href={src}
                            download={`Lodestar-${i}.png`}
                            className="absolute right-2 top-2 grid size-8 place-items-center rounded-lg bg-background/80 text-foreground opacity-0 transition-opacity group-hover:opacity-100"
                            aria-label="下载"
                        >
                            <Download className="size-4" />
                        </a>
                    </div>
                ))}
            </div>
        </div>
    );
}
