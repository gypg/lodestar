# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> Lodestar is derived from the octopus project; see [NOTICE.md](NOTICE.md) for the
> upstream lineage and attribution. Entries below cover Lodestar's own history only —
> upstream releases are documented in their respective repositories.

## [Unreleased]

### 🌍 Internationalization
- **Batch 10 — completion** (2026-08-14): Internationalize the remaining user-facing
  labels in site-channel views, request-rewrite controls, and the self-hosted
  subscription notice. Route labels and request-history placeholders are now resolved
  at UI call sites, so their locale keys remain visible to the i18n validation gate.
  Removes an unreachable evaluation warning component. The hardcoded-CJK cleanup is
  complete: the 196 AST findings left are intentionally retained developer console
  logs, product/brand data, test expectations, theme presets, or China-mode numeric
  notation—not user-facing copy.
- **Batch 9** (2026-08-14): Internationalize remote-site type labels in the site
  dialog and remove the static label map. Import validation keeps the localized UI
  message while using an English defensive error outside the normal UI path.
- **Batch 8** (2026-08-14): Internationalize model-selector, PortalHealthStrip, SuccessSparkline, and base-url-latency components (206 remaining hardcoded strings, down from 817 - 75% complete)
- **Batch 7** (2026-08-14): Internationalize winter-landing, site/index, and BillingExpr components (139 remaining hardcoded strings, down from 817 - 83% complete)

### 🚀 Features
- **Site hub with reachable channel-key completion** (`c63de8d`, 2026-08-18): the
  `site` nav entry now renders the `remote-site` tab hub instead of the bare `Site`
  module. The hub is a thin wrapper: one tab hosts the existing live `<Site />`
  view unchanged, a second tab exposes `<SiteChannelSection />`, which had no
  reachable entry point before — so site-channel key completion was implemented but
  could not be opened from the UI.
- **Passthrough outbound format** (`f1483ab`, 2026-08-17): new "raw passthrough"
  outbound transformer that forwards the client's original JSON body unchanged,
  rewriting only the top-level `model` field to the group-resolved upstream model
  name, and optionally preserving the client's original request path. For omp /
  custom clients that need byte-exact path forwarding. Routes via an explicit
  `passthrough` group outbound format that short-circuits the chat↔responses
  adapter fallback; does not enter `isLLMRequestFormat` (R-6 unaffected). Response
  parsing returns an empty `InternalLLMResponse` on 200 + empty/invalid body so the
  existing fake-200 defense (`isFake200Response` / `isUnbillableFake200Response`)
  still catches it — passthrough cannot bypass billing.
- **429 in-channel delay retry** (`85af664`, 2026-08-17): opt-in `rate_limit_hold`
  policy — on 429, wait inside the current channel and retry instead of switching
  keys/channels immediately. Cheaper with few or expensive keys. Ctx-cancellable
  waits (`select ctx.Done()`, no bare `time.Sleep`), total-wait cap, off by default.
  LLM and media paths share `retryWithChannels`, so hold is implemented once for
  both. Only `Code==429 && Scope==ScopeSameChannel` is affected; 400-class terminal
  errors keep their pass-through contract (R-3) and hold waits don't consume
  forward budget (R-5).
- **Strategy presets UX** (`c3cde9c`, 2026-08-17): named presets (Guardian / Balanced
  / Velocity / Fairshare / Adaptive) that batch-fill the recommended group mode +
  circuit-breaker/retry knobs by key count and traffic shape. Pure frontend + thin
  config, zero backend changes. StrictPriority→Failover approximation,
  LeastInflight omitted, racing fanout not introduced.
- **Error log persistence** (`b3292f1`, 2026-08-17): backend panics (via
  `panicRecoveryHandler` with `debug.Stack()`) and frontend JS errors
  (`window.onerror` / `unhandledrejection` / existing error boundaries) are
  persisted to a main-DB `ErrorLog` table (survives restart, separate from relay
  logs). 5000-entry retention (oldest half deleted on overflow), 60/min per-user
  report rate limit, 6-hour cleanup task. Uses `context.Background()` + 5s timeout
  (not the request ctx — client disconnect is the most common panic trigger, and
  the request ctx would be cancelled exactly when we need to record the crash).
