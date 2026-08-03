# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> Lodestar is derived from the octopus project; see [NOTICE.md](NOTICE.md) for the
> upstream lineage and attribution. Entries below cover Lodestar's own history only —
> upstream releases are documented in their respective repositories.

## [Unreleased]

### 🚀 Features
- Validate group routing conditions on save. Unknown keys and operators are now
  rejected at configuration time with an actionable message, instead of silently
  producing a group that can never match a request.

### 🐛 Bug Fixes
- Fail closed on unknown condition keys and non-numeric operands, so a typo can no
  longer turn a rule into an always-true match.
- Populate every condition key at both relay call sites.
- Return delta-seconds in `Retry-After` and fix the hourly email quota accounting.

### 📚 Documentation
- Replace real deployment coordinates (database host, SSH endpoint, production
  domain) with `YOUR_*` placeholders throughout the docs.
- Fix stale `OCTOPUS_*` environment variable names in the Chinese README; the
  binary reads the `LODESTAR_*` prefix.
