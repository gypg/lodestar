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
- **Balance ledger with admin audit and reconciliation** (`edaf545`, `cc3620f`,
  `fcd4d27`, 2026-08-26): every discrete change to a user's balance now flows
  through a single funnel that writes the balance update and a `quota_ledgers`
  row in one transaction, so neither "money arrived untraced" nor "traced but no
  money" is reachable. Each row carries a signed delta, an event kind, the
  originating document, the acting admin, and a reason. Previously balance
  changes were spread across five packages writing their own bare UPDATE, and an
  admin crediting or debiting an account left no trace whatsoever, so a user
  dispute could not be settled from the data. Per-request usage settlement stays
  outside the funnel by design — it runs on the hot path and `used_quota`
  already accumulates wallet spend exactly — and the invariant
  `quota == sum(delta) - used_quota` still closes. `AddQuota` and `SetQuota` are
  removed: absolute overwrite cannot be expressed as a delta and read-then-write
  is unsafe under concurrency. Non-finite deltas are now rejected at the entry
  point rather than relying on one caller's JSON decoder, which matters because
  `quota + NaN` poisons the column permanently and locks the account beyond
  arithmetic repair. Admin adjustments require a reason and record the acting
  admin rather than the beneficiary, and the audit middleware now captures the
  beneficiary's id, which it previously dropped — the row recorded that someone
  called the endpoint but not who received the money. Existing users get a
  one-time opening row of `quota + used_quota` (not `quota`, which would leave
  the two sides a whole `used_quota` apart). `GET /api/v1/wallet/reconcile`
  reports accounts failing the invariant with a signed drift and a 1e-9
  tolerance, since float residue would otherwise flag every active account.
  Verified by eleven mutations, each confirmed to turn a test red: removing the
  non-finite guard, splitting the ledger write out of the transaction, dropping
  the affordability guard, moving the CAS boundary off `>=`, recording the
  beneficiary as the actor, dropping the reason requirement, backfilling with
  `quota` alone, zeroing the drift tolerance, taking the absolute value of the
  delta, bypassing the funnel at the epay call site both literally and via Raw
  SQL, and removing the audit target key.
- **Overdraft bound control** (`fd74d54`, 2026-08-26): `max_expected_request_cost` is now
  editable from the commercial settings page instead of the settings API only. It
  sits with the other billing knobs inside the commercial-only block, because the
  admission gate short-circuits when `commercial_mode` is off. The panel states the
  trade-off it governs — raising the value holds the exposure tighter and admits
  fewer parallel requests on a thin balance, lowering it favours throughput — and
  derives the consequence of the current value: an account ends at most about that
  much in debt, whatever concurrency it chooses. Setting it to 0 renders a warning
  rather than silently accepting it, since 0 disables the bound and returns exposure
  to concurrency x cost. Invalid input is rejected client-side and reverted, and a
  server rejection surfaces its message instead of leaving the typed number in the
  box looking saved. Keys are literal `t('...')` calls, not the declarative
  `labelKey` indirection, so the i18n gate reconciles them — verified by mutating a
  key name and dropping an interpolation argument, both of which the gate caught.
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
- **The Stripe top-up entry point never rendered for paying customers**
  (`572976a`, 2026-08-28): the button was gated on finding `stripe_enabled` in the
  settings list, but end customers hold the `user` role, which deliberately carries no
  `settings:read` — that list exposes `stripe_api_key`, `epay_key` and `smtp_pass`. For
  them the request returned 403, the settings array stayed undefined, and the gate
  evaluated `undefined === 'true'`, so the top-up entry was invisible to everyone who
  was not an admin. Both halves were individually correct; only their junction was
  wrong, which is why neither an admin session nor a per-file review surfaced it.
  Stripe checkout itself had been wired and working the entire time.
  `GET /api/v1/wallet/balance` requires only authentication, making it the one place
  every signed-in user can learn which top-up methods exist, and it already carried
  `epay_configured`; it now also reports `stripe_configured` — which is what the
  previously uncalled `payment.StripeConfigured()` was written for. Gating on that
  value is additionally stricter than the old check, requiring the toggle *and* an API
  key *and* a webhook secret, so the button no longer appears when a top-up would fail
  for missing credentials. Guarded by wiring tests that assert the response body rather
  than the status code, plus a test pinning the premise that the `user` role cannot read
  settings, since re-granting that permission would hand every customer the secret list.
  Not reachable before this fix landed: production runs with `commercial_mode` off, so
  this blocked go-live rather than leaking money.
