'use client';

/*
Lodestar — API 使用指引（上手引导）。

让拿到密钥的用户知道：站点 OpenAI 兼容 Base URL + 调用示例（curl / python）。
Base URL 取当前站点 origin + /v1。复制按钮一键拷贝。
*/

import { useEffect, useState } from 'react';
import { BookOpen, Copy, Check } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { BaseUrlLatencyPanel } from './BaseUrlLatencyPanel';

function CopyBox({ label, text }: { label: string; text: string }) {
    const tCommon = useTranslations('common');
    const [copied, setCopied] = useState(false);
    const copy = () => {
        navigator.clipboard?.writeText(text).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        });
    };
    return (
        <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between">
                <label className="ml-1 text-xs font-medium text-muted-foreground">{label}</label>
                <button type="button" onClick={copy} className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground">
                    {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
                    {copied ? tCommon('copy.success') : tCommon('copy.button')}
                </button>
            </div>
            <pre className="overflow-x-auto rounded-lg border border-border/40 bg-background p-3 font-mono text-xs leading-5 text-card-foreground">{text}</pre>
        </div>
    );
}

export function ApiUsageGuide() {
    const t = useTranslations('setting.apiUsageGuide');
    const [origin, setOrigin] = useState('');
    useEffect(() => {
        if (typeof window !== 'undefined') setOrigin(window.location.origin);
    }, []);
    const base = `${origin || 'https://your-site'}/v1`;
    const curl = `curl ${base}/chat/completions \\
  -H "Authorization: Bearer sk-Lodestar-your-api-key" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello"}]}'`;
    const py = `from openai import OpenAI

client = OpenAI(base_url="${base}", api_key="sk-Lodestar-your-api-key")
resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello"}],
)
print(resp.choices[0].message.content)`;

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <BookOpen className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">{t('title')}</span>
                    <p className="text-xs text-muted-foreground">{t('description')}</p>
                </div>
            </div>
            <div className="flex flex-col gap-3">
                <CopyBox label={t('baseUrl')} text={base} />
                <BaseUrlLatencyPanel />
                <CopyBox label={t('curlExample')} text={curl} />
                <CopyBox label={t('pythonExample')} text={py} />
            </div>
        </div>
    );
}
