# NOTICE

Lodestar is a personal, self-hostable, highly-customizable LLM gateway / relay.

## Upstream lineage & attribution

Lodestar is derived from the **octopus** project and its **lingyuins/octopus** fork.
We gratefully acknowledge that lineage and retain the upstream license in full.

- Upstream: octopus — "A Simple, Beautiful, and Elegant LLM API Aggregation &
  Load Balancing Service for Individuals" (github.com/bestruirui/octopus).
- Fork that contributed the multi-site **hub** aggregation feature:
  github.com/lingyuins/octopus.

This project is distributed under the **GNU Affero General Public License v3.0**
(see `LICENSE`), the same license as the upstream work. All copyright notices and
the license text are preserved unchanged. Source for this and any modified
network-deployed version is made available as required by the AGPL.

## What Lodestar adds on top of upstream

- Rebrand to a self-owned product identity (display name, banner, API key prefix).
- A per-user **theme preset engine**: built-in presets (incl. a 冬日/Winter theme)
  that live-recolor the whole UI via CSS design tokens, persisted per user.
- **API-uploadable custom themes** (`custom_themes` setting): themes uploaded
  through the settings API become selectable by every user.

## Features absorbed from the lingyuins/octopus fork

The following features are adapted from implementations in the
github.com/lingyuins/octopus fork (AGPL-3.0), with adjustments to fit
Lodestar's pricing chain, DB layer, and router conventions:

- **Price category fallback** (`internal/model/price_category.go`,
  `internal/op/llm/price_category.go`): exact/prefix/contains rules with
  `sort_order` and an in-memory snapshot cache, used as a fallback price for
  models without an exact price hit. Source: octopus `op/llm/price_category.go`,
  `model/price_category.go`.
- **Manual price presets** (`internal/price/presets_manual.go`): hand-maintained
  price entries kept in a file the generator never rewrites. Source: octopus
  `price/presets_manual.go`.
- **Error log persistence** (`internal/model/error_log.go`,
  `internal/op/errorlog/`, `internal/server/handlers/error_log.go`,
  `web/src/lib/error-report*.ts`): backend panics and frontend JS errors are
  persisted to the main DB with a retention policy (5000 entries, oldest half
  deleted on overflow) and a per-user report rate limit (60/min). Source:
  octopus `op/errorlog/`, `model/error_log.go`, `handlers/error_log.go`,
  `web/src/lib/error-report*.ts`.
- **In-channel 429 hold & retry** (`internal/relay/rate_limit_hold.go`, the
  `ScopeSameChannel` hold branch in `internal/relay/retry_shared.go`, settings
  `rate_limit_hold_*`): on 429, optionally wait inside the current channel and
  retry instead of switching keys/channels immediately — cheaper with few or
  expensive keys. Ctx-cancellable waits (`select ctx.Done()`, no bare
  `time.Sleep`), total-wait cap, off by default; only 429+same-channel
  decisions are affected, so 400-class terminal errors keep their pass-through
  contract. Source: octopus `internal/relay/rate_limit_hold.go` and its
  call sites in `internal/relay/relay.go` / `internal/relay/media_relay.go`,
  adapted to Lodestar's shared `retryWithChannels` loop.

The upstream relay, multi-site hub aggregation, user/auth, stats, and alerting
systems are used substantially as provided by octopus/lingyu.

## Code incorporated from New API

- Project: **New API** — github.com/QuantumNous/new-api (earlier
  github.com/Calcium-Ion/new-api).
- License: **GNU Affero General Public License v3.0** — the same license as this
  project, so the code below is incorporated under AGPL-3.0 and stays under it.

### Copied source (verbatim / near-verbatim)

