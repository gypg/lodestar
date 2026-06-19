'use client';

/*
Lodestar — 表达式计费设置（billingexpr）可视化编辑器。

管理员可为任意模型配置表达式，定义完整定价逻辑。
支持实时预览：输入示例 token 数即可看到计算结果。
*/

import { useEffect, useState, useMemo } from 'react';
import { Calculator, Eye, EyeOff, Plus, Trash2, ChevronDown, ChevronRight, Info } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { useTranslations } from 'next-intl';

// Simple expression evaluator for preview (subset of billingexpr syntax)
function previewExpr(expr: string, vars: Record<string, number>): string {
    try {
        // Replace variable names with values
        let js = expr;
        for (const [k, v] of Object.entries(vars)) {
            js = js.replace(new RegExp(`\\b${k}\\b`, 'g'), String(v));
        }
        // Replace tier("name", value) with just value
        js = js.replace(/tier\s*\(\s*"[^"]*"\s*,\s*([^)]+)\)/gi, '($1)');
        // Replace param("x") with 0 (unknown at preview time)
        js = js.replace(/param\s*\([^)]*\)/gi, '0');
        // Safety: only allow numbers, operators, parens, spaces
        if (!/^[\d\s+\-*/().%?:<>=!&|,]+$/.test(js)) return '—';
        // eslint-disable-next-line no-eval
        const result = Function(`"use strict"; return (${js})`)();
        if (typeof result === 'number' && isFinite(result)) {
            return result.toFixed(6);
        }
        return '—';
    } catch {
        return '—';
    }
}

// Convert expression output to USD (billingexpr v1 formula)
function toUSD(exprOutput: number, quotaPerUnit = 0.002, groupRatio = 1): string {
    const usd = exprOutput / 1_000_000 * quotaPerUnit * groupRatio;
    if (usd < 0.001) return `$${(usd * 1_000_000).toFixed(2)}/1M tokens`;
    return `$${usd.toFixed(6)}`;
}

const SAMPLE_VARS = { p: 1000, c: 500, len: 1500, cr: 0, cc: 0, cc1h: 0, img: 0, img_o: 0, ai: 0, ao: 0 };

const VAR_REFERENCE = [
    { name: 'p', desc: '输入 token' },
    { name: 'c', desc: '输出 token' },
    { name: 'len', desc: '上下文总长度' },
    { name: 'cr', desc: '缓存读 token' },
    { name: 'cc', desc: '缓存写 token' },
    { name: 'cc1h', desc: '缓存 1h TTL' },
    { name: 'img', desc: '图片输入 token' },
    { name: 'img_o', desc: '图片输出 token' },
    { name: 'ai', desc: '音频输入 token' },
    { name: 'ao', desc: '音频输出 token' },
];

