import assert from 'node:assert/strict';
import test from 'node:test';

import { AUTO_STRATEGY_FIELDS, RETRY_FIELDS } from './runtime-settings.ts';

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
