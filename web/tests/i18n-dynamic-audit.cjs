/* eslint-disable @typescript-eslint/no-require-imports */
/**
 * Audits the dynamic-key blind spot: for every `t(`prefix.${expr}`)` call, checks
 * that the static prefix resolves to an object in each locale. A missing prefix
 * means the whole family of keys is absent; a present one still can't prove every
 * individual member exists, so those are listed for manual review.
 */
const fs = require('node:fs');
const path = require('node:path');
const { analyze, LOCALE_FILES, localeDir } = require('./i18n-keys.cjs');

function resolve(obj, dotted) {
    return dotted.split('.').reduce((cur, seg) => (cur && typeof cur === 'object' ? cur[seg] : undefined), obj);
}

const locales = LOCALE_FILES.map((f) => ({
    fileName: f,
    data: JSON.parse(fs.readFileSync(path.join(localeDir, f), 'utf8')),
}));

const result = analyze();

const rows = [];
for (const dynamic of result.dynamic) {
    // Template literal beginning with a static prefix, e.g. `range.${option}`
    const match = /^`([^`$]*)\$\{/.exec(dynamic.text);
    if (!match) {
        rows.push({ ...dynamic, kind: 'opaque', prefix: null });
        continue;
    }
    rows.push({ ...dynamic, kind: 'prefixed', prefix: match[1].replace(/\.$/, '') });
}

console.log(`dynamic call sites: ${rows.length}`);
console.log(`  with static prefix: ${rows.filter((r) => r.kind === 'prefixed').length}`);
console.log(`  fully opaque:       ${rows.filter((r) => r.kind === 'opaque').length}`);
console.log('');

const seen = new Set();
console.log('=== prefixed dynamic families ===');
for (const row of rows.filter((r) => r.kind === 'prefixed')) {
    // namespace comes straight from the resolved translator binding
    const full = [row.namespace, row.prefix].filter(Boolean).join('.');
    if (seen.has(full)) continue;
    seen.add(full);

    const verdicts = locales.map(({ fileName, data }) => {
        const value = resolve(data, full);
        if (value === undefined) return `${fileName}:ABSENT`;
        if (typeof value !== 'object') return `${fileName}:NOT-OBJECT`;
        return `${fileName}:${Object.keys(value).length}keys`;
    });
    const bad = verdicts.some((v) => v.includes('ABSENT') || v.includes('NOT-OBJECT'));
    console.log(`${bad ? '!! ' : '   '}${full}   [${verdicts.join(' ')}]   ${row.file}:${row.line}`);
}

console.log('');
console.log('=== opaque dynamic call sites (need manual review) ===');
for (const row of rows.filter((r) => r.kind === 'opaque')) {
    console.log(`   ${row.file}:${row.line}  ${row.translator}(${row.text.replace(/\s+/g, ' ').slice(0, 80)})`);
}
