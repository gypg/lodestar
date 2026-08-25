// Mock OpenAI-compatible upstream for scripts/verify-payment-chain.sh.
//
// Returns a FIXED usage block so the expected billing amount is exactly
// predictable. Every request is appended to $WORK/upstream-calls.log so the
// caller can prove which requests actually reached an upstream — in particular
// that a request refused by the balance gate never does.

import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'

const PORT = Number(process.env.MOCK_PORT || 8899)
const WORK = process.env.WORK || path.join(process.cwd(), '.tmp', 'payment-chain')
const LOG = path.join(WORK, 'upstream-calls.log')

// Artificial upstream latency. Zero by default; set it to model a real provider.
// This matters for any test about concurrency: with an instant upstream, request
// 1 can finish and settle before request 2 even reaches the balance gate, so a
// burst looks bounded when a real upstream would let the whole burst through.
const DELAY_MS = Number(process.env.MOCK_DELAY_MS || 0)

// Per-request latency opt-in: a body containing this marker is answered slowly
// even when DELAY_MS is 0. Lets one harness run mix instant requests (for exact
// charge assertions) with slow ones (for concurrency assertions) — the marker
// travels in the prompt, which the relay forwards verbatim.
const SLOW_MARKER = 'LODESTAR_SLOW_UPSTREAM'
const SLOW_MARKER_MS = Number(process.env.MOCK_SLOW_MARKER_MS || 1500)

// Fixed usage; keep in sync with PROMPT_TOKENS/COMPLETION_TOKENS in
// verify-payment-chain.mjs, which predicts the charge from these numbers.
const PROMPT_TOKENS = 1000
const COMPLETION_TOKENS = 500

let calls = 0

fs.mkdirSync(WORK, { recursive: true })

function log(line) {
  fs.appendFileSync(LOG, line + '\n')
}

const server = http.createServer((req, res) => {
  let body = ''
  req.on('data', (c) => (body += c))
  req.on('end', () => {
    calls += 1
    log(JSON.stringify({ n: calls, method: req.method, url: req.url, len: body.length }))

    if (req.url.startsWith('/v1/models')) {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ object: 'list', data: [{ id: 'e2e-model', object: 'model' }] }))
      return
    }

    if (req.method === 'POST' && req.url.includes('/chat/completions')) {
      const reply = () => {
        res.writeHead(200, { 'Content-Type': 'application/json' })
        res.end(
          JSON.stringify({
            id: 'chatcmpl-e2e-' + calls,
            object: 'chat.completion',
            created: 1756000000,
            model: 'e2e-model',
            choices: [
              {
                index: 0,
                message: { role: 'assistant', content: 'mock reply ' + calls },
                finish_reason: 'stop',
              },
            ],
            usage: {
              prompt_tokens: PROMPT_TOKENS,
              completion_tokens: COMPLETION_TOKENS,
              total_tokens: PROMPT_TOKENS + COMPLETION_TOKENS,
            },
          }),
        )
      }
      if (DELAY_MS > 0) setTimeout(reply, DELAY_MS)
      else if (body.includes(SLOW_MARKER)) setTimeout(reply, SLOW_MARKER_MS)
      else reply()
      return
    }

    // Channel creation probes the base URL with HEAD /; anything else is a
    // genuine surprise and should be visible in the log.
    res.writeHead(req.method === 'HEAD' ? 200 : 404, { 'Content-Type': 'application/json' })
    res.end(
      req.method === 'HEAD'
        ? ''
        : JSON.stringify({ error: { message: 'mock: unhandled ' + req.method + ' ' + req.url } }),
    )
  })
})

server.listen(PORT, '127.0.0.1', () => {
  log('--- mock upstream listening on ' + PORT + ' ---')
  console.log('mock upstream on http://127.0.0.1:' + PORT)
})
