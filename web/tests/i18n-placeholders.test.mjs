import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { placeholdersIn } = require('./i18n-keys.cjs');

const args = (message) => [...placeholdersIn(message)].sort();

test('simple placeholder', () => {
    assert.deepEqual(args('Hello {name}'), ['name']);
});

test('multiple placeholders', () => {
    assert.deepEqual(args('{a} and {b}'), ['a', 'b']);
});

test('no placeholders', () => {
    assert.deepEqual(args('Plain message'), []);
});

test('placeholder with surrounding whitespace', () => {
    assert.deepEqual(args('Hello { name }'), ['name']);
});

// The reason this module exists: branch bodies are nested messages, not args.
test('plural argument does not leak branch text', () => {
    assert.deepEqual(
        args('{count, plural, =0 {No models} one {# model} other {# models}}'),
        ['count'],
    );
});

test('plural with CJK branch text', () => {
    assert.deepEqual(args('{count, plural, =0 {暂无分组} other {# 个分组}}'), ['count']);
});

test('select argument does not leak branch text', () => {
    assert.deepEqual(
        args('{status, select, ok {All good} fail {It broke} other {Unknown}}'),
        ['status'],
    );
});

test('placeholder after a plural argument is still found', () => {
    assert.deepEqual(
        args('{count, plural, other {# items}} for {siteName}'),
        ['count', 'siteName'],
    );
});

test('nested placeholder inside a plural branch counts as an argument', () => {
    // `name` really is substituted here, so it must be reported.
    assert.deepEqual(
        args('{count, plural, other {# items for {name}}}'),
        ['count', 'name'],
    );
});

test('unmatched brace does not throw', () => {
    assert.deepEqual(args('broken {'), []);
});

test('non-identifier braces are ignored', () => {
    assert.deepEqual(args('JSON like {"a":1} stays literal'), []);
});
