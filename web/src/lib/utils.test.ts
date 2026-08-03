import test from 'node:test';
import assert from 'node:assert/strict';

import { significantDecimalPlaces } from './utils.ts';

test('significantDecimalPlaces trims trailing zeros for integer-like counts', () => {
    assert.equal(significantDecimalPlaces('5.00'), 0);
    assert.equal(significantDecimalPlaces('0.00'), 0);
});

test('significantDecimalPlaces keeps one decimal when only the second digit is zero', () => {
    assert.equal(significantDecimalPlaces('1.50'), 1);
    assert.equal(significantDecimalPlaces('12.30'), 1);
});

test('significantDecimalPlaces keeps two decimals when both are meaningful', () => {
    assert.equal(significantDecimalPlaces('1.23'), 2);
    assert.equal(significantDecimalPlaces('95.05'), 2);
});

test('significantDecimalPlaces returns 0 for integers and non-strings', () => {
    assert.equal(significantDecimalPlaces('5'), 0);
    assert.equal(significantDecimalPlaces('123'), 0);
    assert.equal(significantDecimalPlaces(5), 0);
    assert.equal(significantDecimalPlaces(undefined), 0);
});
