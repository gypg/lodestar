import { useMutation, useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

/** GGZERO 意见反馈 */
export interface FeedbackItem {
    id: number;
    user_id: number;
    content: string;
    contact: string;
    status: string;
    created_at: number;
}

export function useSubmitFeedback() {
    return useMutation({
        mutationFn: async (data: { content: string; contact: string }) =>
            apiClient.post('/api/v1/feedback/submit', data),
    });
}

export function useFeedbackList(enabled: boolean) {
    return useQuery({
        queryKey: ['feedback', 'list'],
        queryFn: async () => apiClient.get<FeedbackItem[]>('/api/v1/feedback/list'),
        enabled,
        retry: false,
        refetchOnWindowFocus: false,
    });
}
