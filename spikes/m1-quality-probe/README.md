# M1 Quality Probe Helper

Helper for [M1-T8](../../docs/probes/m1-quality-probe.md): runs the five
sample goals through a live Athanor daemon, computes per-phase timing
and token totals from the EventLog, and writes per-sample results to a
markdown table.

## Usage

```bash
# from the project root:
make build

# start the daemon with the probe config
./bin/athanor serve -config config-probe.yaml -state-dir state-probe -addr 127.0.0.1:7420

# in another shell, build and run the probe
CGO_ENABLED=1 go build -o /tmp/probe ./spikes/m1-quality-probe/
ATHANOR_STATE_DIR=state-probe /tmp/probe
```

The probe writes:

- `state-probe/probe/probe-results.md` — table of per-sample results
- `state-probe/probe/artifact-N-*.txt` — final artifact content per sample

Re-run with `ollama stop <model>` first if you want to clear Ollama's
resident-model cache.

## Test

```bash
CGO_ENABLED=1 go test -count=1 ./spikes/m1-quality-probe/
```

The pure aggregation functions (`parseTransitionSequence`,
`computePhaseDurations`, `summarizeLLMCalls`,
`detectContextFloorViolation`, `renderMarkdownRow`) are
unit-tested. The HTTP plumbing is exercised end-to-end when the probe
runs.

## Design notes

- The probe talks to the daemon over the loopback HTTP API
  (`internal/api`), the same surface a CLI user would use. It does
  not import any `internal/*` packages, so it has no coupling to
  the daemon's internals — only its public API.
- Per-phase time is measured as the gap between consecutive
  `transition` events. The M1 engine records the transition
  immediately after the LLM call returns, so this gap equals the
  LLM call duration plus a few milliseconds — i.e. the
  user-visible "time spent in phase X".
- The `completed` state has no successor, so the probe does not
  record a duration for it. It is a terminal state, not a phase.
- Token counts are summed from the engine's `llm_call` audit
  events, which include `prompt_tokens` and `completion_tokens`
  (Ollama's `prompt_eval_count` and `eval_count`).
