import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { test } from 'node:test';
import { ROLE_PERMISSIONS, hasPermission, type Permission } from './permissions.ts';

/**
 * ROLE_PERMISSIONS duplicates internal/server/auth/permissions.go. A duplicate of
 * a fact drifts, and this one drifting is silent in both directions: grant the
 * frontend too much and a control reappears for a role that gets a 403 on click;
 * grant too little and a legitimate feature vanishes for a staff role with no
 * error to explain it. So the Go file is the source of truth and this parses it.
 */

const GO_FILE = path.resolve(
    import.meta.dirname,
    '../../../internal/server/auth/permissions.go',
);

/** Maps the Go constant identifiers to their string values, from the Go source. */
function parsePermissionConstants(src: string): Map<string, string> {
    const out = new Map<string, string>();
    const re = /(Perm[A-Za-z]+)\s+Permission\s*=\s*"([^"]+)"/g;
    for (const m of src.matchAll(re)) out.set(m[1], m[2]);
    return out;
}

/** Extracts one `var <name> = []Permission{ ... }` block's permission strings. */
function parseRoleBlock(
    src: string,
    varName: string,
    consts: Map<string, string>,
): string[] {
    const start = src.indexOf(`var ${varName} = []Permission{`);
    assert.notEqual(start, -1, `${varName} not found in permissions.go`);
    const open = src.indexOf('{', start);
    const close = src.indexOf('\n}', open);
    assert.notEqual(close, -1, `${varName} block is not terminated`);
    const body = src.slice(open + 1, close);

    const perms: string[] = [];
    for (const m of body.matchAll(/Perm[A-Za-z]+/g)) {
        const value = consts.get(m[0]);
        assert.ok(value, `${m[0]} used in ${varName} but has no constant`);
        perms.push(value);
    }
    return perms;
}

const ROLE_VARS: Record<string, string> = {
    admin: 'adminPermissions',
    editor: 'editorPermissions',
    viewer: 'viewerPermissions',
    user: 'userPermissions',
};

test('ROLE_PERMISSIONS matches the Go source exactly', () => {
    const src = readFileSync(GO_FILE, 'utf8');
    const consts = parsePermissionConstants(src);
    assert.ok(consts.size >= 17, `parsed only ${consts.size} permission constants`);

    // The premise: the parser found real data. Without this a broken regex would
    // yield empty sets on both sides and the comparison would pass vacuously.
    assert.deepEqual(
        Object.keys(ROLE_PERMISSIONS).sort(),
        Object.keys(ROLE_VARS).sort(),
        'the mirror and the role list disagree on which roles exist',
    );

    for (const [role, varName] of Object.entries(ROLE_VARS)) {
        const fromGo = parseRoleBlock(src, varName, consts).slice().sort();
        assert.ok(fromGo.length > 0, `${varName} parsed as empty`);
        const fromTs = [...ROLE_PERMISSIONS[role]].sort();
        assert.deepEqual(
            fromTs,
            fromGo,
            `role "${role}" drifted from ${varName} in permissions.go`,
        );
    }
});

test('every permission string in the mirror exists as a Go constant', () => {
    const src = readFileSync(GO_FILE, 'utf8');
    const known = new Set(parsePermissionConstants(src).values());
    for (const [role, perms] of Object.entries(ROLE_PERMISSIONS)) {
        for (const p of perms) {
            assert.ok(known.has(p), `role "${role}" claims unknown permission "${p}"`);
        }
    }
});

test('the end-customer role holds no settings permission', () => {
    // This is the invariant the settings gating leans on, and the reason the
    // customer-facing panels are picked by permission rather than by role name.
    assert.equal(hasPermission('user', 'settings:read'), false);
    assert.equal(hasPermission('user', 'settings:write'), false);
    // viewer is read-only staff and does hold settings:read -- an admin||editor
    // check would draw this line in the wrong place.
    assert.equal(hasPermission('viewer', 'settings:read'), true);
    assert.equal(hasPermission('viewer', 'settings:write'), false);
});

test('hasPermission denies unknown and missing roles', () => {
    assert.equal(hasPermission(undefined, 'stats:read'), false);
    assert.equal(hasPermission('', 'stats:read'), false);
    assert.equal(hasPermission('nonexistent', 'stats:read'), false);
    // A role that does hold it, so the above are not passing for a trivial reason.
    assert.equal(hasPermission('user', 'stats:read'), true);
});

test('hasPermission does not match on prefixes', () => {
    // "settings:read" must not satisfy a "settings:write" check.
    assert.equal(hasPermission('viewer', 'settings:write'), false);
    assert.equal(hasPermission('editor', 'settings:write'), true);
    assert.equal(hasPermission('user', 'apikeys:write'), true);
    assert.equal(hasPermission('viewer', 'apikeys:write' as Permission), false);
});
