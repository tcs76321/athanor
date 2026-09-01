# ADR 0014 — Drop the M2-T4 pod sub-steps from `phaseSynthesize` (test execution lives in `phaseEvaluate`)

**Status:** Accepted · **Date:** 2026-09-01 · **Refs:** ARCHITECTURE §8.1, §13.1, §25; ROADMAP M3-T2; ADR-0009; `docs/m3-t1-plan.md:114–118`

## Context

The §13.1 Dialectical Loop has six phases. Phases 3 (Evaluating) and 5 (Synthesizing) both touch the Job Pod, and the boundary between them has drifted since M2-T4.

The M2-T4 implementation (ADR-0009) introduced two engine methods, `runCodeInPod` and `runTestsInPod` (`internal/engine/pod_wiring.go:36–154`), and put them in `phaseSynthesize` as sub-steps. The M3-T1 close-out (`docs/m3-t1-plan.md:114–118`) recorded the intent: "Linter adapter and test-runner in the evaluating phase (M3-T2). M3-T1's evaluating phase just calls the security persona and persists the record; M3-T2 brings in the per-archetype linter and moves the test-runner from `phaseSynthesize` to `phaseEvaluate`."

The M3-T1 commit landed a *partial* version of that intent: `phaseEvaluate.evaluateCandidate` (`internal/engine/evaluate.go:168–212`) already calls `runner.RunTests` per candidate and folds the exit code into `verdict.Passed`:

```go
if p.Archetype == project.ArchetypeCode && e.runner != nil {
    // ... audit "substate_entered" "running_tests" ...
    req := toolenvelope.ExecuteRequest{Command: "pytest -q"}
    res, runErr := e.runner.RunTests(ctx, j.ID, req)
    // ... testsPassed = res.ExitCode == 0 ...
    // ... audit "substate_exited" "running_tests" ...
}
```

The `phaseSynthesize` code, however, **still** calls both sub-steps after the LLM refines the candidate into the final artifact (`phases.go:193–203`):

```go
if p.Archetype == project.ArchetypeCode {
    if err := e.runCodeInPod(ctx, j, p, t); err != nil { return err }
    if err := e.runTestsInPod(ctx, j, p, t); err != nil { return err }
}
```

This produces three concrete problems:

