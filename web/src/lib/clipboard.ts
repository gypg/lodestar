type ClipboardLike = Pick<Clipboard, 'writeText'>;
type DocumentLike = Pick<Document, 'createElement' | 'execCommand'> & {
    body: Pick<HTMLElement, 'appendChild' | 'removeChild'>;
};

type ClipboardOptions = {
    clipboard?: ClipboardLike;
    document?: DocumentLike;
};

function fallbackCopyText(text: string, documentLike?: DocumentLike) {
    if (!documentLike) {
        throw new Error('Clipboard unavailable');
    }

    const textArea = documentLike.createElement('textarea');
    textArea.value = text;
    textArea.setAttribute('readonly', 'true');
    textArea.style.position = 'fixed';
    textArea.style.top = '0';
    textArea.style.left = '-9999px';

    documentLike.body.appendChild(textArea);
    textArea.select();

    try {
        if (!documentLike.execCommand('copy')) {
            throw new Error('Clipboard unavailable');
        }
    } finally {
        documentLike.body.removeChild(textArea);
    }
}

export async function writeClipboardText(text: string, options: ClipboardOptions = {}) {
    const clipboardLike = options.clipboard ?? navigator.clipboard;
    const documentLike = options.document ?? (typeof document !== 'undefined' ? document : undefined);

    try {
        await clipboardLike.writeText(text);
    } catch (error) {
        if (documentLike) {
            fallbackCopyText(text, documentLike);
            return;
        }
        throw error;
    }
}
