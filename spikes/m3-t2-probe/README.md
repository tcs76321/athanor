# M3-T2 Per-Task Probe

Helper for the M3-T2 commit 2.6 acceptance: runs five code-archetype
goals through a live Athanor daemon and reports the rubric-coverage
of the EvaluationRecords produced by the security persona. The
goal is to confirm that:

- every EvaluationRecord has populated `missing_criteria` /
  `security_issues` / `style_issues` arrays (even if empty)
- rubric items the LLM should have flagged are actually surfaced
  in the arrays
- the deterministic §19.3 guard (the `DecideWinner` pure function,
  M3-T2 commit 2.5) correctly downgrades verdicts that lack a
  backing record

The probe reuses the [M1-T8 helper](../m1-quality-probe/) pattern
but is narrower: it focuses on the rubric contract, not the full
M1 prose pipeline. Both probes talk to the daemon over the
loopback HTTP API and never import `internal/*`.

## Usage

```bash
# from the project root:
make build

# start the daemon with a probe config
./bin/athanor serve -config config-probe.yaml -state-dir state-probe -addr 127.0.0.1:7420

# in another shell, build and run the probe
CGO_ENABLED=1 go build -o /tmp/m3-t2-probe ./spikes/m3-t2-probe/
PROBE_RESULTS_DIR=state-probe/m3-t2-probe /tmp/m3-t2-probe
```

The probe writes:

- `m3-t2-probe/probe-results.md` — per-sample table with rubric
  coverage columns (`missing`, `security`, `style`).

## Samples

The five code-archetype samples are designed so every rubric item
has at least one goal where it should fire:

| # | Name                       | Rubric items that should fire               |
|---|----------------------------|---------------------------------------------|
| 1 | `fibonacci-clean`          | (clean baseline; arrays should be empty)   |
| 2 | `stringutils-no-docs`      | `DOCSTRINGS` (function has no docstring)   |
| 3 | `cache-with-todo`          | `NO PLACEHOLDERS` (function has `TODO`)    |
| 4 | `always-reverse-impossible` | `TESTS PASS` (the "always" claim fails)    |
| 5 | `todo-list-clean`          | (clean baseline; arrays should be empty)   |

The clean baselines (1, 5) prove the rubric does *not* fire on
a passing candidate. The other three prove the rubric *does*
fire on a failing candidate. Together they cover every line in
the §19.1 code rubric (`internal/engine/rubric.go`).

## Test

```bash
CGO_ENABLED=1 go test -count=1 ./spikes/m3-t2-probe/
```

The pure aggregation functions (`parseEvaluationRecords`,
`computeRubricCoverage`, `renderMarkdownRow`, `trunc`, `itoa`)
are unit-tested. The HTTP plumbing is exercised end-to-end when
the probe runs.

## Findings

After running, the per-sample table and a `What M3-T2 commit 2.6
did NOT test` section land in `docs/probes/m3-t2-probe.md`. The
probe is intentionally narrow: the M3-T7 quality probe (the
project's next major probe) is the right place to measure
calibration and stability.

## Design notes

- The probe reads `evaluation_record_created` audit rows from
  `GET /jobs/{id}/events`, not from a dedicated API endpoint. The
  daemon's external API does not yet expose evaluation records
  directly; the M3-T7 probe (or M3-T3 follow-up) will add the
  endpoint if needed.
- The em-dash placeholder (`—`) for empty union arrays in the
  markdown table is intentional: empty table cells are ambiguous
  in markdown renderers, and the em-dash makes "rubric fired
  nothing" visually distinct from "rubric fired something with a
  comma" (which renders as `a, b, c`).
- The `package main` shape means the probe does not need a
  `go.mod` file of its own; it builds with the project's
  module. `spikes/m1-quality-probe/` follows the same pattern.
