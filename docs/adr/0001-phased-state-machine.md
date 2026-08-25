# ADR 0001 — Phased introduction of the job state machine

**Status:** Accepted · **Date:** 2026-08-25 · **Refs:** ARCHITECTURE §8.2; ROADMAP M1-T4, M3-T1

## Context

§8.2 mandates the `diverging → evaluating` transition, but Job Pods (and therefore test-running evaluation) do not exist until M2/M3. Building the full state machine in M1 would drag container work into the walking skeleton, violating the milestone's containment goal — Gate G1 requires provably zero tool-execution paths.

## Decision

Introduce state-machine edges together with their enabling capability: M1 ships the skeleton without `evaluating`/`reflecting`; M3-T1 completes §8 exactly, with unit tests enforcing every mandatory edge from that point on. The partial §8.2 conformance during M1–M2 is an intentional, scoped deviation — not drift.

## Consequences

- M1 crash-recovery tests (M1-T4) cover only the subset of edges that exist.
- Gate G1's "grep-level proof that no tool execution exists" stays trivially satisfiable.
- The full §8 conformance suite lands in M3-T1; its illegal-transition tests guard against any skeleton-era shortcuts leaking forward.
