# M3-T7 Quality Probe — Experiment Protocol

This document captures the experiment protocol for
the M3-T7-b (calibration) and M3-T7-c (verdict
stability) measurement experiments. The code path
that produces the data is already in place
(M3-T1's `comparison` event with `confidence`,
M3-T3's `comparison_unknown_winner_downgraded`,
the §19.3 deterministic guard); what's missing is
the *measurement* — running each verdict twice
under controlled conditions and reporting the
calibration + stability table.

The M3-T7-a (Jaccard diversity) measurement is a
code change in `internal/engine/diverge.go` that
emits the `divergence_jaccard` event. See the
`spikes/m3-t7-probe/` scaffold for the runner.

## T-b: Judge-confidence calibration

**Question:** Does the LLM's reported confidence
in the winning verdict match the observed
outcome (the rubric-graded score)?

**Setup:**

1. Sample N=10–20 goals from the M3-T2 probe's
   curated set (the 5 code-archetype goals plus
   a matching set of text-archetype goals).
2. Run each goal through the dialectical loop.
3. For each job that reaches `StateComparing`:
   - Record the `comparison` event's `confidence`
     field as the LLM's reported value.
   - Record the `EvaluationRecord.score` of the
     winning candidate as the observed quality
     (the rubric's numerical grade, 0.0–1.0).
4. Bin by confidence (e.g. 0.5–0.6, 0.6–0.7,
   0.7–0.8, 0.8–0.9, 0.9–1.0) and compute the
   mean observed score per bin.
5. Plot a reliability diagram: bin confidence
   on the x-axis, mean observed score on the
   y-axis. The diagonal y=x is "perfectly
   calibrated."

**Trigger conditions for follow-up work:**

- >20% of bins more than 0.2 off the diagonal:
  the §19.3 threshold (`min_judge_confidence`,
  default 0.7) is mis-calibrated. The first
  lever, per ROADMAP §7 M3-T7-b, is ADR-0013's
  prompt disclosure (showing the judge the
  truncation), not changing the default.
- The 0.7–0.8 bin is consistently below the
  diagonal (LLM over-claims in the "default
  accept" zone): raise the threshold to 0.8.

**Data inputs:**

- `events.data_json` for the `comparison` event
  (category=`jobs`, level=`info`).
- `evaluation_records.score` joined on
  `comparison.new_artifact_id`.

**Output:**

A `docs/probes/m3-t7-quality-probe.md` results
section (or a new file
`docs/probes/m3-t7-quality-probe-results.md`) with
the reliability diagram and the per-bin table.

## T-c: Verdict stability at T=0

**Question:** How often does the security persona
produce the same verdict when called twice with
the same input at T=0?

**Setup:**

1. Sample N=10–20 goals (same set as T-b).
2. For each goal, run the dialectical loop
   once, then re-run the comparison phase
   alone (3×) with `temperature=0` on the
   security persona.
3. Record the `winner` field for each of the
   3 re-runs.
4. Classify as:
   - 3-same (all three re-runs agree)
   - 2-1 (one outlier)
   - 3-way (no two agree)
5. Report the distribution.

**Trigger conditions:**

- <70% 3-same rate: the verdict is not
  reproducible. Per ROADMAP §7 M3-T7-c, the
  empirical case is then either (a) raise
  `min_judge_confidence`, (b) require two
  `EvaluationRecord` instances to agree
  before accepting `winner: new`, or (c)
  both.
- The 3-way rate is non-negligible: the
  §19.3 guard's `confidence > threshold`
  check is unreliable; the multi-instance
  consensus path is the right fix.

**Data inputs:**

- The `comparison` event's `winner` and
  `confidence` fields.
- The `comparison_unknown_winner_downgraded`
  event (counted as a stability failure in
  the 3-way bucket).

**Output:**

A `docs/probes/m3-t7-quality-probe.md` results
section with the verdict-distribution table
and a per-archetype breakdown.

## How the data is already in events

Both experiments read from the events table. The
M3-T1 engine already emits:

- `comparison` (winner, confidence, reasons)
- `comparison_unknown_winner_downgraded`
  (raw_winner, downgraded_to)
- `evaluation` (per-record score)

The M3-T7-a Jaccard event was added in commit
`a11ab63` and is also in events. None of T-b
or T-c requires a code change to the engine;
they require a *runner* that drives the
comparison phase repeatedly under controlled
conditions. The runner is the next commit in
the M3-T7 sequence (post-spike).

## Status

M3-T7-a: code landed (`a11ab63`).
M3-T7-b: protocol captured (this document);
runner is post-spike work.
M3-T7-c: protocol captured (this document);
runner is post-spike work.
