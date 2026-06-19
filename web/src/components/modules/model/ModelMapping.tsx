'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Plus, Trash2, ArrowRight, ChevronDown, ChevronRight, ToggleLeft, ToggleRight } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import {
    useModelMappings,
    useCreateModelMapping,
    useUpdateModelMapping,
    useDeleteModelMapping,
    type ModelMapping,
    type CreateModelMappingRequest,
} from '@/api/endpoints/model-mapping';

const MATCH_TYPE_LABELS: Record<string, string> = {
    exact: '精确',
    wildcard: '通配',
    regex: '正则',
};

export function ModelMappingPanel() {
    const t = useTranslations('model');
    const { data: mappings, isLoading } = useModelMappings();
    const createMapping = useCreateModelMapping();
    const updateMapping = useUpdateModelMapping();
    const deleteMapping = useDeleteModelMapping();
    const [expanded, setExpanded] = useState(false);
    const [showCreate, setShowCreate] = useState(false);

    const [form, setForm] = useState<CreateModelMappingRequest>({
        name: '',
        pattern: '',
        match_type: 'exact',
        target_model: '',
        priority: 0,
    });

    const handleCreate = () => {
        if (!form.pattern.trim() || !form.target_model.trim()) {
            toast.error('请输入匹配模式和目标模型');
            return;
        }
        createMapping.mutate(
            { ...form, name: form.name || form.pattern },
            {
                onSuccess: () => {
                    toast.success('映射已创建');
                    setForm({ name: '', pattern: '', match_type: 'exact', target_model: '', priority: 0 });
                    setShowCreate(false);
                },
                onError: () => toast.error('创建失败'),
            },
        );
    };

    const handleToggle = (mapping: ModelMapping) => {
        updateMapping.mutate(
            { id: mapping.id, data: { enabled: !mapping.enabled } },
            { onError: () => toast.error('更新失败') },
        );
    };

    const handleDelete = (id: number) => {
        if (!confirm('确认删除此映射？')) return;
        deleteMapping.mutate(id, {
            onSuccess: () => toast.success('已删除'),
            onError: () => toast.error('删除失败'),
        });
    };

    return (
        <div className="rounded-xl border border-border/35 bg-card">
            <button
                type="button"
                onClick={() => setExpanded(!expanded)}
                className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-semibold text-card-foreground hover:bg-muted/30 transition-colors"
            >
                {expanded ? <ChevronDown className="size-4 shrink-0" /> : <ChevronRight className="size-4 shrink-0" />}
                <span>{t('mapping.title') || '模型映射'}</span>
                <span className="ml-auto text-xs text-muted-foreground">
                    {mappings?.length ?? 0} {t('mapping.count') || '条规则'}
                </span>
            </button>

            {expanded && (
                <div className="border-t border-border/35 p-4 space-y-3">
                    {/* Existing mappings */}
                    {isLoading && <p className="text-sm text-muted-foreground">加载中...</p>}

                    {(mappings ?? []).length === 0 && !isLoading && (
                        <p className="text-sm text-muted-foreground text-center py-2">
                            {t('mapping.empty') || '暂无映射规则'}
                        </p>
                    )}

                    {(mappings ?? []).map((m) => (
                        <div
                            key={m.id}
                            className="flex items-center gap-2 rounded-lg border border-border/30 bg-muted/30 px-3 py-2 text-sm"
                        >
                            <button
                                type="button"
                                onClick={() => handleToggle(m)}
                                className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                                title={m.enabled ? '点击禁用' : '点击启用'}
                            >
                                {m.enabled
                                    ? <ToggleRight className="size-5 text-primary" />
                                    : <ToggleLeft className="size-5" />}
                            </button>
                            <span className="truncate font-mono text-xs">{m.pattern}</span>
                            <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
                            <span className="truncate font-medium text-xs">{m.target_model}</span>
                            <span className="ml-auto shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                                {MATCH_TYPE_LABELS[m.match_type] || m.match_type}
                            </span>
                            <button
                                type="button"
                                onClick={() => handleDelete(m.id)}
                                className="shrink-0 p-1 rounded text-muted-foreground hover:text-red-500 hover:bg-red-500/10 transition-colors"
                            >
                                <Trash2 className="size-3.5" />
                            </button>
                        </div>
                    ))}

                    {/* Create form */}
                    {showCreate ? (
                        <div className="space-y-2 rounded-lg border border-border/30 bg-muted/20 p-3">
                            <div className="grid grid-cols-2 gap-2">
                                <Input
                                    placeholder="匹配模式 (如 gpt-4o)"
                                    value={form.pattern}
                                    onChange={(e) => setForm({ ...form, pattern: e.target.value })}
                                    className="rounded-lg text-xs"
                                />
                                <Input
                                    placeholder="目标模型 (如 gpt-4o-2024-08-06)"
                                    value={form.target_model}
                                    onChange={(e) => setForm({ ...form, target_model: e.target.value })}
                                    className="rounded-lg text-xs"
                                />
                            </div>
                            <div className="flex items-center gap-2">
                                <select
                                    value={form.match_type}
                                    onChange={(e) => setForm({ ...form, match_type: e.target.value as CreateModelMappingRequest['match_type'] })}
                                    className="rounded-lg border border-input bg-background px-2 py-1.5 text-xs"
                                >
                                    <option value="exact">精确匹配</option>
                                    <option value="wildcard">通配符</option>
                                    <option value="regex">正则表达式</option>
                                </select>
                                <Input
                                    type="number"
                                    placeholder="优先级"
                                    value={form.priority}
                                    onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })}
                                    className="w-20 rounded-lg text-xs"
                                />
                                <div className="ml-auto flex gap-1.5">
                                    <Button variant="ghost" size="sm" onClick={() => setShowCreate(false)} className="rounded-lg text-xs">
                                        取消
                                    </Button>
                                    <Button size="sm" onClick={handleCreate} disabled={createMapping.isPending} className="rounded-lg text-xs">
                                        {createMapping.isPending ? '创建中...' : '创建'}
                                    </Button>
                                </div>
                            </div>
                        </div>
                    ) : (
                        <button
                            type="button"
                            onClick={() => setShowCreate(true)}
                            className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed border-border/50 py-2 text-xs text-muted-foreground hover:text-foreground hover:border-border transition-colors"
                        >
                            <Plus className="size-3.5" />
                            {t('mapping.add') || '添加映射'}
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
