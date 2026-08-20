# Athanor — Work Plan (MVP v0.1)

**Status**: Draft | **Target**: MVP (v0.1) | **Philosophy**: Quantum steps, each testable, each shippable.

---

## Legend

| Field | Meaning |
|-------|---------|
| **ID** | Unique task identifier (TASK-XXX) |
| **Epic** | Logical grouping |
| **Depends On** | Task IDs that must complete first |
| **Size** | S (1-2h), M (2-4h), L (4-8h) |
| **Type** | `code`, `test`, `config`, `doc`, `infra` |
| **Acceptance** | Verifiable done criteria |

---

## TASK-000: SQLite + sqlite-vec Go Spike (PRE-REQUISITE)

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-000 | SQLite + sqlite-vec Go Spike | — | S | infra | Create a standalone Go script that successfully loads `sqlite-vec`, inserts a vector, and performs a KNN search. Document the exact Go driver, CGO flags, and build constraints required in a `docs/sqlite-setup.md` file. Must work on both macOS and Linux. |

---

## Phase 0: Foundation (Week 1)

### Epic: FDN — Go Module, Config, Logging, SQLite Migrations

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-001 | Initialize Go module `github.com/athanor/athanor` | — | S | code | `go mod tidy` succeeds; module path correct |
| TASK-002 | Add core dependencies (sqlite, fsnotify, gopsutil, yaml, chi, sse) | TASK-001 | S | code | `go mod verify` passes; versions pinned |
| TASK-003 | Create project layout per ARCHITECTURE.md §4 | TASK-001 | S | code | Directories exist: `cmd/daemon`, `internal/{config,store,daemon,vm,proxy,tools,airlock,orchestrator,alarm}`, `web/ui`, `migrations` |
| TASK-004 | Implement `config.yaml` loader with validation | TASK-003 | M | code | Loads YAML, validates required fields, defaults for optional; unit test covers malformed/empty/missing |
| TASK-005 | Structured logging (slog + rotating file) | TASK-003 | M | code | Logs to `~/athanor/athanor.log` with rotation (10MB, 5 files); levels: debug/info/warn/error; JSON + human formats |
| TASK-006 | SQLite connection pool + migration runner | TASK-003, TASK-004 | M | code | Opens `~/athanor/athanor.db`; runs embedded `.sql` migrations in order; idempotent; rolls back on failure; `go test -run TestMigrations` passes |
| TASK-007 | Migration `001_init.sql`: core tables (settings, tasks, artifacts, alarms, audit_log) | TASK-006 | S | code | Tables created with indexes; `sqlite3 athanor.db ".schema"` matches spec |
| TASK-008 | Migration `002_ontology.sql`: topics, goals, projects, tasks, FTS5 virtual tables | TASK-007 | M | code | FTS5 tables for full-text search; foreign keys; triggers for updated_at |
| TASK-009 | Migration `003_memory.sql`: long-term memory layers, vectors (sqlite-vec), compaction log | TASK-008 | M | code | `sqlite-vec` virtual tables; layer columns; pinned flag; compaction_log table |
| TASK-010 | Health check endpoint `GET /healthz` | TASK-003 | S | code | Returns 200 + JSON `{status: "ok", version, uptime}`; wired in main router |
---

## Phase 1: Daemon Core (Week 1-2)

