<div align="center">

# ⚗️ Athanor

### An alchemical furnace for your thoughts and code.

**Local-first · Container-native · Semi-autonomous · Built to burn 24/7**

**License: [AGPL-3.0](LICENSE)**

</div>

---

An *athanor* is an alchemical furnace designed to burn continuously without interruption. Athanor is a local-first, container-native, semi-autonomous agent system that runs on your own hardware, works while you sleep, and never sends your data anywhere unless you explicitly allow it.

You give it a **goal**. It autonomously decomposes the goal into a Directed Acyclic Graph (DAG) of tasks, then executes those tasks through a **Dialectical Execution Engine** that generates multiple candidates, tests them in isolated containers, evaluates them deterministically, and keeps only the best result. A **Multidimensional Context Engine (MCE)** maintains full-fidelity memory across long sessions through lossless division and deterministic Temp 0.0 compaction. When idle, it *daydreams* — consolidating memory, indexing repositories, and proactively documenting code.

## Core Characteristics

| | |
|---|---|
| 🏠 **Local-First** | Ollama by default. Cloud inference is optional, budget-gated, and HITL-approved. |
| 🔁 **24/7 Endurance** | Survives sleep, power loss, Podman restarts, and Ollama restarts. Resumes from checkpoint. |
| 📦 **Container-Native** | Rootless Podman. A persistent Core Pod (control plane) plus ephemeral, hardened Job Pods for execution. |
| 🧠 **Multidimensional Context** | Lossless AST/structural division for code and data; deterministic Temp 0.0 compaction for logs and conversations. |
| 📈 **Self-Improving** | Structured `CorrectionRecord`s are harvested from every failure and rejection, injected into future prompts via vector similarity — plus automatic win/loss mining of execution strategies. |
| 💭 **Daydreaming** | Idle cycles consolidate memory, index repositories, and proactively document code — always as reviewable drafts. |
| 🔒 **Secure by Default** | Internet Gated Reader (no browser, no JavaScript), file airlock with ClamAV/YARA scanning, ephemeral hardened Job Pods. |
| ✅ **Goal-Oriented Validation** | Goals become tasks with explicit `AcceptanceCriteria`. Artifacts are validated before synthesis and compared against prior best before acceptance. |

## Key Concepts

| Concept | One-liner |
|---|---|
| **Dialectical Execution Engine** | Multi-phase, multi-temperature state machine: diverge → evaluate → reflect → synthesize → compare. Multiple candidates enter; the deterministically-judged best leaves. |
| **Multidimensional Context Engine (MCE)** | N-dimensional memory system. Code is divided losslessly by AST/structure; logs and conversations are compacted deterministically at Temp 0.0. |
| **Personas** | Models assigned to functional roles — `wide` (ingestion), `tall` (hard reasoning), `main` (execution), `security` (judging & all compaction), `alternative` (orthogonal perspective). |
| **Job Pods** | Ephemeral, rootless Podman containers with read-only rootfs, no network, and resource limits. One per execution, destroyed after. |
| **CorrectionRecords** | Structured negative feedback (category, severity, derived rule) that makes every rejection improve every future job. |
| **Strategy Insights** | Automatic win/loss analysis of execution strategies (personas, temperatures, candidate counts). Statistical evidence only; advisory until approved. |
| **Daydreaming** | Budget-limited idle-time jobs: memory consolidation, repo indexing, skill refinement, proactive documentation. Output is always draft status. |
| **HITL Queue** | Human-in-the-loop as a live approval queue, not a report. External, irreversible, or high-risk actions pause for you. |
| **File Airlock** | All files entering or leaving agent-managed space pass through symlink/traversal/malware/prompt-injection scanning. |

## Architecture at a Glance

```text
Host Adapter (power, sleep, idle)  →  Core Pod [core · gateway · scanner · ui]
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    ▼                     ▼                     ▼
                 SQLite               Ollama               Job Pods
              (WAL + vectors)      (local inference)    (ephemeral,
                                                        sandboxed execution)
```

