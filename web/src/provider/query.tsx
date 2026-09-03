'use client';

import { QueryClientProvider } from '@tanstack/react-query';
import { queryClientInstance, setQueryErrorNotifier } from '@/lib/query-client-instance';
import { toast } from '@/components/common/Toast';

// Attach the UI error notifier (toast) to the shared singleton's cache-level
// onError handlers. The singleton itself is created with the full retry /
// staleTime / cache policy in src/lib/query-client-instance.ts — previously it
// lived in this provider's useState; it moved so the auth store's logout can
// clear the cache (WO-029 defect 1).
setQueryErrorNotifier((message) => toast.error(message));

export default function QueryProvider({ children }: { children: React.ReactNode }) {
    return (
        <QueryClientProvider client={queryClientInstance}>
            {children}
        </QueryClientProvider>
    );
}
