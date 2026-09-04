/**
 * 「密钥登录成功后立刻被踢出」的守卫测试。
 *
 * 缺陷原貌（WO-033 生产实测两次复现）：`GET /api/v1/apikey/login` 返回 200 后约
 * 2ms，`GET /api/v1/user/me` 返回 401 → client.ts 的全局 401 分支调 logout() →
 * 刚建立的会话被自己清空，用户永远登不进去。根因是 useCurrentUser 的 enabled
 * 只判 isAuthenticated，没排除 API Key 会话，于是拿 API Key 去打 JWT-only 的
 * /user/me。同仓 apikey.ts:72 与 app.tsx 的 settings 查询都带这个守卫，唯它漏了。
 *
 * 为什么用 QueryObserver 而不是断言源码文本：断言文本（或在测试里重实现一份
 * `enabled` 表达式）都杀不掉变异——把守卫删掉，那种测试照样绿。这里把**真的**
 * options 交给**真的** query-core，观测的是"queryFn 到底被调用了没有"，
 * 所以删掉 `&& !session.isAPIKeyAuth` 会让第一条测试立刻变红（请求真的发出去）。
 * 教训来源：记忆 lodestar-test-assertion-gaps、lodestar-i18n-gate-scope-blindspots。
 */
import test from 'node:test';
import assert from 'node:assert/strict';

// 从 react-query 导入：query-core 是间接依赖，pnpm 不 hoist，直接 import 会
// ERR_MODULE_NOT_FOUND。QueryClient/QueryObserver 由 react-query re-export。
import { QueryClient, QueryObserver } from '@tanstack/react-query';

const globalShim = globalThis as unknown as { window?: unknown; localStorage?: unknown };
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

const { getCurrentUserQueryOptions } = await import('./user.ts');

/**
 * 用真 QueryObserver 跑一遍 options，返回 queryFn 是否被调用过。
 * queryFn 被换成计数器：断言的是"请求有没有发出"，不是响应内容。
 */
async function didFetch(session: { isAuthenticated: boolean; isAPIKeyAuth: boolean }) {
    const options = getCurrentUserQueryOptions(session);
    const client = new QueryClient();
    let calls = 0;

    const observer = new QueryObserver(client, {
        ...options,
        // 保留真实的 enabled/queryKey/retry，只替换掉网络那一步
        queryFn: async () => {
            calls += 1;
            return { id: 1, username: 'x', role: 'user', quota: 0, used_quota: 0 };
        },
    });
    const unsubscribe = observer.subscribe(() => {});
    await new Promise((resolve) => setTimeout(resolve, 10));
    const fetchStatus = observer.getCurrentResult().fetchStatus;
    unsubscribe();
    client.clear();

    return { calls, fetchStatus };
}

test('API Key session must NOT hit /user/me (that 401 is what logged them straight back out)', async () => {
    const { calls, fetchStatus } = await didFetch({ isAuthenticated: true, isAPIKeyAuth: true });

    assert.equal(
        calls,
        0,
        'an API Key session fetched /user/me — that request 401s (JWT-only route) and the ' +
        'global 401 handler calls logout(), which is exactly the "key login kicks you out" bug',
    );
    assert.equal(fetchStatus, 'idle', 'the query must be disabled, not merely slow');
});

test('JWT session still fetches /user/me (the role must keep loading)', async () => {
    const { calls } = await didFetch({ isAuthenticated: true, isAPIKeyAuth: false });

    assert.equal(
        calls,
        1,
        'a normal signed-in session must still load its role, otherwise nav/permissions break',
    );
});

test('signed-out session fetches nothing', async () => {
    const { calls } = await didFetch({ isAuthenticated: false, isAPIKeyAuth: false });
    assert.equal(calls, 0, 'no session must not call /user/me');
});

/**
 * 接线断言：上面三条只证明 getCurrentUserQueryOptions 本身正确。若有人把 hook 改回
 * 内联 useQuery({...})、绕开这个函数，上面三条照样全绿——这正是记忆
 * lodestar-i18n-gate-scope-blindspots 记的第四盲区（守卫存在但调用点绕过它）。
 * 所以这里额外钉死 useCurrentUser 确实经由它构造 options。
 */
test('useCurrentUser is wired to getCurrentUserQueryOptions', async () => {
    const { readFileSync } = await import('node:fs');
    const { fileURLToPath } = await import('node:url');
    const path = await import('node:path');

    const here = path.dirname(fileURLToPath(import.meta.url));
    const src = readFileSync(path.join(here, 'user.ts'), 'utf8');

    const idx = src.indexOf('export function useCurrentUser()');
    assert.notEqual(idx, -1, 'useCurrentUser has been renamed or removed');
    const body = src.slice(idx, src.indexOf('\n}', idx));

    assert.match(
        body,
        /useQuery\(\s*getCurrentUserQueryOptions\(/,
        'useCurrentUser must build its options via getCurrentUserQueryOptions, or the ' +
        'behavioural tests above stop covering the real call site',
    );

    // 实参必须是从 store 读来的那两个变量，写成简写形态。
    //
    // 只断言"body 里出现 isAPIKeyAuth 字样"是不够的：WO-034 的对抗验收找到一个真绕过——
    //     useQuery(getCurrentUserQueryOptions({ isAuthenticated: true, isAPIKeyAuth: false }))
    // 硬编码两个值，既是直接调用形态（过上面那条正则）、又含 isAPIKeyAuth 键名
    //（过旧的宽松断言），而运行时守卫恒真、查询恒发，原缺陷原样回归，四条测试全绿。
    // 上面那些行为测试也抓不到它：它们直接调 options 函数，不经过这个 hook。
    // 所以这里钉的是**参数来源**而不只是调用形态。
    assert.match(
        body,
        /getCurrentUserQueryOptions\(\{\s*isAuthenticated,\s*isAPIKeyAuth\s*\}\)/,
        'useCurrentUser must pass the two store values through as shorthand properties. ' +
        'Hardcoding them (e.g. `{ isAuthenticated: true, isAPIKeyAuth: false }`) satisfies a ' +
        'looser check while defeating the guard at runtime',
    );

    // 且这两个值必须真的来自 store，不是同名局部常量。
    assert.match(
        body,
        /useAuthStore\(\(s\) => s\.isAPIKeyAuth\)/,
        'isAPIKeyAuth must be read from the auth store',
    );
    assert.match(
        body,
        /useAuthStore\(\(s\) => s\.isAuthenticated\)/,
        'isAuthenticated must be read from the auth store',
    );
});
