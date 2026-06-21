'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export interface ImageRecordSummary {
    id: number;
    prompt: string;
    model: string;
    size: string;
    api_key_id: number;
    created_at: number;
}

export interface ImageRecordDetail extends ImageRecordSummary {
    url: string;
}

const key = ['image', 'records'] as const;

export function useImageRecords() {
    return useQuery({
        queryKey: key,
        queryFn: async () => apiClient.get<ImageRecordSummary[]>('/api/v1/image/records'),
        refetchOnWindowFocus: false,
    });
}

export function useCreateImageRecord() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (body: {
            model: string;
            prompt: string;
            size: string;
            api_key_id: number;
            url: string;
        }) => apiClient.post<ImageRecordDetail>('/api/v1/image/records', body),
        onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    });
}

export function useDeleteImageRecord() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.delete(`/api/v1/image/records/${id}`),
        onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    });
}
