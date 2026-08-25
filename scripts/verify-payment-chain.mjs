// Payment-chain verification for Lodestar.
//
// Proves the money path against a REAL running server plus a mock upstream that
// returns a fixed usage block, so every expected charge is exact:
//   price input=3 / output=15 USD per 1M tokens, usage 1000 prompt + 500 completion
//   => cost per request = 1000*3e-6 + 500*15e-6 = 0.003 + 0.0075 = 0.0105 USD
//
// What it pins down:
//   - top-up codes credit the exact amount and cannot be redeemed twice
//   - a relay request deducts exactly the computed cost, once
//   - a user whose balance is smaller than one request still gets served, but
//     the delivered usage is booked as debt so the NEXT request is refused
//     (regression guard: overdraft used to be unlimited free service)
//   - a plan granting no quota pool cannot be sold, and the refusal takes no money
//   - a funded plan IS sellable, and the pool it grants pays for real requests
//     before the wallet is touched, capping exactly at the pool size
//     (regression guard: buying a plan used to grant nothing at all)
//
// Run the whole thing with: bash scripts/verify-payment-chain.sh
// (that script wipes a throwaway DB, boots the server and the mock, then runs this)

const BASE = process.env.BASE || 'http://127.0.0.1:8123'
const MOCK = process.env.MOCK || 'http://127.0.0.1:8899'

const ADMIN = { username: 'e2eadmin', password: 'E2eAdminPass123!' }
const MODEL = 'e2e-model'
const PRICE_IN = 3 // USD / 1M prompt tokens
const PRICE_OUT = 15 // USD / 1M completion tokens
const COST_PER_REQ = 1000 * PRICE_IN * 1e-6 + 500 * PRICE_OUT * 1e-6 // 0.0105

let pass = 0
let fail = 0
const failures = []

function ok(name, extra = '') {
  pass += 1
  console.log(`  PASS  ${name}${extra ? '  (' + extra + ')' : ''}`)
}
function bad(name, detail) {
  fail += 1
  failures.push(`${name}: ${detail}`)
  console.log(`  FAIL  ${name}\n        ${detail}`)
}
function eq(name, actual, expected) {
  if (actual === expected) ok(name, `= ${JSON.stringify(actual)}`)
  else bad(name, `expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`)
}
// Money comparison with a tolerance far below one cent, to absorb float noise
// while still catching any real mis-charge.
function eqMoney(name, actual, expected) {
  const d = Math.abs(actual - expected)
  if (d < 1e-9) ok(name, `= ${actual}`)
  else bad(name, `expected ${expected}, got ${actual} (delta ${d})`)
}

