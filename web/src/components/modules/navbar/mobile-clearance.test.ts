import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const source = fs.readFileSync(path.join(__dirname, '../../app.tsx'), 'utf8');

test('mobile content keeps bottom scroll clearance for fixed navbar', () => {
    assert.match(source, /pb-\[calc\(5\.5rem\+env\(safe-area-inset-bottom,0px\)\)\]/);
    assert.match(source, /md:pb-\[calc\(1rem\+env\(safe-area-inset-bottom,0px\)\)\]/);
});
