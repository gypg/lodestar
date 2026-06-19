import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface SubscriptionPlan {
    id: number;
    name: string;
    price: number;
    duration_days: number;
    quota: number;
    description?: string;
    enabled: boolean;
    sort: number;
    created_at: number;
    updated_at: number;
}

export interface SubscriptionOrder {
    id: number;
    user_id: number;
    plan_id: number;
    plan_name: string;
    price: number;
    duration_days: number;
    quota: number;
    status: number; // 0=pending 1=paid 2=cancelled
    created_at: number;
}

export interface UserSubscription {
    id: number;
    user_id: number;
    plan_id: number;
    plan_name: string;
    start_time: number;
    end_time: number;
    quota: number;
    used_quota: number;
    status: number; // 0=inactive 1=active 2=expired
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
        queryFn: async () => apiClient.get<UserSubscription | null>('/api/v1/subscription/self'),
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
            apiClient.post<SubscriptionPlan>('/api/v1/subscription/admin/plans', data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] }),
    });
}

export function useUpdatePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: Partial<SubscriptionPlan> & { id: number }) =>
            apiClient.put<SubscriptionPlan>(`/api/v1/subscription/admin/plans/${data.id}`, data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['subscription', 'admin', 'plans'] }),
    });
}

export function useDeletePlan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (planId: number) =>
            apiClient.delete(`/api/v1/subscription/admin/plans/${planId}`),
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
