# ADR 0009 — Engine-driven tool execution via internal API for M2-T4

**Status:** Accepted · **Date:** 2026-08-27 · **Refs:** ARCHITECTURE §25; ROADMAP M2-T4; ADR-0007; ADR-0008

## Context

M2-T3 (ADR-0008) shipped the per-job bearer-token internal API with
three routes: `GET /internal/v1/jobs/{id}` (job context),
`POST /internal/v1/jobs/{id}/heartbeat`, and
`POST /internal/v1/jobs/{id}/log`. Those routes prove the auth
mechanism but do not yet give the pod anything to do. M2-T4 closes
the loop: two new tool routes (`execute_code`, `run_tests`) backed
by a per-job allowlist, with the engine as the orchestrator that
decides when to call them.

The ROADMAP M2-T4 line is intentionally light on internals: "Internal
API: `execute_code`, `run_tests` enforcing each job's tool allowlist."
This ADR records the design decisions behind the implementation:
who drives the call, where the allowlist lives, why the work is a
sub-step inside `synthesizing` (not a new state), how the engine
authenticates to the internal API, and what Gate G2 covers.

## Decision

### D1. Engine drives; pod is the worker

The engine is the orchestrator. It decides when to call
`execute_code` (after the LLM has produced a proposal in `diverging`)
and when to call `run_tests` (after `execute_code` returns). The pod
is the worker: it receives the call, runs the code, and returns
`{exit_code, stdout, stderr, duration_ms}`. The pod never pulls
work on its own.

This matches the M1 walking-skeleton pattern (engine is the
deterministic phase machine; the pod is an effect). The M3
dialectical loop will keep this shape and add N-candidate
divergence on top.

The "pod pulls the prompt" alternative was considered and rejected:
it would invert control flow, make the engine a passive observer,
and require the pod to be aware of phase ordering, persona
selection, and the prompt-assembly v1 surface. Every one of those
concerns belongs in `internal/engine` today; moving them to the pod
duplicates state.

### D2. The tool envelope lives in `internal/toolenvelope`

A new internal package `internal/toolenvelope` holds the closed
set of tools and the per-job allowlist type. The package is the
### D3. The per-job allowlist is config-default ∪ per-task override

`config.job_pod.default_tools` is the daemon-wide default envelope.
A task may declare `allowed_tools_json` (migration 0005) to
override the default. The merge rule in `(*project.Repo).EnvelopeFor`
is "if the task override is non-empty, use it; otherwise fall back
to the config default". An empty envelope is valid and means
"this job may not call any tool".

The engine never looks at the envelope directly. The engine
looks at the runtime — the per-job envelope is consulted in the
internal API handler before dispatch, and rejected calls return
403 with an audit event (`event: tool_disallowed`). The engine's
contract is "I want to call execute_code for this job"; whether
the call is allowed is the handler's responsibility. The engine
treats 403 as a normal result and continues to `comparing` (M3
will turn 403 into a HITL escalation).

### D4. The work is a sub-step inside `synthesizing`, not a new state

`internal/job/state.go` defines the §8.1 transition table.
`state_test.go` asserts `synthesizing → completed` is illegal
(comparing is mandatory) and that no new state may be inserted
between `synthesizing` and `comparing` without invalidating the
table. The new pod execution is therefore a *sub-step* within
`phaseSynthesize`: after the LLM call, after the final artifact
is persisted, the engine calls `runCodeInPod` and `runTestsInPod`
sequentially, then transitions to `comparing`. The state machine
is unchanged.

This matches the M3-T1 "`running_tests` sub-state" pattern
described in the ROADMAP. When M3 lands and the sub-step gets
its own observability (an `evaluating` state with the sub-state
visible to `/jobs/{id}/events`), the engine code will need a
slight refactor; the M2-T4 sub-step is the pre-M3 stand-in.

Non-code archetypes (`text`, `document`, `data`, `media`) skip
both sub-steps. The M1 walking-skeleton tests for `text` still
see 3 LLM calls and 0 runner calls. The M2-T4 acceptance
criterion applies only to `code`.

### D5. Engine authenticates to the internal API as the pod would

The engine does not impersonate the pod. When the engine needs
to call `execute_code` for jobID `J`, it constructs a
`*HTTPClient` (in `internal/internalapi/runner`) configured
with the loopback base URL and the per-job bearer token
retrieved from `jobpod.Manager.TokenFor(J)`. The same lookup
the pod itself would do internally.

