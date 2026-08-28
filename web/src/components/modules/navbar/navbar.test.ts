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

// 门户白名单守卫。
//
// 这里必须解析数组本身，不能只断言源文件里出现 'wallet' —— 上方注释里也写着这个词，
// 那样的断言在条目被删掉后照样绿（假守卫）。无 React 测试环境，只能读源码，
// 所以退而求其次：把数组字面量抠出来解析。
function readPortalNav(constName: string): string[] {
    const m = new RegExp(`const ${constName}: NavItem\\[\\] = \\[([^\\]]*)\\]`).exec(source);
    assert.ok(m, `${constName} not found — did the declaration get renamed?`);
    return [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1]);
}

// 付费客户（user 角色）看余额和充值的唯一入口是 wallet 路由：全仓只有 SettingWallet
// 调 useWallet()，而它只挂在那条路由下。navbar 在 restrictToPortal 时用这份白名单
// 替换整个导航（visibleItems 不参与），所以漏掉 wallet 不是"管理员改配置能补上"，
// 而是该角色彻底到不了那一页 —— 连自己的余额都看不到。
test('commercial portal nav exposes the wallet entry', () => {
    const items = readPortalNav('USER_PORTAL_NAV_COMMERCIAL');
    assert.ok(
        items.includes('wallet'),
        `paying customers would have no way to reach the top-up page; got ${JSON.stringify(items)}`,
    );
});

// 充值是订阅的前置：PurchaseWithBalance 从钱包余额扣款，没有充值入口时余额恒为 0，
// 订阅同样买不动。所以钱包要排在订阅之前。
test('wallet comes before subscription in the commercial portal nav', () => {
    const items = readPortalNav('USER_PORTAL_NAV_COMMERCIAL');
    const wallet = items.indexOf('wallet');
    const subscription = items.indexOf('subscription');
    assert.ok(wallet >= 0 && subscription >= 0, `both entries must be present; got ${JSON.stringify(items)}`);
    assert.ok(wallet < subscription, `wallet must precede subscription; got ${JSON.stringify(items)}`);
});

// 自用模式没有计费也没有支付渠道，钱包页只会显示恒为 0 的余额 —— 与 subscription
// 同样按商业模式收窄。这条钉住的是"两份白名单不该被写成同一份"。
test('self-use portal nav omits the commercial-only entries', () => {
    const items = readPortalNav('USER_PORTAL_NAV');
    assert.equal(items.includes('wallet'), false, `got ${JSON.stringify(items)}`);
    assert.equal(items.includes('subscription'), false, `got ${JSON.stringify(items)}`);
});

// 'model' 曾在两份白名单里，但该页唯一的数据源 GET /api/v1/model/market 挂在需要
// settings:read 的组上，而终端客户角色刻意没有这个权限 —— 于是那一页对他们只能是
// 一个 "permission denied" toast 加一片空白。
//
// 放宽权限不是正确解法：ModelMarketItem 内嵌 Channels []ModelMarketChannel，
// 带着每个上游渠道的 id 与**名称**及其可用 key 数，等于告诉客户你在转售谁、有几路冗余。
// 客户真正需要的"有哪些模型、什么价"由无需鉴权的 /api/v1/public/overview 提供
// （只返回 name/input/output），首页已经在渲染它。
for (const name of ['USER_PORTAL_NAV', 'USER_PORTAL_NAV_COMMERCIAL'] as const) {
    test(`${name} omits the model page (its payload names upstream channels)`, () => {
        const items = readPortalNav(name);
        assert.equal(
            items.includes('model'),
            false,
            `${name} must not contain 'model': /api/v1/model/market needs settings:read, ` +
            `which the end-customer role lacks, and its response embeds upstream channel ` +
            `names. got ${JSON.stringify(items)}`,
        );
        // 前提校验：解析器确实拿到了数据。否则 readPortalNav 返回空数组时
        // includes 恒为 false，这条断言会空转绿。
        assert.ok(items.length > 0, `${name} parsed as empty`);
    });
}
