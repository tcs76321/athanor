# Changelog

All notable changes to Athanor are recorded here. Format: one line per
`M#-T#: <title>` commit, in reverse chronological order (newest first),
with a backreference to the gate test, demo script, or probe that
proves the change. The "Prior work" section below summarizes the
commits before this changelog was initialized (F6).

New entries are appended at the top. Do not rewrite history.

## Unreleased

### M2 — Container Spine (continuing)

- **M2-T4.** Internal API `execute_code` and `run_tests` routes
  with a per-job tool envelope (ARCHITECTURE §25). Closed set of
  two tools lands in `internal/toolenvelope`; per-task override
  via `tasks.allowed_tools_json` (migration 0006); the handler
  enforces the envelope server-side and returns 403 with an
  audit event `tool_disallowed` on rejection. The engine
  decides when to call (sub-steps in `phaseSynthesize` for the
  `code` archetype; text/document/data/media skip them and
  complete via the M1 walking skeleton). The production
  `*internalapi/runner.HTTPClient` talks to the loopback
  internal API with the per-job bearer token retrieved from
  `jobpod.Manager.TokenFor(jobID)` — the engine is just
  another client of the same surface. Decisions: ADR-0009.
  Structural proof: Gate G2 extended in
  `internal/gate/gate_g2_test.go`
  (`TestGateG2ToolEnvelopeBypassImpossible` greps every
  handler file for `a.tools.EnvelopeFor`; the route-count test
  grows from 3 to 5 to cover the new routes). Behavior: 4
  commits (`chore: add tool envelope package + per-job
  allowlist config` `ef0f4b9`, `docs: ADR-0009 engine-pod-wiring`
  `f7674a3`, `M2-T4: internal API execute_code + run_tests
  routes with allowlist` `8ed7ee9`, `M2-T4: engine sub-step pod
  execution + production wiring` `843e123`). All
  internalapi + engine + project + gate tests green;
  `make test-race` clean; Gate G1 still passes. No new
  dependencies.
- **M2-T3.** Per-job tokens (16-byte random hex, mounted at
  `/run/athanor/token` via the existing bind-mount argv) plus
  the `/internal/v1/` HTTP surface behind a bearer-token auth
  middleware. Three routes in M2-T3 scope: GET job context, POST
  heartbeat, POST log. `crypto/subtle.ConstantTimeCompare` guards
  the bearer; per-job binding via `r.PathValue("id")` ensures a
  token for job A is rejected on job B. Decisions: ADR-0008.
  Structural proof: Gate G2 in `internal/gate/gate_g2_test.go`
  (no `internal/llm` import, `ConstantTimeCompare` present in
  middleware, every route wrapped in `authMiddleware`).
  Behavior: 6 commits (`1fc6116` token dir, `90938a9` auth
  middleware, `aa4e354` handlers, `18d684a` Gate G2, `ba54a49`
  daemon boot wiring, ADR-0008). All 26 internalapi tests +
  Gate G2 tests green; `make test-race` clean. No new
  dependencies.

### Foundation layer (F1–F6)

- **F6.** Initialize `CHANGELOG.md`. One line per M#-T# entry from now
  on; this section is the durable record of layer transitions.
- **F5.** Add `make bench` target and `BenchmarkRunFullChain`. Baseline
  numbers recorded in `docs/benchmarks/engine-m1.txt` (2.32 ms per M1
  chain on Apple M2 Max, fake Ollama, no network). M3 multi-candidate
  evaluation will be compared against this baseline.
- **F4.** Complete truncated M1-T8 sentence in
  `docs/probes/m1-quality-probe.md` and add a "What M1-T8 Did NOT
  Test" section that pre-declares the M3-T7 probe's scope. M3-T7
  must cover multi-candidate divergence, evaluating/reflecting phases,
  Job Pod test execution, multi-language code, large-context retrieval,
  hallucination rate, strategy-mining signal, HITL latency, daydreaming,
  and OS watcher behavior — none of which M1-T8 measured.
- **F3.** Document what `internal/gate/gate_test.go` proves and what
  it does not. Gate G1 is a structural containment guarantee (no
  tool-execution imports, no raw `syscall` in agent code, signal-
  constant-only `syscall` in `cmd/`, single named file allowlist for
  `os/exec`); it is not a behavioral guarantee against prompt
  injection or unsafe LLM outputs.
- **F2.** Replace prose README "Status" section with a structured
  three-block layout: "What works today (M0 + M1 + M2-T1 + M2-T2)",
  "What's next (M2 finish)", "What's deferred (M3..M7)". The block is
  updated as the final step of every layer transition.
- **F1.** Verify the `LICENSE` file is GNU AGPL v3 and surface it in
  the README preamble with a one-line `License: [AGPL-3.0](LICENSE)`
  badge. LICENSE was already present and referenced in
  `DEVELOPMENT.md`; this makes the license visible at a glance for
  any newcomer.

## Prior work (pre-changelog, summarized)

The 57 commits before F1 are summarized below by milestone. Full
history is in `git log`.

### M2 — Container Spine (in progress)

