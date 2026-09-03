/**
 * WO-029 T1-T3: logout must clear the react-query cache.
 *
 * These tests exercise the REAL chain: useAuthStore (user.ts) -> logout() ->
 * queryClientInstance.clear(), and T2 goes through the REAL client.ts 401
 * branch (handleError -> getAuthStore().logout()). node --test has no DOM, so:
 *   - a localStorage shim satisfies zustand persist (installed BEFORE the
 *     dynamic imports, which capture storage at module load);
 *   - `window` is aliased to globalThis so client.ts's auth-store getter
 *     registration runs (it is gated on `typeof window !== 'undefined'` in
 *     production as well);
 *   - fetch is mocked via the shared test-utils/fetch-mock.
 * The imports are dynamic so the shims land first. The process must exit
 * explicitly: the import chain keeps timer handles alive that would otherwise
 * prevent node from exiting.
 */
import test from 'node:test';
import assert from 'node:assert/strict';

import { installFetchMock } from '../../test-utils/fetch-mock.ts';

const globalShim = globalThis as unknown as {
    window?: unknown;
    localStorage?: unknown;
};
const memoryStorage = new Map<string, string>();
globalShim.localStorage = {
    getItem: (key: string) => memoryStorage.get(key) ?? null,
    setItem: (key: string, value: string) => {
        memoryStorage.set(key, String(value));
    },
    removeItem: (key: string) => {
        memoryStorage.delete(key);
    },
};
globalShim.window = globalThis;

const { queryClientInstance } = await import('../../lib/query-client-instance.ts');
const { useAuthStore } = await import('./user.ts');
const { apiClient } = await import('../client.ts');

function seedLoggedInSession(token: string) {
    useAuthStore.setState({
        isAuthenticated: true,
        isAPIKeyAuth: false,
        token,
        expireAt: null,
        isLoading: false,
    });
}

function seedLoggedOutSession() {
    useAuthStore.setState({
        isAuthenticated: false,
        isAPIKeyAuth: false,
        token: null,
        expireAt: null,
        isLoading: false,
    });
}

function installLogoutFriendlyFetchMock() {
    const mock = installFetchMock();
    mock.setDefaultHandler((call) => {
        if (call.method === 'GET') {
            return new Response(JSON.stringify({ message: 'token expired' }), {
                status: 401,
                headers: { 'content-type': 'application/json' },
            });
        }
        return new Response(JSON.stringify({ data: null }), {
            status: 200,
            headers: { 'content-type': 'application/json' },
        });
    });
    return mock;
}

/** handleError fires logout() without awaiting it; wait for the dust to settle. */
async function waitForCacheKeyGone(queryKey: readonly unknown[], timeoutMs = 2000): Promise<boolean> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        if (queryClientInstance.getQueryData(queryKey) === undefined) {
            return true;
        }
        await new Promise((resolve) => setTimeout(resolve, 10));
    }
    return queryClientInstance.getQueryData(queryKey) === undefined;
}

test('T1: logout clears the seeded query cache (key-specific assertion)', async () => {
    seedLoggedInSession('token-A');
    const mock = installLogoutFriendlyFetchMock();
    try {
        queryClientInstance.setQueryData(['models', 'capabilities'], { owner: 'user-A' });
        assert.deepEqual(
            queryClientInstance.getQueryData(['models', 'capabilities']),
            { owner: 'user-A' },
            'test precondition: cache entry must exist before logout',
        );

        await useAuthStore.getState().logout();

        assert.equal(
            queryClientInstance.getQueryData(['models', 'capabilities']),
            undefined,
            'T1: logout must wipe the query cache — query keys carry no identity, so ' +
                'the next visitor on a shared device must not be served user-A data',
        );
        assert.equal(useAuthStore.getState().isAuthenticated, false, 'T1: logout must clear auth state');
    } finally {
        mock.uninstall();
    }
});

test('T2: the 401 auto-logout path in client.ts clears the cache too', async () => {
    seedLoggedInSession('token-A');
    const mock = installLogoutFriendlyFetchMock();
    try {
        queryClientInstance.setQueryData(['wallet', 'balance'], { owner: 'user-A', balance: 42 });

        await assert.rejects(
            apiClient.get('/api/v1/user/status'),
            (error: unknown) => (error as { code?: number }).code === 401,
            'T2 precondition: the request must surface the 401',
        );

        const cleared = await waitForCacheKeyGone(['wallet', 'balance']);
        assert.equal(cleared, true, 'T2: the 401 branch (handleError -> getAuthStore().logout()) must clear the cache');
        assert.equal(useAuthStore.getState().isAuthenticated, false, 'T2: 401 must log the user out');
    } finally {
        mock.uninstall();
    }
});

test('T3: a fresh login after logout refetches instead of serving stale data', async () => {
    seedLoggedInSession('token-A');
    const mock = installLogoutFriendlyFetchMock();
    try {
        // Fresh data for user-A must not survive logout even within staleTime.
        queryClientInstance.setQueryData(['models', 'capabilities'], { owner: 'user-A' });

        await useAuthStore.getState().logout();
        assert.equal(
            queryClientInstance.getQueryData(['models', 'capabilities']),
            undefined,
            'T3: cache must be empty after logout, so user-B cannot be served user-A data',
        );

        seedLoggedInSession('token-B');
        let fetchCount = 0;
        const served = await queryClientInstance.fetchQuery({
            queryKey: ['models', 'capabilities'],
            queryFn: async () => {
                fetchCount += 1;
                return { owner: 'user-B-fresh' };
            },
            // staleTime override in fetchQuery options: even with a nonzero
            // staleTime the query must hit the network, because the cache was
            // cleared and holds no fresh entry for this key.
            staleTime: 60_000,
        });
        assert.deepEqual(served, { owner: 'user-B-fresh' }, 'T3: user-B must get freshly fetched data');
        assert.equal(fetchCount, 1, 'T3: the query must hit the network exactly once (no stale hit)');
        assert.deepEqual(
            queryClientInstance.getQueryData(['models', 'capabilities']),
            { owner: 'user-B-fresh' },
            'T3: user-B cache now holds only user-B data',
        );
    } finally {
        mock.uninstall();
        seedLoggedOutSession();
    }
});
