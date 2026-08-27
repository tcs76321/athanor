# Implementation Plan — M2-T4: Wire Job Pods into the engine phase chain

## Overview

Add the two new authenticated internal-API routes `POST /internal/v1/jobs/{id}/execute_code` and `POST /internal/v1/jobs/{id}/run_tests`, with a per-job tool allowlist (ARCHITECTURE §25), and wire them into the engine as sub-steps inside the existing `synthesizing` phase so the M1 walking skeleton can be exercised end-to-end through a real (or fake) Job Pod. The plan finishes the M2 Container Spine: after this lands, the agent has actual tool execution available behind hardened, network-isolated, rootless containers, callable from the deterministic phase machine that the engine already drives.

The scope is deliberately the M2-T4 minimum: add the routes, add the allowlist check, add a narrow engine-side interface so the synthesizing phase can call them, and add the wiring in `cmd/athanor/serve.go` plus the test scaffolding (fake `ToolRunner` for unit tests, structural tests for the allowlist rule, Gate G2 extension to cover the two new routes). The full M3 multi-candidate evaluation that uses these routes in anger arrives in M3-T2 and is **not** in this plan.

Context:
- M2-T1/T2/T3 are complete (commits through `b2c56d8`). The Job Pod manager (`internal/jobpod`) and the auth-protected internal API surface (`internal/internalapi`) are in place and boot-time integrated.
- The engine (`internal/engine`) currently runs an M1 walking skeleton (planning → diverging → synthesizing → comparing) with no tool execution.
- The "engine drives the call" model is the one confirmed for this plan: the engine decides when to call `execute_code` / `run_tests`; the pod is a worker that runs the code and returns stdout/stderr/exit-code.
- The 32-char hex bearer token issued at pod start is the existing auth surface; the two new routes must follow Gate G2 (constant-time compare, every route through `authMiddleware`, no `internal/llm` import).
- Gate G1 forbids `os/exec`/container clients/`syscall` in `internal/`; the existing production client lives in `cmd/athanor/jobpod_client.go` and is the only allowlisted file. M2-T4 must not add another one.
- The job state machine has no slot for new states between `synthesizing` and `comparing` (state_test asserts `synthesizing → completed` is illegal). The new pod execution is therefore a sub-step *within* `synthesizing`, not a new state. This matches ROADMAP M3-T1's "`running_tests` sub-state" pattern in `evaluating`.

## Types

### `internal/toolenvelope/allowlist.go` (new package)

```go
package toolenvelope

// Tool is the closed set of job-pod-callable tools from ARCHITECTURE §25.
// M2-T4 ships only the two routes the engine calls today; the rest of the
// §25 table is out of scope and arrives in M3+ (ADR-0008 §D5).
type Tool string

const (
    ToolExecuteCode Tool = "execute_code"
    ToolRunTests    Tool = "run_tests"
)

// Envelope is the per-job tool allowlist.
type Envelope struct {
    tools map[Tool]struct{}
}

// Parse parses a []string into an Envelope. Unknown tool names are rejected.
func Parse(names []string) (Envelope, error)

// Allows reports whether t is in the envelope.
func (e Envelope) Allows(t Tool) bool

// Tools returns the sorted, deterministic list of tools in the envelope.
func (e Envelope) Tools() []Tool

var ErrUnknownTool = errors.New("toolenvelope: unknown tool")
```

### `internal/config/config.go` (modify)

Add a new top-level section:

```go
type JobPod struct {
    DefaultTools []string `yaml:"default_tools"`
    Image        string   `yaml:"image"`


### `internal/project/project.go` (modify)

```go
type Task struct {
    // ... existing fields ...
    AllowedTools []string // optional override; empty = use config default
}
```

### `internal/internalapi/handlers.go` (modify)

```go
type ExecuteCodeRequest struct {
    Language string `json:"language"`         // "python" | "bash" | "sh"
    Code     string `json:"code"`
    Timeout  int    `json:"timeout_seconds"`
}

type ExecuteCodeResponse struct {
    ExitCode   int    `json:"exit_code"`
    Stdout     string `json:"stdout"`
    Stderr     string `json:"stderr"`
    DurationMS int64  `json:"duration_ms"`
}

type RunTestsRequest struct {
    Command string `json:"command"`
    Timeout int    `json:"timeout_seconds"`
}

type RunTestsResponse = ExecuteCodeResponse