- **Money columns were single-precision on PostgreSQL, which broke the reconcile
  endpoint it shipped with** (2026-08-27): 25 columns across 14 tables — `users.quota`,
  `users.used_quota`, `quota_ledgers.delta`, the payment/top-up/subscription amounts,
  and `input_cost`/`output_cost` on all seven stats tables — were tagged
  `gorm:"type:real"`. That type name means different things per dialect: SQLite's REAL
  is always 8-byte IEEE double and MySQL's REAL defaults to DOUBLE, but PostgreSQL's
  `real` is float4 — 4 bytes, ~7 significant decimal digits. So the SQLite test suite
  was green while production ran at single precision. Two measured consequences.
  First, the reconcile endpoint (`GET /api/v1/wallet/reconcile`, shipped the day
  before) reported **every active user as drifted**: on production's real schema, a
  perfectly balanced account (top up $100, then 50 charges of $0.000123) yields
  `quota=99.993896484375, used_quota=0.006149996, ledger_sum=100`, a drift of
  `4.58e-05` — about 45,800× `ReconcileTolerance` (1e-9). The tolerance exists
  precisely to stop float noise from flagging every active user; float4 turned the
  failure mode it was guarding against into a certainty, so the only tool for
  detecting a lost payment was itself unusable. Second, amounts lost sub-cent
  precision: float4 stores 99999.99 with an error of 0.0022 (over 0.2 cents) and
  1234.56 with 5.9e-05. The stats costs are worse than a one-off rounding, since
  they are accumulated read-modify-write — at $1000 cumulative the float4 ULP is
  6e-5, so any charge below that adds nothing at all.
  Fixed by migration 020, which widens the columns to `double precision` on
  PostgreSQL (registered as a *before* migration so the type is already correct when
  AutoMigrate inspects it), plus the struct tags so fresh databases are built right.
  Widening is lossless — every float4 value is exactly representable as float8 — but
  it does not recover precision already discarded at write time; production carried
  no non-zero money values, so nothing needed repair. No ALTER is needed on SQLite or
  MySQL, where the storage is already 8 bytes; that is the absence of the defect, not
  an unsupported dialect. Verified on a copy of the production database: 25 columns
  widened, zero float4 remaining, every stored value bit-identical, NOT NULL and
  DEFAULT preserved, index count unchanged (149), and a post-widening write of
  99999.99 round-trips exactly. Guarded by a schema assertion that the whole
  PostgreSQL schema contains no float4 column (so a future `type:real` on any new
  money column fails CI, not just the 25 known ones), a completeness check pinning
  the migration's column list against the production catalog, a legacy-schema
  upgrade test, a registration test, SQLite type-affinity tests, and two reconcile
  tests that reproduce the false drift on float4 and its absence on float8. All
  seven mutations of the fix were killed.