### Epic: DCO — HTTP Server, SSE, State Machine Skeleton

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-011 | Chi router + middleware (request ID, logging, recovery, CORS) | TASK-010 | M | code | All routes wrapped; request ID propagated; panic recovery returns 500 |
| TASK-012 | SSE endpoint `GET /api/events` for server→client streaming | TASK-011 | M | code | Client receives typed events (task_started, tool_call, artifact_created, approval_required, etc.); reconnection handled |
| TASK-013 | REST endpoints: `POST /api/tasks`, `GET /api/tasks/:id`, `GET /api/tasks` | TASK-011 | M | code | CRUD for tasks; persists to SQLite; returns proper HTTP codes |
| TASK-014 | AgentState struct + JSON serialization | TASK-006 | S | code | `AgentState` marshals/unmarshals; includes: task_id, mode, context_layers, tool_history, recovery_count |
| TASK-015 | State machine runner: `Node` func type, `Run(ctx, initialState)` | TASK-014 | M | code | Executes nodes sequentially; persists state to SQLite after each node; respects context cancellation; max 100 nodes/task |
| TASK-016 | Built-in nodes: `BuildPromptNode`, `ExecuteLLMNode`, `ExecuteToolsNode`, `CheckHITLNode`, `PersistArtifactNode` | TASK-015 | L | code | Each node unit-tested; `ExecuteToolsNode` calls tool registry; `CheckHITLNode` pauses state machine |
| TASK-017 | Task lifecycle: queued → running → waiting_hitl → completed / failed / halted | TASK-013, TASK-015 | M | code | State transitions persisted; SSE events emitted on each transition |
| TASK-018 | Graceful shutdown: drain in-flight tasks, close DB, stop VMs | TASK-011, TASK-015 | M | code | SIGTERM/SIGINT handled; `go test -run TestGracefulShutdown` passes |

---

## Phase 2: Ollama Integration (Week 2)

### Epic: OLL — Client, Model Management, Context Calculation

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-019 | Ollama HTTP client with retries/backoff | TASK-002 | M | code | `Generate`, `Chat`, `ListModels`, `PullModel` methods; 3 retries with exponential backoff |
| TASK-020 | Model registry: load `config.yaml` models section, track loaded/unloaded | TASK-004, TASK-019 | M | code | In-memory registry; `GetModel(name)` returns config; `EnsureLoaded(ctx, name)` loads via Ollama |
| TASK-021 | Context calculator: implement ARCHITECTURE.md §5.1 formula | TASK-004, TASK-020 | M | code | `CalculateMaxContext(modelSize, availableRAM, bufferPercent) -> int`; unit tests for edge cases (OOM, tiny RAM) |
| TASK-022 | Model downgrade recommender: compare `maxContext` vs `context_minimums[taskType]` | TASK-021 | M | code | Returns `DowngradeRecommendation{from, to, reason}` or nil; integrates with HITL for user confirmation |
| TASK-023 | Performance guard: latency monitor (3× avg), RAM pressure, battery, thermal | TASK-005, TASK-020 | M | code | Background goroutine samples every 30s; emits alarms via alarm system; pauses background tasks at thresholds |
| TASK-024 | Model lifecycle: startup load, idle unload (30min), reload on demand | TASK-020, TASK-023 | M | code | `Start()` loads default model; `IdleMonitor` unloads after 30min; `EnsureLoaded` reloads transparently |
---

## Phase 3: Airlock & Filesystem (Week 2-3)

### Epic: AIR — File Isolation, Validation, Virtio-fs Mount

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-025 | Workspace root resolver: `~/athanor/workspace/` with inbox/output/projects/scratch | TASK-004 | S | code | `ResolveWorkspacePath(relative)` returns absolute; validates within root |
| TASK-026 | Path validator: `EvalSymlinks`, root check, `O_NOFOLLOW` on all opens | TASK-025 | M | code | `ValidatePath(userInput, allowedRoots)` rejects traversal, symlinks outside root; unit tests for attack vectors |
| TASK-027 | File operations: `ReadFile`, `WriteFile`, `ListFiles`, `SearchFiles` (tools) | TASK-026 | M | code | All ops use validator; `WriteFile` creates parent dirs; `SearchFiles` uses `filepath.Glob` + content grep |
| TASK-028 | Ingress handler: copy user uploads to `inbox/`, sanitize names, scan for threats | TASK-026 | M | code | `IngestFile(reader, filename)` → writes to `inbox/{uuid}_{sanitized}`; ClamAV stub (log only for MVP) |
| TASK-029 | Egress handler: validate `output/` files, copy to user destination | TASK-026 | M | code | `EgressFile(src, dest)` validates src in `output/`, dest allowed; rejects symlinks, devices, setuid |
| TASK-030 | Periodic scanner: 30s interval, delete outbound symlinks/setuid/devices in workspace | TASK-025 | S | code | `StartScanner(ctx)` goroutine; logs violations to audit_log; `go test -run TestScanner` passes |
| TASK-031 | Virtio-fs mount helper (Linux): `virtiofsd --sandbox=namespace` config | TASK-025 | M | code | `MountWorkspace(vmID, workspacePath) -> (mountPath, cleanupFn)`; integration test with Firecracker VM |
| TASK-032 | VZSharedDirectory mount helper (macOS): Virtualization.framework Swift bridge | TASK-025 | M | code | Swift helper `vmount` called from Go; `MountWorkspace` returns mount path; cleanup on VM destroy |

