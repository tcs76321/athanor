# M2 boundary plan review (2026-08-30)

Per ROADMAP §8 ("Plan reviews at milestone boundaries: update
Status table, re-estimate remaining work honestly, prune tasks
that reality made irrelevant"), this note records the
post-M2 review.

## M2 status

M2 is complete. T1–T6 all done. Gate G2 is fully green:

- **Structural half (CI):**
  `TestGateG2JobPodArgvCannotEscape` (M2-T6, this milestone)
  plus the four pre-existing structural tests in
  `internal/gate/gate_g2_test.go` from M2-T3/T4.
- **Behavioral half (opt-in):** five probes in
  `internal/jobpod/security_test.go`, all passing on the
  2026-08-30 reference run.

## Reality-vs-plan changes captured in this milestone

| Plan | Reality | Action |
|---|---|---|
| M2-T6 argv regression test would catch forbidden flags *added* to `args_*.go` source. | True; tested with an injected `--net=slirp4netns` and confirmed the test fails fast. | None. |
| The behavioral probes would use `podman exec` into a long-running container (per the spike pattern). | One-shot `podman run --rm` is simpler: argv is byte-identical to production except `--detach` is stripped, and the test captures exit code + output directly. | Recorded in the plan doc + ADR-0010 D3. |
| M2-T6 events would use a new `security` event-log category. | `config.Categories` is a closed set; adding `security` would force a config-schema change for a single use. Co-opting the existing `podman` category is correct because the events are literally about the pod surface. | ADR-0010 D5 records the choice. The `data_json` field on `events` carries a `probe` and `result` field, so a future M7-T3 alarm can filter for security-relevant events without a schema change. |
| "pytest-in-a-pod" Gate G2 clause has no current owner. | M3-T2 ("Evaluation phase: … test runs in Job Pod") is the natural owner. | ADR-0010 D6 records the ownership. Demo-m2.md "What the suite does NOT prove" section cross-references. No ROADMAP change needed; M3-T2's acceptance criterion already requires "test runs in Job Pod." |

## M3 plan re-check (no changes required)

Walked the six M3 tasks (T1–T6) plus the M7 quality probe T7
against what M2 actually shipped. Findings:

- **M3-T1 (full phase executor).** No change. The state
  machine in §8 and the temperature resolution in §13.1 are
  M2-independent. The engine in `internal/engine` has the
  M1 walking skeleton; M3 expands it.
- **M3-T2 (evaluation phase).** No change. "Test runs in Job
  Pod" is the M2-T4 `run_tests` route, exercised end-to-end
  here. The pytest-in-a-pod Gate G2 clause lives here per
  ADR-0010 D6.
- **M3-T3 (comparison phase).** No change. Pure function over
  EvaluationRecords; no pod interaction.
- **M3-T4 (budgets & retries).** No change. The recovery
  counters land in `system_state`; the persistence layer is
  in M2.
- **M3-T5 (git tool).** No change. M2-T6's argv regression
  test is orthogonal: even when the git tool runs in a
  pod, the §21.2 hardening flags are unchanged.
- **M3-T6 (crash-recovery E2E).** Could *use* the M2-T6
  integration suite as a smoke test before running the
  recovery E2E. Recording this as a M3-T6 follow-up, not
  a T6 scope creep.
- **M3-T7 (quality probe #2).** No change.

## Risks and open questions for M3

- **M3-T1 (full phase executor) is sized L (4–8h).** The
  state machine has 13 states in §8.1, but the M3 transition
  table is the §8.2 subset that M3 actually exercises. M1's
  walking skeleton is in M2, and M3 expands it. If the
  implementation runs over 8h, split into M3-T1a (state
  table) and M3-T1b (branch logic) per the existing task
  splitting guidance.
- **M3-T2 (evaluation phase) is also sized L.** This is the
  first task to actually run code in a pod at scale. The
  M2-T6 probes give us confidence the pod is contained;
  M3-T2 will be the first task to exercise the `run_tests`
  route with a real test runner and a multi-candidate
  workload. Wall-time budgets must accommodate the M2-T6
  per-probe evidence: ~5s for the Ollama probe (two 2s
  timeouts), 100–150ms for the others. The `evaluating`
  phase budget (default 600s per `config.example.yaml`) is
  generous; if M3 needs more, update the budget, not the
  schedule.
- **M3-T5 (git tool) and the M2-T6 structural test.** M3-T5
  runs `git` commands; if the tool ever needs to shell out
  from a pod, that is its own concern. The structural argv
  test catches forbidden flags in the `args_*.go` source
  files only; a future `git` tool would have its own
  argv-construction source, and a sibling structural test
  would be the right place to enforce git-specific
  containment. Recording this for M3-T5's planning.

## Conclusion

M2 is closed cleanly. The plan review found no ROADMAP
changes required for M3. The M2-T6 plan and ADR-0010 are
the durable record of the design decisions. The next unit
of work is M3-T1 (full phase executor).