type ToolEnvLookup interface {
    EnvelopeFor(ctx context.Context, jobID string) (toolenvelope.Envelope, error)
}
```

### `internal/engine/engine.go` (modify)

```go
type ToolRunner interface {
    RunCode(ctx context.Context, jobID string, req toolenvelope.ExecuteRequest) (toolenvelope.ExecuteResult, error)
    RunTests(ctx context.Context, jobID string, req toolenvelope.ExecuteRequest) (toolenvelope.ExecuteResult, error)
}
```

Add `runner ToolRunner` field to `Engine` struct. The `toolenvelope.ExecuteRequest` and `toolenvelope.ExecuteResult` types live in `internal/toolenvelope/types.go` so the runner package can use them too.

## Files


### Existing files to modify

| Path | Change |
|---|---|
| `internal/config/config.go` | `JobPod` struct, `Config.JobPod`, helpers. |
| `internal/config/defaults.go` | `JobPod` defaults. |
| `internal/config/validate.go` | Validate `DefaultTools`. |
| `internal/config/config_test.go` | New defaults + validation tests. |
| `internal/project/project.go` | `Task.AllowedTools`. |
| `internal/project/repo.go` | Persist + read column; widen signatures; `EnvelopeFor` method. |
| `internal/project/project_test.go` + `repo_test.go` | New column + `EnvelopeFor` tests. |
| `internal/internalapi/handlers.go` | Widen `API` + `New`; register two new routes. |
| `internal/internalapi/internalapi.go` | `ErrToolDisallowed` sentinel. |
| `internal/internalapi/handlers_test.go` | Widen test env with `fakeToolEnv`. |
| `internal/internalapi/internalapi_test.go` | Add new routes to wrapped-everywhere test; new rejection tests. |
| `internal/engine/engine.go` | `ToolRunner` interface, `runner` field, widen `New`. |
| `internal/engine/phases.go` | `phaseSynthesize` calls sub-steps for `code` archetype. |
| `internal/engine/engine_test.go` | `newEnv` passes `fakeRunner`; new code-archetype test. |
| `internal/engine/recovery_test.go` | `fakeRunner` in both `New` calls. |
| `cmd/athanor/serve.go` | Build `*HTTPClient`, wire to `engine.New`. |
| `internal/gate/gate_g2_test.go` | Update route count; add envelope-bypass test. |
| `config.example.yaml` | Document `job_pod:`. |
| `ROADMAP.md` | Status table: M2 T1–T4 done. |
| `CHANGELOG.md` | M2-T4 entry. |
| `README.md` | One-line addition. |

### Deleted

None.

## Functions

### New

| Signature | File | Purpose |
|---|---|---|
| `func Parse(names []string) (Envelope, error)` | `toolenvelope/allowlist.go` | Build envelope; reject unknown. |
| `func (Envelope) Allows(Tool) bool` | same | Membership. |
| `func (Envelope) Tools() []Tool` | same | Sorted slice. |
| `func (a *API) handleExecuteCode(w, r)` | `internalapi/exec.go` | New route. |
| `func (a *API) handleRunTests(w, r)` | same | Mirror. |
| `func (HTTPClient) RunCode(ctx, jobID, req) (res, err)` | `internalapi/runner/httpclient.go` | POST execute_code. |
| `func (HTTPClient) RunTests(ctx, jobID, req) (res, err)` | same | POST run_tests. |
| `func (e *Engine) runCodeInPod(...)` | `engine/pod_wiring.go` | Sub-step. |


## Classes

### New

| Name | File | Methods |
|---|---|---|
| `Envelope` | `toolenvelope/allowlist.go` | `Allows`, `Tools` |
| `HTTPClient` | `internalapi/runner/httpclient.go` | `RunCode`, `RunTests` |

### Modified

| Name | File | Change |
|---|---|---|
| `Engine` | `engine/engine.go` | Add `runner`. |
| `API` | `internalapi/handlers.go` | Add `tools`. |
| `Task` | `project/project.go` | Add `AllowedTools`. |
| `Config` | `config/config.go` | Add `JobPod` + helpers. |
| `Repo` | `project/repo.go` | Add `EnvelopeFor`; widen signatures. |

## Dependencies

**No new module dependencies.** `net/http` + stdlib only. Per AGENTS.md the project stays at two deps.

## Testing

### New

| Path | Coverage |
|---|---|
| `toolenvelope/allowlist_test.go` | Round-trip, unknown, empty, deterministic. |
| `internalapi/exec_test.go` | Round-trip; 403 on disallowed; auth still required. |
| `internalapi/runner/httpclient_test.go` | Bearer sent; body decoded; timeout cancellation. |
| `engine/pod_wiring_test.go` | Sub-step calls; non-code skips; envelope-empty skips. |

### Modified

| Path | Change |
|---|---|
| `engine/engine_test.go` | `fakeRunner` in `newEnv`; code-archetype test. |
| `engine/recovery_test.go` | `fakeRunner` in both `New` calls. |
| `internalapi/handlers_test.go` | `fakeToolEnv`; `WithEnvelope` helper. |
| `internalapi/internalapi_test.go` | New routes in wrap-test; rejection tests. |
| `project/*_test.go` | New column + `EnvelopeFor`. |
| `config/config_test.go` | New defaults + validation. |
| `gate/gate_g2_test.go` | Route count 5; new envelope-bypass test. |

### Validation

1. `make check` (lint + vet + test-race) green.
2. Re-prove Gate G1 (`CGO_ENABLED=1 go test ./internal/gate/`).
3. Re-prove Gate G2.
4. `make test-race ./...` — no regressions.

## Implementation Order

1. `chore: add tool envelope package + per-job allowlist config` — new `toolenvelope` package, `config.JobPod`, validation, defaults. No behavior change.
2. `docs: ADR-0009 engine-pod-wiring` — design decisions.
3. `M2-T4: internal API execute_code + run_tests routes with allowlist` — handlers, types, Gate G2 extension.
4. `M2-T4: engine sub-step pod execution + production wiring` — `ToolRunner`, sub-steps, `HTTPClient`, `cmd/athanor/serve.go`, engine tests.
5. `docs: ROADMAP + CHANGELOG + README + config.example.yaml` — bookkeeping.

## Out of scope (in ADR)

- True streaming of `run_tests` output. M3-T2.
- Per-archetype allowlist policy. M3 ergonomic.
- Multi-candidate execution. M3-T1/T2.
- Image-build pipeline.
- Security suite for new routes. M2-T6.

## Open questions (resolved)

1. Body validation: handler.
2. Sub-step within `synthesizing` (not new states): state machine + M3 sub-state pattern.
3. Language: `python` only.
4. Engine auth: bearer via `jobpod.Manager.TokenFor(jobID)`.
5. Empty `Image`: `serve.go` fails fast.

| `func (e *Engine) runTestsInPod(...)` | same | Sub-step. |
| `func (*project.Repo) EnvelopeFor(ctx, jobID) (Envelope, error)` | `project/repo.go` | Per-job envelope. |
| `func (c *Config) JobPodEnvelope() (Envelope, error)` | `config/config.go` | Default envelope. |
| `func (c *Config) JobPodResourceLimits() jobpod.Limits` | same | Resource limits. |

### Modified

| Name | File | Change |
|---|---|---|
| `New(...) *Engine` | `engine/engine.go` | Add `runner ToolRunner`. |
| `New(...) *API` | `internalapi/handlers.go` | Add `tools ToolEnvLookup`. |
| `(API).Register(mux)` | same | Register 2 new routes. |
| `run(...)` | `cmd/athanor/serve.go` | Build `*HTTPClient`, wire. |
| `(Repo).Create(...)` | `project/repo.go` | Add `allowedTools []string`. |
| `(Repo).SubmitGoal(...)` | same | Same widening. |
| `(Repo).Task(...)` | same | Decode new column. |
| `phaseSynthesize(...)` | `engine/phases.go` | Call sub-steps for `code` archetype. |

### Removed

None.


### New files

| Path | Purpose |
|---|---|
| `internal/toolenvelope/allowlist.go` | `Envelope` type, `Parse`, `Allows`, `Tools`, `ErrUnknownTool`. |
| `internal/toolenvelope/allowlist_test.go` | Round-trip, unknown rejection, empty allowed, deterministic `Tools()`. |
| `internal/toolenvelope/types.go` | `ExecuteRequest` / `ExecuteResult` shared between engine and runner. |
| `internal/internalapi/exec.go` | `handleExecuteCode`, `handleRunTests`, `ErrToolDisallowed` sentinel. |
| `internal/internalapi/exec_test.go` | Round-trip with fake `ToolEnvLookup`; auth-rejection. |
| `internal/internalapi/runner/httpclient.go` | Production `*HTTPClient`. |
| `internal/internalapi/runner/httpclient_test.go` | `httptest.NewServer` round-trip. |
| `internal/engine/pod_wiring.go` | `runCodeInPod`, `runTestsInPod`. |
| `internal/engine/pod_wiring_test.go` | Sub-step unit tests. |
| `migrations/0005_task_allowed_tools.sql` | `tasks.allowed_tools_json` column. |
| `docs/adr/0009-engine-pod-wiring.md` | M2-T4 design decisions. |

    PidsLimit    int      `yaml:"pids_limit"`
    MemoryMB     int      `yaml:"memory_mb"`
    CPUs         float64  `yaml:"cpus"`
}
```

Add a `Config.JobPod JobPod` field, plus helpers:
- `(c *Config) JobPodEnvelope() (toolenvelope.Envelope, error)`
- `(c *Config) JobPodResourceLimits() jobpod.Limits`
