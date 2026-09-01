# M3-T1 — Full phase executor (retrospective plan)

**Status:** Closed (4 commits on `main`: `c73a115`, `9e99e20`, `9cb4ccd`, `62ae865`) · **Milestone:** M3 (Dialectical Loop v1) · **Refs:** ROADMAP M3-T1; ARCHITECTURE §8, §13, §19; ADR-0001, ADR-0006

## Goal

Close M3-T1 by implementing the §13.1 Dialectical Loop's six
phases end-to-end on top of the M1 walking skeleton, with every
transition persisted (§8.2 crash safety), every state in the §8.1
machine reachable, and a deterministic §19.3 comparison guard
that downgrades an LLM `winner: new` verdict when no
`EvaluationRecord` has `better_than_previous: true` with
confidence > `min_judge_confidence`.

## Acceptance criteria (from ROADMAP M3-T1)

> State machine in §8 implemented exactly; every transition
> persisted; unit tests cover all legal/illegal edges; temperature
> resolution follows §13.1 precedence (ExplorationPath stage >
> phase > persona).

## What got built (4 commits)

| # | Commit | Surface | Purpose |
|---|---|---|---|
| 1 | `M3-T1: full §8 state machine + transition table + tests` (`c73a115`) | `internal/job/state.go` + `internal/engine/phases.go` + `internal/job/state_test.go` | §8.1 13-state schema, `CanTransition` table, engine dispatch |
| 2 | `M3-T1: EvaluationRecord schema + repository` (`9e99e20`) | `internal/evaluation/{record.go,repo.go,repo_test.go}` + `migrations/0007_evaluation_records.sql` | §19.2 durable record; one-row + audit-event-in-tx invariant |
| 3 | `M3-T1: full phase executor in internal/engine` (`9cb4ccd`) | `internal/engine/{diverge.go,evaluate.go,reflect.go,compare.go,phases.go}` + `internal/artifact/queries.go` (`LatestAcceptedByProject`) | §13.1 six phases wired; §19.3 guard; per-candidate EvaluationRecord |
| 4 | `M3-T1: close out — reflection budget, idempotent evaluation, dialectical tests` (`62ae865`) | `internal/engine/{engine.go,evaluate.go,reflect.go,engine_test.go,multicandidate_test.go}` | four follow-up bug fixes + E1/E2/E4/E5 dialectical-loop tests |

## The dialectical-loop test suite (E1–E5)

`internal/engine/multicandidate_test.go` lands with the close-out
commit. It uses the existing `newEnv` test fixture plus a
verdict-queue mechanism on the Ollama fake so the security
persona can return scripted per-candidate verdicts (the M1
prose-returning default is overridden by the queue when present).
E3 (budget exhausted at 1 iteration) is structurally covered by
E2: the `maxReflectionIterations` constant is 2, and E2 exercises
exactly two failed reflection iterations.

| Test | Path | What it proves |
|---|---|---|
| E1 `TestRun_FullDialecticalChain_ThreeCandidates_CodeArchetype` | happy path | 9 LLM calls (1+3+3+1+1), 3 EvaluationRecords, 3 `proposal` artifacts, final `code` artifact `accepted` |
| E2 `TestRun_AllCandidatesFail_ReflectsThenFails` | reflection budget exhausted | 2 reflection iterations, 6 divergence + 6 evaluation calls, ends in `failed` |
| E4 `TestRun_ComparisonPicksPreviousWhenNewIsWorse` | §19.3 guard downgrade | LLM says `winner: new` but no `better_than_previous`; engine downgrades to `previous`, new artifact `rejected`, previous stays `accepted` |
| E5 `TestRun_ComparisonPicksNoneWhenAllFail` | no passing candidate | all candidates fail; no comparison call; ends in `failed` |

## The four close-out bug fixes

The close-out commit (4) is materially a bug-fix commit, not a
"polish" commit. The four findings:

1. **Stale `RecoveryFlag` in `phaseReflect`.** The `j job.Job`
   parameter passed to phase handlers is a snapshot taken at the
   top of `engine.Run`'s `for` loop. `phaseReflect` was reading
   `j.RecoveryFlag` directly, so the iteration counter lagged the
   DB by one cycle. Fix: re-fetch the job inside `phaseReflect`
   before reading the counter.

2. **`engine.Run` wiping the reflection counter.** After every
   successful step, `engine.Run` cleared `RecoveryFlag` to recover
   from a previous kill-mid-phase. But the M3-T1 reflection
   counter co-opts the `RecoveryFlag` column with a `"reflect-N"`
   prefix, so the clear step reset the counter to 0 on every
   successful step and the budget check would never fire. Fix:
   skip the clear when the flag starts with `"reflect-"`.

