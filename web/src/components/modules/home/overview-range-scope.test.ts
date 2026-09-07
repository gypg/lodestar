/**
 * 主页「中枢概览」时间范围选择器的可见性守卫（WO-040 ② 的收尾）。
 *
 * 缺陷原貌：WO-040 第 2 轮把 overview 按租户收窄后，客户侧的四个用量指标改从
 * `StatsAPIKey` 取数 —— 那是累计计数器、没有按天序列，所以 `range` 参数对它们
 * 不起作用（后端 `analyticsOverviewGetScoped` 的注释已申报这一取舍）。但前端
 * 仍然渲染 7d/30d/90d 三个页签，于是客户点了标签**四张卡一动不动**：控件看着
 * 能用却没有效果，等于把"时间范围"这个承诺写在界面上又不兑现。
 *
 * 修法：范围选择器改为 staff 专属，判定用**权限**（`channels:read`）而不是角色名
 * —— 与后端 `canSeeSiteWideAnalytics` 同一判据，也避免把 viewer（持
 * settings:read 的只读 staff）误判成客户。
 *
 * 为什么是源码断言而不是渲染测试：本仓库前端测试跑在 `node --test` 上，没有
 * React 渲染环境（无 jsdom / testing-library），组件挂载不起来。所以这里断言的是
 * "那段 JSX 被权限条件包着"这一结构事实，与 navbar / group 等既有测试同一路数。
 *
 * 两个方向都要覆盖：只断言"存在权限判定"会让**恒为 false**（对 staff 也隐藏，
 * 把运营者的范围切换整个弄丢）的实现照样通过；只断言"渲染了 Tabs"又会让回到
 * 无条件渲染的实现通过。
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const SOURCE = readFileSync(join(import.meta.dirname, 'analytics-overview.tsx'), 'utf8');

test('range selector is gated by a permission check, not rendered unconditionally', () => {
    // 门必须存在：拿到判定结果的变量，且判据是 channels:read。
    assert.match(
        SOURCE,
        /hasPermission\(\s*currentUser\?\.role\s*,\s*'channels:read'\s*\)/,
        'the range selector must be gated by hasPermission(role, channels:read) — ' +
            'mirroring canSeeSiteWideAnalytics on the backend',
    );

    // 门必须真的包住 Tabs：<Tabs 之前最近的一个 JSX 表达式是那个条件。
    const tabsAt = SOURCE.indexOf('<Tabs value={range}');
    assert.notEqual(tabsAt, -1, 'the range Tabs block must still exist for staff');
    const before = SOURCE.slice(0, tabsAt);
    assert.match(
        before.slice(-120),
        /mayPickRange\s*&&\s*\(/,
        'the range Tabs must be wrapped in the mayPickRange condition',
    );
});

test('the gate is not hardcoded — staff must still get the selector', () => {
    // 反向：`mayPickRange` 不能被写死成常量，否则 staff 侧的范围切换会整个消失，
    // 而那是一个同样严重的反向缺陷（运营者失去 7d/30d/90d 对比能力）。
    assert.doesNotMatch(
        SOURCE,
        /const\s+mayPickRange\s*=\s*(false|true)\s*;/,
        'mayPickRange must be derived from the current user permission, not hardcoded',
    );
});

test('role names are not used to decide customer vs staff', () => {
    // viewer 是持 settings:read 的只读 staff，按角色名判会把它误分类。
    // 这条同时挡住"按 role === user 判定"这种看似等价的写法。
    assert.doesNotMatch(
        SOURCE,
        /currentUser\?\.role\s*===\s*'(user|admin|editor|viewer)'/,
        'decide by permission, not by role name — viewer is read-only staff',
    );
});
