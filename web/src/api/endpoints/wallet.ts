import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

/** GGZERO 商业层：钱包/余额（USD），按请求成本扣减（商业模式开时） */
export interface WalletBalance {
    quota: number;
    used_quota: number;
}

export interface TopupCode {
    id: number;
    code: string;
    quota: number;
    used: boolean;
    used_by: number;
    created_at: number;
    used_at: number;
}

export function useWallet() {
    return useQuery({
        queryKey: ['wallet', 'balance'],
        queryFn: async () => apiClient.get<WalletBalance>('/api/v1/wallet/balance'),
        refetchOnWindowFocus: false,
    });
}

export function useRedeemCode() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (code: string) =>
            apiClient.post<{ credited: number }>('/api/v1/wallet/redeem', { code }),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['wallet', 'balance'] }),
    });
}

export function useGenerateCodes() {
    return useMutation({
        mutationFn: async (data: { count: number; quota: number }) =>
            apiClient.post<TopupCode[]>('/api/v1/wallet/codes', data),
    });
}

export function useGrantQuota() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (data: { user_id: number; amount: number }) =>
            apiClient.post('/api/v1/wallet/grant', data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['wallet', 'balance'] }),
    });
}