---

## Phase 4: Executor Abstraction & Local Executor (Week 3)

### Epic: EXEC — Executor Interface, Local Executor, Sandbox Profiles

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-031 | Executor Interface: Define `internal/executor/Executor` (`Run`, `Stop`) | TASK-025 | S | code | Interface defined; handles timeouts and context cancellation. |
| TASK-032 | Local Executor (Dev/Safe): Implement `LocalExecutor` using `os/exec` in a temp dir | TASK-031 | M | code | Runs bash/python safely; captures stdout/stderr; cleans up temp dir; unit tested. |
| TASK-033 | macOS Seatbelt / Linux Bubblewrap profile for Local Executor | TASK-032 | M | code | Applies OS-level sandboxing to the `os/exec` process to restrict network and filesystem access. |
| TASK-034 | Code Execution Tool: Wire `execute_code` to the Executor interface | TASK-033, TASK-045 | M | code | Tool registry calls `Executor.Run`; respects 30s timeout. |

> **Note:** Firecracker (Linux) and Virtualization.framework (macOS) implementations are moved to **Post-MVP / v1.0 backlog**. The `Executor` interface allows swapping implementations via config without changing agent logic.

---

## Phase 5: Egress Proxy & Web Fetch (Week 3-4)

### Epic: EG — Allow-list, Rate Limiting, Reader Mode

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-039 | Egress proxy: Go HTTP proxy handler, allow-list from config | TASK-011 | M | code | `ServeHTTP` proxies only to `network.allow_list` domains; logs all requests; 403 for denied |
| TASK-040 | Rate limiter: token bucket per domain, `rate_limit_per_minute` | TASK-039 | M | code | `AllowRequest(domain) -> bool`; resets per minute; persisted across restarts |
| TASK-041 | Header sanitizer: strip `Cookie`, `Authorization`, `X-Forwarded-*` | TASK-039 | S | code | `SanitizeHeaders(req)` mutates request; unit test verifies stripping |
| TASK-042 | `fetch_url` tool: GET via proxy, Reader Mode default | TASK-027, TASK-039 | M | code | Uses `go-readability` + `bluemonday` → Markdown; 10s timeout; size limit 2MB |
| TASK-043 | `search_web` tool: duckduckgo/html scrape (no API key) → top 5 results | TASK-042 | M | code | Returns `{title, url, snippet}[]`; respects rate limit; cached 5min |
| TASK-044 | Browser mode stub: headless Chrome in VM (opt-in, HITL gate) | TASK-038, TASK-042 | L | code | `fetch_url` with `mode: "browser"` launches VM with Chrome; returns rendered DOM; **MVP: log "not implemented"** |
---

## Phase 6: Tool Interface & Orchestrator (Week 4)

