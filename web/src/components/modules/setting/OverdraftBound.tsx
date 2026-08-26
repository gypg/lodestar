'use client';

/*
Lodestar commercial layer — concurrency overdraft bound.

max_expected_request_cost is the assumed worst-case USD cost of a single request.
The admission gate (internal/op/billing/inflight.go) admits a request only while
`headroom > inflight * thisValue`, so whatever concurrency a caller picks, the
account ends at most about one such request in debt.

Raising it holds the exposure tighter and admits fewer parallel requests on a thin
balance; 0 turns the bound off, and exposure goes back to concurrency x cost.

Only rendered inside the commercial-only block: the gate short-circuits when
commercial_mode is off, so the knob would be inert there.

Keys are written as literal t('...') calls on purpose. The i18n gate only
reconciles statically resolvable keys, so the declarative labelKey pattern in
./runtime-settings.ts would leave these unverified — a typo would then ship as a
raw key path in the UI.
*/

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { ShieldAlert, TriangleAlert } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { parseOverdraftBound } from './runtime-settings';

export function OverdraftBound() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const [value, setValue] = useState('');
    const [loaded, setLoaded] = useState(false);
    const initial = useRef('');

    useEffect(() => {
        if (!settings || loaded) return;
        const stored = settings.find((s) => s.key === SettingKey.MaxExpectedRequestCost)?.value ?? '';
        setValue(stored);
        initial.current = stored;
        setLoaded(true);
    }, [settings, loaded]);

    const parsed = parseOverdraftBound(value);
    const boundOff = parsed === 0;

    const save = () => {
        const next = value.trim();
        if (next === initial.current.trim()) return;
        if (parsed === null) {
            toast.error(t('overdraftBound.invalid'));
            setValue(initial.current);
            return;
        }
        setSetting.mutate(
            { key: SettingKey.MaxExpectedRequestCost, value: next },
            {
                onSuccess: () => {
                    initial.current = next;
                    toast.success(t('saved'));
                },
                onError: (e) => {
                    // Revert on failure: leaving the rejected number in the box reads
                    // as "saved", and this control is the difference between a bounded
                    // and an unbounded overdraft.
                    setValue(initial.current);
                    toast.error(e instanceof Error ? e.message : t('overdraftBound.saveFailed'));
                },
            },
        );
    };

    return (
        <div className="flex flex-col gap-3 rounded-xl border border-border/30 bg-card p-4">
            <div className="flex items-center gap-3">
                <ShieldAlert className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0">
                    <span className="text-sm font-medium text-card-foreground">{t('overdraftBound.title')}</span>
                    <p className="mt-0.5 text-xs text-muted-foreground">{t('overdraftBound.description')}</p>
                </div>
            </div>

            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div className="min-w-0 flex flex-col gap-1">
                    <label className="text-xs font-medium text-muted-foreground" htmlFor="overdraft-bound">
                        {t('overdraftBound.label')}
                    </label>
                    <span className="text-xs text-muted-foreground">{t('overdraftBound.hint')}</span>
                </div>
                <Input
                    id="overdraft-bound"
                    type="number"
                    step="0.01"
                    min="0"
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    onBlur={save}
                    placeholder="0.5"
                    aria-invalid={parsed === null}
                    className="w-full rounded-lg md:w-32"
                />
            </div>

            {boundOff ? (
                <p className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-2.5 text-xs text-destructive">
                    <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
                    {t('overdraftBound.boundOff')}
                </p>
            ) : parsed !== null ? (
                <p className="text-xs text-muted-foreground">{t('overdraftBound.maxDebt', { cost: parsed.toFixed(4) })}</p>
            ) : null}
        </div>
    );
}
