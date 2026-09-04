import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { test } from 'node:test';
import { hasPermission } from './permissions.ts';

/**
 * Guards the defect class behind "permission denied appears while just moving
 * around the app": a query fires for a role that cannot make it, gets a 403, and
 * the global QueryCache.onError in provider/query.tsx turns every rejection into
 * a toast. Hiding the resulting markup does not help -- the request already went.
 *
 * These are source-level assertions because there is no React test environment
 * here, so nothing can observe a real render or a real fetch. They pin the guard
 * expressions in place; they cannot prove the runtime behaviour.
 */

const WEB_ROOT = path.resolve(import.meta.dirname, '..', '..');
const read = (rel: string) => readFileSync(path.join(WEB_ROOT, rel), 'utf8');

test('the always-mounted settings query is gated on settings:read', () => {
    const src = read('src/components/app.tsx');

    // AppContainer never unmounts and this query re-polls, so an ungated version
    // toasts on login and then on every interval for an end customer.
    //
    // Anchor on the spread call site, not the bare name: the bare name also matches
    // the function DEFINITION earlier in the file, and scanning from there reads a
    // region with no `enabled` in it at all. (That mistake failed this test once.)
    const idx = src.indexOf('...getSettingsListQueryOptions(),');
    assert.notEqual(idx, -1, 'the settings list query call site has moved or been renamed');

    // Scan to the end of the options object rather than a fixed byte count: the
    // explanatory comment above `enabled` is itself ~380 chars, so a 400-char
    // window ended before the line it was meant to check. (That failed twice.)
    const objectEnd = src.indexOf('});', idx);
    assert.notEqual(objectEnd, -1, 'could not find the end of the query options object');
    const options = src.slice(idx, objectEnd);

    assert.match(
        options,
        /enabled:[^\r\n]*hasPermission\([^)]*'settings:read'\)/,
        'the settings list query must be gated on settings:read, not only on being signed in',
    );
});

test('the bootstrap prefetch of settings is permission-aware', () => {
    const src = read('src/components/app.tsx');
    const idx = src.indexOf('queryClient.fetchQuery(getSettingsListQueryOptions())');
    assert.notEqual(idx, -1, 'the bootstrap settings prefetch has moved');

    // fetchQuery is a direct call: `enabled` does not apply, so the skip must be
    // an explicit branch around it.
    const before = src.slice(Math.max(0, idx - 1200), idx);
    assert.match(
        before,
        /hasPermission\([^)]*'settings:read'\)/,
        'the bootstrap prefetch must consult settings:read before fetching',
    );
});

/**
 * The permission check above is worthless if it runs before the role is known:
 * hasPermission(undefined, ...) is false, and an earlier version compensated by
 * inverting the test to "skip only when known to lack it" -- which let every cold
 * load through and produced a 403 toast once per page load. The real fix is to not
 * decide until /user/me settles.
 *
 * Pinned because the previous test passed while that bug was live.
 */
test('bootstrap waits for the role to settle before deciding', () => {
    const src = read('src/components/app.tsx');

    assert.match(
        src,
        /isPending:\s*currentUserPending/,
        'app.tsx must read isPending from useCurrentUser to know whether the role has settled',
    );
    // The guard is deliberately NOT the bare `if (currentUserPending) return;` any
    // more: useCurrentUser is disabled for API Key sessions (/user/me is JWT-only),
    // and a disabled query's isPending stays true forever, so the bare form parked
    // those sessions behind the full-screen loader. It must still wait for a JWT
    // session, hence "must mention isAPIKeyAuth AND currentUserPending on one line".
    assert.match(
        src,
        /if\s*\(\s*!isAPIKeyAuth\s*&&\s*currentUserPending\s*\)\s*return;/,
        'the bootstrap effect must return early while a JWT role is in flight, but must ' +
        'not wait on the permanently-pending disabled query of an API Key session',
    );

    // The early return only resumes if the flag is a dependency; otherwise bootstrap
    // never completes and the app sits behind its loader.
    const depsIdx = src.indexOf('}, [authLoading, isAPIKeyAuth, isAuthenticated');
    assert.notEqual(depsIdx, -1, 'the bootstrap effect dependency array has moved');
    const deps = src.slice(depsIdx, src.indexOf(']', depsIdx) + 1);
    assert.match(
        deps,
        /currentUserPending/,
        'currentUserPending must be a dependency of the bootstrap effect, or the early ' +
        `return never resumes. got: ${deps}`,
    );

    // And the guard must be the positive form now -- the inverted shape is the bug.
    assert.doesNotMatch(
        src,
        /lacksSettingsRead/,
        'the inverted "skip only when known to lack it" guard is the cold-load bug; ' +
        'with the pending check in place the positive form is correct',
    );
});

