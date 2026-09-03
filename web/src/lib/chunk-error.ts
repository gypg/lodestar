/**
 * WO-029 defect 2: chunk-load failures cannot be fixed by re-rendering the
 * same JS (Next.js `reset`) — after a deploy the old chunk URL no longer
 * exists on the server, so the only working recovery is a full page reload
 * that fetches fresh HTML with the new build manifest. The recovery decision
 * lives here so error.tsx and global-error.tsx cannot drift apart.
 *
 * Different bundlers/browsers word this failure differently; matching one
 * signature only would miss real chunk errors. Signatures are lowercased;
 * inputs are matched case-insensitively.
 */
const CHUNK_ERROR_SIGNATURES = [
    'loading chunk', // webpack: "Loading chunk 5 failed"
    'loading css chunk', // webpack mini-css-extract: "Loading CSS chunk 5 failed"
    'failed to fetch dynamically imported module', // Chrome/Edge native ESM (also Vite)
    'error loading dynamically imported module', // Firefox native ESM
    'importing a module script failed', // Safari native ESM
];

export function isChunkLoadError(error: unknown): boolean {
    if (!error || typeof error !== 'object') {
        return false;
    }
    const name = String((error as { name?: unknown }).name ?? '').toLowerCase();
    const message = String((error as { message?: unknown }).message ?? '').toLowerCase();
    // webpack names the error class ChunkLoadError; some minified stacks only
    // mention the class name inside the message.
    if (name === 'chunkloaderror' || message.includes('chunkloaderror')) {
        return true;
    }
    return CHUNK_ERROR_SIGNATURES.some((signature) => message.includes(signature));
}

export type RecoveryAction = 'reset' | 'reload';

/**
 * Which recovery a given error needs: 'reload' for chunk-load failures,
 * 'reset' for everything else. Plain render errors must stay on reset() —
 * a full reload would discard unsaved form state for errors reset() can fix.
 */
export function recoveryActionFor(error: unknown): RecoveryAction {
    return isChunkLoadError(error) ? 'reload' : 'reset';
}

/**
 * Executes the recovery with injected actions so tests can spy on which
 * branch ran without a DOM.
 */
export function runRecovery(error: unknown, reset: () => void, reload: () => void): void {
    if (recoveryActionFor(error) === 'reload') {
        reload();
        return;
    }
    reset();
}
