'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type LogFieldName =
    | 'endpointType'
    | 'channelName'
    | 'actualModel'
    | 'apiKeyName'
    | 'clientIP'
    | 'cost';

export type LogFieldVisibility = Record<LogFieldName, boolean>;

export const DEFAULT_LOG_FIELD_VISIBILITY: LogFieldVisibility = {
    endpointType: true,
    channelName: true,
    actualModel: true,
    apiKeyName: true,
    clientIP: true,
    cost: true,
};

type LogFieldVisibilityState = {
    visibility: LogFieldVisibility;
    toggleField: (field: LogFieldName) => void;
    resetFields: () => void;
};

export const useLogFieldVisibilityStore = create<LogFieldVisibilityState>()(
    persist(
        (set) => ({
            visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY },
            toggleField: (field) =>
                set((state) => ({
                    visibility: {
                        ...state.visibility,
                        [field]: !state.visibility[field],
                    },
                })),
            resetFields: () =>
                set({ visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY } }),
        }),
        {
            name: 'log-field-visibility-storage',
            partialize: (state) => ({
                visibility: state.visibility,
            }),
        },
    ),
);

export function useLogFieldVisibility() {
    return useLogFieldVisibilityStore((s) => s.visibility);
}

/**
 * 日志模型搜索文本，在标题栏搜索框与 Log 组件筛选逻辑之间共享。
 */
export const useLogModelSearchStore = create<{
    modelSearch: string;
    setModelSearch: (value: string) => void;
}>()((set) => ({
    modelSearch: '',
    setModelSearch: (value) => set({ modelSearch: value }),
}));
