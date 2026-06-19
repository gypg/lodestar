'use client';

/*
Lodestar — 表达式计费设置（billingexpr）。

管理员可为任意模型配置表达式，定义完整定价逻辑。
表达式变量：p=输入token, c=输出token, len=总长度, cr=缓存读, cc=缓存写, img=图片token 等。
示例：p * 2.5 + c * 10 → $2.50/1M 输入, $10.00/1M 输出。
*/

import { useEffect, useState } from 'react';
import { Calculator } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

export function BillingExpr() {
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [raw, setRaw] = useState('{}');
    const [loaded, setLoaded] = useState(false);
    const [entries, setEntries] = useState<{ model: string; expr: string }[]>([]);

    useEffect(() => {
        if (!settings || loaded) return;
        const val = settings.find((s) => s.key === SettingKey.BillingExpr)?.value ?? '{}';
        setRaw(val);
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
                onSuccess: () => toast.success('表达式计费已保存'),
                onError: () => toast.error('保存失败'),
            },
        );
    };

    return (
        <div className="flex flex-col gap-4 rounded-lg border border-primary/20 bg-gradient-to-br from-primary/5 to-transparent p-4 shadow-sm">
            <div className="flex items-center gap-3">
                <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/12">
                    <Calculator className="h-5 w-5 text-primary" />
                </div>
                <div className="space-y-0.5">
                    <span className="text-sm font-semibold text-card-foreground">表达式计费</span>
                    <p className="text-xs text-muted-foreground">
                        为模型配置表达式定价（可选）。留空则使用上游 USD 成本直通。
                    </p>
                </div>
            </div>

            <div className="flex flex-col gap-2 text-xs text-muted-foreground">
                <p>
                    <strong>变量：</strong>p=输入token, c=输出token, cr=缓存读, cc=缓存写, img=图片token
                </p>
                <p>
                    <strong>示例：</strong><code className="rounded bg-muted px-1">p * 2.5 + c * 10</code>（$2.50/1M 输入, $10/1M 输出）
                </p>
                <p>
                    <strong>阶梯：</strong><code className="rounded bg-muted px-1">p &lt;= 128000 ? tier(&quot;std&quot;, p*3+c*15) : tier(&quot;long&quot;, p*6+c*30)</code>
                </p>
            </div>

            {entries.length > 0 && (
                <div className="flex flex-col gap-2">
                    {entries.map((e, i) => (
                        <div key={i} className="flex items-center gap-2">
                            <Input
                                value={e.model}
                                onChange={(ev) => update(i, 'model', ev.target.value)}
                                placeholder="模型名 (如 gpt-4o)"
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
                                className="shrink-0 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                onClick={() => remove(i)}
                            >
                                ✕
                            </button>
                        </div>
                    ))}
                </div>
            )}

            <div className="flex gap-2">
                <Button type="button" variant="outline" size="sm" onClick={add}>
                    + 添加模型
                </Button>
                <Button type="button" size="sm" onClick={save} disabled={setSetting.isPending}>
                    保存表达式
                </Button>
            </div>
        </div>
    );
}