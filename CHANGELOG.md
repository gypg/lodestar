# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> Lodestar is derived from the octopus project; see [NOTICE.md](NOTICE.md) for the
> upstream lineage and attribution. Entries below cover Lodestar's own history only —
> upstream releases are documented in their respective repositories.

## [Unreleased]

### 🚀 Features
- **Hub/Sub2API integration**: Real redemption code exchange via `/api/v1/redeem` (P1 #10)
- **Media endpoints**: Capture upstream usage metrics for images/audio/video endpoints (P1 #11)
- **Proxy pool**: Channels can now use proxy pools via `proxy_mode` and `proxy_config_id` (R-10)
- **Deployment automation**: One-click deploy script with polling-based auto-update
- Validate group routing conditions on save. Unknown keys and operators are now
  rejected at configuration time with an actionable message, instead of silently
  producing a group that can never match a request.

### 🐛 Bug Fixes

#### Critical (Production Impact)
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

#### Security
- **S-1**: Never delete users unless backup can restore logins
- **S-2**: Refuse startup without encryption key; unlock `SetString` self-deadlock
- **S-3**: Narrow trusted proxy list to block X-Forwarded-For spoofing
- **S-5**: Validate WebDAV base_url against SSRF including redirects
- **S-6**: Restrict database migration paths to data directory, blocking arbitrary directory creation
- **S-7**: Add 64MiB limit on site import payloads
- **S-8**: Add rate limiting to WebAuthn login begin endpoint and 4096-entry session cap
- Add 1MiB limit on anonymous Stripe webhook payloads

#### Relay & Protocol
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
- Fix endpoint type column showing internal key paths instead of labels
- Fix `SiteChannelDialog` crash due to missing `useTranslations` hook
- Move locale files out of `public/` to enable cache busting via JS bundle hashing
- Clear all 18 TypeScript errors and add tsc CI gate
- Remove `console.error` from `normalizeTimeZone` (Node test runner treated as failure)

#### Other
- Infer i18n message keys for plain errors in `ErrorWithAppError`
- Remove unused `const Stub = true` dead symbol from sitesync

### 📚 Documentation
- Replace real deployment coordinates (database host, SSH endpoint, production
  domain) with `YOUR_*` placeholders throughout the docs.
- Fix stale `OCTOPUS_*` environment variable names in the Chinese README; the
  binary reads the `LODESTAR_*` prefix.

### 🔧 CI & Testing
- Add gofmt gate (fails at 12s if format issues, before tests run)
- Add tsc gate (TypeScript type checking)
- Add i18n reconciliation gate (catches missing translation keys)
- Add mutation testing for billing, relay, and media endpoints
- Add integration tests for media handler billing wiring
- Pin usage-log fixtures to relative time for reproducibility