- **Non-finite overdraft bound reopened the unlimited-overdraft hole** (`7891478`, 2026-08-26):
  `max_expected_request_cost` had no branch in `Setting.Validate()`, so it fell
  through to the validator's trailing `return nil` and accepted any string. Two of
  those strings are not harmless: `strconv.ParseFloat` succeeds on `"NaN"` and
  `"Inf"`, and neither is caught by the runtime's `err != nil || v < 0` filter —
  every comparison with NaN is false, and `0 * (+Inf)` is NaN. Stored, the admission
  rule `headroom <= inflight * bound` became constant-false, so `AcquireForKey`
  admitted **every** request, including from accounts whose wallet was already
  negative. That is not "the concurrency bound is off"; it is the unlimited-overdraft
  hole (`f6c0128`) reopened by a single settings write — and `settings:write` is held
  by the `editor` role, not only by admins. Fixed at both ends: the validator now
  requires a finite number >= 0, and `maxExpectedRequestCost()` additionally screens
  NaN/±Inf so values already sitting in a database are clamped to 0 (bound off,
  balance gate intact) rather than trusted. Confirmed by probe before fixing: with
  `"NaN"` configured, an account at -$1 was admitted; the same probe now refuses it,
  and a top-up positive control proves the gate is not simply refusing everything.
  Pinned by 15 validator cases, a gate-level test over all non-finite spellings, a
  handler wiring test asserting both the 400 and that the rejected value never
  reaches the database, and 7/7 killed mutations — deleting the `Validate()` call
  from `setSetting` leaves the model and billing tests green and is caught **only**
  by the wiring test.
- **Concurrency-multiplied overdraft** (`efc8ebe`, 2026-08-25): the relay gate was a
  pure predicate (`remaining > 0`), and a request's cost is unknown until the
  response arrives, so a burst could all pass the gate before any of it settled.
  Measured against a real server with a slow upstream: a wallet holding $0.005
  served **20 of 20** concurrent requests and ended at **-$0.205**, 41x the prepaid
  amount. The exposure was concurrency x cost, with the caller choosing the
  concurrency. `AcquireForKey` now reserves an in-flight slot and admits only when
  `max(wallet, 0) + pool > inflight * max_expected_request_cost`; the slot is
  released by `defer` in `APIKeyAuth`, which wraps the whole chain so aborts, panics
  and client disconnects all return it. With nothing in flight the rule is exactly
  the old `headroom > 0`, so a thin-but-positive account still gets its one request
  and still owes for it — accounts that cannot cover what is already in flight are
  serialized, not refused. Deliberately not a pre-deduction: no money moves, so a
  leaked release only over-restricts one account until restart. New setting
  `max_expected_request_cost` (default $0.5); 0
  restores the old behaviour, and negative/unparsable values clamp to 0 instead of
  inverting the comparison. Same probe with the guard on: 1/20 served, -$0.0055, and
  the mock's log shows only one request ever reached the upstream. Pinned by 8 unit
  tests plus 2 middleware wiring tests and 11/11 killed mutations — `defer release`
  deletion and a call site reverted to the bare predicate are caught only by the
  wiring tests. Counters are per-process: multi-instance deployments get a looser
  bound of instances x the assumed cost.
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
- **Structured Outputs lost its schema on the Responses path** (`c0e9c73`, 2026-08-26):
  both Responses-API converters copied only `text.format.type`. The inbound one never
  populated `ResponseFormat.JSONSchema` even though its `ResponsesTextFormat` had
  already parsed `name` and `schema` off the wire, and the outbound one emitted a bare
  `{"type":"json_schema"}` — so the caller's schema reached no upstream, in either
  direction. A `/v1/chat/completions` caller routed to a Responses-format upstream
  lost it on the way out too, even though its own inbound had parsed it correctly. The
  two failure shapes differ and neither is loud: OpenAI-family upstreams reject
  `json_schema` with no schema (400), while the Gemini outbound sets `ResponseSchema`
  only when `JSONSchema != nil` and otherwise just asks for `application/json`,
  returning JSON that ignores the caller's schema with nothing logged. Both structs
  now also carry `strict` and `description`, which neither had — an unenforced schema
  is its own quiet wrong answer. The nested-to-flat mapping stays conditional on
  `JSONSchema` being present: `model.ResponseFormatJSONSchema.Schema` has no
  `omitempty`, so building one for a schema-less `json_object` request would serialise
  `"schema":null` and trade the dropped schema for a guaranteed 400. Assertions run
  against the marshalled wire body, since a correct struct field under a wrong json
  tag fails exactly as silently as the bug itself; 5/5 mutations killed, including
  corrupting the outbound `schema` tag.
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
- **Subscription expiry sweep had no caller** (`58aaf9d`, 2026-08-26):
  `ExpireDueSubscriptions` shipped with a doc comment saying it was "intended for
  periodic background invocation" and zero callers — not even a test. Nothing ever
  flipped a due subscription's status, so every expired row stayed `active` forever and
  both subscription lists rendered it with a green active badge. Not a billing hole:
  the only two readers of that column (`GetUserSubscription` and the quota pool's
  `activePoolSubscription`) both AND in `expires_at > now`, so an expired-but-active
  row funds nothing — confirmed by grepping `SubStatusActive` exhaustively for a third
  reader. What was broken is the column's truthfulness: admins could not tell who had
  actually lapsed, and any later query filtering on status alone would have mis-billed.
  Now registered on the existing task scheduler, hourly with `runOnStart`, routed
  through the SQLite serial writer like the other periodic writes. Covered on both
  halves, because either alone leaves the same gap open: a wiring assertion (the
  registry must hold the task after `Init`), since that is precisely the call site that
  went missing, plus a behavioural table proving the sweep spares a never-expiring
  grant, a live subscription, and an admin-cancelled row — it writes the same column
  the pool reads, so an over-broad `WHERE` would defund subscriptions people paid for.
  6/6 mutations killed.
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
- **Attribute New API for copied and ported code** (`a06c212`, 2026-08-25):
  `NOTICE.md` credited only octopus/lingyu, but an audit against the reference
  upstreams (byte-identical hashing plus same-basename diffing over every Go and
  TS file) found New API code present and uncredited. `internal/pkg/billingexpr/`
  is New API's `pkg/billingexpr/` — `compile.go` byte-for-byte identical — and
  octopus has no billingexpr at all, so it cannot be inherited from the declared
  upstream. Three files also read "Ported from GGGZERO", a private local folder
  name that means nothing in published source; they now name New API and the
  specific upstream files. New API is AGPL-3.0 like Lodestar, so the
  incorporation was always permitted — only the attribution was missing. Also
  discloses three projects that contributed **no** code (Sub2API interop client,
  Metapi import format, N-SLMCRS preset design), because the audit found zero
  copied files from them and the opposite error is worth preventing too.
