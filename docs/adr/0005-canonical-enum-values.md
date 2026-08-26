# ADR 0005 — Canonical enum values via rebuild migration 0003

**Status:** Accepted · **Date:** 2026-08-25 · **Refs:** ARCHITECTURE §9.1, §18.2, §20.2; ROADMAP M0; depends on [ADR 0003](0003-sqlite-driver-cgo-single-connection.md) (forward-only migration policy)

## Context

Review during M0 found the shipped schema drifting from ARCHITECTURE's object model: `corrections.severity` used `minor/major/critical`, `hitl_requests.severity` used `info/normal/high/critical`, and `artifacts.kind` omitted the `media` and `configuration` kinds. Per §2 of the ROADMAP, ARCHITECTURE wins for behavior and the discrepancy is fixed in both files. Because migrations are forward-only (§23.5), the CHECK constraints cannot be edited in place — SQLite requires a table rebuild to change them.

## Decision

Add migration `0003_canonical_enums.sql` immediately, rebuilding the three tables with canonical values:

- `corrections.severity`: `minor→low`, `major→medium`, `critical→critical` (§18.2: low|medium|high|critical)
- `hitl_requests.severity`: `info→low`, `normal→medium`, high/critical unchanged (§20.2)
- `artifacts.kind`: adds `media`, `configuration` (§9.1)

Each rebuild renames the old table, creates the new definition, copies rows with explicit CASE remapping, drops the old table, and restores indexes/triggers — all inside the migration transaction. `PRAGMA defer_foreign_keys = ON` makes row-copy ordering irrelevant; FK integrity is verified at COMMIT.

## Consequences

- ARCHITECTURE §9.1/§18.2/§20.2 are the single source of truth for these enums; the schema now enforces exactly those sets.
- The tables had no write paths yet (empty in practice), so the remapping carries no data risk today — it exists so every future environment converges on canonical values.
- Future enum changes follow the same rebuild recipe (rename → create → remap → drop → restore); the recipe is proven and testable here.
