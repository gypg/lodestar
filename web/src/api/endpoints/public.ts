import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

/** Lodestar 公开平台概览（无需登录，供落地页展示站点信息/内容） */
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
    landing_ambient_mode?: 'photo' | 'classic' | 'color4bg';
    site_banner_enabled?: boolean;
    site_banner_text?: string;
    site_banner_tone?: 'info' | 'warning' | 'success';
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
