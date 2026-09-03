import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query';
import { QUERY_MAX_RETRIES, QUERY_RETRY_BACKOFF_CAP, QUERY_STALE_TIME } from '../api/constants';

function getErrorMessage(error: unknown, fallback = 'An error occurred') {
    if (error instanceof Error && error.message) {
        return error.message;
    }
    if (typeof error === 'object' && error !== null && 'message' in error && typeof error.message === 'string') {
        return error.message;
    }
    return fallback;
}

/**
 * Error notifier for the cache-level onError handlers. Defaults to a no-op so
 * non-UI consumers (and node tests) can import this module without pulling in
 * the Toast component; QueryProvider injects toast.error at module load.
 */
type ErrorNotifier = (message: string) => void;
let notifyError: ErrorNotifier = () => {};

export function setQueryErrorNotifier(notify: ErrorNotifier) {
    notifyError = notify;
}

/**
 * Module-level QueryClient singleton.
 *
 * Why not keep the client inside QueryProvider's useState: the zustand auth
 * store's logout action must clear the react-query cache (WO-029 defect 1) so
 * a shared device never shows the previous identity's data to the next user.
 * The store lives outside the React tree and cannot read provider state; a
 * module singleton is reachable from both worlds, and the provider shares this
 * exact instance with the whole app.
 */
export const queryClientInstance = new QueryClient({
    defaultOptions: {
        queries: {
            staleTime: QUERY_STALE_TIME,
            refetchOnWindowFocus: false,
            retry: (failureCount, error) => {
                if (error instanceof Error && 'code' in error) {
                    const code = (error as { code: number }).code;
                    if (code >= 400 && code < 500) return false;
                }
                return failureCount < QUERY_MAX_RETRIES;
            },
            retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, QUERY_RETRY_BACKOFF_CAP),
        },
        mutations: {
            retry: false,
        },
    },
    queryCache: new QueryCache({
        onError: (error, query) => {
            if (query.meta?.skipGlobalErrorHandler) return;
            notifyError(getErrorMessage(error));
        },
    }),
    mutationCache: new MutationCache({
        onError: (error, _variables, _context, mutation) => {
            if (mutation.meta?.skipGlobalErrorHandler) return;
            notifyError(getErrorMessage(error));
        },
    }),
});
