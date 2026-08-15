/* eslint-disable @typescript-eslint/no-require-imports */
// CJK 硬编码扫描器（审计基线 + CI 回归门）
//
// 用途：扫描 web/src 下 .ts/.tsx/.js/.jsx 的中文硬编码，输出文件级计数。
// 设计基线：196 findings / 19 files / 0 parse errors（2026-08-15，见 00-当前状态基线.md）
//
// 三种模式：
//   node tests/cjk-scan.cjs              # 默认：扫描 + 打印 + 与基线对比（CI 门模式）
//   node tests/cjk-scan.cjs --report     # 只扫描 + 打印详情（含每文件命中数），不门禁
//   node tests/cjk-scan.cjs --update     # 重新生成基线快照到 tests/cjk-baseline.json
//
// 门禁逻辑（默认模式）：
//   1. 扫描全量，得当前 findings
//   2. 与 tests/cjk-baseline.json 对比
//   3. 基线内文件 count 不超过快照值 → 放行
//   4. 基线内文件 count 增加 → FAIL（已有文件新增硬编码）
//   5. 出现基线外的新文件带 CJK → FAIL（新增硬编码文件）
//   6. 基线内文件 count 减少 → WARN（提示更新基线，不阻塞，属改进）
//
// allowlist（tests/cjk-allowlist.json）：
//   { files: { "<相对路径>": "<理由key>" } }
//   语义：allowlist 文件被"豁免新增检测"——其 CJK count 增加不触发 FAIL
//   （仍会进基线以锁上限，但增量为允许项）。
//   非 allowlist 文件 CJK count 增加 → FAIL。
//   新增 allowlist 项需在 PR 里说明理由（logger / 品牌名 / 测试断言 / chinaMode 记数法 等）。
//
// 基线 vs allowlist 的关系：
//   基线 = 当前全量快照（锁所有文件的上限，含 allowlist 文件）
//   allowlist = 豁免"新增检测"的文件子集（通常是基线内文件，纯 logger/测试/注释类）
//   非 allowlist 文件若想加 CJK → 必须挪进 locale，不能靠 allowlist 豁免
//   allowlist 文件若想加 CJK → 仍要更新基线 + 理由合理（PR review 把关）

const fs = require('fs');
const path = require('path');
let parser, traverse;
try {
  parser = require('@babel/parser');
  traverse = require('@babel/traverse').default;
} catch (e) {
  console.error('FAIL: @babel/parser 或 @babel/traverse 不可用。它们是 next/eslint 的间接依赖，');
  console.error('     正常 pnpm install 后应在 node_modules 里。若 CI 报此错，检查 pnpm-lock.yaml 是否含 @babel/parser。');
  console.error('     原始错误: ' + e.message);
  process.exit(1);
}

const hasCJK = (s) => /[一-鿿㐀-䶿豈-﫿]/.test(s);

const BASELINE_PATH = path.join(__dirname, 'cjk-baseline.json');
const ALLOWLIST_PATH = path.join(__dirname, 'cjk-allowlist.json');

function scanFile(fp) {
  const content = fs.readFileSync(fp, 'utf8');
  const isTsx = /\.(tsx|jsx)$/.test(fp);
  const plugins = isTsx ? ['typescript', 'jsx'] : ['typescript'];
  let ast;
  try {
    ast = parser.parse(content, { sourceType: 'module', plugins });
  } catch (e) {
    return { error: e.message };
  }
  const findings = [];
  const visitors = {
    StringLiteral(p) {
      if (hasCJK(p.node.value)) findings.push(1);
    },
    TemplateLiteral(p) {
      for (const q of p.node.quasis) if (hasCJK(q.value.raw)) findings.push(1);
    },
  };
  if (isTsx) {
    visitors.JSXText = (p) => {
      if (hasCJK(p.node.value)) findings.push(1);
    };
  }
  traverse(ast, visitors);
  return { count: findings.length };
}

function walk(dir) {
  const out = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const f = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (!e.name.startsWith('.') && e.name !== 'node_modules') {
        out.push(...walk(f));
      }
    } else if (/\.(tsx?|jsx?)$/.test(e.name)) {
      out.push(f);
    }
  }
  return out;
}

function scanAll() {
  const root = path.join(__dirname, '..', 'src');
  const files = walk(root);
  const results = {};
  const parseErrors = [];
  for (const fp of files) {
    const r = scanFile(fp);
    if (r.error) {
      parseErrors.push(path.relative(path.join(__dirname, '..'), fp) + ': ' + r.error);
      continue;
    }
    if (r.count > 0) {
      const rel = path.relative(path.join(__dirname, '..', 'src'), fp).split(path.sep).join('/');
      results[rel] = r.count;
    }
  }
  return { results, parseErrors };
}

function loadJson(p, fallback) {
  try {
    return JSON.parse(fs.readFileSync(p, 'utf8'));
  } catch {
    return fallback;
  }
}

