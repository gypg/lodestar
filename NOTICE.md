# NOTICE

GGZERO is a personal, self-hostable, highly-customizable LLM gateway / relay.

## Upstream lineage & attribution

GGZERO is derived from the **octopus** project and its **lingyuins/octopus** fork.
We gratefully acknowledge that lineage and retain the upstream license in full.

- Upstream: octopus — "A Simple, Beautiful, and Elegant LLM API Aggregation &
  Load Balancing Service for Individuals" (github.com/bestruirui/octopus).
- Fork that contributed the multi-site **hub** aggregation feature:
  github.com/lingyuins/octopus.

This project is distributed under the **GNU Affero General Public License v3.0**
(see `LICENSE`), the same license as the upstream work. All copyright notices and
the license text are preserved unchanged. Source for this and any modified
network-deployed version is made available as required by the AGPL.

## What GGZERO adds on top of upstream

- Rebrand to a self-owned product identity (display name, banner, API key prefix).
- A per-user **theme preset engine**: built-in presets (incl. a 冬日/Winter theme)
  that live-recolor the whole UI via CSS design tokens, persisted per user.
- **API-uploadable custom themes** (`custom_themes` setting): themes uploaded
  through the settings API become selectable by every user.

The upstream relay, multi-site hub aggregation, user/auth, stats, and alerting
systems are used substantially as provided by octopus/lingyu.
