import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

// --- Types ---

export interface OAuthStatus {
    enabled: boolean;
    bound: boolean;
    provider_username: string | null;
}

export interface GitHubAuthURLResponse {
    authorize_url: string;
}

// --- Hooks ---

export function useGitHubOAuthStatus() {
    return useQuery({
        queryKey: ['oauth', 'github', 'status'],
        queryFn: async () => apiClient.get<OAuthStatus>('/api/v1/user/oauth/github/status'),
        refetchInterval: false,
        staleTime: 30_000,
    });
}

export function useUnbindGitHub() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () => apiClient.post('/api/v1/user/oauth/github/unbind'),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['oauth', 'github', 'status'] }),
        onError: (error) => logger.error('GitHub unbind failed:', error),
    });
}

export function getGitHubAuthURL() {
    return apiClient.get<GitHubAuthURLResponse>('/api/v1/oauth/github/state', undefined, false);
}