function main() {
  const mode = process.argv[2] || 'gate';
  const { results, parseErrors } = scanAll();

  if (mode === '--report') {
    const sorted = Object.entries(results).sort((a, b) => b[1] - a[1]);
    for (const [file, count] of sorted) console.log(`${count}\t${file}`);
    const total = sorted.reduce((s, [, c]) => s + c, 0);
    console.log(`\nTOTAL: ${total} findings / ${sorted.length} files`);
    console.log(`Parse errors: ${parseErrors.length}`);
    if (parseErrors.length) parseErrors.forEach((e) => console.error('  PARSE_ERR: ' + e));
    return;
  }

  if (mode === '--update') {
    const sorted = Object.entries(results).sort((a, b) => b[1] - a[1]);
    const total = sorted.reduce((s, [, c]) => s + c, 0);
    fs.writeFileSync(
      BASELINE_PATH,
      JSON.stringify({ total, files: results, generatedAt: new Date().toISOString() }, null, 2) + '\n'
    );
    console.log(`Baseline written: ${total} findings / ${sorted.length} files → ${BASELINE_PATH}`);
    return;
  }

  // gate 模式
  const baseline = loadJson(BASELINE_PATH, null);
  if (!baseline) {
    console.error('FAIL: no baseline at ' + BASELINE_PATH + '. Run `node tests/cjk-scan.cjs --update` first.');
    process.exit(1);
  }
  const allowlist = loadJson(ALLOWLIST_PATH, { files: {} });
  const allowFiles = new Set(Object.keys(allowlist.files || {}));

  const curFiles = new Set(Object.keys(results));
  const baseFiles = new Set(Object.keys(baseline.files || {}));

  const newFiles = [...curFiles].filter((f) => !baseFiles.has(f));
  const increased = [];
  const decreased = [];
  for (const f of curFiles) {
    if (!baseFiles.has(f)) continue;
    const diff = results[f] - (baseline.files[f] || 0);
    if (diff > 0) increased.push({ file: f, base: baseline.files[f], now: results[f], allowed: allowFiles.has(f) });
    else if (diff < 0) decreased.push({ file: f, base: baseline.files[f], now: results[f] });
  }

  // allowlist 里但不在基线里的文件 = allowlist 过期（allowlist 应是基线子集）
  const staleAllow = [...allowFiles].filter((f) => !baseFiles.has(f));

  // 基线里有 CJK 但既不在 allowlist 的文件 = 需要 allowlist 收口（首次接门时全部已知项）
  const unallowlisted = [...baseFiles].filter((f) => results[f] > 0 && !allowFiles.has(f));

  const total = Object.values(results).reduce((s, c) => s + c, 0);
  console.log(`CJK scan: ${total} findings / ${Object.keys(results).length} files (baseline ${baseline.total}/${Object.keys(baseline.files).length})`);

  let fail = false;
  if (parseErrors.length) {
    console.error(`\nFAIL: ${parseErrors.length} parse errors (must be 0)`);
    parseErrors.forEach((e) => console.error('  ' + e));
    fail = true;
  }
  // 新文件且非 allowlist → FAIL（allowlist 文件本应在基线内，新文件不可能是 allowlist）
  const newFilesFail = newFiles.filter((f) => !allowFiles.has(f));
  if (newFilesFail.length) {
    console.error(`\nFAIL: ${newFilesFail.length} new file(s) with CJK not in baseline/allowlist:`);
    newFilesFail.forEach((f) => console.error(`  + ${f} (${results[f]})`));
    console.error('  → 要么把中文挪进 locale 文件，要么在 tests/cjk-allowlist.json 加理由并更新基线');
    fail = true;
  }
  // 非 allowlist 文件 count 增加 → FAIL（这些文件不该再加中文）
  const increasedFail = increased.filter((x) => !x.allowed);
  if (increasedFail.length) {
    console.error(`\nFAIL: ${increasedFail.length} non-allowlist file(s) with increased CJK:`);
    increasedFail.forEach((x) => console.error(`  ↑ ${x.file}: ${x.base} → ${x.now}`));
    fail = true;
  }
  // allowlist 文件 count 增加 → WARN（允许但要 review 是否合理）
  const increasedAllow = increased.filter((x) => x.allowed);
  if (increasedAllow.length) {
    console.log(`\nWARN: ${increasedAllow.length} allowlist file(s) increased (review if justified, then update baseline):`);
    increasedAllow.forEach((x) => console.log(`  ↑ ${x.file}: ${x.base} → ${x.now} [allowlisted]`));
  }
  if (decreased.length) {
    console.log(`\nWARN: ${decreased.length} file(s) decreased (improvement; update baseline when convenient):`);
    decreased.forEach((x) => console.log(`  ↓ ${x.file}: ${x.base} → ${x.now}`));
  }
  if (staleAllow.length) {
    console.log(`\nWARN: ${staleAllow.length} allowlist entry no longer in baseline (stale):`);
    staleAllow.forEach((f) => console.log(`  ? ${f}`));
  }
  if (unallowlisted.length) {
    console.log(`\nINFO: ${unallowlisted.length} baseline file(s) with CJK not yet in allowlist (first-run only; add reasons to silence):`);
    unallowlisted.forEach((f) => console.log(`  · ${f}`));
  }

  if (fail) {
    console.error('\nCJK regression gate FAILED. Fix above or update baseline with justification.');
    process.exit(1);
  }
  console.log('\nCJK gate passed.');
}

main();
