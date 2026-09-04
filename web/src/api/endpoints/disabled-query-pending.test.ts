/**
 * 钉死 @tanstack/react-query v5 里 `enabled: false` 时 `isPending` 的取值。
 *
 * 为什么需要这条测试：修「密钥登录即被踢出」的做法是给 useCurrentUser 的
 * `enabled` 补上 `&& !isAPIKeyAuth`（user.ts:291-301，同仓 apikey.ts:72 与
 * app.tsx:243 都已有此守卫，唯它漏了）。但 app.tsx 的 bootstrap 里有一句
 * `if (currentUserPending) return;`——若 disabled query 的 isPending 恒为 true，
 * 只补 enabled 会把密钥会话从「登录即踢出」换成「卡在全屏 loader」，
 * 等于换一种坏法。改源码前必须先知道这个行为，不能靠版本号推断。
 *
 * useQuery 的状态字段由 query-core 计算（React 层只做订阅），所以用
 * QueryObserver 直接观测即可，无需渲染组件（本仓 node --test 无 DOM）。
 */
import test from 'node:test';
import assert from 'node:assert/strict';

// 从 react-query 导入（query-core 是它的间接依赖，pnpm 不 hoist——直接 import
// 会 ERR_MODULE_NOT_FOUND，教训见记忆 lodestar-ci-gofmt-gate 那条同类坑）。
// QueryClient / QueryObserver 都由 react-query re-export，是同一份实现。
import { QueryClient, QueryObserver } from '@tanstack/react-query';

function makeClient() {
    return new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
}

test('disabled query stays pending forever (no fetch, status never settles)', async () => {
    const client = makeClient();
    let fetchCount = 0;

    const observer = new QueryObserver(client, {
        queryKey: ['user', 'me'],
        queryFn: async () => {
            fetchCount += 1;
            return { id: 1 };
        },
        enabled: false,
    });

    const unsubscribe = observer.subscribe(() => {});
    // 给 query-core 一个宏任务窗口：若它打算取数，这里足够发起。
    await new Promise((resolve) => setTimeout(resolve, 10));
    const result = observer.getCurrentResult();

    assert.equal(fetchCount, 0, 'disabled query must not call queryFn');
    assert.equal(result.status, 'pending', 'disabled query status stays "pending"');
    assert.equal(
        result.isPending,
        true,
        'isPending is TRUE while disabled — so a bootstrap gate on isPending would hang forever',
    );
    assert.equal(result.fetchStatus, 'idle', 'not fetching: fetchStatus is idle, which is what tells them apart');

    unsubscribe();
    client.clear();
});

test('enabled query settles, so isPending clears', async () => {
    const client = makeClient();

    const observer = new QueryObserver(client, {
        queryKey: ['user', 'me'],
        queryFn: async () => ({ id: 1 }),
        enabled: true,
    });

    const unsubscribe = observer.subscribe(() => {});
    await new Promise((resolve) => setTimeout(resolve, 10));
    const result = observer.getCurrentResult();

    assert.equal(result.isPending, false, 'enabled query resolves and clears isPending');
    assert.equal(result.status, 'success');

    unsubscribe();
    client.clear();
});

/**
 * 上面两条合起来给出结论：`isPending` 无法区分「还在取数」和「被禁用」,
 * 区分二者的是 `fetchStatus`（disabled → 'idle'，in-flight → 'fetching'）。
 * 因此 app.tsx 的 pending 门必须同时排除 API Key 会话，不能只改 enabled。
 */
test('isPending alone cannot distinguish disabled from in-flight; fetchStatus can', async () => {
    const client = makeClient();

    // 永不 resolve 的 queryFn = 真正的 in-flight。
    const inFlight = new QueryObserver(client, {
        queryKey: ['in', 'flight'],
        queryFn: () => new Promise(() => {}),
        enabled: true,
    });
    const disabled = new QueryObserver(client, {
        queryKey: ['dis', 'abled'],
        queryFn: async () => ({ ok: true }),
        enabled: false,
    });

    const un1 = inFlight.subscribe(() => {});
    const un2 = disabled.subscribe(() => {});
    await new Promise((resolve) => setTimeout(resolve, 10));

    const a = inFlight.getCurrentResult();
    const b = disabled.getCurrentResult();

    assert.equal(a.isPending, true);
    assert.equal(b.isPending, true);
    assert.equal(a.isPending, b.isPending, 'isPending is identical in both cases — it is not a usable signal');

    assert.equal(a.fetchStatus, 'fetching');
    assert.equal(b.fetchStatus, 'idle');
    assert.notEqual(a.fetchStatus, b.fetchStatus, 'fetchStatus is what separates them');

    un1();
    un2();
    client.clear();
});
