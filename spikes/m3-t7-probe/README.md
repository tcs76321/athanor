# M3-T7 — Dialectical-vs-Single-Shot Quality Probe

This is the M3-T7 spike (ROADMAP §7, M3-T7-a/b/c). It
measures the dialectical loop against a single-shot
baseline on three axes:

| Sub-measurement | Question | Tool |
|---|---|---|
| **T-a** Calibration | Does the LLM's reported confidence match observed accuracy? | Reliability diagram (confidence-binned accuracy) |
| **T-b** Stability at T=0 | Is the loop's quality reproducible across re-runs? | Variance of the rubric-graded score across N runs |
| **T-c** Diversity | Are the N divergence candidates actually different? | Average pairwise Jaccard distance over candidate text |

## Status: scaffold only

This commit is the planning + scaffolding commit. The
three sub-measurements land as separate spike
commands in follow-up work; they need a running
daemon, a model, and time, none of which a CI run can
supply. The scaffold:

- Defines the `dialecticalResult` contract (the per-sample
  data shape all three sub-measurements consume).
- Sets up the loopback HTTP client to the daemon.
- Adds tests for the JSON round-trip and env-override
  semantics.

## Why not land the experiments now

Each of the three sub-measurements is a separate
end-to-end experiment (sample 5–10 goals, run the
dialectical loop and a single-shot baseline on each,
collect rubric scores + confidence + candidate text,
post-process). The work is real but the *setup* is
what the scaffold pins; the actual measurement scripts
are follow-up commits that build on this scaffold
without redesigning it.

## How to run

```sh
# The daemon must be running on ATHANOR_ADDR (default loopback).
go run ./spikes/m3-t7-probe
```

The current scaffold prints a banner and exits 0; the
follow-up work replaces `main` with the real
per-sample runner.
