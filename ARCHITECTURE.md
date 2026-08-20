# Athanor — System Architecture

An *athanor* is an alchemical furnace designed to burn continuously without interruption.

---

## 1. Design Principles

- **Local-first.** Everything is designed to stay local and secure. Storage, inference, etc. Cloud inference optional.
- **Containment-based trust.** Contained + reversible = no approval. External + irreversible = approval.
- **Ephemeral execution.** Every task runs in a fresh MicroVM, destroyed on completion.
- **Git as undo.** All code modifications are atomic commits on agent-created branches.
- **Hardware-adaptive.** Ollama handles physical device routing (GPU/CPU). The daemon handles capacity planning (preventing OOM) and performance guarding.
- **Agentic context floors.** Agentic work requires deep context. We enforce strict minimum context windows and downgrade models rather than cripple the context.
- **Feedback compounds.** Rejections with reasons stored and injected into future prompts.
- **Mountains and rivers.** Security constraints never change. Prompts and strategies evolve.
- **Test everything.** TDD at integration scale. Security tests adversarial. Prompt regressions tracked.

---

## 2. Stack

| Layer | Technology |
|---|---|
| Core daemon, web server, state machine, proxy, file validation | **Go** |
| macOS VM helper | **Swift** (Virtualization.framework) |
| Web UI | **HTML/CSS/JS** (vanilla, no framework) |
| Persistent state | **SQLite** (+ `sqlite-vec`, FTS5) |
| LLM inference | **Ollama** (prerequisite, REST API `localhost:11434`) |
| Sandbox | **Firecracker** (Linux) / **Virtualization.framework** (macOS) |
| File sharing into VMs | **virtio-fs** |
| Web extraction | **Go `net/http` + `go-readability` + `bluemonday`** |
| Filesystem events | **`fsnotify`** |
| System metrics | **`gopsutil`** |

---

## 3. Topology

The Go daemon is the **sole router**. No component communicates peer-to-peer.

```
┌─────────────────────────────────────────────────────────────┐
│                        User Device                          │
│  ┌─────────┐    HTTP      ┌──────────────┐                  │
│  │ Browser │◄────────────►│  Go Daemon   │                  │
│  │(Vanilla)│   SSE/fetch  │   (Router)   │                  │
│  └─────────┘              └──────┬───────┘                  │
│                                  │                          │
│        ┌─────────────────────────┼─────────────────┐        │
│        ▼                         ▼                 ▼        │
│   ┌─────────┐              ┌──────────┐      ┌─────────┐    │
│   │ SQLite  │              │  Ollama  │      │ Airlock │    │
│   │(State)  │              │  (LLM)   │      │(Files)  │    │
│   └─────────┘              └──────────┘      └─────────┘    │
│        ▼                         ▼                 ▼        │
│   ┌─────────┐              ┌──────────┐      ┌─────────┐    │
│   │  Sensor │              │ MicroVM  │      │  Egress │    │
│   │   Bus   │              │ Manager  │      │  Proxy  │    │
│   └─────────┘              └────┬─────┘      └────┬────┘    │
│                                 │                 │         │
│                                 ▼                 ▼         │
│                          ┌─────────────┐    ┌──────────┐    │
│                          │  Ephemeral  │    │ Internet │    │
│                          │   MicroVM   │    │(Allowlist│    │
│                          │(Isolated)   │    │  gated)  │    │
│                          └─────────────┘    └──────────┘    │
└─────────────────────────────────────────────────────────────┘
```

**Isolation rule:** The MicroVM reaches *only* the Go daemon's internal API. It cannot reach Ollama, the internet, or the host filesystem directly.

---

## 4. Directory Layout & Configuration

```
~/athanor/
├── athanor.db                 # SQLite: all state
├── config.yaml                # User-editable configuration
├── athanor.log                # Rotating text log
├── backups/                   # Local compressed archives
├── quarantine/                # Security-flagged files
└── workspace/                 # THE AIRLOCK
    ├── inbox/                 # User uploads
    ├── output/                # AI-generated files awaiting review
    ├── projects/              # Git-tracked project directories
    └── scratch/               # Ephemeral VM working space
```

