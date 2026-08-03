'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export interface ChatMessage {
    role: 'user' | 'assistant';
    content: string;
}

export interface ChatSessionSummary {
    id: number;
    title: string;
    model: string;
    api_key_id: number;
    updated_at: number;
    created_at: number;
}

export interface ChatSessionDetail extends ChatSessionSummary {
    messages: ChatMessage[];
}

const key = ['chat', 'sessions'] as const;

export function useChatSessions() {
    return useQuery({
        queryKey: key,
        queryFn: async () => apiClient.get<ChatSessionSummary[]>('/api/v1/chat/sessions'),
        refetchOnWindowFocus: false,
    });
}

export function useChatSession(id: number | null) {
    return useQuery({
        queryKey: [...key, id],
        queryFn: async () => apiClient.get<ChatSessionDetail>(`/api/v1/chat/sessions/${id}`),
        enabled: id != null && id > 0,
        refetchOnWindowFocus: false,
    });
}

export function useCreateChatSession() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (body: { title?: string; model: string; api_key_id: number }) =>
            apiClient.post<ChatSessionDetail>('/api/v1/chat/sessions', body),
        onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    });
}

export function useSaveChatSession() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (args: {
            id: number;
            model: string;
            api_key_id: number;
            messages: ChatMessage[];
        }) =>
            apiClient.put(`/api/v1/chat/sessions/${args.id}`, {
                model: args.model,
                api_key_id: args.api_key_id,
                messages: args.messages,
            }),
        onSuccess: (_, vars) => {
            qc.invalidateQueries({ queryKey: key });
            qc.invalidateQueries({ queryKey: [...key, vars.id] });
        },
    });
}

export function useDeleteChatSession() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.delete(`/api/v1/chat/sessions/${id}`),
        onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    });
}