3. **`phaseEvaluate` re-evaluating past cycles' candidates.**
   `listCandidateArtifacts` returns *all* `proposal` artifacts
   for the job, regardless of which divergence cycle produced
   them. On the second `phaseEvaluate` (after a reflection cycle
   re-enters divergence), the engine re-evaluated the previous
   cycle's 3 candidates plus the new 3, doubling LLM calls and
   making the test-time verdict queue impossible to size. Fix:
   filter to "unevaluated" proposals using
   `eval.ListByJob(artifactIDs)`. The "all-already-evaluated"
   case routes through `passCount == 0` → `reflecting` (not
   `failed`), so a re-entry where divergence produced no new
   candidates still hits the reflection path.

4. **`phaseReflect` budget check after divergence.** The original
   check fired *after* `phaseReflect` had already transitioned
   the job to `StateDiverging`, so a 2-iteration budget actually
   ran 3 divergence + 3 evaluation cycles before failing. Fix:
   the budget check now runs *after* bumping the counter and
   *before* the diverging transition, so the last allowed
   iteration fails immediately.

## What is deliberately deferred (M3-T4)

- **`maxReflectionIterations` is hard-coded to 2.** A config
  field (`execution.max_reflection_loops`) and a typed counter
  in `system_state` are M3-T4. The constant lives in
  `internal/engine/reflect.go` and is documented as the M3-T1
  placeholder.
- **The reflection counter co-opts `RecoveryFlag` with the
  `"reflect-N"` prefix.** M3-T4 will move it to `system_state`
  with a proper typed counter. The current scheme works but
  requires the explicit `HasPrefix("reflect-")` guard in
  `engine.Run` to keep the recovery-clear step from wiping it.
- **The "difficulty hint" from `phasePlan` is not consumed by
  `phaseDivergeN`.** `cfg.Execution.DivergenceCandidates` is the
  only knob; M3-T2 may feed a planner-driven hint in.

## Out of scope (deferred to later milestones)

- **ExplorationPath stages (§13.2).** The temperature-resolution
  seam is wired (`llm.ResolveTemperature` accepts a
  `stageOverride` pointer) but every caller passes `nil` today
  per ROADMAP §7 backlog. ROADMAP §1: "Excellence is a moving
  target; YAGNI until backlog." Path persistence is post-M7
  per the Deliberately Deferred list.
- **Linter adapter and test-runner in the evaluating phase
  (M3-T2).** M3-T1's evaluating phase just calls the security
  persona and persists the record; M3-T2 brings in the
  per-archetype linter and moves the test-runner from
  `phaseSynthesize` to `phaseEvaluate`.
- **`decideWinner` as a pure function (M3-T3).** The §19.3
  comparison rule is currently interleaved with the LLM call in
  `phaseCompare`. M3-T3 extracts it.
- **Per-phase wall-time budgets (M3-T4).** The seam is there
  (`cfg.Execution.phase_wall_time_budgets` is already in
  config) but no `context.WithTimeout` is applied yet.

## Risks surfaced during M3-T1

- **The "winner" verdict of `comparison` is a string with three
  legal values.** The parser normalizes anything else to
  `"none"` (the safest default), but a model that returns
  `"new "` (with whitespace) or `"new\n"` would be normalized
  to `"none"` and the job would fail even when the candidate
  genuinely passed. The `parseComparisonVerdict` function
  trims JSON wrapping but not the value. A follow-up could
  `strings.TrimSpace(v.Winner)` before the switch. Tracking
  as a polish item; not blocking.
- **`phaseEvaluate` uses `LatestAcceptedByProject` for the
  `compared_against` field.** A project with multiple accepted
  artifacts in history (after a `superseded` → `accepted` cycle)
  would compare against the *latest* accepted one, not the
  one that was `accepted` at the time of the prior `phaseCompare`.
  This is the right semantics (the §19.3 rule is "better than
  the current best," not "better than the previous attempt at
  this task"), but it is worth noting.
- **The `running_tests` sub-state is event-row only.** The §8.1
  "tracked sub-state" note is honored: the Job row's `state`
  column does not get a `running_tests` value; instead, the
  M2-T4 sub-steps emit `substate_entered` /
  `substate_exited` audit rows. The state machine is unchanged
  in M3. The risk: a reader who filters on `state =
  running_tests` will see no rows. The `events` table is the
  canonical record.

## Evidence

- `make check` is green: `golangci-lint` 0 issues, `go vet`
  clean, `go test -race ./...` all green.
- Gate G1 (`internal/gate/gate_test.go`): 6 tests pass.
- Gate G2 (`internal/gate/gate_g2_test.go`): 5 tests pass.
- Benchmark: `docs/benchmarks/engine-m3.txt` records the 4.51
  ms/op baseline (vs 2.32 ms/op for M1, 1.94× — within the
  expected N=3 multi-candidate expansion).