export function BillingExpr() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [loaded, setLoaded] = useState(false);
    const [entries, setEntries] = useState<{ model: string; expr: string }[]>([]);
    const [showRef, setShowRef] = useState(false);
    const [showPreview, setShowPreview] = useState(true);
    const [customVars, setCustomVars] = useState({ ...SAMPLE_VARS });

    useEffect(() => {
        if (!settings || loaded) return;
        const val = settings.find((s) => s.key === SettingKey.BillingExpr)?.value ?? '{}';
        try {
            const m = JSON.parse(val);
            setEntries(Object.entries(m).map(([k, v]) => ({ model: k, expr: v as string })));
        } catch {
            setEntries([]);
        }
        setLoaded(true);
    }, [settings, loaded]);

    const add = () => setEntries((prev) => [...prev, { model: '', expr: '' }]);
    const remove = (i: number) => setEntries((prev) => prev.filter((_, idx) => idx !== i));
    const update = (i: number, field: 'model' | 'expr', value: string) => {
        setEntries((prev) => prev.map((e, idx) => (idx === i ? { ...e, [field]: value } : e)));
    };

    const save = () => {
        const map: Record<string, string> = {};
        for (const e of entries) {
            const k = e.model.trim().toLowerCase();
            if (k && e.expr.trim()) map[k] = e.expr.trim();
        }
        const json = JSON.stringify(map);
        setSetting.mutate(
            { key: SettingKey.BillingExpr, value: json },
            {
                onSuccess: () => toast.success(t('billingExpr.toastSaved') || '表达式计费已保存'),
                onError: () => toast.error(t('billingExpr.toastFailed') || '保存失败'),
            },
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <Calculator className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5 flex-1">
                    <span className="text-sm font-semibold text-card-foreground">{t('billingExpr.title') || '表达式计费'}</span>
                    <p className="text-xs text-muted-foreground">
                        {t('billingExpr.description') || '为模型配置表达式定价（可选）。留空则使用上游 USD 成本直通。'}
                    </p>
                </div>
            </div>

            {/* Variable reference (collapsible) */}
            <button
                type="button"
                onClick={() => setShowRef(!showRef)}
                className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
                {showRef ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                <Info className="size-3.5" />
                <span>{t('billingExpr.variables') || '变量参考'}</span>
            </button>
            {showRef && (
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-lg border border-border/30 bg-muted/20 p-3 text-xs">
                    {VAR_REFERENCE.map((v) => (
                        <div key={v.name} className="flex items-center gap-2">
                            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{v.name}</code>
                            <span className="text-muted-foreground">{v.desc}</span>
                        </div>
                    ))}
                    <div className="col-span-2 mt-1 border-t border-border/20 pt-1 text-muted-foreground">
                        <strong>{t('billingExpr.functions') || '函数'}：</strong>
                        <code className="mx-1 rounded bg-muted px-1 font-mono text-[11px]">tier(name, value)</code>
                        <code className="mx-1 rounded bg-muted px-1 font-mono text-[11px]">max(a, b)</code>
                        <code className="mx-1 rounded bg-muted px-1 font-mono text-[11px]">hour(tz)</code>
                    </div>
                </div>
            )}

            {/* Expression entries */}
            {entries.length > 0 && (
                <div className="flex flex-col gap-2">
                    {entries.map((e, i) => {
                        const preview = e.expr.trim() ? previewExpr(e.expr, customVars) : '—';
                        return (
                            <div key={i} className="rounded-lg border border-border/30 bg-muted/10 p-2.5 space-y-1.5">
                                <div className="flex items-center gap-2">
                                    <Input
                                        value={e.model}
                                        onChange={(ev) => update(i, 'model', ev.target.value)}
                                        placeholder={t('billingExpr.modelPlaceholder') || '模型名 (如 gpt-4o)'}
                                        className="h-8 w-40 rounded-lg font-mono text-xs"
                                    />
                                    <Input
                                        value={e.expr}
                                        onChange={(ev) => update(i, 'expr', ev.target.value)}
                                        placeholder="p * 2.5 + c * 10"
                                        className="h-8 flex-1 rounded-lg font-mono text-xs"
                                    />
                                    <button
                                        type="button"
                                        className="shrink-0 rounded p-1.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors"
                                        onClick={() => remove(i)}
                                    >
                                        <Trash2 className="size-3.5" />
                                    </button>
                                </div>
                                {showPreview && e.expr.trim() && (
                                    <div className="flex items-center gap-3 pl-1 text-[11px] text-muted-foreground">
                                        <span>
                                            {t('billingExpr.preview') || '预览'}：p={customVars.p.toLocaleString()} c={customVars.c.toLocaleString()} →{' '}
                                            <span className="font-mono font-medium text-foreground">{preview}</span>
                                        </span>
                                        <span className="text-primary font-medium">{toUSD(Number(preview))}</span>
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
            )}

            {/* Preview sample controls */}
            {showPreview && entries.length > 0 && (
                <div className="flex items-center gap-3 text-xs">
                    <span className="text-muted-foreground">{t('billingExpr.sampleTokens') || '示例 token 数'}：</span>
                    <Input
                        type="number"
                        value={customVars.p}
                        onChange={(e) => setCustomVars({ ...customVars, p: Number(e.target.value) })}
                        className="h-7 w-24 rounded-lg text-xs"
                        placeholder="p"
                    />
                    <span className="text-muted-foreground">→</span>
                    <Input
                        type="number"
                        value={customVars.c}
                        onChange={(e) => setCustomVars({ ...customVars, c: Number(e.target.value) })}
                        className="h-7 w-24 rounded-lg text-xs"
                        placeholder="c"
                    />
                </div>
            )}

            <div className="flex gap-2">
                <Button type="button" variant="outline" size="sm" onClick={add} className="rounded-lg">
                    <Plus className="mr-1 size-3.5" />
                    {t('billingExpr.addModel') || '添加模型'}
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={() => setShowPreview(!showPreview)} className="rounded-lg">
                    {showPreview ? <EyeOff className="mr-1 size-3.5" /> : <Eye className="mr-1 size-3.5" />}
                    {showPreview ? (t('billingExpr.hidePreview') || '隐藏预览') : (t('billingExpr.showPreview') || '显示预览')}
                </Button>
                <div className="flex-1" />
                <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending} className="rounded-lg">
                    {setSetting.isPending ? (t('billingExpr.saving') || '保存中...') : (t('billingExpr.save') || '保存表达式')}
                </Button>
            </div>
        </div>
    );
}
