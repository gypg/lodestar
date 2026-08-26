import assert from 'node:assert/strict';
import test from 'node:test';

import { AUTO_STRATEGY_FIELDS, RETRY_FIELDS, parseOverdraftBound } from './runtime-settings.ts';

test('retry fields expose cooldown and total-attempt controls', () => {
    assert.deepEqual(
        RETRY_FIELDS.map((field) => field.key),
        [
            'relay_retry_count',
            'ratelimit_cooldown',
            'relay_max_total_attempts',
            'rate_limit_hold_interval',
            'rate_limit_hold_max_wait',
        ]
    );
});

test('rate limit hold fields require positive intervals', () => {
    for (const key of ['rate_limit_hold_interval', 'rate_limit_hold_max_wait']) {
        const field = RETRY_FIELDS.find((item) => item.key === key);

        assert.ok(field, `${key} must be exposed in the retry panel`);
        assert.equal(field.min, '1', `${key} must not accept 0 or negative values`);
    }
});

test('auto strategy fields expose latency weight with bounded range', () => {
    const latencyWeight = AUTO_STRATEGY_FIELDS.find((field) => field.key === 'auto_strategy_latency_weight');

    assert.ok(latencyWeight);
    assert.equal(latencyWeight.min, '0');
    assert.equal(latencyWeight.max, '100');
});

test('overdraft bound accepts finite non-negative numbers', () => {
    assert.equal(parseOverdraftBound('0.5'), 0.5);
    assert.equal(parseOverdraftBound('0'), 0);
    assert.equal(parseOverdraftBound('2'), 2);
    assert.equal(parseOverdraftBound(' 1.25 '), 1.25);
    assert.equal(parseOverdraftBound('1000'), 1000);
});

test('overdraft bound rejects blank input rather than reading it as zero', () => {
    // Number('') and Number('   ') are both 0. Returning 0 here would render the
    // "bound disabled" warning for an empty box and, worse, offer to save a value
    // that switches the concurrency bound off.
    assert.equal(parseOverdraftBound(''), null);
    assert.equal(parseOverdraftBound('   '), null);
});

test('overdraft bound rejects non-finite values that would neuter the gate', () => {
    // These parse successfully in both JS and Go. Stored, they make the admission
    // gate's `headroom <= inflight * bound` comparison constant-false, so it stops
    // refusing accounts that owe money — see internal/op/billing/inflight.go.
    for (const raw of ['NaN', 'nan', 'Infinity', '-Infinity', 'Inf']) {
        assert.equal(parseOverdraftBound(raw), null, `${raw} must not be accepted as a bound`);
    }
});

test('overdraft bound rejects negatives and garbage', () => {
    for (const raw of ['-1', '-0.01', 'abc', '1,5', '1.2.3']) {
        assert.equal(parseOverdraftBound(raw), null, `${raw} must not be accepted as a bound`);
    }
});
