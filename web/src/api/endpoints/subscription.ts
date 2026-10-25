import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface SubscriptionPlan {
    id: number;
    name: string;
    description?: string;
    price: number;
    currency?: string;
    duration_type?: string;
    duration_days: number;
    custom_duration_s?: number;
    quota_amount: number;
    /** Rolling spend windows (USD); 0 = disabled. */
    window_4h_limit?: number;
    window_weekly_limit?: number;
    enabled: boolean;
    sort_order?: number;
    created_at: number;
    updated_at: number;
}

export interface SubscriptionOrder {
    id: number;
    user_id: number;
    plan_id: number;
    trade_no: string;
    money: number;
    payment_method: string;
    /** 后端 varchar，默认 'pending'；不是数字枚举 */
    status: string;
    created_at: number;
    completed_at: number;
}

/** 与后端 model.SubStatus* 对齐（varchar，不是数字枚举） */
export type SubscriptionStatus = 'active' | 'expired' | 'cancelled';

export interface UserSubscription {
    id: number;
    user_id: number;
    plan_id: number;
    order_id: number;
    amount_total: number;
    amount_used: number;
    starts_at: number;
    expires_at: number;
    status: SubscriptionStatus;
    source: string;
    created_at: number;
}

export interface SubscriptionWindowState {
    subscription_id: number;
    kind: '4h' | 'weekly';
    limit_usd: number;
    window_start: number;
    used: number;
    remaining: number;
    resets_at: number;
    updated_at: number;
}

// ── User hooks ────────────────────────────────────────────────────────────────

export function useSubscriptionPlans() {
    return useQuery({
        queryKey: ['subscription', 'plans'],
        queryFn: async () => apiClient.get<SubscriptionPlan[]>('/api/v1/subscription/plans'),
        refetchOnWindowFocus: false,
    });
}

export function useMySubscription() {
    return useQuery({
        queryKey: ['subscription', 'self'],
        queryFn: async () => {
            const result = await apiClient.get<UserSubscription | null>('/api/v1/subscription/self');
            if (!result || typeof result !== 'object' || !('expires_at' in result)) {
                return null;
            }
            return result;
        },
        refetchOnWindowFocus: false,
    });
}

export function usePurchaseSubscription() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (planId: number) =>
            apiClient.post<SubscriptionOrder>('/api/v1/subscription/purchase', { plan_id: planId }),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['subscription', 'self'] });
            qc.invalidateQueries({ queryKey: ['wallet', 'balance'] });
        },
    });
}

// ── Admin hooks ───────────────────────────────────────────────────────────────

/**
 * Admin-only: requires subscriptions:write. Pass `enabled: false` for roles that
 * lack it -- an unconditional fetch 403s and the global query error handler shows
 * that as a toast, so the caller must not merely hide the resulting markup.
 */
export function useAdminPlans(enabled = true) {
    return useQuery({
        enabled,
        queryKey: ['subscription', 'admin', 'plans'],
        queryFn: async () => apiClient.get<SubscriptionPlan[]>('/api/v1/subscription/admin/plans'),
        refetchOnWindowFocus: false,
    });
}

/** Admin-only: requires subscriptions:write. See useAdminPlans on `enabled`. */
export function useAdminSubscriptions(enabled = true) {
    return useQuery({
        enabled,
        queryKey: ['subscription', 'admin', 'subscriptions'],
        queryFn: async () => apiClient.get<UserSubscription[]>('/api/v1/subscription/admin/subscriptions'),
        refetchOnWindowFocus: false,
    });
}

export function useCreatePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: Omit<SubscriptionPlan, 'id' | 'created_at' | 'updated_at'>) =>
            apiClient.post<SubscriptionPlan>('/api/v1/subscription/admin/plans/create', data),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] });
            qc.invalidateQueries({ queryKey: ['subscription', 'plans'] });
        },
    });
}

export function useUpdatePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: Partial<SubscriptionPlan> & { id: number }) =>
            apiClient.post<SubscriptionPlan>('/api/v1/subscription/admin/plans/update', data),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] });
            qc.invalidateQueries({ queryKey: ['subscription', 'plans'] });
        },
    });
}

export function useDeletePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (planId: number) =>
            apiClient.delete(`/api/v1/subscription/admin/plans/delete/${planId}`),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] });
            qc.invalidateQueries({ queryKey: ['subscription', 'plans'] });
        },
    });
}

export function useBindSubscription() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: { user_id: number; plan_id: number }) =>
            apiClient.post<UserSubscription>('/api/v1/subscription/admin/bind', data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'subscriptions'] }),
    });
}

/** Admin-only: rolling-window states for one subscription (0 = all). */
export function useAdminWindows(subscriptionID: number, enabled = true) {
    return useQuery({
        enabled,
        queryKey: ['subscription', 'admin', 'windows', subscriptionID],
        queryFn: async () =>
            apiClient.get<SubscriptionWindowState[]>(
                `/api/v1/subscription/admin/windows?subscription_id=${subscriptionID}`,
            ),
        refetchOnWindowFocus: false,
    });
}

/** Admin-only: move a window's used amount by delta USD (negative = grant back). */
export function useAdjustWindow() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: { subscription_id: number; kind: '4h' | 'weekly'; delta_usd: number }) =>
            apiClient.post<SubscriptionWindowState>('/api/v1/subscription/admin/windows/adjust', data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'windows'] }),
    });
}

/** Own rolling-window states; empty when the plan has no windows. */
export function useSelfWindows(enabled = true) {
    return useQuery({
        enabled,
        queryKey: ['subscription', 'self', 'windows'],
        queryFn: async () => {
            const data = await apiClient.get<SubscriptionWindowState[]>('/api/v1/subscription/self/windows');
            return Array.isArray(data) ? data : [];
        },
        refetchInterval: 60_000,
    });
}
