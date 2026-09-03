// Registers the test-only resolve hook (ts-alias-loader.mjs) for node --test.
// Loaded via node --import ./tests/wo029-register.mjs.
import { register } from 'node:module';

register('./ts-alias-loader.mjs', import.meta.url);