**`config.yaml` (Key Sections):**
```yaml
agent: { active_hours: "08:00-23:00", idle_threshold: "30m", max_proactive_tasks_per_day: 10 }
hardware: { vram_buffer_percent: 10, battery_pause_background: true, thermal_throttle_pause: true }
models: { default: "gemma3:12b", filtering: "gemma3:4b", coding: "qwen2.5-coder:32b", creative: "gemma3:27b" }
context_minimums: { coding: 32768, research: 32768, simple: 16384 } # Hard floors for agentic work
execution_modes: { forge: { enabled: true }, whittle: { enabled: true }, harden: { enabled: false }, tend: { enabled: true } }
theoretical_modes: { daydream: { enabled: true }, blue_sky: { enabled: true }, deep_dream: { enabled: false }, genesis: { enabled: false } }
limits: { max_concurrent_vms: 2, max_concurrent_fetches: 5, max_concurrent_llm_calls: 1, max_tasks_per_hour: 20 }
recovery: { max_tool_call_retries: 2, max_loop_interventions: 1, max_context_compactions: 3, max_total_recoveries: 8 }
network: { allow_list: [github.com, arxiv.org, pypi.org], rate_limit_per_minute: 30 }
backup: { auto: true, schedule: "0 3 * * *", max_local_backups: 10 }
```

---

## 5. Hardware Adaptation & Context Management

Ollama abstracts the physical hardware. It natively handles GPU vs. CPU routing, partial VRAM offloading, and quantization. **The daemon does not route inference; it manages capacity and performance.**

### 5.1 Context Calculation (Capacity Planning)

Ollama sets `num_ctx` at model load time. Changing it requires reloading. Calculation is **not** per-request.

**Formula:**
```
maxContext = (available_system_memory - model_size - overhead - buffer) / kv_per_token
```

**Agentic Context Floors:**
Agentic work (coding, RAG, multi-step reasoning) fails at low context (e.g., 4k or 8k). We enforce strict minimums defined in `context_minimums`.

1. Calculate `maxContext` for the selected model.
2. If `maxContext >= task_minimum` (e.g., 32k for coding): Load model with calculated context.
3. If `maxContext < task_minimum`: **Do not cripple the context.** Instead, trigger a **Model Downgrade Recommendation**.
   - *Daemon logic:* "Hardware supports `gemma3:12b` at 8k, but coding requires 32k. Recommend switching to `gemma3:4b` which supports 32k on this hardware. [Switch] [Cancel Task]"
4. Clamp final value to the model's architectural maximum.

### 5.2 Performance Guarding

| Signal | Adaptation |
|---|---|
| **Ollama Latency > 3× avg** | Indicates CPU fallback or memory swapping. Notify user: "Inference is slow. Consider a smaller model." |
| **RAM pressure >85%** | Pause background tasks. >95%: save state, pause all, notify. |
| **Battery < 20%** | Pause all, save state, low-power mode. |
| **Thermal throttling** | Reduce concurrent VMs to 1. Reduce LLM call frequency. |
| **Laptop sleep** | Flush state to SQLite. Pause VMs gracefully. Resume on wake. |

### 5.3 Model Lifecycle

- **Startup:** Load default model with calculated `num_ctx`.
- **Model change / Context recalculation:** Reload at next idle point.
- **Idle > 30 min:** Allow Ollama to unload to free memory. Reload on next request.

---

## 6. Memory Architecture

### 6.1 Tiers

| Tier | Scope | Storage |
|---|---|---|
| Working | Current prompt + active task | Native context window |
| Session | Conversation / active document | Auto-compacted near limit |
| Long-term | All history, docs, code, feedback | SQLite `sqlite-vec` + FTS5 |

### 6.2 Progressive Halving Compaction

```
Layer 0: Originals (always retained on disk)
Layer 1: 2× summaries
Layer 2: 4× summaries
...
Layer N: Until total ≤ 1,000,000 tokens
```

- **Below 1M:** Progressive halving. Retrieval prefers most detailed layer that fits.
- **Above 1M:** Fixed windows (e.g., 220k tokens). Vector search selects relevant windows.
- **Pinned:** Never compressed. Always injected.
- **Compaction log:** Visible and restorable in Memory Browser.

