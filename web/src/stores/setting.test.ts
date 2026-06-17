import assert from 'node:assert/strict';
import test from 'node:test';

import { normalizeLocale, normalizeTimeZone } from './setting.ts';

test('normalizeLocale keeps existing locale normalization behavior', () => {
    assert.equal(normalizeLocale('zh_CN'), 'zh-Hans');
    assert.equal(normalizeLocale('zh-HK'), 'zh-Hant');
    assert.equal(normalizeLocale('en-US'), 'en');
    assert.equal(normalizeLocale('unknown'), 'zh-Hans');
});

test('normalizeTimeZone defaults to Asia/Shanghai when value is missing or invalid', () => {
    assert.equal(normalizeTimeZone(undefined), 'Asia/Shanghai');
    assert.equal(normalizeTimeZone(''), 'Asia/Shanghai');
    assert.equal(normalizeTimeZone('Mars/OlympusMons'), 'Asia/Shanghai');
});

test('normalizeTimeZone preserves valid IANA time zones', () => {
    assert.equal(normalizeTimeZone('Asia/Shanghai'), 'Asia/Shanghai');
    assert.equal(normalizeTimeZone('America/Los_Angeles'), 'America/Los_Angeles');
    assert.equal(normalizeTimeZone('Europe/London'), 'Europe/London');
});
