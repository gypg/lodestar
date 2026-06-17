import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const source = fs.readFileSync(path.join(__dirname, 'index.tsx'), 'utf8');

test('group create dialog keeps the wide desktop wrapper needed by GroupEditor', () => {
    assert.match(
        source,
        /if \(activeItem === 'group'\)\s*{\s*return 'h-\[calc\(100dvh-2rem\)\] w-\[min\(100vw-2rem,92rem\)\]/,
        'group create dialog wrapper should stay wide enough for the two-column GroupEditor layout',
    );
});