This keeps the auth surface single-source-of-truth: the only
way to talk to the internal API is to present a valid per-job
bearer, and the only way to obtain a bearer is to have started
a pod (or to be the engine calling itself with the token
retrieved on the pod's behalf). The engine cannot escalate to
a different job's tools because each call carries the
job-specific token.

### D6. Gate G2 covers the new routes

The structural test in `internal/gate/gate_g2_test.go` is
extended in M2-T4, not weakened:

- The existing route-count test
  (`TestGateG2InternalAPIRoutesGoThroughMiddleware`) counts
  `mux.Handle` calls vs `authMiddleware(a.tokens, ...)` wraps.
  M2-T4 adds two more of each, so the count goes from 3 to 5.
  A future route registered without the wrapper fails the
  structural test.
- A new test `TestGateG2ToolEnvelopeBypassImpossible` greps
  `handlers.go` for `a.tools.EnvelopeFor` in the
  `execute_code` and `run_tests` handlers. A future refactor
  that bypasses the envelope check (by reading the tool name
  from the URL or the body without consulting the per-job
  envelope) fails the structural test.

Gate G2 is not relaxed: the new routes are bound by the same
constant-time compare, the same `authMiddleware`, and the same
no-`internal/llm` rule as the M2-T3 routes.

### D7. Production `os/exec` stays in `cmd/athanor/jobpod_client.go`

Gate G1 forbids `os/exec` in `internal/`. The M2-T4 work
introduces no new `os/exec` callsite: the HTTP client is a
`*http.Client` over loopback, and the `client := &http.Client{}`
call lives in `internal/internalapi/runner/httpclient.go`. No
new allowlist entry under `internal/gate/gate_test.go` is
needed.

The M2-T6 security suite will exercise the new routes from
inside a pod (pod→internet denied, pod→Ollama denied, …) and
is out of scope for M2-T4.

## Consequences

- The engine has a new `runner ToolRunner` field that is
  `nil` in unit tests and a `*HTTPClient` in production. When
  `nil`, the `code`-archetype sub-steps short-circuit to
  `comparing` without making any HTTP call. This keeps the
  M1 walking-skeleton tests usable without a pod manager.
- The `*project.Repo` gets a new `EnvelopeFor` method that
  satisfies `internalapi.ToolEnvLookup`. The method is the
  structural seam between the per-task override and the
  config default; the engine does not call it directly.
- The M3 dialectical loop will replace the M2-T4 sub-step
  with a proper `evaluating` state and an
  `EvaluationRecord` capture, but the `ToolRunner` interface
  and the internal API surface stay the same. M3 is additive
  on top of M2-T4, not a rewrite.
- A future `*HTTPClient` failure (loopback down, internal
  API 500) becomes a `runner.RunCode` error. The engine
  treats that as a phase failure today (the job transitions
  to `failed`); M3 will distinguish "tool not allowed" from
  "tool failed" with separate audit events.

## Not in M2-T4

- True streaming of `run_tests` output (server-sent events,
  long-poll, or chunked HTTP). M2-T4 returns one response.
- Per-archetype allowlist policy (e.g. "text never gets
  execute_code"). The allowlist is per-job today; per-archetype
  policy is a M3 ergonomic concern.
- Multi-candidate execution (running `execute_code` in N pods
  and picking the best). M3-T1/T2.
- Image-build pipeline (per-project images). M2-T4 uses
  `cfg.JobPod.Image` as-is.
- The security suite that verifies the new routes from
  inside a pod. M2-T6.

single source of truth for "what is a tool":

- `Tool` is a string-typed enum with the two M2-T4 values
  (`execute_code`, `run_tests`).
- `Envelope` is an immutable set built once at config-load or
  task-load time.
- `Parse([]string) (Envelope, error)` rejects unknown names with
  an error that names the offender.

Closed-set rationale: the §25 tool surface is wide (read_file,
write_file, git_operation, fetch_url, …). Shipping the engine
with a closed two-tool set keeps the §21 containment boundary
narrow. Adding a tool requires updating the constant, the test
in `allowlist_test.go`, the matching internal API route, and the
Gate G2 extension. That is a deliberate project decision, not
something an agent can do at will.
