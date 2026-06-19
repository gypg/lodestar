'use client';

import { useState } from 'react';
import {
    CreditCard,
    Loader,
    Plus,
    Pencil,
    Trash2,
    Save,
    X,
    Clock,
    CheckCircle,
    XCircle,
    Package,
    UserPlus,
} from 'lucide-react';
import {
    useSubscriptionPlans,
    useMySubscription,
    usePurchaseSubscription,
    useAdminPlans,
    useAdminSubscriptions,
    useCreatePlan,
    useUpdatePlan,
    useDeletePlan,
    useBindSubscription,
    type SubscriptionPlan,
    type UserSubscription,
} from '@/api/endpoints/subscription';
import { useCurrentUser, isStaffRole } from '@/api/endpoints/user';
import { PageWrapper } from '@/components/common/PageWrapper';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';

// ── Plan card (user-facing) ───────────────────────────────────────────────────

function PlanCard({
    plan,
    onPurchase,
    purchasing,
}: {
    plan: SubscriptionPlan;
    onPurchase: () => void;
    purchasing: boolean;
}) {
    const t = useTranslations('subscription');

    return (
        <Card className="flex flex-col">
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <Package className="h-5 w-5 text-primary" />
                    {plan.name}
                </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-1 flex-col gap-4">
                {plan.description && (
                    <p className="text-sm text-muted-foreground">{plan.description}</p>
                )}
                <div className="grid grid-cols-2 gap-3 text-sm">
                    <div>
                        <span className="text-muted-foreground">{t('price')}</span>
                        <div className="text-lg font-semibold text-primary">${plan.price}</div>
                    </div>
                    <div>
                        <span className="text-muted-foreground">{t('duration')}</span>
                        <div className="text-lg font-semibold">{t('days', { count: plan.duration_days })}</div>
                    </div>
                    <div className="col-span-2">
                        <span className="text-muted-foreground">{t('quota')}</span>
                        <div className="text-lg font-semibold">{formatQuota(plan.quota)}</div>
                    </div>
                </div>
                <div className="mt-auto pt-2">
                    <Button
                        className="w-full rounded-xl"
                        onClick={onPurchase}
                        disabled={purchasing || !plan.enabled}
                    >
                        {purchasing ? (
                            <Loader className="h-4 w-4 animate-spin" />
                        ) : (
                            <CreditCard className="h-4 w-4" />
                        )}
                        {plan.enabled ? t('purchase') : t('unavailable')}
                    </Button>
                </div>
            </CardContent>
        </Card>
    );
}

// ── My subscription card ──────────────────────────────────────────────────────

function MySubscriptionCard({ sub }: { sub: UserSubscription }) {
    const t = useTranslations('subscription');
    const statusConfig = {
        0: { label: t('status.inactive'), color: 'secondary' as const },
        1: { label: t('status.active'), color: 'default' as const },
        2: { label: t('status.expired'), color: 'destructive' as const },
    };
    const status = statusConfig[sub.status as keyof typeof statusConfig] ?? statusConfig[0];

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <CreditCard className="h-5 w-5 text-primary" />
                    {t('mySubscription')}
                </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
                <div className="flex items-center gap-3">
                    <span className="text-sm text-muted-foreground">{t('plan')}</span>
                    <span className="text-sm font-medium">{sub.plan_name}</span>
                    <Badge variant={status.color}>{status.label}</Badge>
                </div>
                <div className="flex items-center gap-3">
                    <span className="text-sm text-muted-foreground">{t('expiresAt')}</span>
                    <span className="text-sm font-medium flex items-center gap-1.5">
                        <Clock className="h-3.5 w-3.5" />
                        {new Date(sub.end_time * 1000).toLocaleString()}
                    </span>
                </div>
                <div className="flex items-center gap-3">
                    <span className="text-sm text-muted-foreground">{t('quotaUsed')}</span>
                    <span className="text-sm font-medium">
                        {formatQuota(sub.used_quota)} / {formatQuota(sub.quota)}
                    </span>
                </div>
                <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                    <div
                        className="h-full rounded-full bg-primary transition-all"
                        style={{ width: `${Math.min((sub.used_quota / sub.quota) * 100, 100)}%` }}
                    />
                </div>
            </CardContent>
        </Card>
    );
}

