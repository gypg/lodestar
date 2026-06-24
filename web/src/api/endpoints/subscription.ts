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
    status: number;
    created_at: number;
    completed_at: number;
}

export interface UserSubscription {
    id: number;
    user_id: number;
    plan_id: number;
    order_id: number;
    amount_total: number;
    amount_used: number;
    starts_at: number;
    expires_at: number;
    status: number;
    source: string;
    created_at: number;
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

export function useAdminPlans() {
    return useQuery({
        queryKey: ['subscription', 'admin', 'plans'],
        queryFn: async () => apiClient.get<SubscriptionPlan[]>('/api/v1/subscription/admin/plans'),
        refetchOnWindowFocus: false,
    });
}

export function useAdminSubscriptions() {
    return useQuery({
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
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] }),
    });
}

export function useUpdatePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: Partial<SubscriptionPlan> & { id: number }) =>
            apiClient.post<SubscriptionPlan>('/api/v1/subscription/admin/plans/update', data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] }),
    });
}

export function useDeletePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (planId: number) =>
            apiClient.delete(`/api/v1/subscription/admin/plans/delete/${planId}`),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] }),
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
