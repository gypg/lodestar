import assert from 'node:assert/strict';
import test from 'node:test';
import { formatCommit, formatVersion } from './version-format.ts';

test('formatVersion collapses the dev-<full sha> shape the old docker build produced', () => {
    assert.equal(
        formatVersion('dev-1939e98c4d2a6b7f8e0a1c3d5e7f9b1d3f5a7c9e'),
        'dev (1939e98)',
    );
    // 短 sha 变体也要收敛，构建脚本历史上两种都出现过。
    assert.equal(formatVersion('dev-1939e98'), 'dev (1939e98)');
});

test('formatVersion passes semantic versions through untouched', () => {
    assert.equal(formatVersion('v2.1.4'), 'v2.1.4');
    assert.equal(formatVersion('v2.1.4-3-gabc1234'), 'v2.1.4-3-gabc1234');
    assert.equal(formatVersion('v2.1.4-dirty'), 'v2.1.4-dirty');
    // 仓库当前没有 tag，docker.yml 会产出 <默认版本>+<短sha> 这种 SemVer
    // build-metadata 形态，必须原样透出，不能被当成 sha 截断。
    assert.equal(formatVersion('v2.1.4+1939e98'), 'v2.1.4+1939e98');
    // "dev" 本身不带 sha，不该被改写成 "dev ()"。
    assert.equal(formatVersion('dev'), 'dev');
});

test('formatVersion shortens a bare full sha and handles empty input', () => {
    assert.equal(
        formatVersion('1939e98c4d2a6b7f8e0a1c3d5e7f9b1d3f5a7c9e'),
        '1939e98',
    );
    assert.equal(formatVersion(''), '-');
    assert.equal(formatVersion('   '), '-');
    assert.equal(formatVersion(undefined), '-');
    assert.equal(formatVersion(null), '-');
});

test('formatCommit shortens full shas but leaves other markers alone', () => {
    assert.equal(
        formatCommit('1939e98c4d2a6b7f8e0a1c3d5e7f9b1d3f5a7c9e'),
        '1939e98',
    );
    assert.equal(formatCommit('1939e98'), '1939e98');
    assert.equal(formatCommit('unknown'), 'unknown');
    assert.equal(formatCommit(''), '-');
    assert.equal(formatCommit(undefined), '-');
});