### Epic: TOOL — Constrained Tools, Git Operations, Memory Query

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-045 | Tool registry: schema (JSON Schema), executor, timeout, retry | TASK-016 | M | code | `RegisterTool(name, schema, handler)`; `Execute(name, args) -> (result, error)`; validates args against schema |
| TASK-046 | Git tool: `git_operation(repo, op: init|add|commit|branch|diff|log|status)` | TASK-027, TASK-045 | M | code | Operates only under `workspace/projects/`; non-bare repos; commits signed with agent key |
| TASK-047 | Memory query tool: `query_memory(query, layer?, project?)` → hybrid search | TASK-009, TASK-045 | M | code | FTS5 + sqlite-vec combined; BM25 + vector rerank; returns top 10 with scores |
| TASK-048 | Artifact tool: `create_artifact(type: draft|idea|code|research, content, metadata)` | TASK-045, TASK-006 | M | code | Writes to `output/{type}/{uuid}.md`; metadata JSON sidecar; SSE event emitted |
| TASK-049 | Tool call validator: schema check, path containment, arg size limits | TASK-045 | M | code | Rejects oversized args (>1MB), paths outside workspace, unknown tools; logs to audit |

---

## Phase 7: HITL & Containment Trust (Week 4)

### Epic: HITL — Approval Gates, Classification, Audit

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-050 | Containment classifier: `ClassifyAction(action) -> Contained \| External` | TASK-045 | M | code | Rules: file ops in workspace=Contained; network=External; git push=External; config change=External |
| TASK-051 | Approval queue: `RequestApproval(action, reason) -> (approved, reason)` | TASK-012, TASK-050 | M | code | Persists to SQLite; SSE `approval_required` event; `POST /api/approvals/:id` resolves; 5min timeout → deny |
| TASK-052 | HITL node integration: `CheckHITLNode` pauses state machine, resumes on approval | TASK-016, TASK-051 | M | code | State machine yields; `AgentState` stores pending approval ID; resumes on callback |
| TASK-053 | Audit log: every external action + approval decision + user identity | TASK-051 | S | code | `audit_log` table: timestamp, action, classification, approved_by, reason, outcome |
| TASK-054 | Kill switch: `POST /api/kill` → stops VMs, cancels LLM/fetches, freezes daemon | TASK-011, TASK-036 | M | code | Sets global `atomic.Bool`; all subsystems check; `GET /api/status` returns `frozen`; requires restart to unfreeze |

---

## Phase 8: Ontology & Goal Expansion (Week 4-5)

### Epic: ONT — CRUD, Expansion Flow, Task Scheduling

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-055 | Ontology CRUD API: Topics, Goals, Projects, Tasks (REST + SSE) | TASK-008, TASK-013 | M | code | `GET/POST/PATCH/DELETE /api/ontology/{type}`; FTS5 search; SSE on changes |
| TASK-056 | Goal expansion node: streams LLM expansion (assumptions, questions, structure) | TASK-016, TASK-020 | L | code | `ExpandGoalNode` uses `ExecuteLLMNode` with expansion prompt; streams partial via SSE; 2min timeout |
| TASK-057 | Expansion approval flow: user answers questions → refined expansion → create Goal+Projects+Tasks | TASK-051, TASK-056 | M | code | 1-3 cycles; each cycle creates revision; final approval creates ontology entries |
| TASK-058 | Task scheduler: cron (recurring), one-time, triggered (sensor events) | TASK-008, TASK-017 | M | code | `ScheduleTask(task)` registers with cron lib; `TriggerTask(event)` matches sensor triggers; persists next_run |
| TASK-059 | Sensor bus stub: `fsnotify` on workspace, logs events to sensor_bus table | TASK-025, TASK-006 | M | code | `StartSensorBus(ctx)` watches `inbox/`, `projects/`; debounced 1s; emits `file_created` events |
---

## Phase 9: Intelligence Layer (Week 5)

