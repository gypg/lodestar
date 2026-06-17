import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const source = fs.readFileSync(path.join(__dirname, 'navbar.tsx'), 'utf8');

test('active navbar item uses a visible accent color', () => {
    assert.match(source, /text-primary/);
    assert.equal(
        source.includes('text-sidebar-primary-foreground'),
        false,
        'active navbar items should not use the near-white sidebar foreground token',
    );
});
