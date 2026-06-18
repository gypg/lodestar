import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

/** GGZERO 公开平台概览（无需登录，供落地页展示站点信息/内容） */
export interface PublicModel {
    name: string;
    input: number;
    output: number;
}

export interface PublicOverview {
    site_name: string;
    description: string;
    announcement: string;
    footer: string;
    model_count: number;
    models: PublicModel[];
    total_requests: number;
    total_tokens: number;
}

export function usePublicOverview(enabled = true) {
    return useQuery({
        queryKey: ['public', 'overview'],
        queryFn: async () => apiClient.get<PublicOverview>('/api/v1/public/overview', undefined, false),
        enabled,
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: 60_000,
    });
}
