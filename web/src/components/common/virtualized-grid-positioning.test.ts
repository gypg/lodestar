/**
 * VirtualizedGrid 行定位方式的结构守卫（WO-048 / octopus PR #252）。
 *
 * 缺陷原貌：虚拟行用 `transform: translateY(...)` 定位。transform 会创建
 * containing block，把后代 `position: fixed` 的参照系从视口劫持到该行 ——
 * 而 `@hello-pangea/dnd` 拖起元素时正是用 fixed + 视口坐标定位，于是分组页
 * 展开后拖模型不跟手，且越靠列表下方（translateY 越大）偏差越大。
 *
 * 修法是给出两种定位模式，让**只有需要 dnd 的页面**切到 `inset`（top 定位，
 * 不创建 containing block），其余页面保持 transform 的合成层性能。
 *
 * 这里守两个方向 —— 单守一个都会漏：
 *   1. 默认值必须是 `'transform'`。若被改成 `'inset'`，log / model / Audit /
 *      site-channel 四个大列表会一起退化成每行触发重排，而它们没有 dnd、
 *      本不需要付这个代价。
 *   2. 分组页必须显式传 `'inset'`。若这行被删掉，拖拽缺陷直接回归，而
 *      TypeScript 不会报错（prop 是可选的）。
 *
 * 为什么是源码断言而不是渲染测试：本仓库前端测试跑在 `node --test` 上，
 * 没有 React 渲染环境（无 jsdom / testing-library），组件挂载不起来。
 * 与 overview-range-scope / permissions 等既有测试同一路数。
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const GRID_SOURCE = readFileSync(
    join(import.meta.dirname, 'VirtualizedGrid.tsx'),
    'utf8',
);
const GROUP_SOURCE = readFileSync(
    join(import.meta.dirname, '../modules/group/index.tsx'),
    'utf8',
);

test('rowPositioning defaults to transform, so non-dnd lists keep compositor positioning', () => {
    assert.match(
        GRID_SOURCE,
        /rowPositioning\s*=\s*'transform'/,
        "the default must stay 'transform' — flipping it to 'inset' makes every " +
            'consumer (log / model / Audit / site-channel) reflow per row for a ' +
            'dnd fix none of them need',
    );
    assert.doesNotMatch(
        GRID_SOURCE,
        /rowPositioning\s*=\s*'inset'/,
        "'inset' must never be the default value inside the component",
    );
});

test('both virtual row branches honour the positioning mode', () => {
    // footer 行与内容行是两段独立 JSX，历史上就因为缩进不同容易只改一处。
    const insetBranches = GRID_SOURCE.match(/rowPositioning === 'inset'/g) ?? [];
    assert.equal(
        insetBranches.length,
        2,
        'both the footer row and the item row must branch on rowPositioning — ' +
            `found ${insetBranches.length}, expected 2 (one of the two rows was missed)`,
    );
    // inset 模式必须用 top 定位；仍留 transform 就等于没修。
    assert.match(
        GRID_SOURCE,
        /\{\s*top:\s*`\$\{virtualRow\.start\}px`\s*\}/,
        'inset mode must position with top, not transform',
    );
});

test('the group page opts into inset, because its dnd list lives inside virtual rows', () => {
    // group/ItemList.tsx 的 Droppable/Draggable 就在虚拟行内，所以这一页
    // 必须显式退出 transform 定位。prop 可选 → 删掉这行 tsc 不会报错。
    assert.match(
        GROUP_SOURCE,
        /rowPositioning="inset"/,
        'group/index.tsx must pass rowPositioning="inset" — without it the ' +
            'member-model drag regresses and TypeScript stays silent',
    );
});
