# ADR 0004 — Store concurrency model

**Status:** Accepted · **Date:** 2026-08-25 · **Refs:** ARCHITECTURE §8, §23; ROADMAP M1-T4, M3-T2, M5; depends on [ADR 0003](0003-sqlite-driver-cgo-single-connection.md)

## Context

ADR 0003 caps the pool at one connection for extension affinity. The dialectical loop (M3) will evaluate candidates concurrently, and M1's state machine persists after every transition. The concurrency model must be decided before M1 so job-state persistence doesn't collide with concurrent candidate work.

Options considered:

1. **Keep single-connection access everywhere** — simplest; all DB calls serialize at the pool. Correct but makes concurrent evaluation paths wait on unrelated reads/writes as load grows.
2. **Read pool + dedicated writer** — a small read-only pool plus one write path. Regains concurrent readers under WAL, but re-introduces per-connection extension loading concerns for vec0 on every read connection.

## Decision

Phase it:

- **Now through M4:** option 1. One connection; callers treat `*store.Store` methods as the sole DB surface. Contention at this stage is negligible (single daemon, ≤2 concurrent jobs by default config).
- **From M3 onward, only if profiling shows contention:** introduce a small read pool for plain (non-vec0) queries, keeping exactly one extension-affinity connection reserved for vec0 operations. Reads stay on `QueryRow`/`Query` paths; writes always go through the single connection.
- **In M5:** the vec0-affinity connection is carved out explicitly (as documented in `docs/sqlite-setup.md`) rather than shared with general traffic.

## Consequences

- No code change today; this ADR sets the boundary future tasks implement against.
- M3-T2/M3-T6 tests must not assume intra-job DB parallelism; candidate evaluation parallelism comes from LLM/pod work, not DB concurrency.
- Any future package reaching for `Store.DB()` directly is an ADR-worthy deviation; prefer adding methods to `internal/store`.
