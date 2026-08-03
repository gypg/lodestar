# Technical Debt Registry

## N-36: Comma-separated Fields Assessment

**Date:** 2026-06-25
**Status:** Documented (evaluation only)

### Summary

Several database columns store comma-separated values in a single TEXT/VARCHAR field
instead of using normalized relational tables. This document catalogues every such
field, its usage pattern, and the estimated impact of migrating to association tables.

---

### 1. `channels.model` and `channels.custom_model`

**Type:** `string` (comma-separated model names)
**Example:** `"gpt-4o,gpt-4o-mini,claude-3-sonnet"`
**Read sites:** ~15 locations (auto_group, channel filter, analytics, relay routing, sync)
**Write sites:** ~5 locations (channel CRUD, sync, projected channel)

**Migration impact: MEDIUM**

Would require:
- New `channel_models` junction table (`channel_id INT, model_name VARCHAR(256), is_custom BOOLEAN`)
- Refactor `xstrings.SplitTrimCompact(",", ...)` calls across 8+ packages
- Update all GORM queries that filter/split on `channel.Model`
- Touch the relay hot path (performance-sensitive)

**Recommendation:** HIGH VALUE but HIGH RISK. The model list is read on every relay
request and split with `xstrings.SplitTrimCompact`. Moving to a junction table would
enable indexed lookups and eliminate the split cost, but requires careful performance
testing. **Defer until relay path is benchmarked.**

---

### 2. `api_keys.supported_models`

**Type:** `string` (comma-separated model names)
**Example:** `"gpt-4o,claude-3-sonnet"`
**Read sites:** ~5 locations (relay channel filter, API handler, model handler)
**Write sites:** ~2 locations (API key CRUD)

**Migration impact: LOW-MEDIUM**

Would require:
- New `api_key_models` junction table (`api_key_id INT, model_name VARCHAR(256)`)
- Refactor `strings.Split(apiKey.SupportedModels, ",")` in handlers and relay

**Recommendation:** MEDIUM VALUE. The key-level model filter is a security/ACL
feature. A junction table would allow more expressive queries (e.g., pattern matching,
model group membership). **Consider alongside `channels.model` refactor.**

---

### 3. `api_keys.allowed_ips`

**Type:** `string` (comma-separated IP/CIDR)
**Example:** `"192.168.1.0/24,10.0.0.1"`
**Read sites:** ~2 locations (auth middleware, API handler)
**Write sites:** ~1 location (API key CRUD)

**Migration impact: LOW**

Would require:
- New `api_key_allowed_ips` table (`api_key_id INT, ip_cidr VARCHAR(45)`)
- Refactor `strings.Split(allowedIPs, ",")` in `middleware/auth.go`

**Recommendation:** LOW VALUE. IP lists are small (typically 1-5 entries) and only
read during authentication. The comma-split cost is negligible. **Keep as-is unless
IP list management UI requires complex queries.**

---

### 4. `api_keys.tags`

**Type:** `string` (comma-separated tags)
**Example:** `"production,openai,priority"`
**Read sites:** ~3 locations (listing, filtering)
**Write sites:** ~1 location (API key CRUD)

**Migration impact: LOW**

Would require:
- New `api_key_tags` table or a `tags` + `api_key_tags` junction table
- Refactor tag-based filtering

**Recommendation:** LOW VALUE. Tags are used for display and simple filtering.
A tag model would enable autocomplete and tag management UI, but current usage
is minimal. **Keep as-is.**

---

### 5. `api_keys.excluded_channels`

**Type:** `string` (comma-separated channel IDs)
**Example:** `"3,7,12"`
**Read sites:** ~2 locations (relay channel filter)
**Write sites:** ~1 location (API key CRUD)

**Migration impact: LOW**

Would require:
- New `api_key_excluded_channels` junction table (`api_key_id INT, channel_id INT`)
- Refactor `strings.Split(s, ",")` in `relay/channel_filter.go`

**Recommendation:** LOW VALUE. The exclude list is typically small and checked
in the relay hot path via a simple string split + int parse. A junction table
adds JOIN cost that may be worse for small lists. **Keep as-is.**

---

### 6. `alerts.to`

**Type:** `string` (comma-separated email addresses)
**Example:** `"admin@example.com,ops@example.com"`
**Read sites:** ~1 location (notification send)
**Write sites:** ~1 location (alert CRUD)

**Migration impact: LOW**

Would require:
- New `alert_recipients` table (`alert_id INT, email VARCHAR(255)`)
- Refactor `strings.Split(cfg.To, ",")` in `helper/notify.go`

**Recommendation:** KEEP AS-IS. Alert recipient lists are small and this pattern
is standard for email notification configs. No benefit from normalization.

---

### 7. `settings` key `cors_allow_origins` (value is comma-separated)

**Type:** `Setting.Value` string (comma-separated origins)
**Example:** `"https://example.com,https://app.example.com"`
**Read sites:** ~1 location (CORS middleware)
**Write sites:** ~1 location (settings CRUD)

**Migration impact: NONE (settings are already key-value)**

**Recommendation:** KEEP AS-IS. This is a configuration value, not relational data.

---

### 8. `settings` key `webauthn_origins` (value is comma-separated)

**Type:** `Setting.Value` string (comma-separated origins)
**Read sites:** ~1 location (WebAuthn verification)
**Write sites:** ~1 location (settings CRUD)

**Recommendation:** KEEP AS-IS. Configuration value, same rationale as CORS.

---

### Summary Table

| # | Field | Table | Complexity | Value | Priority |
|---|-------|-------|-----------|-------|----------|
| 1 | `model`, `custom_model` | channels | HIGH (15+ read sites, relay hot path) | HIGH | P2-DEFER |
| 2 | `supported_models` | api_keys | MEDIUM (5 read sites) | MEDIUM | P3 |
| 3 | `allowed_ips` | api_keys | LOW (2 read sites, small lists) | LOW | P4 |
| 4 | `tags` | api_keys | LOW (3 read sites, small lists) | LOW | P4 |
| 5 | `excluded_channels` | api_keys | LOW (2 read sites, small lists) | LOW | P4 |
| 6 | `to` | alerts | LOW (1 read site, small lists) | NONE | P5-KEEP |
| 7 | `cors_allow_origins` | settings | NONE | NONE | P5-KEEP |
| 8 | `webauthn_origins` | settings | NONE | NONE | P5-KEEP |

### Recommended Action Plan

1. **No immediate changes.** All comma-separated fields work correctly and have
   no known data integrity issues.

2. **If refactoring `channels.model` (P2):** This is the highest-impact item.
   A junction table would improve:
   - Model-level query capabilities (find all channels supporting model X)
   - Eliminate per-request string splitting in the relay hot path
   - Enable proper foreign key constraints
   But requires: relay performance benchmarking, migration script with data
   backfill, and updating 15+ call sites across 8 packages.

3. **Batch refactor opportunity:** If `channels.model` is refactored, include
   `api_keys.supported_models` and `api_keys.excluded_channels` in the same
   migration batch since they share the same junction table pattern and many
   of the same code paths.
