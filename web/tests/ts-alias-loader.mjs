// Node ESM resolve hook for the node --test suite (WO-029 test infra).
//
// The production source targets the Next bundler, which supports tsconfig
// path aliases (`@/`), extensionless relative imports, and extensionless JSON
// imports. Node's built-in type stripping supports none of those, so the
// logout-cache tests (which must exercise the REAL user.ts -> client.ts ->
// i18n-runtime.ts chain) need this resolver:
//   1. `.json` imports get the `{ type: 'json' }` import attribute injected
//      (node requires it; the bundler does not).
//   2. On ERR_MODULE_NOT_FOUND only: `@/foo` probes src/foo(.ts|/index.ts),
//      extensionless relative specifiers probe (.ts|/index.ts).
// Everything else — bare packages, explicit extensions — goes to the default
// resolver first, so existing test files are unaffected.
import { existsSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

const SRC_DIR = path.resolve(import.meta.dirname, '..', 'src');

function isFileUrl(url) {
    return typeof url === 'string' && url.startsWith('file:');
}

function probeTs(basePath) {
    const candidates = [`${basePath}.ts`, path.join(basePath, 'index.ts')];
    for (const candidate of candidates) {
        if (existsSync(candidate)) {
            return pathToFileURL(candidate).href;
        }
    }
    return null;
}

export async function resolve(specifier, context, nextResolve) {
    if (specifier.endsWith('.json') && isFileUrl(context.parentURL)) {
        return {
            url: new URL(specifier, context.parentURL).href,
            shortCircuit: true,
            format: 'json',
            importAttributes: { type: 'json' },
        };
    }

    try {
        return await nextResolve(specifier, context);
    } catch (error) {
        if (error?.code !== 'ERR_MODULE_NOT_FOUND') {
            throw error;
        }
    }

    if (specifier.startsWith('@/')) {
        const url = probeTs(path.join(SRC_DIR, specifier.slice(2)));
        if (url) {
            return { url, shortCircuit: true };
        }
    }

    if ((specifier.startsWith('./') || specifier.startsWith('../')) && isFileUrl(context.parentURL)) {
        const parentDir = path.dirname(fileURLToPath(context.parentURL));
        const url = probeTs(path.resolve(parentDir, specifier));
        if (url) {
            return { url, shortCircuit: true };
        }
    }

    throw new Error(`ts-alias-loader: cannot resolve '${specifier}' from '${context.parentURL ?? ''}'`, { cause: error });
}
