/* eslint-disable @typescript-eslint/no-require-imports */
/**
 * Merges translation additions into the three locale files.
 *
 * Usage: node tests/../scripts/merge-locale.cjs <additions.json>
 *
 * The additions file maps locale file name -> nested object to deep-merge.
 * Existing values are never overwritten; the script reports any collision so a
 * typo in a key path can't silently replace copy that is already correct.
 */
const fs = require('node:fs');
const path = require('node:path');

const localeDir = path.join(__dirname, '..', 'public', 'locale');

function deepMerge(target, source, prefix, collisions) {
    for (const [key, value] of Object.entries(source)) {
        const dotted = prefix ? `${prefix}.${key}` : key;
        const isObject = value && typeof value === 'object' && !Array.isArray(value);

        if (!(key in target)) {
            target[key] = isObject ? {} : value;
            if (isObject) deepMerge(target[key], value, dotted, collisions);
            continue;
        }

        const existing = target[key];
        const existingIsObject = existing && typeof existing === 'object' && !Array.isArray(existing);

        if (isObject && existingIsObject) {
            deepMerge(existing, value, dotted, collisions);
        } else if (isObject !== existingIsObject) {
            collisions.push(`${dotted}: type mismatch (existing ${existingIsObject ? 'object' : 'string'})`);
        } else if (existing !== value) {
            collisions.push(`${dotted}: already set to ${JSON.stringify(existing)}`);
        }
    }
}

const additionsPath = process.argv[2];
if (!additionsPath) {
    console.error('usage: merge-locale.cjs <additions.json>');
    process.exit(1);
}

const additions = JSON.parse(fs.readFileSync(additionsPath, 'utf8'));
let totalCollisions = 0;

for (const [fileName, tree] of Object.entries(additions)) {
    const filePath = path.join(localeDir, fileName);
    const data = JSON.parse(fs.readFileSync(filePath, 'utf8'));
    const collisions = [];
    deepMerge(data, tree, '', collisions);

    if (collisions.length > 0) {
        totalCollisions += collisions.length;
        console.error(`${fileName}: ${collisions.length} collision(s)`);
        for (const c of collisions) console.error(`   ${c}`);
    }

    fs.writeFileSync(filePath, `${JSON.stringify(data, null, 2)}\n`, 'utf8');
    console.log(`${fileName}: merged`);
}

if (totalCollisions > 0) {
    console.error(`\n${totalCollisions} collision(s) — existing values were kept, review the additions file.`);
    process.exit(1);
}
