import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// SiteChannelCompletionAction was exported and fully implemented but never
// mounted anywhere in the app. That made the "待补全" (masked source key) state a
// dead end: cards displayed it, projection refused to build a channel from a
// masked key, and no reachable control could replace the value. Nothing failed
// loudly -- the export kept it looking wired.
//
// Asserted against source text rather than a render, because this package has no
// React test environment. What matters is that the component is *used as JSX*
// somewhere, not merely exported.
const source = readFileSync(join(import.meta.dirname, 'index.tsx'), 'utf8');

test('SiteChannelCompletionAction is rendered, not just exported', () => {
    assert.match(
        source,
        /<SiteChannelCompletionAction\s*\/>/,
        'the completion action must be mounted as JSX; exporting it alone leaves masked keys unresolvable',
    );
});

test('SiteChannelCompletionAction is mounted inside the section that is actually imported', () => {
    // remote-site imports SiteChannelSection only. Mounting the action outside
    // that component would re-orphan it while still satisfying the check above.
    const sectionStart = source.indexOf('export function SiteChannelSection');
    assert.ok(sectionStart > 0, 'SiteChannelSection must exist');

    const mountIndex = source.indexOf('<SiteChannelCompletionAction');
    assert.ok(mountIndex > 0, 'the completion action must be mounted');
    assert.ok(
        mountIndex > sectionStart,
        'the mount must live inside SiteChannelSection, the only component remote-site imports',
    );
});