### Epic: INT — Prompt Architecture, Recovery, Feedback Loop

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-060 | Prompt builder: 5-layer composition (Base, Mode, Task, Context, Feedback) | TASK-014, TASK-020 | M | code | `BuildPrompt(state) -> string`; layers injected in order; token count estimated; truncates Context layer first |
| TASK-061 | Base prompt (Mountain): identity, safety, tool definitions — embedded in binary | TASK-060 | S | code | `base_prompt.txt` embedded; immutable; versioned in SQLite `prompt_versions` table |
| TASK-062 | Mode prompts (River): Forge, Whittle, Harden, Tend, Daydream, Blue Sky — stored in SQLite | TASK-060, TASK-008 | M | code | `mode_prompts` table: mode, version, content, created_at; `GET /api/prompts/:mode` returns latest |
| TASK-063 | Error recovery nodes: InvalidToolCall, HallucinatedPath, LoopDetector, ContextOverflow, Timeout, SandboxFailure | TASK-016, TASK-060 | L | code | Each recovery node implements ARCHITECTURE.md §12.2 table; max retries per type; budget check (8 total) |
| TASK-064 | Feedback injection: store rejections with reasons, inject as Layer 5 for same project/mode | TASK-009, TASK-060 | M | code | `feedback` table: project, mode, reason, timestamp; `BuildPrompt` queries last 20 for project+mode |
| TASK-065 | Context compaction: progressive halving (Layer 0→N) + pinned protection | TASK-009, TASK-060 | L | code | `CompactMemory(ctx, project)` runs when tokens > 1M; creates summary layers; preserves pinned; logs to compaction_log |

---

## Phase 10: Alarms & Reliability (Week 5)

### Epic: ALM — Alarm System, Watchdog, Backup, Sleep/Wake

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-066 | Alarm types: Notice, Warning, Alert, Critical — with escalation policies | TASK-006 | M | code | `Alarm{Level, Source, Message, Timestamp}`; `Alert` pauses task; `Critical` freezes daemon; persisted |
| TASK-067 | Alarm triggers: Loop, Drift, Resource, Quality, Security, Stuck, Hallucination, Budget, Self-Mod | TASK-063, TASK-066 | M | code | Each trigger implemented as monitor goroutine; configurable thresholds in config.yaml |
| TASK-068 | Watchdog: HTTP health check every 60s; restart daemon if unresponsive | TASK-010, TASK-018 | M | code | External watchdog process (separate binary); `systemd`/`launchd` managed; logs to separate file |
| TASK-069 | Auto-backup: cron schedule, compressed archive, retention `max_local_backups` | TASK-004, TASK-006 | M | code | `BackupNow() -> path`; includes DB + config + workspace/projects; `Restore(backupPath)` verified |
| TASK-070 | Sleep/wake handler: macOS `NSWorkspace` / Linux `systemd-sleep` hooks | TASK-018 | M | code | On sleep: flush SQLite, pause VMs, unload Ollama; On wake: reload model, resume VMs, reschedule tasks |
---

## Phase 11: Web UI (Week 5-6)

