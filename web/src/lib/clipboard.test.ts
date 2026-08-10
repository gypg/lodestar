import test from 'node:test';
import assert from 'node:assert/strict';

import { writeClipboardText, type ClipboardLike, type DocumentLike } from './clipboard.ts';

/**
 * 桩 textarea 只实现 fallbackCopyText 真正用到的成员，不是完整 HTMLElement。
 * 之前这里另声明了一套同名 DocumentLike，与源类型结构不兼容（TS2719）；
 * 现在直接复用导出的源类型，仅在传参处做一次窄化断言。
 */
type TextAreaStub = {
    value: string;
    style: Record<string, string>;
    setAttribute: (name: string, value: string) => void;
    select: () => void;
};

type DocumentStub = {
    body: {
        appendChild: (node: TextAreaStub) => void;
        removeChild: (node: TextAreaStub) => void;
    };
    createElement: (tag: string) => TextAreaStub;
    execCommand: (command: string) => boolean;
};

const asDocumentLike = (stub: DocumentStub): DocumentLike => stub as unknown as DocumentLike;

test('writeClipboardText falls back to execCommand when clipboard permission is denied', async () => {
    const appended: TextAreaStub[] = [];
    const removed: TextAreaStub[] = [];
    let selected = false;

    const documentLike: DocumentStub = {
        body: {
            appendChild: (node) => appended.push(node),
            removeChild: (node) => removed.push(node),
        },
        createElement: (tag) => {
            assert.equal(tag, 'textarea');
            return {
                value: '',
                style: {},
                setAttribute: () => {},
                select: () => {
                    selected = true;
                },
            };
        },
        execCommand: (command) => {
            assert.equal(command, 'copy');
            return true;
        },
    };

    const clipboardLike: ClipboardLike = {
        writeText: async () => {
            throw new Error(`Failed to execute 'writeText' on 'Clipboard': Write permission denied.`);
        },
    };

    await assert.doesNotReject(() => writeClipboardText('sk-lodestar-test', {
        clipboard: clipboardLike,
        document: asDocumentLike(documentLike),
    }));
    assert.equal(appended.length, 1);
    assert.equal(removed.length, 1);
    assert.equal(appended[0], removed[0]);
    assert.equal(appended[0].value, 'sk-lodestar-test');
    assert.equal(selected, true);
});

test('writeClipboardText surfaces the original clipboard error when no fallback is available', async () => {
    const expected = new Error(`Failed to execute 'writeText' on 'Clipboard': Write permission denied.`);
    const clipboardLike: ClipboardLike = {
        writeText: async () => {
            throw expected;
        },
    };

    await assert.rejects(
        () => writeClipboardText('sk-lodestar-test', { clipboard: clipboardLike }),
        expected
    );
});
