import test from 'node:test';
import assert from 'node:assert/strict';

import { writeClipboardText } from './clipboard.ts';

type ClipboardLike = {
    writeText: (text: string) => Promise<void>;
};

type TextAreaLike = {
    value: string;
    style: Record<string, string>;
    setAttribute: (name: string, value: string) => void;
    select: () => void;
};

type DocumentLike = {
    body: {
        appendChild: (node: TextAreaLike) => void;
        removeChild: (node: TextAreaLike) => void;
    };
    createElement: (tag: string) => TextAreaLike;
    execCommand: (command: string) => boolean;
};

test('writeClipboardText falls back to execCommand when clipboard permission is denied', async () => {
    const appended: TextAreaLike[] = [];
    const removed: TextAreaLike[] = [];
    let selected = false;

    const documentLike: DocumentLike = {
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
        document: documentLike,
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