---

## 7. Agent Orchestration

### 7.1 State Machine & Execution

```go
type Node func(ctx context.Context, s *AgentState) (*AgentState, error)
```

State serialized to SQLite after every node.
**Flow:** Task triggered → Build 5-layer prompt → Enter state machine → Execute nodes → HITL gates → Produce artifact → Persist state.

### 7.2 Constrained Tool Interface

| Tool | Description |
|---|---|
| `read_file(path)` / `write_file` / `list_files` / `search_files` | File ops within workspace/allowed dirs |
| `execute_code(lang, code)` | Execute in ephemeral MicroVM |
| `fetch_url(url)` / `search_web(query)` | Via Egress Proxy (Reader Mode) |
| `git_operation(repo, op)` | Git operations within workspace |
| `query_memory(query)` | Hybrid search over long-term memory |
| `create_artifact(type, content)` | Create draft or idea artifact |

---

## 8. HITL: Containment-Based Trust

**No Approval:** Read/write workspace, create git branches/commits, run tests in VM, query memory, generate proposals, self-improvement on non-security files.

**Approval Required:** New domains, external data transmission, modifying files outside workspace, permanent deletion, remote git push, system package installation, modifying daemon config/security, unresolvable questions.

---

## 9. Sandbox & Security

### 9.1 Ephemeral MicroVMs

One fresh VM per task. Destroyed after.
- **Linux:** Firecracker + `jailer` (KVM, ~125ms boot).
- **macOS:** Virtualization.framework (Swift helper, ~300ms boot).
- **Inside VM:** Non-root, `noexec,nosuid,nodev`, seccomp. Network routes *only* to Go daemon.

### 9.2 Airlock (File Isolation)

1. **Daemon scope:** Ops only under workspace + allowed dirs.
2. **Ingress:** `filepath.EvalSymlinks` → validate roots → copy content only.
3. **Egress:** `os.Lstat` → reject symlinks/non-regular → copy content only.
4. **Open flags:** `O_NOFOLLOW` on all workspace opens.
5. **Periodic scan:** 30s interval. Delete outbound symlinks, setuid, devices.
6. **VM mount:** `virtiofsd --sandbox=namespace` / `VZSharedDirectory`.

### 9.3 Network & Web

- **Egress Proxy:** Go HTTP proxy. Allow-list, logging, header sanitization, rate limiting.
- **Reader Mode (Default):** `net/http` → `go-readability` → `bluemonday` → Markdown. No JS.
- **Browser Mode (Opt-in):** Headless in MicroVM. HITL gate with warning.

### 9.4 Alarms & Kill Switch

- **Kill Switch:** Persistent STOP button. Kills VMs, cancels LLM/fetches, enters freeze.
- **Alarms:** Notice, Warning, Alert (pause task), Critical (freeze daemon). Triggers: Loop, Drift (modifying Mountains), Resource, Quality (>80% rejection), Security, Stuck, Hallucination, Budget, Self-Mod.

---

## 10. Ontology & Goal Expansion

```
Topics    ← broad interests, tag everything
Goals     ← high-level missions, created via expansion
Projects  ← concrete containers (repos, reports, workflows)
Tasks     ← recurring, one-time, or triggered. Belong to Projects.
```

**Goal Expansion Flow:**
1. User writes 1–2 sentences.
2. Agent streams expansion (1–2 mins): assumptions, questions, proposed structure.
3. User answers/refines (1–3 cycles).
4. Goal + Projects + Tasks + Topics created. Agent begins.

**Task Types:**
- **Recurring:** Cron schedule (e.g., `0 8 * * *`).
- **One-time:** Run once.
- **Triggered:** Sensor event (e.g., `new_file:inbox/*.pdf`).

---

## 11. Proactive Modes & Artifacts

**Priority:** `user tasks → execution → theoretical → genesis → idle`

