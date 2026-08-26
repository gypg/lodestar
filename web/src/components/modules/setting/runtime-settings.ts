export interface RuntimeSettingField {
    key: string;
    labelKey: string;
    placeholderKey: string;
    hintKey?: string;
    min: string;
    max?: string;
}

/**
 * Parses the `max_expected_request_cost` input, mirroring the server-side
 * validator in internal/model/setting.go: a finite number >= 0, or null when the
 * input is unusable.
 *
 * Number('') and Number('  ') are both 0, so the blank check must come first or a
 * cleared box would read as "bound disabled". 'NaN' and 'Infinity' are rejected
 * for the reason the backend rejects them: a non-finite bound makes the admission
 * gate's `headroom <= inflight * bound` comparison constant-false, so it stops
 * refusing anyone — including accounts already in debt.
 */
export function parseOverdraftBound(raw: string): number | null {
    const trimmed = raw.trim();
    if (trimmed === '') return null;
    const parsed = Number(trimmed);
    if (!Number.isFinite(parsed) || parsed < 0) return null;
    return parsed;
}

export const RETRY_FIELDS: RuntimeSettingField[] = [
    {
        key: 'relay_retry_count',
        labelKey: 'retry.count.label',
        placeholderKey: 'retry.count.placeholder',
        min: '1',
    },
    {
        key: 'ratelimit_cooldown',
        labelKey: 'retry.ratelimitCooldown.label',
        placeholderKey: 'retry.ratelimitCooldown.placeholder',
        hintKey: 'retry.ratelimitCooldown.hint',
        min: '0',
    },
    {
        key: 'relay_max_total_attempts',
        labelKey: 'retry.maxTotalAttempts.label',
        placeholderKey: 'retry.maxTotalAttempts.placeholder',
        hintKey: 'retry.maxTotalAttempts.hint',
        min: '0',
    },
    {
        key: 'rate_limit_hold_interval',
        labelKey: 'retry.rateLimitHold.interval.label',
        placeholderKey: 'retry.rateLimitHold.interval.placeholder',
        hintKey: 'retry.rateLimitHold.interval.hint',
        min: '1',
    },
    {
        key: 'rate_limit_hold_max_wait',
        labelKey: 'retry.rateLimitHold.maxWait.label',
        placeholderKey: 'retry.rateLimitHold.maxWait.placeholder',
        hintKey: 'retry.rateLimitHold.maxWait.hint',
        min: '1',
    },
];

export const AUTO_STRATEGY_FIELDS: RuntimeSettingField[] = [
    {
        key: 'auto_strategy_min_samples',
        labelKey: 'autoStrategy.minSamples.label',
        placeholderKey: 'autoStrategy.minSamples.placeholder',
        hintKey: 'autoStrategy.minSamples.hint',
        min: '1',
    },
    {
        key: 'auto_strategy_time_window',
        labelKey: 'autoStrategy.timeWindow.label',
        placeholderKey: 'autoStrategy.timeWindow.placeholder',
        hintKey: 'autoStrategy.timeWindow.hint',
        min: '1',
    },
    {
        key: 'auto_strategy_sample_threshold',
        labelKey: 'autoStrategy.sampleThreshold.label',
        placeholderKey: 'autoStrategy.sampleThreshold.placeholder',
        hintKey: 'autoStrategy.sampleThreshold.hint',
        min: '1',
    },
    {
        key: 'auto_strategy_latency_weight',
        labelKey: 'autoStrategy.latencyWeight.label',
        placeholderKey: 'autoStrategy.latencyWeight.placeholder',
        hintKey: 'autoStrategy.latencyWeight.hint',
        min: '0',
        max: '100',
    },
];
