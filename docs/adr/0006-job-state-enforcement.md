# ADR 0006 — Job states are schema-enforced; transitions are compare-and-swap

**Status:** Accepted · **Date:** 2026-08-26 · **Refs:** ARCHITECTURE §8.1–§8.2, §23.3; ROADMAP M1-T4; builds on [ADR 0001](0001-phased-state-machine.md), [ADR 0005](0005-canonical-enum-values.md)

## Context

M1-T4 introduces the job state machine. Two enforcement questions arose:

1. **Where is the §8.1 state set enforced?** Every other enum in the schema (artifact kinds, severities, statuses) carries a CHECK constraint — `jobs.state` was the lone exception because §8.1 was not final when migration 0001 shipped. Relying on Go alone would make the schema the weakest link: any future write path (a debug script, a migration, a bug) could store a state that no code understands.
2. **How are transitions made atomic?** §8.2 requires "state serialized after every transition, atomically." A read-then-write pattern without a guard admits lost updates if two goroutines ever transition the same job.

## Decision

- Migration `0004_job_state_check.sql` rebuilds `jobs` (ADR-0005 recipe) adding a CHECK over all twelve §8.1 states — including `evaluating`/`reflecting`/`awaiting_approval`, which only become *reachable* in M3/M6. The schema accepts the full §8.1 set; the Go transition table (ADR-0001) decides what is *legal now*. This avoids a third rebuild when those edges arrive.
- Because `jobs` is the first rebuilt table with inbound foreign keys, the recipe needed two corrections, both verified empirically:

1. `ALTER TABLE … RENAME TO` rewrites child FK clauses to the new name — **even under `PRAGMA legacy_alter_table=ON`** (the pragma protects triggers and views, not FK clauses). The migration therefore follows SQLite's documented schema-change procedure: create `jobs_new`, copy rows, `DROP TABLE jobs`, then `ALTER TABLE jobs_new RENAME TO jobs`. Child references to `jobs` are never rewritten.
2. Deferred foreign keys do not survive a parent `DROP`+`RENAME` — `PRAGMA defer_foreign_keys` still fails at COMMIT. Since `PRAGMA foreign_keys` is a no-op inside a transaction, the migration *runner* now disables enforcement around each migration and gates the transaction on an explicit `PRAGMA foreign_key_check`: a migration that ends with broken references fails and rolls back. Migrations may rebuild parent tables freely; they may never commit broken references.

Future rebuilds of any parent table must use both halves of this recipe.
- The rebuild also adds `paused_from`, which records the state an active job was paused from so `paused → <paused_from>` is an explicit, crash-safe resume edge. A CHECK restricts it to non-terminal, non-queued states and requires it to be NULL unless `state = 'paused'`.
- `Repository.Transition` performs a compare-and-swap: `UPDATE jobs SET state = ? WHERE id = ? AND state = <expected>`. Zero affected rows means a concurrent transition won — the loser gets a typed conflict error, never a silent overwrite. The transition row update and its `events` audit entry are written in one transaction, so the audit trail never diverges from committed state.

## Consequences

- The DB and Go agree on the state alphabet; Go remains the sole authority on legality (the CHECK deliberately accepts states that are currently unreachable).
- Crash recovery stays simple: resume from the last committed `state`; `paused` jobs resume to exactly `paused_from`.
- CAS transitions serialize concurrent writers per job without table locks; the single-connection pool (ADR 0003/0004) keeps this cheap.
- Changing the §8.1 state set later requires another rebuild migration — appropriate, since state sets are protocol, not tuning.
