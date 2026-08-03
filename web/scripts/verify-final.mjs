import { chromium } from 'playwright';

const base = process.env.BASE || 'http://127.0.0.1:8080';
const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, storageState: undefined });
const page = await ctx.newPage();

// Disable cache so we always get fresh chunks
await page.route('**/*', (route) => {
  return route.continue({ headers: { ...route.request().headers(), 'Cache-Control': 'no-cache' } });
});

try {
  await page.goto(base, { waitUntil: 'networkidle', timeout: 60000 });
  // Wait for React to hydrate: look for either winter TOC or newspaper h1 or Loading spinner gone
  await page.waitForFunction(() => {
    const body = document.body?.innerText || '';
    // Winter landing: has "章 节 目 录"; Newspaper: has "报 · 纸 · 版"; Loading: has "Loading..."
    return (body.includes('章') || body.includes('报') || body.includes('Lodestar')) && !body.includes('Loading...');
  }, { timeout: 15000 });

  const body = await page.textContent('body');
  console.log('=== PAGE TEXT (first 300 chars) ===');
  console.log(body?.slice(0, 300));
  console.log('=== MATCH ===');
  console.log('has 报:', body?.includes('报'));
  console.log('has 章:', body?.includes('章'));
  console.log('has Lodestar:', body?.includes('Lodestar'));
  console.log('has 增强波C:', body?.includes('增强波C'));
  console.log('has Loading:', body?.includes('Loading'));

  await page.screenshot({ path: '../../verify-final.png', fullPage: true });
  console.log('Screenshot saved to verify-final.png');
} catch (e) {
  console.log('ERROR:', String(e));
  await page.screenshot({ path: '../../verify-error.png', fullPage: true });
} finally {
  await browser.close();
}