/**
 * WO-029 T4-T5: chunk-load errors must recover via full page reload; plain
 * errors must keep using Next.js reset().
 *
 * The production components (app/error.tsx, app/global-error.tsx) both call
 * runRecovery from src/lib/chunk-error.ts, so testing that function tests the
 * real decision path without a DOM. window.location.reload is swapped for a
 * spy via the injected action, mirroring how the component wires
 * `() => window.location.reload()`.
 */
import test from 'node:test';
import assert from 'node:assert/strict';

import { isChunkLoadError, runRecovery, recoveryActionFor } from './chunk-error.ts';

function makeChunkLoadError(): Error {
    // Mirrors webpack's runtime: error subclass named ChunkLoadError with the
    // classic "Loading chunk" message.
    const error = new Error('Loading chunk 5 failed.\n(error: https://app.example.com/5.js)');
    error.name = 'ChunkLoadError';
    return error;
}

test('T4: a chunk-load error recovers via reload, not reset', () => {
    let resets = 0;
    let reloads = 0;
    runRecovery(
        makeChunkLoadError(),
        () => {
            resets += 1;
        },
        () => {
            reloads += 1;
        },
    );
    assert.equal(reloads, 1, 'T4: chunk errors must trigger a full page reload');
    assert.equal(resets, 0, 'T4: chunk errors must NOT call reset — it can never succeed for them');
});

test('T4: every known chunk-error wording routes to reload', () => {
    const variants: Array<{ name: string; message: string }> = [
        { name: 'ChunkLoadError', message: 'Loading chunk 5 failed' },
        { name: 'ChunkLoadError', message: 'Loading CSS chunk 12 failed' },
        { name: 'TypeError', message: 'Failed to fetch dynamically imported module: /app/page-abc.js' },
        { name: 'Error', message: 'error loading dynamically imported module https://x/y.js' },
        { name: 'Error', message: 'Importing a module script failed.' },
    ];
    for (const variant of variants) {
        assert.equal(
            recoveryActionFor(variant),
            'reload',
            `T4: variant ${variant.name}: ${variant.message} must recover via reload (bundlers/browsers word this differently; one signature is not enough)`,
        );
    }
});

test('T5: a plain render error keeps reset() and never reloads', () => {
    let resets = 0;
    let reloads = 0;
    runRecovery(
        new Error('Cannot read properties of undefined (reading map)'),
        () => {
            resets += 1;
        },
        () => {
            reloads += 1;
        },
    );
    assert.equal(resets, 1, 'T5: plain errors must still use reset()');
    assert.equal(reloads, 0, 'T5: plain errors must NEVER reload — a full reload discards unsaved form state');
});

test('isChunkLoadError: negative guards', () => {
    assert.equal(isChunkLoadError(new Error('Network timeout')), false, 'network errors are not chunk errors');
    assert.equal(isChunkLoadError(null), false, 'null input must not throw or match');
    assert.equal(isChunkLoadError(undefined), false, 'undefined input must not throw or match');
    assert.equal(isChunkLoadError('Loading chunk 7 failed'), false, 'bare strings are not Error instances');
});