// ── Admin: plan editor form ───────────────────────────────────────────────────

function PlanForm({
    initial,
    onSave,
    onCancel,
    saving,
}: {
    initial?: Partial<SubscriptionPlan>;
    onSave: (data: Omit<SubscriptionPlan, 'id' | 'created_at' | 'updated_at'>) => void;
    onCancel: () => void;
    saving: boolean;
}) {
    const t = useTranslations('subscription');
    const [name, setName] = useState(initial?.name ?? '');
    const [price, setPrice] = useState(initial?.price?.toString() ?? '');
    const [duration, setDuration] = useState(initial?.duration_days?.toString() ?? '');
    const [quota, setQuota] = useState(initial?.quota?.toString() ?? '');
    const [description, setDescription] = useState(initial?.description ?? '');
    const [enabled, setEnabled] = useState(initial?.enabled ?? true);

    const handleSubmit = () => {
        if (!name.trim() || !price || !duration || !quota) return;
        onSave({
            name: name.trim(),
            price: Number(price),
            duration_days: Number(duration),
            quota: Number(quota),
            description: description.trim() || undefined,
            enabled,
            sort: initial?.sort ?? 0,
        });
    };

    return (
        <div className="p-4 rounded-xl bg-muted/30 border border-border space-y-3">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.form.name')}</span>
                    <Input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder={t('admin.form.namePlaceholder')}
                        className="rounded-xl"
                    />
                </label>
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.form.price')}</span>
                    <Input
                        type="number"
                        min="0"
                        step="0.01"
                        value={price}
                        onChange={(e) => setPrice(e.target.value)}
                        placeholder={t('admin.form.pricePlaceholder')}
                        className="rounded-xl"
                    />
                </label>
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.form.duration')}</span>
                    <Input
                        type="number"
                        min="1"
                        value={duration}
                        onChange={(e) => setDuration(e.target.value)}
                        placeholder={t('admin.form.durationPlaceholder')}
                        className="rounded-xl"
                    />
                </label>
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.form.quota')}</span>
                    <Input
                        type="number"
                        min="1"
                        value={quota}
                        onChange={(e) => setQuota(e.target.value)}
                        placeholder={t('admin.form.quotaPlaceholder')}
                        className="rounded-xl"
                    />
                </label>
            </div>
            <label className="grid gap-1">
                <span className="text-xs font-medium text-muted-foreground">{t('admin.form.description')}</span>
                <Input
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder={t('admin.form.descriptionPlaceholder')}
                    className="rounded-xl"
                />
            </label>
            <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer">
                <input
                    type="checkbox"
                    checked={enabled}
                    onChange={(e) => setEnabled(e.target.checked)}
                    className="rounded"
                />
                {t('admin.form.enabled')}
            </label>
            <div className="flex gap-2">
                <Button onClick={handleSubmit} disabled={saving} className="flex-1 rounded-xl">
                    {saving ? <Loader className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                    {t('admin.actions.save')}
                </Button>
                <Button variant="outline" onClick={onCancel} className="flex-1 rounded-xl">
                    <X className="h-4 w-4" />
                    {t('admin.actions.cancel')}
                </Button>
            </div>
        </div>
    );
}

// ── Admin: bind subscription form ─────────────────────────────────────────────

function BindForm({
    plans,
    onBind,
    onCancel,
    binding,
}: {
    plans: SubscriptionPlan[];
    onBind: (userId: number, planId: number) => void;
    onCancel: () => void;
    binding: boolean;
}) {
    const t = useTranslations('subscription');
    const [userId, setUserId] = useState('');
    const [planId, setPlanId] = useState('');

    const handleSubmit = () => {
        if (!userId || !planId) return;
        onBind(Number(userId), Number(planId));
    };

    return (
        <div className="p-4 rounded-xl bg-muted/30 border border-border space-y-3">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.bind.userId')}</span>
                    <Input
                        type="number"
                        min="1"
                        value={userId}
                        onChange={(e) => setUserId(e.target.value)}
                        placeholder={t('admin.bind.userIdPlaceholder')}
                        className="rounded-xl"
                    />
                </label>
                <label className="grid gap-1">
                    <span className="text-xs font-medium text-muted-foreground">{t('admin.bind.planId')}</span>
                    <select
                        value={planId}
                        onChange={(e) => setPlanId(e.target.value)}
                        className="h-10 rounded-xl bg-background border border-border text-sm px-3"
                    >
                        <option value="">{t('admin.bind.selectPlan')}</option>
                        {plans.map((p) => (
                            <option key={p.id} value={p.id}>{p.name} (${p.price})</option>
                        ))}
                    </select>
                </label>
            </div>
            <div className="flex gap-2">
                <Button onClick={handleSubmit} disabled={binding} className="flex-1 rounded-xl">
                    {binding ? <Loader className="h-4 w-4 animate-spin" /> : <UserPlus className="h-4 w-4" />}
                    {t('admin.bind.submit')}
                </Button>
                <Button variant="outline" onClick={onCancel} className="flex-1 rounded-xl">
                    <X className="h-4 w-4" />
                    {t('admin.actions.cancel')}
                </Button>
            </div>
        </div>
    );
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatQuota(value: number): string {
    if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`;
    if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
    if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
    return value.toString();
}

// ── Tab button (matches alert module pattern) ────────────────────────────────

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            onClick={onClick}
            className={`shrink-0 whitespace-nowrap px-4 py-2 rounded-xl text-sm font-medium transition-all active:scale-95 ${
                active ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:bg-muted/80'
            }`}
        >
            {children}
        </button>
    );
}

// ── Main component ────────────────────────────────────────────────────────────

export function Subscription() {
    const t = useTranslations('subscription');
    const { data: currentUser } = useCurrentUser();
    const isAdmin = currentUser !== undefined && isStaffRole(currentUser.role);

    const [tab, setTab] = useState<'plans' | 'my' | 'admin'>('plans');
    const [showNewPlan, setShowNewPlan] = useState(false);
    const [showBindForm, setShowBindForm] = useState(false);
    const [editingPlanId, setEditingPlanId] = useState<number | null>(null);

    // User hooks
    const { data: plans, isLoading: plansLoading } = useSubscriptionPlans();
    const { data: mySub, isLoading: subLoading } = useMySubscription();
    const purchase = usePurchaseSubscription();

    // Admin hooks
    const { data: adminPlans, isLoading: adminPlansLoading } = useAdminPlans();
    const { data: adminSubs, isLoading: adminSubsLoading } = useAdminSubscriptions();
    const createPlan = useCreatePlan();
    const updatePlan = useUpdatePlan();
    const deletePlan = useDeletePlan();
    const bindSub = useBindSubscription();

    const handlePurchase = (planId: number) => {
        if (!confirm(t('confirmPurchase'))) return;
        purchase.mutate(planId, {
            onSuccess: () => toast.success(t('toast.purchaseSuccess')),
            onError: (e) => toast.error(t('toast.actionFailed'), { description: e.message }),
        });
    };

    const handleCreatePlan = (data: Omit<SubscriptionPlan, 'id' | 'created_at' | 'updated_at'>) => {
        createPlan.mutate(data, {
            onSuccess: () => {
                toast.success(t('toast.planCreated'));
                setShowNewPlan(false);
            },
            onError: (e) => toast.error(t('toast.actionFailed'), { description: e.message }),
        });
    };

    const handleUpdatePlan = (id: number, data: Omit<SubscriptionPlan, 'id' | 'created_at' | 'updated_at'>) => {
        updatePlan.mutate({ id, ...data }, {
            onSuccess: () => {
                toast.success(t('toast.planUpdated'));
                setEditingPlanId(null);
            },
            onError: (e) => toast.error(t('toast.actionFailed'), { description: e.message }),
        });
    };

    const handleDeletePlan = (planId: number) => {
        if (!confirm(t('admin.confirmDelete'))) return;
        deletePlan.mutate(planId, {
            onSuccess: () => toast.success(t('toast.planDeleted')),
            onError: (e) => toast.error(t('toast.actionFailed'), { description: e.message }),
        });
    };

    const handleBind = (userId: number, planId: number) => {
        bindSub.mutate({ user_id: userId, plan_id: planId }, {
            onSuccess: () => {
                toast.success(t('toast.bindSuccess'));
                setShowBindForm(false);
            },
            onError: (e) => toast.error(t('toast.actionFailed'), { description: e.message }),
        });
    };

    const isLoading = plansLoading || subLoading;

    if (isLoading) {
        return <Loader className="size-6 animate-spin mx-auto mt-12" />;
    }

    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl space-y-4 pb-3 md:pb-6">
            <div className="flex items-center gap-2 mb-2 overflow-x-auto scrollbar-none -mx-1 px-1">
                <TabButton active={tab === 'plans'} onClick={() => setTab('plans')}>
                    {t('tabs.plans')}
                </TabButton>
                <TabButton active={tab === 'my'} onClick={() => setTab('my')}>
                    {t('tabs.mySubscription')}
                </TabButton>
                {isAdmin && (
                    <TabButton active={tab === 'admin'} onClick={() => setTab('admin')}>
                        {t('tabs.admin')}
                    </TabButton>
                )}
            </div>

            {/* Plans tab */}
            {tab === 'plans' && (
                <div className="space-y-4">
                    {(!plans || plans.length === 0) ? (
                        <div className="rounded-xl border border-dashed border-border/35 bg-card px-6 py-10 text-center text-sm text-muted-foreground">
                            {t('noPlan')}
                        </div>
                    ) : (
                        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                            {plans.filter((p) => p.enabled).map((plan) => (
                                <PlanCard
                                    key={plan.id}
                                    plan={plan}
                                    onPurchase={() => handlePurchase(plan.id)}
                                    purchasing={purchase.isPending}
                                />
                            ))}
                        </div>
                    )}
                </div>
            )}

            {/* My subscription tab */}
            {tab === 'my' && (
                <div className="space-y-4">
                    {mySub ? (
                        <MySubscriptionCard sub={mySub} />
                    ) : (
                        <div className="rounded-xl border border-dashed border-border/35 bg-card px-6 py-10 text-center text-sm text-muted-foreground">
                            {t('noSubscription')}
                        </div>
                    )}
                </div>
            )}

            {/* Admin tab */}
            {tab === 'admin' && isAdmin && (
                <div className="space-y-6">
                    {/* Plan management */}
                    <div className="rounded-xl border border-border bg-card p-4 sm:p-6 space-y-4">
                        <div className="flex items-center justify-between">
                            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                                <Package className="h-5 w-5" />
                                {t('admin.planManagement')}
                            </h2>
                            <button
                                onClick={() => setShowNewPlan((prev) => !prev)}
                                className="flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5 rounded-xl text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 transition-all active:scale-95 shrink-0"
                            >
                                <Plus className="h-4 w-4" />
                                {t('admin.newPlan')}
                            </button>
                        </div>

                        {showNewPlan && (
                            <PlanForm
                                onSave={handleCreatePlan}
                                onCancel={() => setShowNewPlan(false)}
                                saving={createPlan.isPending}
                            />
                        )}

                        {adminPlansLoading ? (
                            <Loader className="size-6 animate-spin mx-auto" />
                        ) : (
                            <div className="space-y-2">
                                {(!adminPlans || adminPlans.length === 0) ? (
                                    <div className="rounded-xl border border-dashed border-border/35 bg-card px-6 py-8 text-center text-sm text-muted-foreground">
                                        {t('admin.noPlans')}
                                    </div>
                                ) : adminPlans.map((plan) => (
                                    <div key={plan.id}>
                                        {editingPlanId === plan.id ? (
                                            <PlanForm
                                                initial={plan}
                                                onSave={(data) => handleUpdatePlan(plan.id, data)}
                                                onCancel={() => setEditingPlanId(null)}
                                                saving={updatePlan.isPending}
                                            />
                                        ) : (
                                            <div className="flex items-center justify-between gap-3 p-3 rounded-xl bg-muted/50 hover:bg-muted transition-colors">
                                                <div className="min-w-0 flex-1">
                                                    <div className="flex items-center gap-2">
                                                        <span className="font-medium text-sm truncate">{plan.name}</span>
                                                        <Badge variant={plan.enabled ? 'default' : 'secondary'}>
                                                            {plan.enabled ? t('admin.enabled') : t('admin.disabled')}
                                                        </Badge>
                                                    </div>
                                                    <div className="text-xs text-muted-foreground truncate">
                                                        ${plan.price} &middot; {t('days', { count: plan.duration_days })} &middot; {formatQuota(plan.quota)} {t('quotaUnit')}
                                                    </div>
                                                </div>
                                                <div className="flex items-center gap-2 shrink-0">
                                                    <button
                                                        onClick={() => setEditingPlanId(plan.id)}
                                                        className="inline-flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5 rounded-xl text-xs sm:text-sm font-medium bg-background text-foreground hover:bg-card transition-all active:scale-95"
                                                    >
                                                        <Pencil className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                                                        {t('admin.actions.edit')}
                                                    </button>
                                                    <button
                                                        onClick={() => handleDeletePlan(plan.id)}
                                                        className="p-1.5 rounded-xl text-muted-foreground hover:text-red-500 hover:bg-red-500/10 transition-all active:scale-95"
                                                    >
                                                        <Trash2 className="h-4 w-4" />
                                                    </button>
                                                </div>
                                            </div>
                                        )}
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Bind subscription */}
                    <div className="rounded-xl border border-border bg-card p-4 sm:p-6 space-y-4">
                        <div className="flex items-center justify-between">
                            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                                <UserPlus className="h-5 w-5" />
                                {t('admin.bind.title')}
                            </h2>
                            <button
                                onClick={() => setShowBindForm((prev) => !prev)}
                                className="flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5 rounded-xl text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 transition-all active:scale-95 shrink-0"
                            >
                                <Plus className="h-4 w-4" />
                                {t('admin.bind.new')}
                            </button>
                        </div>

                        {showBindForm && (
                            <BindForm
                                plans={adminPlans || []}
                                onBind={handleBind}
                                onCancel={() => setShowBindForm(false)}
                                binding={bindSub.isPending}
                            />
                        )}

                        {adminSubsLoading ? (
                            <Loader className="size-6 animate-spin mx-auto" />
                        ) : (
                            <div className="space-y-2">
                                {(!adminSubs || adminSubs.length === 0) ? (
                                    <div className="rounded-xl border border-dashed border-border/35 bg-card px-6 py-8 text-center text-sm text-muted-foreground">
                                        {t('admin.bind.noSubscriptions')}
                                    </div>
                                ) : adminSubs.map((sub) => (
                                    <div key={sub.id} className="flex items-center justify-between gap-3 p-3 rounded-xl bg-muted/50">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex items-center gap-2">
                                                <span className="font-medium text-sm">#{sub.user_id}</span>
                                                <span className="text-sm text-muted-foreground">{sub.plan_name}</span>
                                                <Badge variant={sub.status === 1 ? 'default' : sub.status === 2 ? 'destructive' : 'secondary'}>
                                                    {sub.status === 1 ? <CheckCircle className="h-3 w-3" /> : sub.status === 2 ? <XCircle className="h-3 w-3" /> : null}
                                                    {sub.status === 1 ? t('status.active') : sub.status === 2 ? t('status.expired') : t('status.inactive')}
                                                </Badge>
                                            </div>
                                            <div className="text-xs text-muted-foreground truncate">
                                                {formatQuota(sub.used_quota)}/{formatQuota(sub.quota)} &middot; {t('expiresAt')}: {new Date(sub.end_time * 1000).toLocaleDateString()}
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </div>
            )}
        </PageWrapper>
    );
}