async function api(method, path, { token, body, key } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  if (token) headers['Authorization'] = `Bearer ${token}`
  if (key) headers['Authorization'] = `Bearer ${key}`
  const res = await fetch(BASE + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let json = null
  try {
    json = JSON.parse(text)
  } catch {
    /* non-JSON body kept in text */
  }
  return { status: res.status, json, text }
}

async function main() {
  console.log(`\n=== Lodestar payment-chain e2e ===`)
  console.log(`server=${BASE}  mock=${MOCK}  expected cost/req=$${COST_PER_REQ}\n`)

  // --- mock upstream reachable -------------------------------------------
  const mockProbe = await fetch(MOCK + '/v1/models').then((r) => r.status)
  eq('mock upstream reachable', mockProbe, 200)

  // --- 0. bootstrap first admin (no-op if already initialized) ----------
  console.log('[0] bootstrap admin')
  const st = await api('GET', '/api/v1/bootstrap/status')
  if (st.json?.data?.initialized) {
    ok('admin already initialized')
  } else {
    const cr = await api('POST', '/api/v1/bootstrap/create-admin', { body: ADMIN })
    eq('create first admin', cr.status, 200)
  }

  // --- 1. admin login ----------------------------------------------------
  console.log('\n[1] admin login')
  const login = await api('POST', '/api/v1/user/login', { body: ADMIN })
  const adminTok = login.json?.data?.token
  if (!adminTok) return bad('admin login', `no token: ${login.status} ${login.text.slice(0, 200)}`)
  ok('admin login')

  // --- 2. commercial mode on --------------------------------------------
  console.log('\n[2] enable commercial mode')
  const setCM = await api('POST', '/api/v1/setting/set', {
    token: adminTok,
    body: { key: 'commercial_mode', value: 'true' },
  })
  eq('set commercial_mode=true', setCM.status, 200)

  // The price table is only authoritative while the expression-billing path is
  // inactive. If billing_expr ever maps this model, it REPLACES the price table
  // and every cost assertion below would be predicting the wrong number — so
  // assert the default here rather than silently mis-verifying.
  const settings = await api('GET', '/api/v1/setting/list', { token: adminTok })
  const bexpr = (settings.json?.data ?? []).find((s) => s.key === 'billing_expr')?.value
  eq('billing_expr inactive, so the price table governs cost', bexpr, '{}')

  // --- 3. model price ----------------------------------------------------
  console.log('\n[3] configure model price')
  const mk = await api('POST', '/api/v1/model/create', {
    token: adminTok,
    body: { name: MODEL, input: PRICE_IN, output: PRICE_OUT, cache_read: 0, cache_write: 0 },
  })
  eq('create priced model', mk.status, 200)

  // --- 4. channel -> mock upstream + routing ----------------------------
  console.log('\n[4] channel + routing')
  const ch = await api('POST', '/api/v1/channel/create', {
    token: adminTok,
    body: {
      name: 'e2e-mock',
      type: 0,
      enabled: true,
      base_urls: [{ url: MOCK }],
      keys: [{ channel_key: 'sk-mock-not-used' }],
      model: MODEL, // NOTE: Channel.Model is a string, not an array
    },
  })
  if (ch.status !== 200) console.log('        body: ' + ch.text.slice(0, 300))
  eq('create channel', ch.status, 200)
  const ag = await api('POST', '/api/v1/group/auto-group?force=true', {
    token: adminTok,
    body: {},
  })
  eq('auto-group routing', ag.status, 200)
  eq('routing group created for the model', ag.json?.data?.created_groups, 1)

  // --- 5. end-customer registration -------------------------------------
  console.log('\n[5] register end-customer')
  const cust = { username: 'e2ebuyer', password: 'E2eBuyerPass123!' }
  const reg = await api('POST', '/api/v1/user/register', { body: cust })
  if (reg.status !== 200) bad('register user', `${reg.status} ${reg.text.slice(0, 300)}`)
  else ok('register user')
  const custLogin = await api('POST', '/api/v1/user/login', { body: cust })
  const custTok = custLogin.json?.data?.token
  if (!custTok) return bad('customer login', `${custLogin.status} ${custLogin.text.slice(0, 200)}`)
  ok('customer login')

  const balance = async (tok) => {
    const r = await api('GET', '/api/v1/wallet/balance', { token: tok })
    return { quota: r.json?.data?.quota, used: r.json?.data?.used_quota, status: r.status }
  }

  const b0 = await balance(custTok)
  eqMoney('fresh user balance is 0', b0.quota, 0)

  // --- 6. top-up code: generate (admin) -> redeem (user) ----------------
  console.log('\n[6] top-up code redeem')
  const gen = await api('POST', '/api/v1/wallet/codes', {
    token: adminTok,
    body: { count: 1, quota: 1.0 },
  })
  const codes = gen.json?.data
  const code = Array.isArray(codes) ? (codes[0]?.code ?? codes[0]) : null
  if (!code) return bad('generate top-up code', `${gen.status} ${gen.text.slice(0, 300)}`)
  ok('generate top-up code', String(code).slice(0, 12) + '...')

  const red = await api('POST', '/api/v1/wallet/redeem', { token: custTok, body: { code } })
  eq('redeem accepted', red.status, 200)
  eqMoney('credited amount', red.json?.data?.credited, 1.0)
  const b1 = await balance(custTok)
  eqMoney('balance after redeem', b1.quota, 1.0)

  const red2 = await api('POST', '/api/v1/wallet/redeem', { token: custTok, body: { code } })
  eq('same code cannot be redeemed twice', red2.status, 400)

  // --- 7. api key + relay request, exact charge -------------------------
  console.log('\n[7] relay request charges the exact cost')
  const kc = await api('POST', '/api/v1/apikey/create', {
    token: custTok,
    body: { name: 'e2e-key', api_key: '' },
  })
  const apiKey =
    kc.json?.data?.api_key ?? kc.json?.data?.key ?? kc.json?.data?.apikey ?? null
  if (!apiKey) return bad('create api key', `${kc.status} ${kc.text.slice(0, 400)}`)
  ok('create api key', String(apiKey).slice(0, 16) + '...')

  const relay = async () =>
    fetch(BASE + '/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` },
      body: JSON.stringify({ model: MODEL, messages: [{ role: 'user', content: 'hi' }] }),
    }).then(async (r) => ({ status: r.status, text: await r.text() }))

  const r1 = await relay()
  eq('relay request 1 succeeds', r1.status, 200)
  if (r1.status !== 200) console.log('        body: ' + r1.text.slice(0, 400))

  const b2 = await balance(custTok)
  eqMoney('balance charged exactly once', b2.quota, 1.0 - COST_PER_REQ)
  eqMoney('used_quota records the spend', b2.used, COST_PER_REQ)

  const r2 = await relay()
  eq('relay request 2 succeeds', r2.status, 200)
  const b3 = await balance(custTok)
  eqMoney('balance charged twice', b3.quota, 1.0 - 2 * COST_PER_REQ)

  // --- 8. overdraft is bounded (regression guard for f6c0128) ----------
  // A user whose remaining balance is smaller than one request's cost used to
  // get UNLIMITED free service: the pre-request gate only checked quota > 0 and
  // settlement was all-or-nothing, so the balance never moved and the loop had
  // no exit. Now the delivered usage must be owed (balance may go negative),
  // which closes the gate on the NEXT request.
  console.log('\n[8] overdraft is bounded, not unlimited')
  const poor = { username: 'e2epoor', password: 'E2ePoorPass123!' }
  await api('POST', '/api/v1/user/register', { body: poor })
  const poorTok = (await api('POST', '/api/v1/user/login', { body: poor })).json?.data?.token
  if (!poorTok) return bad('poor user login', 'no token')

  const smallGen = await api('POST', '/api/v1/wallet/codes', {
    token: adminTok,
    body: { count: 1, quota: 0.005 }, // less than one request (0.0105)
  })
  const smallCodes = smallGen.json?.data
  const smallCode = Array.isArray(smallCodes)
    ? (smallCodes[0]?.code ?? smallCodes[0])
    : null
  if (!smallCode) return bad('generate small code', smallGen.text.slice(0, 300))
  await api('POST', '/api/v1/wallet/redeem', { token: poorTok, body: { code: smallCode } })
  const p0 = await balance(poorTok)
  eqMoney('under-funded user starts at 0.005', p0.quota, 0.005)

  const pk = await api('POST', '/api/v1/apikey/create', {
    token: poorTok,
    body: { name: 'poor-key', api_key: '' },
  })
  const poorKey = pk.json?.data?.api_key ?? pk.json?.data?.key ?? null
  if (!poorKey) return bad('poor api key', pk.text.slice(0, 300))

  const poorRelay = async () =>
    fetch(BASE + '/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${poorKey}` },
      body: JSON.stringify({ model: MODEL, messages: [{ role: 'user', content: 'hi' }] }),
    }).then(async (r) => ({ status: r.status, text: await r.text() }))

  const pr1 = await poorRelay()
  eq('under-funded request is served (gate cannot know cost yet)', pr1.status, 200)
  const p1 = await balance(poorTok)
  eqMoney('delivered usage is owed, balance goes negative', p1.quota, 0.005 - COST_PER_REQ)

  const pr2 = await poorRelay()
  eq('next request is refused, so overdraft cannot repeat', pr2.status, 402)
  const p2 = await balance(poorTok)
  eqMoney('refused request charges nothing more', p2.quota, 0.005 - COST_PER_REQ)

  // --- 9. a plan that grants no quota cannot be sold -------------------
  // Selling a pool-less plan takes money and delivers nothing. That used to be
  // true of EVERY plan (nothing drew the pool down), so sales were suspended
  // wholesale; the block is now narrowed to plans whose pool is not positive.
  console.log('\n[9] a plan granting no quota is refused')
  const emptyPlan = await api('POST', '/api/v1/subscription/admin/plans/create', {
    token: adminTok,
    body: {
      name: 'e2e-empty-plan',
      description: 'grants nothing',
      price: 0.01,
      currency: 'USD',
      duration_type: 'month',
      duration_days: 30,
      quota_amount: 0,
      enabled: true,
      sort_order: 0,
    },
  })
  eq('admin creates a pool-less plan', emptyPlan.status, 200)
  const emptyPlanId = emptyPlan.json?.data?.id ?? emptyPlan.json?.data?.ID
  const badBuy = await api('POST', '/api/v1/subscription/purchase', {
    token: custTok,
    body: { plan_id: emptyPlanId },
  })
  if (badBuy.status === 200) {
    bad(
      'pool-less plan must not be sellable',
      `purchase SUCCEEDED (${badBuy.text.slice(0, 200)}) — money taken for nothing`,
    )
  } else {
    ok('pool-less plan purchase refused', `HTTP ${badBuy.status}`)
    const msg = (badBuy.json?.message ?? badBuy.text ?? '').toString()
    if (/quota/i.test(msg)) ok('refusal explains why', msg.slice(0, 90))
    else bad('refusal message', `unexpected: ${msg.slice(0, 200)}`)
  }
  const afterBadBuy = await balance(custTok)
  eqMoney('refused purchase took no money', afterBadBuy.quota, b3.quota)

  // --- 10. a real plan is sellable AND its pool actually pays ----------
  // The regression guard for "sold but grants nothing": it is not enough that
  // the purchase succeeds — the pool it grants must fund real requests, and it
  // must do so BEFORE the wallet is touched.
  console.log('\n[10] a purchased pool funds requests before the wallet')
  const PLAN_PRICE = 0.1
  const PLAN_POOL = 0.02 // slightly less than two requests (2 * 0.0105)
  const realPlan = await api('POST', '/api/v1/subscription/admin/plans/create', {
    token: adminTok,
    body: {
      name: 'e2e-real-plan',
      description: 'grants a small pool',
      price: PLAN_PRICE,
      currency: 'USD',
      duration_type: 'month',
      duration_days: 30,
      quota_amount: PLAN_POOL,
      enabled: true,
      sort_order: 0,
    },
  })
  eq('admin creates a funded plan', realPlan.status, 200)
  const realPlanId = realPlan.json?.data?.id ?? realPlan.json?.data?.ID

  const goodBuy = await api('POST', '/api/v1/subscription/purchase', {
    token: custTok,
    body: { plan_id: realPlanId },
  })
  if (goodBuy.status !== 200) console.log('        body: ' + goodBuy.text.slice(0, 300))
  eq('funded plan purchase succeeds', goodBuy.status, 200)

  const afterBuy = await balance(custTok)
  const balAfterBuy = b3.quota - PLAN_PRICE
  eqMoney('purchase charged exactly the plan price', afterBuy.quota, balAfterBuy)

  const pool = async () => {
    const r = await api('GET', '/api/v1/subscription/self', { token: custTok })
    return {
      total: r.json?.data?.amount_total,
      used: r.json?.data?.amount_used,
      status: r.json?.data?.status,
    }
  }
  const s0 = await pool()
  eqMoney('granted pool equals the plan quota', s0.total, PLAN_POOL)
  eqMoney('granted pool starts unused', s0.used, 0)

  // Request 3: the pool covers it in full, so the wallet must not move at all.
  const r3 = await relay()
  eq('request funded by the pool succeeds', r3.status, 200)
  const s1 = await pool()
  eqMoney('pool paid for the request', s1.used, COST_PER_REQ)
  const bAfterPoolReq = await balance(custTok)
  eqMoney('wallet untouched while the pool has room', bAfterPoolReq.quota, balAfterBuy)

  // Request 4: only 0.0095 of pool is left against a 0.0105 cost, so the pool
  // pays what it can and the 0.001 remainder falls to the wallet.
  const r4 = await relay()
  eq('request spanning pool and wallet succeeds', r4.status, 200)
  const s2 = await pool()
  eqMoney('pool exhausted, never overdrawn', s2.used, PLAN_POOL)
  const spill = COST_PER_REQ - (PLAN_POOL - COST_PER_REQ)
  const bAfterSpill = await balance(custTok)
  eqMoney('only the uncovered remainder hit the wallet', bAfterSpill.quota, balAfterBuy - spill)

  // --- 11. a burst cannot multiply the overdraft ------------------------
  // Step 8 shows the overdraft is bounded when requests arrive one at a time.
  // Concurrency is the interesting case: the gate only asks `remaining > 0` and
  // nothing settles until a response returns, so N parallel requests can all
  // pass the gate before any of them moves the balance. Exposure would then be
  // N x cost with N chosen by the caller — 20 requests on a $0.005 balance
  // measured at -$0.205, i.e. 41x the prepaid amount.
  //
  // The requests must be SLOW for this to mean anything. Against an instant
  // upstream request 1 finishes and settles before request 2 reaches the gate,
  // so the burst looks bounded and the assertion passes for the wrong reason.
  console.log('\n[11] a concurrent burst cannot multiply the overdraft')
  const burst = { username: 'e2eburst', password: 'E2eBurstPass123!' }
  await api('POST', '/api/v1/user/register', { body: burst })
  const burstTok = (await api('POST', '/api/v1/user/login', { body: burst })).json?.data?.token
  if (!burstTok) return bad('burst user login', 'no token')

  const bGen = await api('POST', '/api/v1/wallet/codes', {
    token: adminTok,
    body: { count: 1, quota: 0.005 }, // under one request
  })
  const bCode = bGen.json?.data?.[0]?.code
  if (!bCode) return bad('generate burst code', bGen.text.slice(0, 200))
  await api('POST', '/api/v1/wallet/redeem', { token: burstTok, body: { code: bCode } })

  const bk = await api('POST', '/api/v1/apikey/create', {
    token: burstTok,
    body: { name: 'burst-key', api_key: '' },
  })
  const burstKey = bk.json?.data?.api_key
  if (!burstKey) return bad('burst api key', bk.text.slice(0, 200))

  const BURST = 20
  const burstResults = await Promise.all(
    Array.from({ length: BURST }, () =>
      fetch(BASE + '/v1/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${burstKey}` },
        body: JSON.stringify({
          model: MODEL,
          // Marker makes the mock answer slowly, so the whole burst is in flight
          // at once — the only arrangement that actually probes the gate.
          messages: [{ role: 'user', content: 'LODESTAR_SLOW_UPSTREAM hi' }],
        }),
      }).then((r) => r.status),
    ),
  )
  const served = burstResults.filter((s) => s === 200).length
  const bFinal = await balance(burstTok)
  console.log(
    `        ${served}/${BURST} served, balance ${bFinal.quota} ` +
      `(prepaid 0.005, one request costs ${COST_PER_REQ})`,
  )
  if (served === 1) {
    ok('burst bounded to a single request', `${served}/${BURST} served`)
  } else if (served === 0) {
    bad(
      'burst refused every request',
      `a wallet holding 0.005 can pay for something, so exactly one request must be served ` +
        `and booked as debt. Serving none means the gate now over-refuses.`,
    )
  } else {
    bad(
      'concurrent burst multiplied the overdraft',
      `${served}/${BURST} requests served on a 0.005 balance; balance ${bFinal.quota}. ` +
        `Exposure is concurrency x cost and the caller picks the concurrency.`,
    )
  }
  // Whatever the gate admits, the debt must be recorded in full — an unbilled
  // served request is worse than a refused one.
  eqMoney('every served request was billed', bFinal.used, served * COST_PER_REQ)
  console.log(`\n=== ${pass} passed, ${fail} failed ===`)
  if (fail) {
    console.log('\nFailures:')
    failures.forEach((f) => console.log('  - ' + f))
    process.exitCode = 1
  }
}

main().catch((e) => {
  console.error('e2e crashed:', e)
  process.exitCode = 1
})
