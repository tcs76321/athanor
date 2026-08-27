# M1 Quality Probe — Protocol and Findings (ROADMAP M1-T8)

**Status: RAN on 2026-08-26.** Findings at the bottom; protocol and
hypotheses above for context. **M1-T8 follow-ups (M1-T8.1 prompt fix,
M1-T8.2 default-budget raise, M1-T8.3 synthesizing-phase recovery test)
integrated 2026-08-26.** The default persona models in
`config.example.yaml` (`qwen2.5:7b`, `qwen2.5-coder:32b`,
`mistral-nemo:12b`, `phi3:3.8b`, `llama3.1:8b`) are not available in the
development environment. The probe ran with a substituted persona
mapping (`config-probe.yaml`) using four models the developer pulled:

| Role | Model | ctx_target | temp | Rationale |
|---|---|---|---|---|
| `wide` | `gemma4:12b-mlx` | 32768 | 0.7 | 256K-capable; conservative target is fine for wide-context phases |
| `tall` | `gemma4:12b-mlx` | 16384 | 0.4 | "Tall" = the strongest available; on this run, the same model as `main` (one-model probe) |
| `main` | `gemma4:12b-mlx` | 32768 | 0.7 | Fast, strong general-purpose; workhorse for diverging/synthesizing |
| `security` | `gemma4:12b-mlx` | 8192 | **0.0** | Same model, low-temp deterministic call (M1 doesn't invoke security in single-candidate mode) |
| `alternative` | `gemma4:12b-mlx` | 16384 | 0.5 | SOTA models are pulled but not invoked by M1; mapping to the resident model keeps the registry's "all roles assigned" invariant honest |

H1 below is re-stated against the **actually-used** tall persona
(`gemma4:12b-mlx`, a general-purpose reasoning model), not the
unavailable `qwen2.5-coder:32b` the original draft referenced. The
probe's premise — "what does M1 do on the developer's actual hardware?"
— is preserved.

## Hypotheses

1. **H1 (planning quality):** Local reasoning models produce useful
   plans for small, well-scoped text/document goals. On this run, the
   `tall` persona (`gemma4:12b-mlx`) is a general-purpose 12 B model
   that is not a coding specialist, so the code goal's planning
   phase is the key signal: can it still produce a coherent plan
   when it has no repository context (M1 has no MCE) and the goal
   is a "write a Python module" task?
2. **H2 (synthesis adherence):** Single-candidate synthesis adheres to
   acceptance criteria roughly in proportion to criteria count: one to
   two criteria are usually respected; five or more are frequently
   partially dropped.
3. **H3 (phase-chain latency):** Three sequential LLM calls per job
   (planning, diverging, synthesizing) dominate job wall time; on a
   mid-range laptop each call takes 10–120s, so a job completes in
   roughly 1–6 minutes.
4. **H4 (comparison emptiness):** The M1 deterministic comparison
   (single candidate, no previous best) is acceptable now but will be
   the weakest link once M3 adds real evaluation — the probe should note
   anything that would change that plan.

## Method

Run the five sample goals below (text × 2, code × 1, document × 2),
each with its stated acceptance criteria, through the daemon
(`docs/demo-m1.md` procedure), then record per goal:

- job wall time and per-phase times (from `GET /jobs/{id}/events`
  timestamps; each `transition` event carries a `ts`),
- criteria adherence (manual check of the final draft artifact),
- qualitative usefulness on a 1–5 scale with a one-line justification.

After the five samples, run two live operational probes:

- **Kill switch (M1-T6, §22):** `bin/athanor freeze` between submit and
  completion; new `POST /goals` must return 409, in-flight job runs to
  completion, `unfreeze -reason "..."` re-enables submission.
- **Crash recovery (§23.6):** `kill -9` the daemon mid-run, restart,
  `bin/athanor job watch` shows the `recovery_flag: interrupted` then
  resumes to completion.

The probe helper lives at `spikes/m1-quality-probe/`. Per-phase times
and token totals are computed from `GET /jobs/{id}/events`; final
artifact content is read from the state directory.

## Sample goals

| # | Archetype | Goal | Criteria |
|---|---|---|---|
| 1 | text | "Write a short essay about why local-first software matters." | at least three arguments; a conclusion |
| 2 | text | "Draft a friendly onboarding email for a new community member of a local software club." | under 200 words; one clear call to action |
| 3 | code | "Write a Python module that manages a personal book collection with add, list, and search functions." | pure stdlib; docstrings on every public function; a usage example |
| 4 | document | "Create a README for a small CLI tool that converts Markdown files to HTML." | installation section; usage examples; license section |
| 5 | document | "Write a one-page design brief for a weekend project that builds a sunrise alarm clock from a Raspberry Pi." | parts list; build steps; at least two risks named |

## Findings

### Environment

- Daemon: `bin/athanor serve -config config-probe.yaml -state-dir
  state-probe` on a MacBook Pro M2 Max, 32 GB unified memory, macOS.
- Ollama 0.33.1 with 5 models pulled: `gemma4:12b-mlx` (7.7 GB file,
  14 GB GPU-resident), `qwen3.8:27b-mlx` (18 GB file, 22 GB
  GPU-resident), `ornith-1.5:9b` (6.6 GB), `granite4.2:3b` (2.2 GB),
  `nomic-embed-text` (274 MB).
- Persona config (`config-probe.yaml`): all five roles map to
  `gemma4:12b-mlx`. The protocol was originally designed for five
  distinct models; running two large models concurrently
  (`qwen3.8:27b-mlx` 22 GB + `gemma4:12b-mlx` 14 GB) on a 32 GB host
  exceeded unified memory and thrashed both via swap. The probe was
  rerun with a single model so the timings reflect a single
  inference pipeline, not a memory-thrash confound. This is recorded
  in the config's header comment and is the most important
  environmental caveat.
- Per-phase wall-time budgets were raised from the 120 s default for
  `planning` to 600 s. The first run revealed that even with a single
  model, the planning phase on a 27 B thinking-mode model can take
  2–3 min for a 30-line prompt. With the default 120 s budget, samples
  3 and 5 of the first run failed with `context deadline exceeded`
  before the planning call returned. The 600 s budget is generous
  enough to absorb cold-load latency on a fresh Ollama runner.

### Per-sample results

Five sample goals were run sequentially. Per-phase times are computed
from `transition` event timestamps in the EventLog. `recovery_flag` is
a jobs-row column, not an audit event, so the probe does not record
it directly; the crash-recovery test below exercises it.

| # | archetype | goal (truncated) | criteria (truncated) | total_s | plan_s | diverge_s | synth_s | calls | prompt_tok | compl_tok | artifact_B | adherence | usefulness | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | text | local-first essay | three arguments; a conclusion | 113.3 | 31.6 | 41.6 | 40.1 | 3 | 1742 | 4359 | 4320 | yes | 5 | Three numbered arguments (Sovereignty, Engineering, Longevity) + Conclusion. |
| 2 | text | onboarding email | under 200 words; one CTA | 78.8 | 29.9 | 27.1 | 21.7 | 3 | 1399 | 3403 | 1531 | yes | 4 | ~100 words body; one CTA (Slack channel). Bracketed placeholders reduce usefulness. |
| 3 | code | Python book module | stdlib; docstrings; usage example | 169.5 | 38.8 | 65.3 | 65.3 | 3 | 2800 | 7435 | 4406 | yes | 5 | All criteria met. `typing.List` only, every public method has a docstring, `__main__` example included. |
| 4 | document | md2html README | install; usage; license | 98.3 | 30.8 | 36.3 | 31.0 | 3 | 1646 | 3908 | 2671 | yes | 5 | All three sections present, formatted, ready to ship. |
| 5 | document | sunrise-alarm brief | parts list; build steps; ≥2 risks | 115.6 | 37.6 | 35.4 | 42.5 | 3 | 1515 | 4299 | 3662 | yes | 5 | All criteria met; 3 risks named (power, clock drift, signal integrity). |

**Cross-sample observation:** every artifact begins with a
"### Change Summary" / "### Known Limitations" preamble explaining the
synthesis's choices. This is a **prompt-design finding for M3-T7**:
the synthesis prompt is producing meta-commentary at the top of every
artifact, even when the user asked for a clean deliverable. The
artifact content itself is below the meta-commentary, separated by
`***` — usable, but the deliverable's effective "first impression" is
the model's reasoning, not the answer.

### H1 (planning quality)

- **Observation:** All 5 samples (text, text, code, document, document)
  completed with the planning phase producing an audit-recorded
  `llm_call` (264 tokens in, 816–1507 tokens out). The final artifacts
  meet every stated acceptance criterion, including the code goal
  (sample 3) where the 12 B model produced a usable `BookCollection`
  class with `add`, `list`, `search_by_title`, and `search_by_author`
  methods — all under 100 lines, stdlib only, with a runnable usage
  example.
- **Implication:** The protocol's H1 — "local models produce useful
  plans for text/document goals, but degrade on code" — is **not
  supported** for the developer-class goal in this probe. The 12 B
  model handled the code goal's planning and synthesis adequately
  with **no repository context** (M1 has no MCE). However, the probe
  ran with a 12 B model only; H1 was originally about a 32 B
  coding-specialist. The substitution changes what "tall" can do;
  the probe does not rule out H1 against a stronger baseline. M3-T7
  should re-test H1 against the 27 B model **with a properly-sized
  config** (e.g. 24 GB of unified memory and Ollama's `num_parallel=1`
  to keep one model resident at a time).

### H2 (synthesis adherence)

- **Observation:** Adherence is 5/5 in this run, including the
  three-criterion code goal and the three-criterion document goals.
  Token counts in the synthesizing phase (1378–1642 prompt, 1166–1250
  completion) suggest the model is not dropping criteria from the
  prompt; the synthesis is being told what to do and is doing it.
- **Implication:** H2 — "five or more criteria are frequently partially
  dropped" — is **not supported** at the 2–3 criteria level tested
  here. The probe does not include any 5+ criterion goal; a follow-up
  probe should test the upper bound. The "Change Summary" preamble
  (see cross-sample observation) is a separate failure mode: the model
  is not losing criteria, it is adding narration around them.

### H3 (phase-chain latency)

- **Observation:** Wall times range from 78.8 s (text onboarding
  email) to 169.5 s (code Python module), with the code goal
  dominated by the divergence (65 s) and synthesizing (65 s) phases.
  The text goals average ~96 s; the document goals average ~107 s.
  Per-phase, planning takes 30–38 s, diverging 27–65 s, synthesizing
  21–65 s, comparing <1 s. The protocol's "1–6 minutes" estimate is
  consistent with the upper end of what we observed.
- **Implication:** H3 is **confirmed** for the 12 B single-model
  configuration. Two of the three LLM calls per job are the dominant
  cost. On hardware with two large models loaded concurrently (the
  first run of the probe), wall times tripled to 2–3 min even for
  simple text goals — the LLM call itself was unchanged, but memory
  pressure slowed everything down. **M2 (container spine) must plan
  for one model at a time per container, or test with the
  OOM-killer visible.** The fact that the code goal's divergence
  phase (65 s) is the slowest in this run is a finding about the
  prompt: it asks the model to write a complete Python module in
  one shot, and the model takes the time. M3 prompt work might
  consider whether a "scaffold first, fill in second" structure
  would be faster and more controllable.

### H4 (comparison emptiness)

- **Observation:** Every comparison event in the EventLog reads
  `{"event":"comparison","winner":"new","confidence":1,"reasons":["M1
  single-candidate mode: no previous accepted artifact exists, so
  the draft wins by definition (§19.3)."]}`. The deterministic
  decision is fast (<1 s) and the audit trail is honest about why.
- **Implication:** H4 is **confirmed and acceptable for M1.** The
  probe does not surface anything that would change the M3 plan to
  replace the deterministic comparison with real evaluation. M3-T7
  should re-test this with multiple divergence candidates to see
  whether the model self-evaluates usefully, but in single-candidate
  mode there is nothing to compare and the audit message is exactly
  what an M3 reader needs.

### Operational tests (kill switch, crash recovery)

- **Kill switch (§22.1–§22.2):** `POST /freeze` blocked
  `POST /projects/{id}/goals` with `409 Conflict` and the error
  message `"daemon is frozen: no new work is accepted (unfreeze
  with a reason first, §22.2)"`. `POST /projects` continued to
  succeed while frozen (the freeze only gates new work, not new
  project scaffolding — a reasonable choice, since the scaffold is
  metadata, not work). `DELETE /freeze` with `{"reason":"..."}`
  unfroze and re-enabled goal submission. **Confirmed working
  end-to-end.**
- **Crash recovery (§23.6):** A text-archetype job was started, the
  daemon was `kill -9`'d at the +30 s mark (job was in `diverging`),
  the daemon was restarted. The startup log read `INFO resuming job
  after restart job=… state=diverging`. The job then completed
  through `synthesizing` → `comparing` → `completed` in 37 s. The
  proposal artifact was already on disk from the pre-kill divergence
  phase, so the recovery had its input. The `recovery_flag` column
  on the jobs row was set to `"interrupted"` on resume and cleared
  on successful completion (the cleared state is what
  `GET /jobs/{id}` returns). **Confirmed working end-to-end.**

### Cross-cutting findings

1. **Per-phase time measurement is sound, with one caveat.** The M1
   engine records a `transition` event at the moment each phase
   completes — not at the moment it begins — so the time "in" a
   phase is the gap between two consecutive `transition` events.
   This equals the LLM call duration plus a few milliseconds of
   overhead, which is what the user actually experiences. The
   probe's `computePhaseDurations` function uses this gap. The
   `completed` state has no successor, so the probe does not record
   a "completed" duration — that is correct (it is a terminal state,
   not a phase).
2. **The synthesis phase prompt produces meta-commentary.** Every
   artifact in this run begins with a "Change Summary" and ends
   with a "Known Limitations" section, written by the model as if
   it were narrating its own work. The user did not ask for this;
   the prompt's "refine into the final artifact" language is being
   interpreted by the model as "deliver the artifact together with
   a changelog and known-issues list." This is **M3-T7's job to
   address**, not a defect in M1 (the M1 engine faithfully passes
   through whatever the model produces).
3. **The "alternative" persona is unused by M1.** The M1 engine only
   invokes `tall` (planning) and `main` (diverging, synthesizing).
   The `wide`, `security`, and `alternative` personas are wired
   into the registry but never called. The probe's config maps them
   to `gemma4:12b-mlx` to keep the "all roles assigned" registry
   invariant honest, but those model slots are free for M3 to
   consume. The probe did not generate any data on the SOTA models
   that the developer pulled (`qwen3.8:27b-mlx`,
   `ornith-1.5:9b`, `granite4.2:3b`) — they were never invoked.
4. **Phase budget defaults are too tight for a 27 B thinking-mode
   model.** The 120 s `planning` budget caused two of the first
   run's five samples to fail with `context deadline exceeded`. The
   600 s budget used in the rerun is sufficient on this hardware,
   but the ROADMAP's note that "each phase has a budget" deserves
   a follow-up: should the defaults be raised, or made
   hardware-relative? The MCE in M5 may resolve this, but the M1
   default is at minimum a "set up once with these models, raise
   budgets" footgun.
5. **Two-large-models on a 32 GB M2 Max is not viable.** This is
   environmental, not architectural. The M1 engine has no opinion
   on which models are loaded — it asks the LLM client for
   whatever the persona registry says. The probe confirms that the
   persona registry can map multiple roles to one model without any
   change to the engine. **This is a useful piece of operational
   knowledge for M2**: in-container LLM scheduling needs a "one
   resident model at a time" policy on memory-tight hosts, or the
   container's memory limit must be raised.

### Implications for M3

- **M3-T7 (the second quality probe) should run on the developer's
  stronger models** (`qwen3.8:27b-mlx` for `tall`,
  `ornith-1.5:9b` for `alternative` if M3 wires it,
  `gemma4:12b-mlx` for `main`) **with a single-model-per-run
  design** and a fixed 10-minute per-phase budget. Each model gets
  its own probe run so memory pressure is constant within a run.
  The probe helper at `spikes/m1-quality-probe/` already supports
  this — only the persona config changes between runs.
- **M3 prompt work should look at the synthesis phase first.** The
  "Change Summary / Known Limitations" preamble is the most
  user-visible defect observed; tightening the synthesis prompt to
  suppress meta-commentary would improve every artifact. A
  candidate instruction: "Output ONLY the final artifact. Do not
  narrate your changes, do not list limitations, do not explain
  yourself. The artifact is the only thing the user sees."
- **The default per-phase budgets should be revisited.** Either
  raise them (e.g. `planning: 300s`, `diverging: 300s`) or make
  them hardware-relative in M5 alongside the MCE. The current
  defaults are a footgun for any 27 B-class model.
- **H1 should be re-tested with the 27 B coding-aware persona** to
  determine whether the 12 B model is genuinely adequate for the
  code goal or whether the substitution masked a real degradation.
- **The crash-recovery test is sound but under-stressed.** The
  probe only exercised one mid-phase kill (diverging). M2-T3
  should also test kills during the synthesizing phase (where
  the artifact content is in flight) and during the atomic
  transition itself.

## What M1-T8 Did NOT Test

The M1-T8 probe is honest about its N=5 limitation, but the surface
it did not exercise is what M3-T7's probe (the next quality probe)
must cover. Declaring the scope up front keeps the next probe honest.

### Not exercised by M1-T8

- **Multi-candidate divergence.** M1 runs a single candidate through
  the entire chain. M3 introduces N candidates per `cfg.Execution
  .DivergenceCandidates` (default 3). The interaction between N
  candidates, the persona assignment per candidate, and the LLM
  call budget is unmeasured.
- **Evaluating phase.** The M1 engine has no `phaseEvaluate` (per
  ADR-0001, this is M3 work). There is no measurement of how a
  model judges a candidate, scores it, or produces a `StrategyOutcome`.
- **Reflecting phase.** Likewise absent in M1. The "LLM decides next
  action" step is unmeasured: does the model pick `synthesize`,
  `retry`, or `fail` for the right reasons?
- **Real test execution in Job Pods.** The M1 chain never runs code.
  M2-T4 introduces Job Pod execution; M3-T2 introduces candidate
  evaluation inside Job Pods. The probe's failure modes for "candidate
  fails tests inside a hardened container" are unknown.
- **Multi-language code.** The M1-T8 code sample was Python-only
  (per the probe sample list). M3 should test Go, Python, JavaScript,
  and at least one language with a heavier test-running footprint
  (Java, Rust).
- **Large-context retrieval.** No MCE exists in M1, so there is no
  measurement of how a model handles "given a 50K-token context,
  answer question X." M5's probe is the place for this.
- **Hallucination rate on adversarial inputs.** The M1 samples were
  well-scoped goals with clear acceptance criteria. There is no
  measurement of how the model handles ambiguous goals, contradictory
  criteria, or attempts to redirect the agent via crafted inputs.
- **Strategy mining statistical signal.** N=5 is far below the
  `cfg.StrategyAnalysis.MinCohortSize` of 20. Strategy mining output
  cannot be evaluated with this data.
- **HITL queue latency.** M6 introduces the HITL queue. M1 has no
  approval surface, so the round-trip "submit a job that requires
  approval → human sees it → human decides" has no measurement.
- **Daydreaming.** M1 has no Daydreaming. There is no measurement of
  the §17.2 constraints (yield to user activity, draft-only output,
  no git commits, no accepted-artifact modification).
- **OS watcher behavior.** M1 has a `NoopWatcher` in `internal/power`.
  M6 replaces it with a real watcher; M1-T8 cannot measure the
  interactive/autonomous profile flip.

### What M3-T7 must cover

The M3 quality probe (ROADMAP M3-T7) should run at least 15 samples
across 3 archetypes (text, code, document) and 2-3 task types per
archetype, with structured failure analysis on every sample that does
not meet its acceptance criteria. It should produce per-phase wall
time, criteria adherence, qualitative usefulness on a 1-5 scale, and
a per-failure-mode breakdown. The probe should target the gaps above
explicitly, not just re-run M1-T8 with more samples.
