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

test('the end-customer role lacks every permission these queries need', () => {
    // The premise the three assertions above rest on. If this ever flips, the
    // gates are not wrong -- but they stop being about this role.
    assert.equal(hasPermission('user', 'settings:read'), false);
    assert.equal(hasPermission('user', 'subscriptions:write'), false);
    // Held, so the checks above are not passing because 'user' has nothing at all.
    assert.equal(hasPermission('user', 'subscriptions:read'), true);
    assert.equal(hasPermission('user', 'apikeys:read'), true);
});
