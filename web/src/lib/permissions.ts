/**
 * Frontend mirror of internal/server/auth/permissions.go.
 *
 * The backend already refuses what a role may not do. This mirror exists so the
 * UI does not *offer* it in the first place: a control that renders, accepts a
 * click and then reports "insufficient permission" reads as a broken product,
 * and on a paying customer's screen it reads as a broken paid product. Admin
 * affordances shown to an end customer also leak how the system is operated.
 *
 * This is a duplicate of a backend fact, so it can drift. permissions.test.ts
 * parses the Go file and fails when the two disagree — do not hand-edit one side
 * without running it.
 *
 * Gating on permissions rather than on a role name is deliberate: `viewer` is
 * read-only STAFF and does hold settings:read, so an isStaffRole() style check
 * (admin || editor) draws the line in the wrong place for anything settings-shaped.
 */

export type Permission =
    | 'channels:read'
    | 'channels:write'
    | 'groups:read'
    | 'groups:write'
    | 'apikeys:read'
    | 'apikeys:write'
    | 'settings:read'
    | 'settings:write'
    | 'logs:read'
    | 'logs:write'
    | 'stats:read'
    | 'users:read'
    | 'users:write'
    | 'sites:read'
    | 'sites:write'
    | 'subscriptions:read'
    | 'subscriptions:write';

export const ROLE_PERMISSIONS: Record<string, readonly Permission[]> = {
    admin: [
        'channels:read', 'channels:write',
        'groups:read', 'groups:write',
        'apikeys:read', 'apikeys:write',
        'settings:read', 'settings:write',
        'logs:read', 'logs:write',
        'stats:read',
        'users:read', 'users:write',
        'sites:read', 'sites:write',
        'subscriptions:read', 'subscriptions:write',
    ],
    editor: [
        'channels:read', 'channels:write',
        'groups:read', 'groups:write',
        'apikeys:read', 'apikeys:write',
        'settings:read', 'settings:write',
        'logs:read', 'logs:write',
        'stats:read',
        'sites:read', 'sites:write',
        'subscriptions:read', 'subscriptions:write',
    ],
    viewer: [
        'channels:read',
        'groups:read',
        'apikeys:read',
        'settings:read',
        'logs:read',
        'stats:read',
        'sites:read',
        'subscriptions:read',
    ],
    // Commercial end customer. Deliberately no settings:read — that list carries
    // secrets such as epay_key and stripe_api_key.
    user: [
        'apikeys:read', 'apikeys:write',
        'stats:read',
        'subscriptions:read',
    ],
};

/** Mirrors auth.HasPermission: an unknown role has nothing. */
export function hasPermission(role: string | undefined, perm: Permission): boolean {
    if (!role) return false;
    return ROLE_PERMISSIONS[role]?.includes(perm) ?? false;
}
