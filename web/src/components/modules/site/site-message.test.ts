import test from 'node:test';
import assert from 'node:assert/strict';

import { translateSiteMessage } from './site-message.ts';

/**
 * These keys are NOT present in any locale file — that is the whole point of the
 * built-in trilingual fallback in site-message.ts. next-intl returns the *full*
 * key path (namespace prefix included) when a key is missing, so a miss check
 * written as `translated !== matched.key` silently passes for any namespaced
 * translator and leaks the key path into the toast.
 */
const MISSING_KEY = 'siteImport.errors.invalidJson';
const BACKEND_MESSAGE = 'site import invalid json';

/** Mirrors a root translator: useTranslations() -> miss returns the bare key. */
function rootTranslatorMiss(key: string) {
    return key;
}

/** Mirrors SiteAutomation.tsx: useTranslations('setting') -> miss is prefixed. */
function settingTranslatorMiss(key: string) {
    return `setting.${key}`;
}

/** Mirrors a translator that actually has the entry. */
function hitTranslator() {
    return '导入内容不是有效的 JSON。';
}

test('namespaced translator miss falls back to built-in copy instead of leaking the key path', () => {
    const out = translateSiteMessage('zh-Hans', BACKEND_MESSAGE, settingTranslatorMiss);

    assert.equal(
        out,
        '导入内容不是有效的 JSON，请检查文件格式或粘贴内容。',
        'a prefixed miss must fall through to fallbackTranslate',
    );
    assert.ok(!out.includes(MISSING_KEY), `must not leak key path, got: ${out}`);
    assert.ok(!out.includes('setting.'), `must not leak namespace prefix, got: ${out}`);
});

test('root translator miss also falls back to built-in copy', () => {
    const out = translateSiteMessage('zh-Hans', BACKEND_MESSAGE, rootTranslatorMiss);

    assert.equal(out, '导入内容不是有效的 JSON，请检查文件格式或粘贴内容。');
    assert.ok(!out.includes(MISSING_KEY), `must not leak key path, got: ${out}`);
});

test('namespaced miss falls back per locale, not just zh-Hans', () => {
    assert.equal(
        translateSiteMessage('en', BACKEND_MESSAGE, settingTranslatorMiss),
        'The import content is not valid JSON. Check the file format or pasted content.',
    );
    assert.equal(
        translateSiteMessage('zh-Hant', BACKEND_MESSAGE, settingTranslatorMiss),
        '匯入內容不是有效的 JSON，請檢查檔案格式或貼上的內容。',
    );
});

test('a translator that resolves the key still wins over the built-in fallback', () => {
    assert.equal(
        translateSiteMessage('zh-Hans', BACKEND_MESSAGE, hitTranslator),
        '导入内容不是有效的 JSON。',
    );
});

test('interpolated messages fall back with values applied, not with raw placeholders', () => {
    const out = translateSiteMessage(
        'zh-Hans',
        'site sync requires a key for group "vip"; create a key for that group on the site and sync again',
        settingTranslatorMiss,
    );

    assert.equal(
        out,
        '分组「vip」没有可用的 Key。请先到站点创建这个分组的 Key，再重新同步。',
    );
    assert.ok(!out.includes('{groupKey}'), `placeholder must be interpolated, got: ${out}`);
});

test('size-limit message keeps the backend-provided limit in the fallback copy', () => {
    const out = translateSiteMessage(
        'zh-Hans',
        'site import payload exceeds 64 MiB limit',
        settingTranslatorMiss,
    );

    assert.equal(out, '导入文件超过 64 MiB 上限，请拆分导出文件或分批导入。');
});

test('unmatched backend messages pass through untouched', () => {
    assert.equal(
        translateSiteMessage('zh-Hans', 'some upstream 502', settingTranslatorMiss),
        'some upstream 502',
    );
    assert.equal(translateSiteMessage('zh-Hans', '', settingTranslatorMiss), '');
    assert.equal(translateSiteMessage('zh-Hans', null, settingTranslatorMiss), '');
});

test('works with no translator at all (fallback-only path)', () => {
    assert.equal(
        translateSiteMessage('zh-Hans', BACKEND_MESSAGE),
        '导入内容不是有效的 JSON，请检查文件格式或粘贴内容。',
    );
});
