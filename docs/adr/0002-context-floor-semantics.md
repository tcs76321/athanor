# ADR 0002 — Context floors are role-scoped, not task-scoped

**Status:** Accepted · **Date:** 2026-08-25 · **Refs:** ARCHITECTURE §12.3, §12.6, §29

## Context

`coding_floor` (32768) exceeds the `tall` persona's context target (16384). Read as a per-task constant, every code-project DAG decomposition or planning run would fail the §12.3 floor check and escalate to HITL — making the shipped default configuration unusable for its primary workload.

Options considered:

1. **Raise `tall`'s target to ≥ 32768** — contradicts `tall`'s purpose (hard reasoning benefits from constrained, dense context) and raises hardware minimums for everyone.
2. **Keep floors per-task and special-case decomposition tasks** — a brittle, undocumented exception that future contributors will trip over.
3. **Scope floors by role/phase** (chosen).

## Decision

Floors bind to the personas whose phases consume full-fidelity content chunks (`main`, `alternative`, `security`, `wide`). `tall` is exempt during Planning and DAG Decomposition only, because those phases work from task descriptions, acceptance criteria, and Dormant Index metadata rather than raw code. Feasibility is checked per (persona, phase) as `required_context = max(persona_context_target, role_applicable_floor)`. Codified in ARCHITECTURE §12.6.

## Consequences

- The default configuration works out of the box for code projects.
- M1-T2's feasibility check implements the (persona, phase) rule, not a single per-task number.
- If a future phase requires `tall` to consume large raw chunks directly, that phase must either delegate ingestion to `wide` or raise the floor check explicitly. Silent reduction below any bound remains prohibited.