1. **Tests run twice when a runner is wired.** The pytest exit code is captured in `phaseEvaluate` and *also* in `phaseSynthesize`. The second call wastes pod boot + wall time; the two results can disagree (e.g., the pod started by `evaluate` has been torn down, so `synthesize`'s `RunTests` runs in a fresh pod and may behave differently).
2. **Code is executed only after evaluation.** A candidate that fails tests in `phaseEvaluate` *still* has its code executed in `phaseSynthesize` — after the LLM has produced a refined version, after the final artifact is persisted. The M2-T4 execute_code path runs even for candidates that will be `rejected`. This is wasted compute and a containment audit-trail concern (an "executed" row for code that the engine will never accept).
3. **The §13.1 phase table and the §8.1 state machine disagree on which phase owns "code in a pod."** ADR-0009 chose `synthesizing` as a sub-state explicitly because the M2-T4 work was the only one touching pods. M3-T1 added per-candidate test execution to `evaluating`; the M2-T4 `synthesizing` sub-steps are now the legacy. The two should converge.

The fix is a pure deletion + the right home for code execution: `runCodeInPod` moves into `phaseEvaluate.evaluateCandidate` (per candidate, before the security-persona verdict is computed), and `phaseSynthesize` becomes the pure LLM-refines-candidate step. After the move, `phaseSynthesize` is identical for every archetype.

## Decision

Three coordinated changes:

1. **Move `runCodeInPod` from `phaseSynthesize` to `phaseEvaluate.evaluateCandidate`.** It runs *before* the security-persona verdict is computed, so the `code` artifact (exit code, stdout, stderr, duration) is part of the evidence the LLM judge sees. The test command already runs here (`evaluate.go:186–207`); the code execution is the missing partner.
2. **Drop the `runCodeInPod` + `runTestsInPod` calls from `phaseSynthesize`.** After the move, `phaseSynthesize` does the LLM refinement and the §9.3 artifact persistence; nothing else. The `pod_wiring.go` file keeps the helpers (the test runner is still called from `phaseEvaluate.evaluateCandidate`), but `runCodeInPod` becomes the only call path for `runner.RunCode`.
3. **Make `phaseSynthesize` archetype-agnostic.** With the sub-steps gone, the `if p.Archetype == project.ArchetypeCode` branch in `phaseSynthesize` (and the `finalKindFor` call) is unchanged — `finalKindFor` was already the right per-archetype decision for *which artifact kind to persist* — but the sub-state "running_tests" emission is now emitted only from `phaseEvaluate`. The audit-event taxonomy gets simpler: one `substate_entered`/`substate_exited` pair per candidate, in `evaluating`.

### D1. The new `phaseEvaluate.evaluateCandidate` shape

The current `evaluate.go:168–212` calls `runner.RunTests` and folds the result into `verdict.Passed`. After this ADR it also calls `runner.RunCode` (the M2-T4 execute_code path) and persists the `code` artifact the same way `runCodeInPod` does today. The per-candidate audit events become:

- `substate_entered` `code_executed` (was `runCodeInPod`'s audit)
- `code_executed` (was the audit emitted by `runCodeInPod` on success)
- `substate_entered` `running_tests`
- `substate_exited` `running_tests` (was the audit emitted by `evaluate.go:201–207`)

The LLM verdict's `passed` field is reconciled with the test result exactly as today (`evaluate.go:241–249`): if `testsPassed == true` and the LLM's `missing_criteria` and `security_issues` are empty, `verdict.Passed = true`. The `code_executed` artifact is the audit log of "what the pod did" (exit code, stdout, stderr, duration) — it's a `code`-kind artifact in the existing §9.1 kind table; M2-T4 already added it (`README.md:138` documents the sub-step).

### D2. The new `phaseSynthesize` shape

`phases.go:162–207` after this ADR:

```go
func (e *Engine) phaseSynthesize(ctx context.Context, j job.Job) error {
    p, t, err := e.contexts(ctx, j)
    if err != nil { return err }
    candidate, err := e.artifacts.LatestForJob(ctx, j.ID, artifact.KindProposal)
    if err != nil { return fmt.Errorf("loading divergence candidate: %w", err) }
    candidateContent, err := e.artifacts.ReadContent(ctx, candidate.ID)
    if err != nil { return fmt.Errorf("reading divergence candidate: %w", err) }

    instructions := "CANDIDATE PROPOSAL (from the divergence phase — refine into the final artifact):\n" + string(candidateContent)
    resp, err := e.call(ctx, j, p, t, llm.PhaseSynthesizing, llm.RoleMain, instructions)
    if err != nil { return err }

    kind := finalKindFor(p.Archetype)
    if prev, err := e.artifacts.LatestForJob(ctx, j.ID, kind); err == nil {
        if _, err := e.artifacts.NewVersion(ctx, prev.ID, []byte(resp.Content)); err != nil {
            return fmt.Errorf("versioning final artifact: %w", err)
        }
    } else {
        if _, err := e.artifacts.CreateDraftFor(ctx, p.ID, t.ID, j.ID, kind, []byte(resp.Content)); err != nil {
            return fmt.Errorf("persisting final artifact: %w", err)
        }
    }
    _, err = e.jobs.Transition(ctx, j.ID, job.StateComparing)
    return err
}
```

The `if p.Archetype == project.ArchetypeCode { runCodeInPod...; runTestsInPod... }` block is gone. The function becomes archetype-agnostic — every archetype runs the same LLM refinement, the same artifact persistence, the same transition to `comparing`.

### D3. The `pod_wiring.go` shape after this ADR

The two helpers stay, but their call sites change:

- `runCodeInPod` is called from `phaseEvaluate.evaluateCandidate` (per candidate) instead of from `phaseSynthesize`. The function body is unchanged; the audit-event shape is unchanged; the `KindCode` artifact persistence is unchanged.
- `runTestsInPod` is **deleted**. `evaluate.go:186–207` already calls `runner.RunTests` directly; the helper duplicates that logic with subtly different audit-event names. The deletion removes one source of truth for "what the test run did."

The deleted helper leaves a 47-line gap in `pod_wiring.go`; the file shrinks to ~100 lines and now exports only `runCodeInPod` (a name that the M2-T4 commit gave it; the name stays for grep-ability in the M2-T6 argv security suite). Future M3-T2 work that adds a linter sub-step will live in a new file `internal/engine/lint.go` (or similar) and follow the same per-candidate pattern as `runCodeInPod`.

### D4. The audit-event taxonomy

The M2-T4 sub-state events were emitted from two different files (`pod_wiring.go` and `evaluate.go`) with subtly different shapes. After this ADR:

| Event | Emitted from | Shape |
|---|---|---|
| `substate_entered` (`code_executed`) | `phaseEvaluate.evaluateCandidate` | `{event, phase, substate, candidate, archetype}` |
| `code_executed` (success) | `phaseEvaluate.evaluateCandidate` | `{event, exit_code, duration_ms, stdout_len, stderr_len, archetype}` |
| `substate_entered` (`running_tests`) | `phaseEvaluate.evaluateCandidate` | `{event, phase, substate, candidate}` |
| `substate_exited` (`running_tests`) | `phaseEvaluate.evaluateCandidate` | `{event, phase, substate, candidate, exit_code}` |
| `code_executed` (skipped) | `phaseEvaluate.evaluateCandidate` | `{event, skipped, reason, archetype}` |

`phaseSynthesize` emits zero sub-state events. The audit trail is now strictly: divergence candidates → per-candidate (code execute + tests) → verdict → synthesize artifact → compare.

### D5. Tests

Three test updates:

1. **`multicandidate_test.go` E1** (`TestRun_FullDialecticalChain_ThreeCandidates_CodeArchetype`) gains three assertions:
   - `phaseCalls()["synthesizing"] == 1` (no change from today, but the assertion becomes meaningful: synthesize is one LLM call, period).
   - `audit_count("substate_entered", "code_executed") == 3` (one per candidate, all in `evaluating`).
   - `audit_count("substate_entered", "running_tests") == 3` (one per candidate, all in `evaluating`).
   - `audit_count("code_executed") in synthesizing == 0` (the explicit M3-T2 acceptance from the plan: synthesize must not call the runner).
2. **`multicandidate_test.go` E2** (`TestRun_AllCandidatesFail_ReflectsThenFails`) gains the matching assertions: 2 reflection iterations, 6 divergence + 6 evaluating sub-state pairs, no `synthesizing` runner calls. The "ends in `failed`" assertion is unchanged.
3. **`pod_wiring_test.go`** (new, in `internal/engine/`): a focused unit test for the moved `runCodeInPod` — the audit-event sequence, the `code` artifact persistence, the `ErrToolDisallowed` short-circuit. Today this is exercised only through `multicandidate_test.go`; the focused test makes the M3-T2 contract legible.

### D6. The Gate G2 implications

Gate G2 (`internal/gate/gate_g2_test.go`) is unaffected. The new path from `phaseEvaluate` to the internal API is the same path that M2-T4 added (bearer-token + envelope + per-job binding); no new routes, no new tools. The `lint` route that M3-T2 adds is a separate change (the linter adapter commits, not this one). The §21 containment boundary is the same.

### D7. M2-T4 sub-step semantics for non-`code` archetypes

`phaseSynthesize` is unchanged for `text`, `document`, `data`, `media` archetypes — the `finalKindFor` mapping is the right per-archetype decision for *which artifact kind to persist*, and the LLM refinement is the same for every archetype. The M1 walking skeleton pattern (a `text` goal is a prose draft, persisted with the right `Kind` and transitioned to `comparing`) is preserved exactly.

The M2-T4 sub-step code path was gated on `p.Archetype == project.ArchetypeCode` (`phases.go:196`); the M3-T2 move puts the same gate on the new `runCodeInPod` call site in `phaseEvaluate.evaluateCandidate`. Non-`code` archetypes emit no `code_executed` events; the audit trail is silent on that for `text`/`document`/etc., which is correct (no pod call happened).

## Consequences

- Tests run once per candidate, in `phaseEvaluate`, with their result folded into the verdict. The double-run in `phaseSynthesize` is gone; the divergence between the two pod results (fresh pod, no shared state) is gone.
- Code is executed only in `phaseEvaluate`, per candidate, before the security-persona verdict. A candidate that fails tests never has its code executed as a "final artifact" (a side benefit: the `code` artifact row is the audit of "the pod actually ran the code," not "the engine ran the code as a final step").
- `phaseSynthesize` is archetype-agnostic. Every archetype runs the same LLM refinement, the same artifact persistence, the same transition to `comparing`. The `if p.Archetype == project.ArchetypeCode` branch in `phaseSynthesize` is the only archetype-specific code, and it stays only for the `finalKindFor` decision (which is right: a `text` goal produces a `document`-kind artifact, a `code` goal produces a `code`-kind artifact).
- The audit-event taxonomy is consolidated in one place (`phaseEvaluate.evaluateCandidate`). The M2-T4 sub-step names (`substate_entered`/`substate_exited`/`code_executed`/`tests_run`) are all emitted from one function, in one order, per candidate. The M3-T7 quality probe can use the audit-event count to assert "every candidate was tested before evaluation."
- The `pod_wiring.go` file shrinks by ~47 lines (`runTestsInPod` deleted). The surviving `runCodeInPod` keeps its M2-T4 audit-event names so the M2-T6 argv security suite (`TestGateG2JobPodArgvCannotEscape`) continues to pass without modification.
- The E1/E2/E4/E5 dialectical-loop test suite (`multicandidate_test.go`) gets stronger assertions. The pre-M3-T2 tests asserted end-state semantics (artifact accepted/rejected, job completed/failed); the M3-T2 tests additionally assert the per-phase LLM call count and the per-candidate sub-state audit-event count, which is the structural proof that the move happened.
- The `runner ToolRunner` interface (`internal/engine/engine.go:73–88`) is unchanged. The interface already supported `RunCode` and `RunTests` independently; the M3-T2 change is in the *call site*, not the interface.

## Not in M3

- The linter sub-step (the M3-T2 rubric's `## LINTER` check) lands in a separate commit with a separate ADR, because the linter is a *new* capability (not a relocation) and the `lint` internal API route is a Gate G2 surface change.
- Per-archetype sub-step ordering. Today every archetype runs the same `code_executed → running_tests → verdict` sequence in `phaseEvaluate`. A future `text` archetype might add a `spell_check` sub-step; that's a M3-T2 follow-up.
- Streaming the test command's output. M2-T4 returns the final `ExecuteResult`; long-running test suites may want a streaming view. M2-T4 ADR-0009 §"Not in M2-T4" deferred this; M3-T2 does not pick it up.

## Forward references

- M3-T2 commits 2.1 (this ADR) + 2.2 (rubric) + 2.3 (linter) are the three M3-T2 evaluation-phase changes. They land as separate commits because the rubric and the linter are independent of the move and the move is the highest-risk of the three (it touches every code-archetype job's audit trail).
- M3-T7 (quality probe #2) measures the post-move behavior: 3 candidates × (1 code exec + 1 test run) per divergence cycle, not 3 × 2. The probe's cost baseline halves.
- M3-T3 (Comparison phase hardening) inherits the cleaned-up `phaseSynthesize`; the per-archetype `finalKindFor` call is unchanged, and the comparison phase sees the same artifact content as before.
