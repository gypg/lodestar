import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

// --- Types ---

export interface TwoFAStatus {
    enabled: boolean;
    backup_codes_remaining: number;
}

export interface TwoFASetupResult {
    qr_code: string;
    secret: string;
    backup_codes: string[];
}

// --- Hooks ---

export function useTwoFAStatus() {
    return useQuery({
        queryKey: ['twofa', 'status'],
        queryFn: async () => apiClient.get<TwoFAStatus>('/api/v1/user/2fa/status'),
        refetchInterval: false,
        staleTime: 30_000,
    });
}

export function useSetup2FA() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () =>
            apiClient.post<TwoFASetupResult>('/api/v1/user/2fa/setup'),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['twofa', 'status'] }),
        onError: (error) => logger.error('2FA setup failed:', error),
    });
}

export function useEnable2FA() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ code, backup_codes }: { code: string; backup_codes: string[] }) =>
            apiClient.post('/api/v1/user/2fa/enable', { code, backup_codes }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['twofa', 'status'] }),
        onError: (error) => logger.error('2FA enable failed:', error),
    });
}

export function useDisable2FA() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (code: string) =>
            apiClient.post('/api/v1/user/2fa/disable', { code }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['twofa', 'status'] }),
        onError: (error) => logger.error('2FA disable failed:', error),
    });
}

export function useRegenerateBackupCodes() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (code: string) =>
            apiClient.post<string[]>('/api/v1/user/2fa/backup-codes', { code }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['twofa', 'status'] }),
        onError: (error) => logger.error('2FA backup code regeneration failed:', error),
    });
}