- **M2-T2.** `internal/jobpod` package: types/errors/Client interface,
  Manager impl (Start, Stop, Get, Supervise), Sweep on daemon boot,
  per-platform hardening flag builders (common + Linux seccomp +
  Darwin no-op), and tests. Wire the manager into daemon boot for a
  startup orphan sweep. Production `os/exec` client lives in
  `cmd/athanor/jobpod_client.go` (the only file allowed by Gate G1).
  Decisions: ADR-0007.
- **M2-T1.** Rootless Podman lifecycle spike (`spikes/podman-lifecycle`).
  Seven §21.2 containment checks pass on macOS 14 + podman-machine
  5.8.2 + applehv. ADR-0007 documents the seven checks, the macOS-
  only validation scope, and the seccomp caveat.
- **M1-T8.4.** Wire `PowerManager.MaxConcurrentJobs()` into the
  engine's concurrency cap. M1-T8 follow-up integrated.

### M1 — Walking Skeleton (code-complete)

- **M1-T8.** Quality probe against a live Ollama. Ran 2026-08-26
  with `gemma4:12b-mlx`; 5/5 acceptance-criteria adherence across
  text/code/document archetypes. Per-model timings and findings:
  `docs/probes/m1-quality-probe.md`. Three follow-ups integrated
  on 2026-08-26: M1-T8.1 (synthesis preamble suppression prompt
  fix), M1-T8.2 (default planning budget 120s → 300s), M1-T8.3
  (synthesizing-phase crash-recovery test).
- **M1-T7.** CLI client (`cmd/athanor/`): `project create`,
  `goal submit`, `job watch`, `artifacts`, `freeze`, `unfreeze`,
  `init`. HTTP API in `internal/api/`. End-to-end test
  `TestEndToEndWalkingSkeleton` is the Gate G1 demo.
- **M1-T6.** Kill switch (`internal/control`). Frozen state
  persists in `system_state` and survives restarts. Unfreeze
  requires a reason. Migration 0005.
- **M1-T5.** Artifact model (`internal/artifact`). Draft creation,
  supersede chain (linear, version-incrementing), SHA-256 content
  hashes, status flow per §9.3.
- **M1-T4.** Job state machine (`internal/job`). All 13 §8.1
  states accepted by schema (migration 0004). M1 transition table
  is the §8.2 subset that M1 actually exercises; `evaluating`/
  `reflecting`/`awaiting_approval` are reachable in M3/M6 per
  ADR-0001. CAS-style transitions per ADR-0006.
- **M1-T3.** Prompt assembly (`internal/prompt`). Deterministic,
  byte-identical output for identical inputs. §11.2 section order
  reserved; M5 fills slots 7-12. Per-section token accounting.
- **M1-T2.** Context feasibility check (`internal/llm/feasibility.go`).
  Floors are role-scoped, not task-scoped (ADR-0002). Pauses the
  job and emits a `context_floor_violation` event on violation
  rather than silently reducing context.
- **M1-T1.** Ollama client + persona registry. The five fixed
  roles (`wide`, `tall`, `main`, `security`, `alternative`) are
  resolved from configuration; missing any role fails boot
  loudly.

### M0 — Foundations

- **M0-T8.** CI: `vet` + `test -race` + `golangci-lint v2.13` on
  push. `make check` is the per-commit contract. `make hooks`
  installs the pre-push gate.
- **M0-T7.** Loopback-only HTTP server. `server.LocalhostAddr`
  rejects non-loopback hosts outright (fail closed).
- **M0-T6.** Append-only EventLog API (`store.AppendEvent`).
  `events` table is DB-enforced append-only via `BEFORE UPDATE/
  DELETE … RAISE(ABORT)` triggers (migration 0001). Query
  surface: `store.QueryEvents` with category/job/project filters.
- **M0-T5b.** Schema migrations 0001 (execution spine), 0002
  (learning/control), 0003 (canonical enum values per ADR-0005),
  0004 (job-state CHECK per ADR-0006), 0005 (system_state). All
  forward-only, backup-before-migrate, FK-checked at COMMIT.
- **M0-T5a.** SQLite store: `mattn/go-sqlite3` with CGO, single
  connection, WAL, `synchronous=NORMAL`, `busy_timeout=5000`,
  `foreign_keys=ON`. Decisions: ADR-0003, ADR-0004.
- **M0-T4.** Migration runner. Pre-migration backup via
  `VACUUM INTO`. Each migration in its own transaction with
  `PRAGMA foreign_key_check` gating the commit (per ADR-0006
  recipe).
- **M0-T3.** Category-tagged structured JSON logging to
  `state/logs/`. Per-category files with size-based rotation
  (10 MiB / 5 files). Closed set of categories from §28.1.
- **M0-T2.** Config loader: strict YAML, built-in defaults,
  `config.example.yaml` matches defaults (test-enforced). Custom
  `Duration` type that rejects bare numbers.
- **M0-T1.** Go module + project layout (`cmd/`, `internal/`,
  `migrations/`, `spikes/`, `docs/`). AGPL-3.0 license.


