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
