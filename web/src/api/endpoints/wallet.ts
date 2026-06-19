import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

/** Lodestar 商业层：钱包/余额（USD），按请求成本扣减（商业模式开时） */
export interface WalletBalance {
    quota: number;
    used_quota: number;
    epay_configured?: boolean;
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

/** 管理员生成邀请码 */
export function useGenerateInvites() {
    return useMutation({
        mutationFn: async (count: number) =>
            apiClient.post<{ code: string }[]>('/api/v1/wallet/invites', { count }),
    });
}

/** 管理员发送测试邮件 */
export function useTestEmail() {
    return useMutation({
        mutationFn: async (to: string) => apiClient.post('/api/v1/wallet/email-test', { to }),
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

/** 在线充值（易支付）：返回网关 URL + 已签名参数，前端构造表单提交跳转 */
export function useTopup() {
    return useMutation({
        mutationFn: async (data: { amount: number; method: string }) =>
            apiClient.post<{ url: string; params: Record<string, string> }>('/api/v1/wallet/topup', data),
    });
}

/** Stripe 在线充值：创建 Checkout Session，返回 hosted page URL */
export function useStripeTopup() {
    return useMutation({
        mutationFn: async (data: { amount: number }) =>
            apiClient.post<{ pay_link: string }>('/api/v1/wallet/stripe/topup', data),
        onSuccess: (d) => {
            window.open(d.pay_link, '_blank', 'noopener');
        },
    });
}

/** 每用户用量（聚合自己名下各 key 的统计） */
export interface UsageKey {
    name: string;
    requests: number;
    tokens: number;
    cost: number;
}
export interface UsageDailyPoint {
    date: string;
    requests: number;
    tokens: number;
    cost: number;
}
export interface UsageHeatmapDay {
    day: string;
    requests: number;
    tokens?: number;
}
export interface UsageModelRow {
    model: string;
    requests: number;
    tokens: number;
    cost: number;
}
export interface UsageSummary {
    total_requests: number;
    total_tokens: number;
    total_cost: number;
    per_key: UsageKey[];
    daily_series?: UsageDailyPoint[];
    usage_chart_available?: boolean;
    heatmap_by_day?: UsageHeatmapDay[];
    per_model?: UsageModelRow[];
}
export function useUsage() {
    return useQuery({
        queryKey: ['wallet', 'usage'],
        queryFn: async () => apiClient.get<UsageSummary>('/api/v1/wallet/usage'),
        refetchOnWindowFocus: false,
    });
}