/**
 * Toolbar is mounted on every page in app.tsx, and its `if (!toolbarItem) return null`
 * cannot stop a hook -- hooks run first. So an ungated useModelMarket() called
 * /api/v1/model/market on every page for every role, including pages with no toolbar
 * at all. That route needs settings:read, so it 403'd for end customers once per load.
 */
test('the toolbar only fetches the model market when it renders', () => {
    const src = read('src/components/modules/toolbar/index.tsx');
    assert.match(
        src,
        /useModelMarket\(\s*toolbarItem\s*!==\s*null\s*\)/,
        'Toolbar must pass an enabled flag to useModelMarket -- it is mounted on every ' +
        'page and the early return below the hooks cannot prevent the request',
    );

    const hookSrc = read('src/api/endpoints/model.ts');
    assert.match(
        hookSrc,
        /export function useModelMarket\(enabled = true\)/,
        'useModelMarket must accept an enabled flag',
    );
    // The flag has to reach useQuery, not just sit in the signature.
    const start = hookSrc.indexOf('export function useModelMarket(');
    const body = hookSrc.slice(start, hookSrc.indexOf('\n}', start));
    assert.match(
        body,
        /^\s*enabled,\s*$/m,
        `useModelMarket must forward enabled into useQuery. got: ${body}`,
    );
});

test('admin subscription hooks accept an enabled flag', () => {
    const src = read('src/api/endpoints/subscription.ts');
    for (const hook of ['useAdminPlans', 'useAdminSubscriptions']) {
        const re = new RegExp(`export function ${hook}\\(enabled = true\\)`);
        assert.match(
            src,
            re,
            `${hook} must take an enabled flag: it hits a subscriptions:write route, ` +
            `and the end-customer role reaches the subscription page`,
        );
    }
    // And the caller must actually pass it -- an unused parameter fixes nothing.
    const caller = read('src/components/modules/subscription/index.tsx');
    assert.match(caller, /useAdminPlans\(isAdmin\)/, 'useAdminPlans must be called with isAdmin');
    assert.match(caller, /useAdminSubscriptions\(isAdmin\)/, 'useAdminSubscriptions must be called with isAdmin');
});

/**
 * activeItem is persisted in nav-storage, so it can name a page the current role
 * cannot reach -- a shared browser left on a staff tab, or 'model' from before it
 * was removed from the portal whitelist. The bootstrap switch prefetches off that
 * stored value, so without a check it calls those endpoints and toasts once per
 * load with nothing on screen to explain it.
 */