- **Price category fallback** (`c3cacd9`, 2026-08-17): four-tier price resolution
  for unbilled models — DB exact → presets exact (incl. manual) → price category
  rules (exact/prefix/contains, by `sort_order`) → whole-word substring heuristic.
  Category hit takes priority over substring. `presets_manual.go` isolates
  hand-maintained prices from the generated `presets.go` so re-running
  `scripts/updatePrice.py` never loses them. `/api/v1/model/price-category/*` CRUD.
- **Site card measuring hook** (`e08e792`, 2026-08-17): extracted the card-height
  measuring chain from `site/index.tsx` (77 lines → 17) into a reusable
  `useElementHeights` hook with the three React error #185 death-loop defenses
  (stable ref cache, ResizeObserver disconnect pairing, same-reference-on-unchanged)
  hardened inside the hook. Callers cannot inline a new ref callback.
- **Hub/Sub2API integration**: Real redemption code exchange via `/api/v1/redeem` (P1 #10)
- **Media endpoints**: Capture upstream usage metrics for images/audio/video endpoints (P1 #11)
- **Proxy pool**: Channels can now use proxy pools via `proxy_mode` and `proxy_config_id` (R-10)
- **Deployment automation**: One-click deploy script with polling-based auto-update
- Validate group routing conditions on save. Unknown keys and operators are now
  rejected at configuration time with an actionable message, instead of silently
  producing a group that can never match a request.

### ♻️ Refactoring
- **Proxy dialler consolidated** (`4d1f600`, 2026-08-23): the scheme dispatch that
  turns a proxy URL into a configured `*http.Transport` existed as three
  byte-identical copies — `internal/client`, `internal/op/airoute` and
  `internal/op` — so any fix to it had to land three times. Extracted into a new
  leaf package `internal/utils/proxydial`. Error strings and mutation order are
  unchanged at all three call sites, and every failure path returns before touching
  the transport, so a failed call cannot leave a half-configured one behind.
  `airoute` keeps its own "empty proxy URL means direct connection" early return,
  because `Apply` deliberately treats the empty string as an unsupported scheme.
- **Two dead delegating shims removed** (`6b7772d`, 2026-08-23):
  `internal/op/ai_route_service_pool.go` had no callers at all, and its comment
  claimed callers that cannot exist — the symbol is unexported in `package op`, so
  the sibling package's tests could never reach it. `internal/op/nav_order.go`
  forwarded to `internal/op/navorder` and was reachable only from `ops_test.go`,
  which meant the tests were exercising a forwarding shell while `navorder` itself
  had no test file. Those tests moved to `internal/op/navorder/navorder_test.go` and
  now cover the real implementations, with the malformed-JSON fallback and
  zero-denominator rate cases added. Reuse was checked scoped to `package op`'s own
  files, not repo-wide: a repo-wide grep for these names hits same-named copies in
  the sibling subpackages and reads as "still referenced" (the false signal WO-016
  was burned by).
- **`navorder.NormalizeNavOrder` removed** (`9ff7059`, 2026-08-23): follow-up to the
  entry above. `6b7772d` moved the deleted shim's tests onto this function on the
  grounds that they would then cover the real implementation — but the forwarding
  target was itself dead. It has no caller repo-wide, the package's only importer
  uses `BuildSemanticCacheEvaluationSummary` alone, and nav order is actually
  normalised in the frontend (`web/src/components/modules/navbar/nav-order.ts`);
  `git log -S` shows it never had a live caller since the initial commit. Removed
  along with its two tests; the summary-builder tests stay, because those do cover a
  live call site that had no coverage at that layer.

### 🔥 Removed
- **Octopus-style site management (dead code)**: The octopus-style site management
  was ported early on but never wired into any page — Lodestar rewrote site
  management as the new-api-style sitesync module (`sites`/`site_accounts` tables,
  `/api/v1/site/*` API, `site/` components), which fully replaced it. The octopus
  side became dead code that repeatedly mislead sessions into chasing "missing
  analytics data" that was never wired up. Removed across 4 batches (~10,400 lines):
  8 dead frontend components + 6 endpoint files; 8 handler files + 4 cron tasks
  that scanned an empty `remote_sites` table; the entire `internal/hub` adapter
  package (7 vendor adapters, not used by relay) and `internal/op/remotesite`;
  6 orphaned model types + AutoMigrate registrations + backup export/import wiring.
  `HealthStatus*` constants migrated to `api_credential.go` (reused by live
  credential code). The live sitesync module is untouched. Existing production
  tables left in place (no destructive DROP).

  > **Do not delete `web/src/components/modules/remote-site/`.** Despite the
  > octopus-era name, the two files that remain there (`index.tsx`,
  > `hub-tab-store.ts`) are live: `index.tsx` is a 53-line tab hub that renders the
  > live `<Site />` and `<SiteChannelSection />`, and `c63de8d` wired it to the
  > `site` nav entry. Only the other 8 components under that path were dead and
  > removed. The name is the trap here, not the contents.

### 🐛 Bug Fixes

#### Critical (Production Impact)
- **Single-failure cooldown loop** (`5991d04`, 2026-08-18): `FailureTracker` never
  cleared its counters when a cooldown expired, so `consecutiveFailures` stayed at
  or above the threshold forever. After the first cooldown lapsed, the *next single*
  failure immediately re-armed a full cooldown — a channel could never again
  accumulate the "3 consecutive failures" the policy actually intended, and instead
  sat in a permanent 1-failure→30m cycle. `ShouldSkip` now resets
  `consecutiveFailures` and clears `cooldownUntil` once the cooldown has expired,
  giving the channel a clean start.
- **Media multipart writer goroutine leak** (`092778a`, 2026-08-18): in
  `forwardMediaRequestMultipart`, an error from `helper.ChannelHttpClient` returned
  without closing `bodyReader`, leaving the multipart writer goroutine blocked on
  the pipe forever. Every media request that failed to obtain a channel HTTP client
  leaked one goroutine (and its buffered body) for the process lifetime. Now closes
  the pipe reader on that path.
- **Fake-200 billing defense-in-depth** (`92aa41d`, 2026-08-17): ad71355 / dd8f26d
  fixed the surface, but the invariant "fake 200 is not charged even when
  `retry_empty_output=false`" did not hold — `isEmptyOutputResponse` was only
  called at `handleResponse` under `isRetryEmptyOutputEnabled()`, so with retry
  off a fake 200 (200 + empty Choices/EmbeddingData) flowed through as success:
  `RecordSuccess` reset the breaker, `RecordAutoSuccess`/`SetSticky` polluted
  routing, `RequestSuccess` suppressed the error-rate alert, and `ChargeKeyWithExpr`
  charged unconditionally. Two-layer defense with separated responsibilities:
  (L1 relay) `isFake200Response` now judges at `handleResponse` **before** the
  retry gate, returning `errFake200Response`; (L2 billing)
  `isUnbillableFake200Response` independently guards `metrics.Save` — zero-payload
  **and** no recorded usage (a zero-payload response with Usage is a legitimate
  stream-aggregation shape, still charged — pinned by
  `TestSaveNonZeroCostOnFailureIsStillCharged`). A fake 200 reaching billing as
  success is demoted to failure (`RequestFailed`, no charge). Also fixed a real
  new leak surfaced during e2e: the responses outbound transformer unconditionally
  fabricated an empty-Message Choice when `Output` was empty, so an error body
  routed through chat→responses adapter fallback had non-empty Choices and
  bypassed both layers — now returns zero Choices (usage preserved) so defenses
  catch it. R-3 (400 pass-through) and ad71355 (legal embedding exemption) intact.
- **Global `db` variable data race** (`78980ee`, 2026-08-17): the main `db` /
  `currentDBType` globals were bare variables — `InitDB`/`Close` wrote while
  `GetDB`/`GetLogDB`/`IsSQLite` read with no synchronization. The race was latent
  (quality CI occasionally flaky) but the new ErrorLog panic-recovery + 429-hold
  concurrent tests increased goroutine count and made it reliably reproduce.
  Guarded with `dbLock sync.RWMutex` (InitDB/Close write-lock, GetDB/GetLogDB/
  IsSQLite read-lock). `logDB` already had `logDBLock`; the main `db` was
  asymmetrically unprotected.
- **Channel cache poisoning**: A single viewer-role `GET /api/v1/channel/list` call
  permanently rewrote every channel's base URL to `https://***` in the live channel
  cache. `chCache` stores `model.Channel` by value, so the copies returned by
  `Get`/`List` still carried slice headers aliasing the cache's backing array, and
  the viewer redaction mutated those elements in place. The relay resolves upstreams
  from that same cache, so affected channels stayed broken for every role until
  restart. Fixed by replacing the `BaseUrls` slice instead of mutating its elements.
- **Group model test reported false success**: `appendGroupTestResult` folded
  per-member results with any-passed instead of all-passed, so a group where one
  member returned `upstream error: 404` still showed PASS. Single-member tests
  cannot distinguish the two, which is why existing tests stayed green.
- **BUG-001**: Reject NaN/Inf in billing expressions to prevent silent free charges
- **BUG-002**: Fix subscription purchase race condition allowing negative balance via atomic `WHERE balance >= price` guard
- **BUG-003**: Enable media endpoint billing (was charging $0) via parameter-based expressions
- **BUG-004**: Fix double-charging on media endpoints by removing redundant expression recompute
- **BUG-005**: Fix singleflight failure path executing relay twice, causing 2× upstream calls and malformed response bodies
- **BUG-006**: Fix infinite CPU spin on single-key channel 401/403 errors due to failed key rewind logic
- **R-1**: Make empty-stream retry reachable by moving check inside SSE loop (was dead code)
- **R-4**: Pass attempts to media relay callbacks; failure-then-success chains now visible in logs and stats
- **R-5**: Fix total attempts quota (was always 0, allowing unlimited retries)
- **Race condition**: Fix relay metrics worker init order race detected by race detector
- **Fake-200 routing pollution** (`ad71355`): `isEmptyOutputResponse` now rejects
  responses with both `EmbeddingData` and `Choices` empty (valid embeddings
  exempted), so a fake 200 no longer resets the circuit breaker, boosts bad
  channels in auto-scoring/sticky routing, or suppresses the error rate. Billing
  invariant holds even when `retry_empty_output=false`.
- **Media billing on relay failure** (`dd8f26d`): wrap `billing.ChargeKey` in
  `if relayErr == nil` at the media relay path so requests that fail the relay
  (e.g. `OnExhausted` returns 502 after all retries, or a fake-200 body) are not
  charged.

#### Security
- **Unbounded regex backtracking on four live paths** (`4e0fcb2`, 2026-08-24): regexp2
  is a backtracking engine, and without `Regexp.MatchTimeout` the zero value is
  `math.MaxInt64` nanoseconds — so a catastrophic pattern could pin a request
  goroutine indefinitely. Of ten compile sites only three set a timeout, and those
  three disagreed on the value (250 ms twice, 200 ms once); `helper/channel.go`,
  `helper/fetch.go` (twice) and `op/group/auto.go` compiled and then matched with no
  bound. The two channel auto-group paths are both live and had drifted apart:
  `op.ChannelAutoGroupWithMode` bounded matching at 200 ms, while
  `helper.ChannelAutoGroup` did not bound it at all. Both halves of the input
  are partly external: the pattern is operator-supplied and the strings matched
  against it are model names arriving from upstream site sync. All sites now compile
  through `internal/utils/xregexp`, which cannot hand back a regex without the
  timeout attached, and a repo walk test fails on any new direct `regexp2.Compile` —
  a missing timeout produces no failure, no error and no log line, so review alone
  cannot catch it.
- **Secrets of 8 characters or fewer were echoed verbatim** (`80bc53c`, 2026-08-23):
  three near-identical masking helpers had drifted, and two returned the trimmed input
  unchanged for short values — `maskSecret` (channel probe) and
  `maskProjectedChannelKey` (projected channels) — while the channel listing's
  `maskSecretValue` starred them. Key length is chosen at channel/API-key creation
  time, so a short key came back in full through `ChannelTestResult.KeyMasked`
  (`/check-keys/:id`, which loads real keys from the DB) and through the
  projected-channel `ChannelKeyMasked` / `TokenMasked`. Consolidated into
  `internal/utils/secretmask`; both output shapes are kept deliberately, since they
  appear in different responses.
- **Self-update zip bomb guards** (`d8cbb29`, 2026-08-18): archive extraction had no
  bounds at all. Three dimensions now capped — entry count (1000), total uncompressed
  size (1 GiB), per-file uncompressed size (1 GiB) — with defense in depth: the
  pre-check rejects archives whose declared `UncompressedSize64` is out of range,
  and `extractFile` additionally wraps each entry in a `LimitReader` at
  `limit+1` bytes, since the declared header size can itself be forged. Exceeding
  the limit is an error rather than a silent truncation, so a partially written
  binary is never produced. Applies even when the SHA256 checksum verifies (checksum
  verification is non-blocking when no checksum file is published).
- **GitHub OAuth response body caps** (`dcce24c`, 2026-08-18): the `/user` response
  was decoded with no read limit. Success-path JSON decoding is now bounded to 1 MiB
  and the error path to 4 KiB (already truncated to 500 chars downstream). The
  endpoint was also extracted into a `githubUserInfoURL` variable so tests can
  inject oversized bodies via `httptest.Server`.
- **S-1**: Never delete users unless backup can restore logins
- **S-2**: Refuse startup without encryption key; unlock `SetString` self-deadlock
- **S-3**: Narrow trusted proxy list to block X-Forwarded-For spoofing
- **S-5**: Validate WebDAV base_url against SSRF including redirects
- **S-6**: Restrict database migration paths to data directory, blocking arbitrary directory creation
- **S-7**: Add 64MiB limit on site import payloads
- **S-8**: Add rate limiting to WebAuthn login begin endpoint and 4096-entry session cap
- Add 1MiB limit on anonymous Stripe webhook payloads

#### Relay & Protocol
- **Streaming aggregate dropped multipart output and aliased reasoning** (`e328640`
  + `29b8aa4`, 2026-08-24): the chunk-merging half of `GetInternalResponse` existed
  once per inbound adapter, and the openai-responses and anthropic-messages copies
  were byte-identical to each other apart from the receiver — one copy pasted twice —
  both missing three branches the openai-chat copy had: `Content.MultipleContent`,
  `Images`, and reasoning read via `GetReasoningContent()`. Folded into one
  `model.AggregateStreamChunks` (net −344 lines). Separately, four client-facing
  conversions tested `ReasoningContent != nil` directly, so an upstream reporting
  reasoning under the `reasoning` spelling (OpenRouter, Ollama cloud) produced no
  reasoning at all for `/v1/messages` and `/v1/responses` clients. Scope, checked
  rather than assumed: the aggregate feeds relay logging and the semantic cache, not
  the client, and billing reads only `resp.Usage` — so this cost log fidelity, not
  money. The three request-direction builders in `outbound/anthropic` are left alone
  on purpose: accepting the alias there would synthesise a thinking block with no
  `reasoning_signature`, which Anthropic can reject outright.
- **Media upstream URLs skipped base-URL normalization** (`1f4d949`, 2026-08-23):
  the media relay read `channel.GetBaseUrl()` — the raw stored value — at both call
  sites, while the LLM relay and every other consumer read
  `GetNormalizedBaseUrl()`, which is what appends the per-channel-type version root
  (`/v1`, `/v1beta`, `/api/v3`). It also carried its own joiner that only
  de-duplicated `/v1`, lacking the two branches the LLM joiner has. Stacked, a
  Volcengine channel — whose media endpoints *are* OpenAI-compatible — produced
  `https://ark…/v1/images/generations` when stored without the root and
  `https://ark…/api/v3/v1/images/generations` when stored with it; both 404. Now both
  resolve correctly. OpenAI-type channels were unaffected either way, which is why
  this went unnoticed. Gemini-type channels remain unsupported for media (their API
  is not OpenAI-shaped), unchanged before and after.
- **`socks://` proxies never dialled** (`4d1f600`, 2026-08-23): the proxy scheme
  switch advertised `case "socks", "socks5"` and `model.NormalizeProxyURL` accepts
  the `socks://` spelling, but `golang.org/x/net/proxy` registers only `socks5` and
  `socks5h`. A proxy-pool entry saved as `socks://host:1080` therefore passed
  validation and then failed **when the client was constructed** — `invalid socks
  proxy: proxy: unknown scheme: socks`, returning a nil client rather than reaching
  a dial at all. Nor was this confined to the pool's test button:
  `helper.ChannelHttpClient` → `client.GetHTTPClientCustomProxy` is the relay path,
  so every request through such a channel failed to obtain an HTTP client at all.
  `socks` is now canonicalised to `socks5` in the one shared dialler. The two
  validators for the same setting also disagreed — the `SettingKeyProxyURL`
  validator rejected `socks` while its own error message listed it as valid — so
  `socks` was added there too, and a test now pins the two to the same scheme set.
- **R-3**: Return 400/ScopeNone errors as-is instead of swallowing into 502
- **R-6**: Add Anthropic to adapter fallback logic
- **R-7**: Stop silently downgrading xhigh/max reasoning effort; clamp Anthropic thinking budget correctly
- **R-8**: Remove second lockless trend implementation; unify on telemetry.Store
- **R-9**: Write relay log attempts in same transaction as parent log; reclaim on cleanup
- Fix TPM bucket charging by actual token usage (was under-counting)
- Roll back transactions on every early return path

#### Site Management
- **Username/password site accounts**: These accounts could never sync or check in.
  NewAPI-style logins authenticate with a session cookie plus a `new-api-user`
  header and return no `access_token`, which the credential path treated as
  success while storing an empty token.
- **Managed channel projection**: Masked and disabled tokens were counted as usable
  keys, so a group whose only token was masked produced an enabled channel with
  zero keys — every request failed while the UI showed it healthy. Channel keys are
  now rebuilt per route bucket, since `op.ChannelCreate` cascades the insert and
  writes generated IDs back into the slice; sharing one slice handed the second
  channel keys already owned by the first.
- **Add Site entry point**: Add a persistent "新增站点" button. Only the empty state
  had one, so after the first site there was no way to add another.
- **Site usage history ignored probe traffic**: Group model tests built a complete
  attempt list but only handed it to the relay log, never to the site model hourly
  recorder, so a site channel's usage and availability history stayed at zero no
  matter how often it was probed. The one-off backfill scan does not filter
  `is_test`, so probe rows already counted once a backfill ran — live probes not
  counting was an inconsistency, not a deliberate exclusion.
- **Passing probes hid the upstream response body**: All three probe result views
  gated the body on `!result.passed`, so a successful test showed only "Passed" with
  no way to tell whether the upstream had actually returned a usable completion.
  Both backends already returned the body on success, so this was purely a render
  condition. Failures keep their expanded layout; successes collapse into a
  one-click panel. Also adds the first tests to assert the body survives the success
  path — nothing covered `ResponseText`/`ResponseBody` before, so returning an empty
  body on success would have stayed green.

#### AI Routing
- Register `GET /api/v1/channel/:id`. The AI route config refetches a channel by id
  to auto-fill `base_url`/`api_key` when the cached list entry has neither, but the
  route was never registered — that fallback could only ever 404.
- Unblock AI routing for single-group setups.
- Confirm local-mode AI route config saves automatically and add a visible
  confirmation line, since the absent Save button read as a broken form.

#### Frontend & i18n
- Show the real version number in the ops center. The root cause was in CI, not the
  UI: `docker.yml` passed `APP_VERSION=dev-<40-char sha>` into `conf.Version` via
  ldflags. Also fixes `BUILD_TIME`, a Dockerfile ARG that was never given a value,
  so every image reported its container start time as its build time.
- Add 385 missing translation keys and enable i18n reconciliation gate
- Internationalize BillingExpr variable descriptions (10 keys: p, c, len, cr, cc, cc1h, img, img_o, ai, ao) and Cloudflare protection text in site probe results
- Fix endpoint type column showing internal key paths instead of labels
- Fix `SiteChannelDialog` crash due to missing `useTranslations` hook
- Move locale files out of `public/` to enable cache busting via JS bundle hashing
- Clear all 18 TypeScript errors and add tsc CI gate
- Remove `console.error` from `normalizeTimeZone` (Node test runner treated as failure)
- Repair the `group.detail.availability` block in `zh_hant.json`, where all nine
  values were literal `?` characters rather than Traditional Chinese. Corrupt since
  the initial commit, so Traditional Chinese users saw `?????????` for the group
  availability check panel.
- **Channel test hid the upstream response**: The backend has always returned the
  raw upstream body for each attempt, but the results list rendered only the Go
  error string, so diagnosing a failing channel meant querying the database for a
  payload that was already on screen's doorstep. Failed rows now expose it in a
  collapsible panel.

#### Other
- **Draft group-test results published before their ClientIDs** (`27e386b`,
  2026-08-24): `runGroupModelTest` publishes a terminal progress record with
  `Done=true`, but it cannot stamp `ClientID` — that mapping lives only in
  `StartDraftGroupModelTest`, which then publishes a *second* record to attach them.
  Between the two stores the record was Done with every `ClientID` empty, so a reader
  polling `GET /api/v1/group/test/progress/:id` in that window — the group editor
  included — got finished verdicts it could not match back to unsaved rows, since
  `client_id` is exactly what it matches on. Surfaced as a CI failure rather than a
  bug report; the run also blocked the docker workflow, and therefore deploys.
- **Prompt-cache savings panel ignored four of five price sources** (`ef15d8f`,
  2026-08-23): `estimateOpsProviderPromptCacheSaved` called `llm.Get` directly, which
  reads only the `model_info` cache — the first of the five sources
  `price.GetLLMPrice` consults (then the preset table, the admin price map,
  `PriceCategoryMatch`, and the provider-prefix/substring fallback). Any model priced
  by anything other than a `model_info` row therefore contributed exactly 0 to the
  "estimated cost saved" figure while the same request billed correctly, with no log
  and no surfaced error.
- **Short-timeout HTTP client cached under the wrong key** (`550dba8`, 2026-08-23):
  `GetHTTPClientShortTimeout` keyed its cache on `systemProxyURL`, which only
  `GetHTTPClientSystemProxy` ever assigns — nothing wrote a key when the
  short-timeout client itself was built. So once the long-timeout path had noticed a
  `proxy_url` change and refreshed the shared key, the short-timeout path compared
  the new URL against that new key, concluded its cache was current, and handed back
  a client still bound to the *previous* proxy. Delay probing and model syncing then
  kept using the old proxy until the process restarted, with nothing in the logs to
  say so. The cache now has its own `shortTimeoutProxyURL`. The package had no tests
  before this; it does now.
- Infer i18n message keys for plain errors in `ErrorWithAppError`
- Remove unused `const Stub = true` dead symbol from sitesync

### 📚 Documentation
- Replace real deployment coordinates (database host, SSH endpoint, production
  domain) with `YOUR_*` placeholders throughout the docs.
- Fix stale `OCTOPUS_*` environment variable names in the Chinese README; the
  binary reads the `LODESTAR_*` prefix.

### 🔧 CI & Testing
- Add CJK regression gate (`web/tests/cjk-scan.cjs`): scans `web/src` for hardcoded
  CJK and compares against a frozen baseline (196 findings / 19 files). Blocks new
  files with CJK and non-allowlist files whose count increased; allowlist files
  (logger / tests / comments / brand data / chinaMode number format) warn instead
  of fail. Prevents the i18n hardcoded-CJK cleanup from silently regressing
- Clear 27 ESLint warnings and gate `lint` at `--max-warnings=0` (also fixed a real
  `Cache.tsx` perf issue: `trend` recreated each render invalidated `chartData`'s
  `useMemo`)
- Document the auto-deploy chain in `docker.yml` summary (quality → GHCR push →
  server cron poller ~10 min lag) so readers don't conclude there is no auto-deploy
- Add gofmt gate (fails at 12s if format issues, before tests run)
- Add tsc gate (TypeScript type checking)
- Add i18n reconciliation gate (catches missing translation keys)
- Add mutation testing for billing, relay, and media endpoints
- Add integration tests for media handler billing wiring
- Pin usage-log fixtures to relative time for reproducibility