### Epic: UI — Vanilla JS App, Views, SSE Client

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-071 | UI shell: `index.html`, `app.js`, `styles.css`, ES modules, no build step. **Include Alpine.js via CDN script tag for reactive state management.** | TASK-012 | M | code | Served by Go `http.FileServer` from `web/ui/`; loads in <500ms; works offline after first load |
| TASK-072 | SSE client: reconnection, event dispatch, typed handlers | TASK-012, TASK-071 | M | code | `EventSource` wrapper; auto-reconnect with backoff; `dispatchEvent(new CustomEvent('task_update', detail))` |
| TASK-073 | View router: hash-based (`#chat`, `#requests`, `#workspace`, `#airlock`, `#network`, `#memory`, `#status`, `#privacy`, `#stats`, `#ontology`, `#settings`) | TASK-071 | M | code | `nav` element; `data-view` attributes; `window.addEventListener('hashchange')` switches views |
| TASK-074 | Chat view: message list, streaming render, tool call visualization, HITL approval buttons | TASK-012, TASK-017, TASK-072 | L | code | User/assistant/tool/approval message types; markdown rendering (marked.js); copy button; scroll lock |
| TASK-075 | Requests view: pending approvals list, detail modal, approve/deny with reason | TASK-051, TASK-073 | M | code | Polls `/api/approvals` + SSE updates; reason textarea required for deny; keyboard shortcuts (A/D) |
| TASK-076 | Workspace view: file tree (inbox/output/projects/scratch), preview, upload, download | TASK-025, TASK-027, TASK-073 | M | code | Virtualized tree (render 50 nodes); `fetch /api/files?path=`; drag-drop upload to inbox |
| TASK-077 | Airlock Monitor: recent ingress/egress, violations, scanner status | TASK-028, TASK-029, TASK-030, TASK-073 | M | code | Table with timestamp, file, action, result; color-coded; filter by type |
| TASK-078 | Network Log: egress requests, allow/deny, latency, rate limit remaining | TASK-039, TASK-040, TASK-073 | M | code | Live table; domain filter; export CSV button |
| TASK-079 | Memory Browser: layer visualization, search, pin/unpin, restore from compaction_log | TASK-009, TASK-065, TASK-073 | L | code | Tree of layers; click → preview; search queries `query_memory` tool; pin toggles `pinned` flag |
| TASK-080 | Agent Status: hardware profile (RAM/VRAM/CPU), Ollama models, alarms, thermal/battery | TASK-020, TASK-023, TASK-066, TASK-073 | M | code | Real-time gauges (Chart.js or Canvas); model load status; alarm badge counts |
| TASK-081 | Privacy Log: sensor events, data collected, retention controls | TASK-059, TASK-073 | M | code | Table of sensor events; `Delete` button per entry; `Clear All` with confirmation |
| TASK-082 | Statistics: approval rates, rejection reasons, task duration, token usage | TASK-017, TASK-051, TASK-073 | M | code | Charts (weekly/monthly); rejection reason breakdown; token cost estimate |
| TASK-083 | Ontology CRUD UI: inline edit, drag-drop reorder, goal expansion wizard | TASK-055, TASK-056, TASK-073 | L | code | Expand wizard: textarea → stream expansion → Q&A cycles → create; keyboard accessible |
| TASK-084 | Settings view: config.yaml editor (YAML + schema validation), model selection, mode toggles | TASK-004, TASK-073 | M | code | `GET/PUT /api/config`; validates against JSON Schema; restarts affected subsystems |
---

## Phase 12: Integration Tests & Polish (Week 6)

### Epic: INT — End-to-End, Security, Packaging

| ID | Title | Depends On | Size | Type | Acceptance Criteria |
|----|-------|------------|------|------|---------------------|
| TASK-085 | Integration test: full task lifecycle (forge mode → write code → test in VM → commit) | TASK-017, TASK-038, TASK-046 | L | test | `go test -tags=integration -run TestForgeTask` passes; creates git commit in `projects/` |
| TASK-086 | Security test: path traversal, symlink escape, VM network isolation, prompt injection | TASK-026, TASK-037, TASK-039 | L | test | `go test -tags=security -run TestSecurity` passes; all attack vectors blocked |
| TASK-087 | Prompt regression test: golden files for each mode, diff on change | TASK-061, TASK-062 | M | test | `testdata/prompts/{mode}.golden`; `go test -run TestPromptRegression` fails on drift |
| TASK-088 | Load test: 10 concurrent tasks, 5 VMs, 20 fetches — no OOM, no deadlock | TASK-036, TASK-040 | M | test | `go test -tags=load -run TestConcurrency`; runs 5min; asserts <5% error rate |
| TASK-089 | macOS packaging: `.dmg` with `.app`, codesign, notarization stub, Virtualization permission | TASK-035 | M | infra | `make dmg` produces `Athanor.dmg`; installs to `/Applications`; first launch requests VM permission |
| TASK-090 | Linux packaging: installer script, Firecracker/jailer deps, systemd service, KVM check | TASK-034 | M | infra | `curl -fsSL https://get.athanor.dev | sh` installs; `systemctl start athanor` works |
| TASK-091 | Onboarding flow: first-run detection, goal input, expansion, first artifact | TASK-056, TASK-074, TASK-083 | M | code | Fresh DB → UI shows "What do you want this agent to work on?" → expansion → artifact in output/ |
| TASK-092 | README + ARCHITECTURE.md sync: verify all MVP features documented | TASK-091 | S | doc | No TODOs in ARCHITECTURE.md for MVP; README has quickstart, config reference, troubleshooting |