- **Execution (Contained):** Forge (new branches), Whittle (reduce), Harden (tests/edges), Tend (deps/lint).
- **Theoretical (Proposals):** Daydream (synthesize), Blue Sky (research), Deep Dream (speculative), Self-Improvement (own code).
- **Genesis:** Finds unexplored intersections, builds PoC.

**Feedback Loop:** Rejections with reasons stored in long-term memory. Injected as Layer 5 in future prompts for the same project/mode.

---

## 12. Intelligence Layer

### 12.1 Prompt Architecture (Five Layers)

1. **Base (Mountain):** Hardcoded identity, safety, tool interface. Immutable.
2. **Mode (River):** Mode-specific behavior. Versioned in SQLite.
3. **Task (River):** Current task, project, goal context.
4. **Context (Dynamic):** Retrieved memories, files, conversation.
5. **Feedback (Dynamic):** Past rejections for this project/mode.

### 12.2 Error Recovery

| Error | Recovery | Escalation |
|---|---|---|
| Invalid tool call | Re-prompt with schema (max 2) | Abandon step |
| Hallucinated path | Inject actual file listing (max 2) | Halt |
| Loop (3× same call) | Inject loop-breaking prompt | Halt |
| Context overflow | Emergency compaction | Split task |
| Timeout (>120s) | Retry simplified (max 1) | Halt |
| Sandbox failure | Boot fresh VM (max 1) | Halt |

**Recovery budget:** Max 8 total recoveries per task. Exceeded → halt.

---

## 13. 24/7 Reliability & System Internals

- **System Tray:** States (idle/active/waiting/degraded/error/battery/thermal).
- **Watchdog:** Unresponsive >60s → restart. Uptime >7 days → restart at idle.
- **Backups:** Local compressed archives. Retention via `max_local_backups`.
- **Logging:** SQLite + rotating file. Categories: tasks, recovery, alarms, airlock, network, VM, sensors.
- **Migration:** Versioned, forward-only SQL embedded in binary. Backup before migration.

---

## 14. Web UI & Sensors

### 14.1 Web UI (Vanilla JS)

**Check-in Flow:** Daily Digest → Completed Work → Issues → Requests → Chat.
**Views:** Chat, Requests, Workspace, Airlock Monitor, Network Log, Memory Browser, Agent Status (Hardware profile, alarms), Privacy Log, Statistics (Approval rates, rejection reasons), Ontology CRUD, Settings.

### 14.2 Sensor Bus

User input (real-time), Workspace files (`fsnotify`), RSS/Email (15-30m), Calendar/System logs (hourly), Browser activity (event-driven). Filtered for signal, logged in Privacy Log.

---

## 15. Deployment & Onboarding

### 15.1 Installation

- **macOS:** `.dmg` → `.app`. Requests Virtualization.framework permission.
- **Linux:** `curl -fsSL https://get.athanor.dev | sh`. Downloads Firecracker/jailer, checks KVM, creates systemd service.

### 15.2 Onboarding (First 5 Minutes)

1. Open `localhost:8080`. Clean screen: *"What do you want this agent to work on?"*
2. User types goal. Agent streams expansion.
3. User approves structure and initial domains.
4. First artifact appears.

---

## 16. Roadmap

### MVP (v0.1)
Go daemon + JS UI, Ollama chat + SSE, Hardware adaptation + context floors, SQLite state/ontology, Goal expansion, File airlock, Ephemeral MicroVM, Git-native workspace, Constrained tools, HITL for external actions, Egress proxy, Meta-prompt, Daily Digest, Kill switch, System tray, Feedback loop, Statistics, Forge mode, Error recovery, Alarms, Sleep/wake handling, Integration/Security tests.

### v1.0
Sensor bus (RSS/fsnotify), Long-term hybrid memory, Progressive compaction, Full artifact taxonomy, Whittle/Harden/Tend, Daydream/Blue Sky, Reader Mode, Airlock/Privacy UI, ClamAV, IMAP, Model routing, Compute budgets, Thermal monitoring, Backup retention, Prompt regression tests, Quality/Drift alarms.

### v2.0+
Genesis, Deep Dream, Self-Improvement, Browser Mode, Cloud backup, Natural-language workflows, Confidence-gated autonomy.