- Replace real deployment coordinates (database host, SSH endpoint, production
  domain) with `YOUR_*` placeholders throughout the docs.
- Fix stale `OCTOPUS_*` environment variable names in the Chinese README; the
  binary reads the `LODESTAR_*` prefix.

### 🔧 CI & Testing
- **PostgreSQL integration tests now cover the whole module** (2026-08-27): the
  dedicated CI step ran `go test ./internal/db/migrate/... -run TestPostgres`, so
  PostgreSQL-only guards outside that one package were silently never executed. The
  money-column schema guards live in `internal/db` and `internal/op/user`, and under
  the old path they would have skipped — a green run proving nothing. Widened to
  `./... -run TestPostgres`. The PostgreSQL-backed tests in each package now use a
  dedicated schema via the DSN's `search_path` rather than `public`: `go test` runs
  packages concurrently against the one shared test database, and while both were in
  `public` they tore down each other's tables (reproduced 3/3 as
  `relation "subscription_plans" does not exist`). Per-schema isolation fixes that
  structurally instead of requiring every caller to remember `-p 1`.
- **Payment-chain verification harness** (`scripts/verify-payment-chain.sh`,
  2026-08-25): boots a throwaway SQLite instance plus a mock upstream that
  returns a fixed usage block, so every expected charge is an exact number rather
  than a plausible one. Pins the money path end to end: top-up codes credit
  exactly once and cannot be reused; a relay request deducts precisely
  `prompt*input*1e-6 + completion*output*1e-6`; an under-funded user is still
  served but the delivered usage becomes debt so the *next* request is refused
  402 (the regression guard for unlimited-overdraft); a paid subscription
  purchase is refused 409 and takes no money. It also asserts `billing_expr` is
  inactive — otherwise expression billing would replace the price table and the
  predicted costs would be wrong — and prints the upstream request log so a
  refused request can be shown never to reach an upstream. Talks to no real
  provider and writes only under `.tmp/`.
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