test('the per-tab bootstrap prefetch is permission-gated', () => {
    const src = read('src/components/app.tsx');

    assert.match(
        src,
        /const prefetchPermissions:\s*Partial<Record<NavItem,\s*Permission>>/,
        'the per-tab prefetch permission table has moved or been removed',
    );
    assert.match(
        src,
        /if\s*\(mayPrefetchActive\)\s*switch\s*\(activeItem\)/,
        'the prefetch switch must be guarded by the permission check',
    );

    // Asserting the guard's shape is not enough: replacing the permission call with
    // `true` keeps that shape and left this test green once. Pin the computation.
    const flagIdx = src.indexOf('const mayPrefetchActive =');
    assert.notEqual(flagIdx, -1, 'mayPrefetchActive has been renamed or removed');
    const flagExpr = src.slice(flagIdx, src.indexOf(';', flagIdx));
    assert.match(
        flagExpr,
        /hasPermission\(\s*currentUser\?\.role\s*,\s*neededForActive\s*\)/,
        'mayPrefetchActive must be computed from hasPermission(currentUser?.role, ' +
        `neededForActive). got: ${flagExpr}`,
    );
    assert.doesNotMatch(
        flagExpr,
        /\|\|\s*true|&&\s*true|=\s*true\b/,
        `mayPrefetchActive must not be short-circuited to true. got: ${flagExpr}`,
    );
    // Unnegated: `!hasPermission(...)` also satisfies the match above, and inverting
    // it swaps the whole behaviour (customers prefetch, staff do not). That mutation
    // survived until this assertion was added.
    assert.doesNotMatch(
        flagExpr,
        /!\s*hasPermission\(/,
        `the permission check must not be negated -- that inverts who prefetches. got: ${flagExpr}`,
    );

    // The table must cover the tabs whose endpoints the end-customer role cannot
    // call. 'model' is the one that actually regressed, so it is asserted by name.
    const tableStart = src.indexOf('const prefetchPermissions:');
    const table = src.slice(tableStart, src.indexOf('};', tableStart));
    for (const [tab, perm] of [
        ['model', 'settings:read'],
        ['ops', 'settings:read'],
        ['channel', 'channels:read'],
        ['group', 'groups:read'],
    ] as const) {
        assert.match(
            table,
            new RegExp(`${tab}:\\s*'${perm}'`),
            `prefetchPermissions must map ${tab} to ${perm}`,
        );
        // And that permission must genuinely be one the customer lacks, or the
        // entry is decorative.
        assert.equal(
            hasPermission('user', perm),
            false,
            `${tab} is mapped to ${perm}, but the end-customer role holds it -- ` +
            `gating on it would not prevent the 403`,
        );
    }
});

/**
 * The API-key form is reachable by end customers -- creating your own key is the
 * point of the portal's API-keys page -- but two of its inputs were built for
 * operators and fetched operator data unconditionally: useGroupList (groups:read)
 * and useChannelList (channels:read). Pressing "new key" produced two toasts.
 *
 * Neither route can be opened up instead: Group.Items carries ChannelID, Priority
 * and Weight (the routing topology) and Channel carries upstream names. So the model
 * names a customer needs come from /model/capabilities, which is Auth()-only.
 */
test('the API-key form gates operator-only queries', () => {
    const src = read('src/components/modules/setting/APIKey.tsx');

    assert.match(
        src,
        /useGroupList\(\s*mayReadGroups\s*\)/,
        'useGroupList must be gated -- it needs groups:read, which end customers lack',
    );
    assert.match(
        src,
        /useChannelList\(\s*mayReadChannels\s*\)/,
        'useChannelList must be gated -- it needs channels:read',
    );
    for (const [flag, perm] of [
        ['mayReadGroups', 'groups:read'],
        ['mayReadChannels', 'channels:read'],
    ] as const) {
        const idx = src.indexOf(`const ${flag} =`);
        assert.notEqual(idx, -1, `${flag} has been renamed or removed`);
        const expr = src.slice(idx, src.indexOf(';', idx));
        assert.match(
            expr,
            new RegExp(`hasPermission\\(\\s*currentUser\\?\\.role\\s*,\\s*'${perm}'\\s*\\)`),
            `${flag} must be computed from hasPermission(..., '${perm}'). got: ${expr}`,
        );
        assert.doesNotMatch(expr, /!\s*hasPermission\(|\|\|\s*true/, `${flag} must not be inverted or forced true. got: ${expr}`);
        assert.equal(
            hasPermission('user', perm),
            false,
            `${flag} gates on ${perm}, but the end-customer role holds it -- gating would not help`,
        );
    }

    // Customers must still be able to scope a key to models, via the safe source.
    assert.match(
        src,
        /useModelCapabilities\(\s*!mayReadGroups\s*\)/,
        'customers need model names from /model/capabilities when groups is unavailable',
    );
    assert.match(
        src,
        /mayReadGroups\s*\n?\s*\?\s*groups\.map/,
        'availableModels must fall back to capabilities for roles without groups:read',
    );

    // The channel-exclusion picker lists upstream names, so it must be hidden
    // outright rather than merely disabled.
    assert.match(
        src,
        /\{mayReadChannels && \(/,
        'the excluded-channels section must be hidden for roles without channels:read',
    );
});

test('the end-customer role lacks every permission these queries need', () => {
    // The premise the three assertions above rest on. If this ever flips, the
    // gates are not wrong -- but they stop being about this role.
    assert.equal(hasPermission('user', 'settings:read'), false);
    assert.equal(hasPermission('user', 'subscriptions:write'), false);
    // Held, so the checks above are not passing because 'user' has nothing at all.
    assert.equal(hasPermission('user', 'subscriptions:read'), true);
    assert.equal(hasPermission('user', 'apikeys:read'), true);
});

/**
 * The proxy-pool list is the same shape of trap as the settings query above, and
 * it became one the moment the backend route gained a settings:read gate:
 * ProxyPoolDialog is mounted unconditionally by app.tsx and calls
 * useProxyConfigurationList at its top level with a refetch interval, so an
 * ungated version polls a 403 for every end customer and toasts on each tick.
 *
 * Gated inside the hook rather than at the call sites, so a new caller cannot
 * reintroduce the toast by forgetting to pass `enabled`.
 */
test('the proxy-pool list query is gated on settings:read', () => {
    const src = read('src/api/endpoints/proxy-pool.ts');

    const idx = src.indexOf('export function useProxyConfigurationList()');
    assert.notEqual(idx, -1, 'useProxyConfigurationList has moved or been renamed');

    // Scan to the end of this hook only, so a sibling hook's gate is not credited
    // to it. The next `export function` is the boundary.
    const nextExport = src.indexOf('export function', idx + 1);
    const body = nextExport === -1 ? src.slice(idx) : src.slice(idx, nextExport);

    assert.match(
        body,
        /hasPermission\([^)]*'settings:read'\)/,
        'useProxyConfigurationList must consult settings:read — the route now answers 403 without it',
    );
    assert.match(
        body,
        /enabled:/,
        'the permission result must be wired into `enabled`, otherwise the query still fires',
    );
});
