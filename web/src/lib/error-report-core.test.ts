import test from 'node:test';
import assert from 'node:assert/strict';

import { ErrorReportDedupe, DEDUP_WINDOW_MS, DEDUP_MAX_ENTRIES } from './error-report-core.ts';

test('same message and stack is deduplicated within the window', () => {
    const d = new ErrorReportDedupe();
    assert.equal(d.isDuplicate('boom', 'at a.js:1:1'), false);
    assert.equal(d.isDuplicate('boom', 'at a.js:1:1'), true);
    assert.equal(d.isDuplicate('boom', 'at a.js:1:1'), true);
});

test('different message or stack is not deduplicated', () => {
    const d = new ErrorReportDedupe();
    assert.equal(d.isDuplicate('boom', 'at a.js:1:1'), false);
    // 不同 message。
    assert.equal(d.isDuplicate('other', 'at a.js:1:1'), false);
    // 不同 stack（去重键含堆栈前 200 字符）。
    assert.equal(d.isDuplicate('boom', 'at b.js:2:2'), false);
    // 缺省 stack 独立成键：首次视为新错误，再次才判重。
    assert.equal(d.isDuplicate('boom'), false);
    assert.equal(d.isDuplicate('boom'), true);
});

test('stack key only uses the first 200 characters', () => {
    const d = new ErrorReportDedupe();
    const longA = 'x'.repeat(300) + 'A';
    const longB = 'x'.repeat(300) + 'B';
    // 前 200 字符相同 ⇒ 视为同一错误。
    assert.equal(d.isDuplicate('boom', longA), false);
    assert.equal(d.isDuplicate('boom', longB), true);
});

test('entries expire after the window elapses', () => {
    const d = new ErrorReportDedupe();
    const realNow = Date.now;
    let now = realNow();
    Date.now = () => now;
    try {
        assert.equal(d.isDuplicate('boom'), false);
        now += DEDUP_WINDOW_MS + 1;
        assert.equal(d.isDuplicate('boom'), false, 'expired entry should be reported again');
    } finally {
        Date.now = realNow;
    }
});

test('dedupe table is capped to prevent unbounded growth', () => {
    const d = new ErrorReportDedupe();
    for (let i = 0; i < DEDUP_MAX_ENTRIES + 50; i++) {
        d.isDuplicate(`error-${i}`);
    }
    // 容量约束在 prune 内部执行；重复调用不应抛错，且最早的条目已被淘汰
    // （error-0 重新视为新错误说明它已被逐出）。
    assert.equal(d.isDuplicate('error-0'), false);
});

test('reset clears all dedupe state', () => {
    const d = new ErrorReportDedupe();
    assert.equal(d.isDuplicate('boom'), false);
    d.reset();
    assert.equal(d.isDuplicate('boom'), false, 'after reset the same error reports again');
});