- **Billing expression engine** (`internal/pkg/billingexpr/`): the versioned
  price-expression compiler, evaluator, and settlement path — a sandboxed
  `expr-lang` program per model that turns request metrics into a cost, with a
  compile cache and a version-tagged (`v1:`) expression format.
  Source: New API `pkg/billingexpr/` (`compile.go`, `run.go`, `settle.go`,
  `types.go`, `billingexpr_test.go`). `compile.go` is byte-for-byte identical to
  upstream; `run.go`, `settle.go`, and `types.go` differ only in small
  adaptations to Lodestar's float-USD cost model. `quota_clamp.go` replaces
  upstream's `round.go`: it is written from scratch and much larger (clamp-kind
  reporting for auditing), but it keeps upstream's `QuotaRound` name and
  contract — half-away-from-zero rounding with int32 saturation — so callers
  port across unchanged. Source of the contract: New API `pkg/billingexpr/round.go`,
  `common/quota_math.go`.
  This package has no counterpart in octopus/lingyu — it is New API's work.

### Logic ports (reimplemented, not copied)

The following were rewritten against Lodestar's DB layer, router conventions,
and float-USD cost accounting rather than copied, but their design and control
flow follow New API and are credited as such:

- **Prepaid balance / quota accounting** (`internal/op/user/quota.go`,
  `internal/op/billing/billing.go`, the `Quota`/`UsedQuota` fields in
  `internal/model/user.go`): per-user prepaid balance checked before a request
  and settled after it. New API keeps integer quota units (`QuotaPerUnit` per
  $1); Lodestar keeps float USD to match its relay cost. Source: New API
  `model/user.go`, `common/quota.go`, and its pre/post-consume relay path.
- **Redemption ("top-up") codes** (`internal/model/topup_code.go`,
  `internal/op/topup/topup.go`): admin-generated codes worth N USD, redeemed
  once, race-safe via a conditional update. Source: New API
  `model/redemption.go`, `controller/redemption.go`.
- **Online top-up orders** (`internal/model/payment_order.go`,
  `internal/op/payment/payment.go`): 易支付/Epay order lifecycle with an
  idempotent `pending -> success` callback, using the same
  `github.com/Calcium-Ion/go-epay` library as upstream. Source: New API
  `model/topup.go`, `controller/topup.go`.
- **Stripe top-up** (`internal/op/payment/stripe.go`). Source: New API
  `controller/topup_stripe.go`.
- **Subscription plans / orders / user subscriptions**
  (`internal/model/subscription.go`, `internal/op/subscription/`,
  `internal/server/handlers/subscription.go`): the plan -> order -> subscription
  lifecycle, simplified (no Creem/Waffo providers, no upgrade-group logic, no
  quota reset period). Source: New API `model/subscription.go`,
  `controller/subscription.go`, `controller/subscription_payment_*.go`.
- **Per-provider request quirks**
  (`internal/transformer/outbound/openai/provider_compat.go`): forcing
  `temperature=1.0` for the kimi-k2.6 family and the zhipu-4v request shaping.
  Source: New API `relay/channel/moonshot/adaptor.go`,
  `relay/channel/zhipu_4v/`.

## Interoperability and design references (no third-party code)

These projects are named for accuracy: Lodestar talks to them or learned from
them, but contains no code copied from them. Listing them is not a license
obligation — it is disclosure.

- **Sub2API** — github.com/Wei-Shaw/sub2api (LGPL-3.0). Lodestar's hub layer is
  an HTTP *client* of Sub2API deployments: access-token refresh
  (`internal/sitesync/sub2api_auth.go`), balance/model sync, and redemption via
  the user-JWT `POST /api/v1/redeem` endpoint. The wire formats were determined
  by reading Sub2API's source; no Sub2API source is included here.
- **Metapi** — github.com/cita-777/metapi (MIT). Lodestar can import a Metapi
  configuration export (`internal/op/site_import.go`, `SiteImportMetAPI`). This
  is payload-format compatibility only; Metapi is TypeScript and nothing was
  copied.
- **N-SLMCRS** (GPL-3.0). The named strategy presets in
  `web/src/lib/strategy-presets.ts` were designed after N-SLMCRS's
  `kernel-rs/src/strategy.rs` preset set (Guardian/Balanced/Velocity/
  Fairshare/Adaptive), reimplemented in TypeScript over Lodestar's existing
  group modes and retry/circuit-breaker settings. That file documents which
  upstream concepts were deliberately not adopted.