---

## Dependency Graph (Critical Path)

```
TASK-000 → TASK-001 → TASK-002 → TASK-003 → TASK-004 → TASK-005 → TASK-006 → TASK-007 → TASK-008 → TASK-009
                                                              ↓
TASK-010 → TASK-011 → TASK-012 → TASK-013 → TASK-014 → TASK-015 → TASK-016 → TASK-017 → TASK-018
                                                              ↓
TASK-019 → TASK-020 → TASK-021 → TASK-022 → TASK-023 → TASK-024
                                                              ↓
TASK-025 → TASK-026 → TASK-027 → TASK-028/029/030
     ↓
TASK-031 → TASK-032 → TASK-033 → TASK-034
                                                              ↓
TASK-039 → TASK-040 → TASK-041 → TASK-042 → TASK-043 → TASK-044
                                                              ↓
TASK-045 → TASK-046/047/048/049
                                                              ↓
TASK-050 → TASK-051 → TASK-052 → TASK-053 → TASK-054
                                                              ↓
TASK-055 → TASK-056 → TASK-057 → TASK-058 → TASK-059
                                                              ↓
TASK-060 → TASK-061 → TASK-062 → TASK-063 → TASK-064 → TASK-065
                                                              ↓
TASK-066 → TASK-067 → TASK-068 → TASK-069 → TASK-070
                                                              ↓
TASK-071 → TASK-072 → TASK-073 → TASK-074...084 (parallel)
                                                              ↓
TASK-085 → TASK-086 → TASK-087 → TASK-088 → TASK-089/090 → TASK-091 → TASK-092
```

---

## MVP Exit Criteria

All of the following must pass:

- [ ] `go test ./...` passes (unit + integration + security)
- [ ] `make dmg` / `make linux-installer` produces working artifacts
- [ ] Fresh install → onboarding → first artifact in <5 minutes
- [ ] 24h soak test: daemon runs idle, no memory leaks, no goroutine leaks
- [ ] Kill switch tested: freezes daemon, survives restart
- [ ] Security audit: no path traversal, no VM escape, no SSRF
- [ ] All ARCHITECTURE.md §16 MVP items implemented

---

## Post-MVP (v1.0+) — Placeholders

| Epic | Description |
|------|-------------|
| SEN | Full sensor bus: RSS, IMAP, Calendar, Browser activity |
| MEM | Progressive compaction background job, vector search optimization |
| ART | Full artifact taxonomy (draft, idea, code, research, report, workflow) |
| EXE | Whittle, Harden, Tend execution modes |
| THE | Daydream, Blue Sky theoretical modes |
| NET | ClamAV integration, IMAP sensor, model routing, compute budgets |
| QAL | Quality/Drift alarms, prompt regression CI |
| VM | **FirecrackerExecutor** (Linux): jailer, seccomp, virtio-fs, TAP networking; **VirtExecutor** (macOS): Virtualization.framework Swift bridge; both implement `Executor` interface |
| PKG | macOS `.dmg` / Linux installer updates for VM dependencies |

---

## Notes for Implementers

1. **Start at TASK-000 and follow the critical path.** Parallelize only where dependency graph allows (e.g., UI views TASK-074–084 can fan out after TASK-073).
2. **Write the test first** for each task — the acceptance criteria *are* the test spec.
3. **Commit after each task** with message: `TASK-XXX: <title>` — keeps git history aligned with plan.
4. **Update this file** when tasks are added/removed/changed — it's the source of truth.
5. **No task > 4 hours.** If a task feels larger, split it.