import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { analyze } = require('./i18n-keys.cjs');

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '..');
const localeDir = path.join(webRoot, 'public', 'locale');
const localeFiles = ['en.json', 'zh_hans.json', 'zh_hant.json'];

function readJson(filePath) {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

function collectKeys(value, prefix = '', keys = new Set()) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return keys;
    }

    for (const [key, child] of Object.entries(value)) {
        const next = prefix ? `${prefix}.${key}` : key;
        keys.add(next);
        collectKeys(child, next, keys);
    }

    return keys;
}

function assertLocaleParity() {
    const [baseName, ...restNames] = localeFiles;
    const base = collectKeys(readJson(path.join(localeDir, baseName)));

    for (const name of restNames) {
        const current = collectKeys(readJson(path.join(localeDir, name)));
        const missing = [...base].filter((key) => !current.has(key));
        const extra = [...current].filter((key) => !base.has(key));
        assert.deepEqual(missing, [], `${name} missing locale keys:\n${missing.join('\n')}`);
        assert.deepEqual(extra, [], `${name} has extra locale keys:\n${extra.join('\n')}`);
    }
}

function assertNoHardcodedCopy(relativePath, forbiddenSnippets) {
    const content = fs.readFileSync(path.join(webRoot, relativePath), 'utf8');
    for (const snippet of forbiddenSnippets) {
        assert.equal(
            content.includes(snippet),
            false,
            `${relativePath} still contains hardcoded copy: ${snippet}`,
        );
    }
}

/**
 * Reconciles every statically resolvable `t('key')` call in src/ against the
 * locale files. Locale parity alone cannot catch a key that is missing from all
 * three files at once — which is exactly how ~385 keys went unnoticed until the
 * UI started rendering raw key paths.
 */
function assertUsedKeysExist() {
    const { missing, notLeaf } = analyze();

    if (missing.length > 0) {
        const byKey = new Map();
        for (const entry of missing) {
            if (!byKey.has(entry.key)) byKey.set(entry.key, { locales: [], usage: entry.usages[0] });
            byKey.get(entry.key).locales.push(entry.locale);
        }
        const lines = [...byKey.entries()].map(
            ([key, { locales, usage }]) =>
                `  ${key}\n      used at ${usage.file}:${usage.line}\n      missing from ${locales.join(', ')}`,
        );
        assert.fail(
            `${byKey.size} translation key(s) used in source are missing from locale files:\n${lines.join('\n')}`,
        );
    }

    if (notLeaf.length > 0) {
        const byKey = new Map();
        for (const entry of notLeaf) {
            if (!byKey.has(entry.key)) byKey.set(entry.key, entry.usages);
        }
        const lines = [...byKey.entries()].map(
            ([key, usages]) =>
                `  ${key} resolves to an object, not a string\n      used at ${usages
                    .map((u) => `${u.file}:${u.line}`)
                    .join(', ')}\n      (did you mean ${key}.title?)`,
        );
        assert.fail(`${byKey.size} translation key(s) do not resolve to a string:\n${lines.join('\n')}`);
    }
}

function run() {
    assertLocaleParity();
    assertUsedKeysExist();
    const en = readJson(path.join(localeDir, 'en.json'));
    assert.equal(en.login?.welcome, 'Welcome back', 'en.json should define login.welcome');
    assertNoHardcodedCopy('src/components/modules/group/Editor.tsx', [
        'API 分类',
        'Condition (JSON)',
        'aria-label="search"',
    ]);
    assertNoHardcodedCopy('src/components/modules/channel/Form.tsx', [
        'title="Remove"',
    ]);
    assertNoHardcodedCopy('src/components/modules/channel/templates.ts', [
        "description: '",
    ]);
}

run();
console.log('i18n checks passed');
