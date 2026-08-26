# M1 Quality Probe — Protocol and Findings (ROADMAP M1-T8)

**Status: NOT YET RUN** — this document is the prepared protocol. The
probe requires a live Ollama with the default persona models
(`mistral-nemo:12b`, `qwen2.5-coder:32b`, `phi3:3.8b`, `qwen2.5:7b`,
`llama3.1:8b`); none was available in the development environment when
M1 completed. Per the ROADMAP's spike rules, findings will be recorded
here in the hypothesis → observation → implication format — never
invented.

## Hypotheses

1. **H1 (planning quality):** Local models produce *useful* plans for
   small, well-scoped text/document goals, but degrade on code goals
   where the `tall` persona (qwen2.5-coder:32b) must reason about
   structure it cannot see (no repository context exists in M1).
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
  timestamps),
- criteria adherence (manual check of the final draft artifact),
- qualitative usefulness on a 1–5 scale with a one-line justification.

## Sample goals

| # | Archetype | Goal | Criteria |
|---|---|---|---|
| 1 | text | "Write a short essay about why local-first software matters." | at least three arguments; a conclusion |
| 2 | text | "Draft a friendly onboarding email for a new community member of a local software club." | under 200 words; one clear call to action |
| 3 | code | "Write a Python module that manages a personal book collection with add, list, and search functions." | pure stdlib; docstrings on every public function; a usage example |
| 4 | document | "Create a README for a small CLI tool that converts Markdown files to HTML." | installation section; usage examples; license section |
| 5 | document | "Write a one-page design brief for a weekend project that builds a sunrise alarm clock from a Raspberry Pi." | parts list; build steps; at least two risks named |

## Findings

_To be filled in when the probe runs. Implications feed directly into
the M3 prompt work (ROADMAP M3-T7 refines this with a second probe)._