Full topology, isolation rules, and every subsystem are documented in [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Quick Start

### Prerequisites

**Required:**
- [Podman](https://podman.io/) (rootless mode capable)
- [Ollama](https://ollama.com/) with persona models pulled (see `personas:` in the config reference)
- Git

**Optional:**
- ClamAV (file scanning) and YARA (rule-based detection)
- Python (for skill development)
- Language toolchains for Job Pods; media tools for media archetypes

> **macOS note:** Podman runs inside a VM (`podman machine`) on macOS. Athanor's Host Adapter accounts for this overhead automatically.

### Install & First Run

> **Note:** Athanor is pre-MVP; the container packaging (`athanor doctor` /
> `athanor start` below) is the M7 target. Today the daemon runs directly
> from a clone with a real CLI:

```bash
make build
make run                                     # daemon on http://127.0.0.1:7420 (defaults, no config needed)
./bin/athanor project create -name demo -archetype text -goal "Write a short essay about why local-first software matters."
./bin/athanor goal submit -project <id> -goal "Summarize it in five bullet points."
./bin/athanor job watch -job <id>            # streams every phase; ends with the draft artifact
```

Full walkthrough (including the kill switch and crash recovery):
[docs/demo-m1.md](docs/demo-m1.md) — that script is the Gate G1 demo.

The eventual flow:

```bash
# 1. Validate your hardware and environment (proposes fixes where possible)
athanor doctor

# 2. Start the Core Pod
athanor start
```

Athanor does not open a generic chat. The first-run flow creates a **project**:

```yaml
name: my-rest-api
goal: "Build a small REST API for managing a personal book collection."
archetype: code            # text | code | document | data | media
acceptance_criteria:
  - "pytest passes with >90% coverage on new endpoints"
start_immediately: true
```

Then watch it work in real time from the Watch View, or queue it and check the Morning Digest later.

> ⚠️ **Cloud warning:** Cloud inference is optional. Athanor is optimized for local iterative execution; cloud calls can increase cost quickly during multi-pass evaluation. Enabling requires explicit confirmation.

## What It Feels Like

| Workflow | You provide | Athanor delivers |
|---|---|---|
| **Code** | A goal + archetype constraints | DAG of tasks → test-driven implementation candidates run in Job Pods → evaluated, compared, committed on an agent-created branch |
| **Brainstorm** | An exploratory goal | Scored option sets with strengths/weaknesses/risks, assumptions, questions, and recommended next tasks |
| **Research** | A question + allowlisted domains | Reader Mode extraction (no JS), cited summaries, contradiction analysis, report artifact |

## Status

Pre-MVP. **M0 Foundations is complete** (Gate G0 passed): Go module layout, config loader with validation, structured category-tagged logging, store layer with forward-only embedded migrations (incl. enum-normalization migration 0003, [ADR-0005](docs/adr/0005-canonical-enum-values.md)), the append-only EventLog API, a runnable daemon serving `/healthz` on loopback, and CI (vet + test-race + lint). Crash recovery verified: kill -9 mid-run leaves a usable database and restarts cleanly. The daemon boots on built-in defaults when no `config.yaml` exists (see `config.example.yaml`). **M1 — Walking Skeleton is code-complete** (T1–T7): a goal goes in, an LLM-generated draft artifact comes out, the run survives kill -9 mid-job, the kill switch freezes all new work, and an executable gate test proves no tool-execution capability exists (Gate G1 evidence: [docs/demo-m1.md](docs/demo-m1.md)). M1-T8 (quality probe against a live Ollama) ran 2026-08-26 with `gemma4:12b-mlx`; 5/5 acceptance-criteria adherence across text/code/document archetypes. Findings and per-model timings at [docs/probes/m1-quality-probe.md](docs/probes/m1-quality-probe.md). The M1-T8 follow-ups — synthesis-prompt preamble suppression, default-budget raise, synthesizing-phase recovery test — are integrated. Next: **M2 — Container Spine**.

## Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)** — the complete design: topology, object model, MCE, personas, dialectical engine, security, configuration reference, testing strategy.
- **[docs/](docs/)** — implementation notes ([SQLite setup](docs/sqlite-setup.md), [ADRs](docs/adr/)).
- **[spikes/](spikes/)** — throwaway validation code.

## License

Athanor is licensed under the [GNU Affero General Public License v3.0](LICENSE).
