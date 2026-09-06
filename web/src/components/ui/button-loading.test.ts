/*
WO-039 2.1：Button loading prop 的守卫。

防重复提交是 loading 的功能而不是外观：loading 为真时按钮必须真的 disabled。
组件渲染难以在 node --test 里跑，这里用本仓库既有的源码接线断言风格
（同 ai-route-source-mode.test.ts）钉住三个关键点：
  1. Button 组件存在 loading prop 且 disabled 计算包含 loading；
  2. loading 态渲染同尺寸 spinner（宽位顶替，不塌缩）；
  3. 走查收敛过的调用点真的在用 loading prop 而不是手写 spinner。
*/
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const uiDir = join(import.meta.dirname, '.');
const componentSource = readFileSync(join(uiDir, 'button.tsx'), 'utf8');

test('Button accepts a loading prop', () => {
    assert.match(componentSource, /loading = false/, 'Button must declare loading with a false default');
});

test('loading forces disabled regardless of the disabled prop', () => {
    assert.match(
        componentSource,
        /disabled=\{asChild \? disabled : loading \|\| disabled\}/,
        'disabled must OR in loading so a loading button cannot be re-submitted',
    );
});

test('loading renders a same-size spinner and keeps children (no width jump)', () => {
    assert.match(componentSource, /<Loader2 className="animate-spin" aria-hidden="true" \/>/);
    assert.match(componentSource, /\{loading && !asChild \? \(/, 'spinner replaces the icon slot only in the non-asChild path');
    assert.doesNotMatch(
        componentSource,
        /loading &&[^]*?children === null/,
        'children must stay rendered while loading so the button keeps its width',
    );
});

test('loading is exposed as data-loading for tests and styling', () => {
    assert.match(componentSource, /data-loading=\{loading \|\| undefined\}/);
});

const modulesDir = join(import.meta.dirname, '..', 'modules');

test('converged call sites use the loading prop instead of hand-written spinners', () => {
    const converged: Array<[string, RegExp]> = [
        ['login/index.tsx', /loading=\{isPending\}/],
        ['first-run-setup.tsx', /loading=\{isPending\}/],
        ['credential/CredentialDialog.tsx', /loading=\{isPending\}/],
        ['analytics/Evaluation.tsx', /loading=\{generateAIRoute\.isPending\}/],
        ['channel/Form.tsx', /loading=\{isPending\}/],
        ['channel/CardContent.tsx', /loading=\{checkChannelKeys\.isPending\}/],
        ['image/index.tsx', /loading=\{loading\}/],
        ['setting/Backup.tsx', /loading=\{testDatabase\.isPending\}/],
        ['setting/WebDAV.tsx', /loading=\{updateConfig\.isPending\}/],
    ];
    for (const [file, pattern] of converged) {
        const src = readFileSync(join(modulesDir, file), 'utf8');
        assert.match(src, pattern, `${file} must pass loading to Button`);
    }
});

test('converged call sites no longer hand-write the spinner ternary', () => {
    // 这些文件收敛后，Button 的 children 里不应再有 X ? <Loader…/> : <Icon…/> 三元。
    const withoutSpinner = [
        'channel/CardContent.tsx',
        'image/index.tsx',
        'setting/WebDAV.tsx',
    ];
    for (const file of withoutSpinner) {
        const src = readFileSync(join(modulesDir, file), 'utf8');
        assert.doesNotMatch(
            src,
            /\? <Loader2 className="size-4 animate-spin" \/> : </,
            `${file} should let Button render the spinner`,
        );
    }
});
