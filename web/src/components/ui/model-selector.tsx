'use client';

/**
 * Grouped model selector dropdown.
 *
 * Groups model names by provider (via getModelIcon) and renders
 * a Radix Select with per-provider sections.  When the model list
 * exceeds 50 items a filter input is shown at the top of the popover.
 */

import { useMemo, useState } from 'react';
import { Search } from 'lucide-react';
import {
    Select,
    SelectContent,
    SelectGroup,
    SelectItem,
    SelectLabel,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

export interface ModelSelectorProps {
    /** Full list of model names available from the API. */
    models: string[];
    /** Currently selected model name (empty string = none). */
    value: string;
    /** Callback when the user picks a model. */
    onChange: (value: string) => void;
    /** Placeholder shown when no model is selected. */
    placeholder?: string;
    /** Extra class names applied to the trigger button. */
    className?: string;
}

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

interface ProviderGroup {
    label: string;
    models: string[];
}

/** Bucket model names into provider groups using getModelIcon. */
function groupModels(models: string[]): ProviderGroup[] {
    const buckets = new Map<string, string[]>();

    for (const name of models) {
        const { label } = getModelIcon(name);
        const key = label || 'Other';
        const arr = buckets.get(key);
        if (arr) {
            arr.push(name);
        } else {
            buckets.set(key, [name]);
        }
    }

    const groups: ProviderGroup[] = [];
    for (const [label, groupModels] of buckets) {
        groups.push({ label, models: groupModels });
    }

    // Sort alphabetically, but push "Other" to the end.
    groups.sort((a, b) => {
        if (a.label === 'Other') return 1;
        if (b.label === 'Other') return -1;
        return a.label.localeCompare(b.label);
    });

    return groups;
}

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

const FILTER_THRESHOLD = 50;

export function ModelSelector({
    models,
    value,
    onChange,
    placeholder = '选择模型',
    className,
}: ModelSelectorProps) {
    const [filter, setFilter] = useState('');
    const showFilter = models.length > FILTER_THRESHOLD;

    const groups = useMemo(() => groupModels(models), [models]);

    const filteredGroups = useMemo(() => {
        if (!filter) return groups;
        const lower = filter.toLowerCase();
        return groups
            .map((g) => ({
                ...g,
                models: g.models.filter((m) => m.toLowerCase().includes(lower)),
            }))
            .filter((g) => g.models.length > 0);
    }, [groups, filter]);

    return (
        <Select value={value} onValueChange={onChange}>
            <SelectTrigger className={cn('h-9 w-48 rounded-lg', className)}>
                <SelectValue placeholder={placeholder} />
            </SelectTrigger>
            <SelectContent>
                {showFilter && (
                    <div className="sticky top-0 z-10 bg-popover px-2 pb-1 pt-1">
                        <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2 py-1.5">
                            <Search className="size-3.5 shrink-0 text-muted-foreground" />
                            <input
                                value={filter}
                                onChange={(e) => setFilter(e.target.value)}
                                placeholder="搜索模型…"
                                className="w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
                                // Prevent Radix Select from stealing focus
                                onKeyDown={(e) => e.stopPropagation()}
                                onMouseDown={(e) => e.stopPropagation()}
                            />
                        </div>
                    </div>
                )}
                {filteredGroups.map((group) => (
                    <SelectGroup key={group.label}>
                        <SelectLabel>{group.label}</SelectLabel>
                        {group.models.map((name) => (
                            <SelectItem key={name} value={name}>
                                {name}
                            </SelectItem>
                        ))}
                    </SelectGroup>
                ))}
                {filteredGroups.length === 0 && (
                    <p className="px-3 py-2 text-center text-xs text-muted-foreground">
                        无匹配模型
                    </p>
                )}
            </SelectContent>
        </Select>
    );
}
