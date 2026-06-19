import { chromium } from 'playwright';

const base = process.env.BASE || 'http://127.0.0.1:8080';
const out = process.env.OUT || '../../verify-landing.png';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
const errors = [];
try {
  const res = await page.goto(base, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.waitForTimeout(3000);
  if (!res || res.status() !== 200) errors.push(`index status ${res?.status()}`);
  const body = await page.textContent('body');
  if (!body || body.length < 50) errors.push('empty body');
  if (!/Lodestar|雪|目录|登录|章/i.test(body)) errors.push('expected landing copy missing');
  await page.screenshot({ path: out, fullPage: false });
  console.log(JSON.stringify({ ok: errors.length === 0, errors, screenshot: out, snippet: body?.slice(0, 200) }));
  process.exit(errors.length ? 1 : 0);
} catch (e) {
  console.log(JSON.stringify({ ok: false, error: String(e) }));
  process.exit(1);
} finally {
  await browser.close();
}