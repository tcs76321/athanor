# ADR 0016 — Third-party skills are out of scope by project policy

**Status:** Accepted · **Date:** 2026-09-04 · **Refs:** ARCHITECTURE §26 (Skills and Extensibility); ROADMAP M4-T3

## Context

ARCHITECTURE.md §26 describes a *Skill* object: a versioned, packageable, Python-first extensibility surface that mounts into Job Pods read-only and declares its permissions. Skills can ship their own `prompt_template`, `dependencies`, and a manifest with `permissions` (network, filesystem, subprocess).

The skill system is structurally sound: the scanner runs against skill manifests before mount, the tool envelope is enforced at the internal API, and skills never see credentials directly (§26.2). All of that is real defense in depth.

The question this ADR answers is whether the *recommended* extension path is third-party skills or direct contribution to Athanor.

The reason to ask: the project is deliberately complete in a way that makes third-party extensions structurally redundant. The M3 dialectical loop, the M2 job pods, the M4 airlock, and the M5 context engine are designed to be a coherent whole. Each milestone closes a gate; each gate makes the next capability *safe* by construction. A third-party skill, by contrast, is opt-in by an operator who has not earned the gate that bounds the capability it provides.

Concretely:

- A skill that does prompt assembly is a partial re-implementation of the §13 engine; it will be subtly different, and the difference will be invisible until it isn't.
- A skill that does scanner integration is a partial re-implementation of the §21 airlock; if the scanner diverges from the in-tree path, the audit trail splits.
- A skill that does file I/O routes through the same internal API, but with its own prompt context, and the LLM cannot distinguish "instructions I should obey" from "data the skill pasted into my context." The same defense-in-depth principle that applies to inbox files (heuristic + size + zipbomb + clamav + yara) does not retroactively apply to skill-rendered prompts.

The project is also small and fast-moving. A user who needs a new capability can submit a PR, get it reviewed, and have it land in days. The cost of the skill system (mount lifecycle, manifest scanning, permission UI, the Supply-Chain-Authorship question of "who audits the skill") is real and ongoing. The benefit (letting users extend without contributing) is small for a project of this size.

## Decision

Third-party skills are **explicitly out of scope** as the recommended extension path. The recommended path is direct contribution to the Athanor repository.

What this means in practice:

1. **The skill loading code path remains in the codebase** (it is part of the §26 architecture and removing it would be a larger decision). It is an *escape hatch* for power users who explicitly accept the supply-chain risk and the absence of scanner-specific defenses for skill-rendered prompt content.
2. **The README and `athanor init` output gain a one-line warning** advising operators to extend Athanor directly rather than install third-party skills. The warning is in user-facing surfaces only; the code path itself is unchanged.
3. **M4-T3 does not build skill-manifest-specific scanning.** The scanner registry's `prompt-injection-heuristic` and `yara` cover whatever skill manifests do cross a trust boundary generically; a dedicated `skill-manifest` scanner is not built. If a future contributor re-enables the skill path, the existing scanners already cover it.
4. **No `airlock.scanners.skill` pipeline kind exists.** The `PipelineKind` enum is closed at three: `ingress`, `egress`, `user-prompt`. Adding a fourth requires editing both this ADR and the registry's CHECK-constrained insert path.
5. **The Skill object chapter of ARCHITECTURE.md is preserved verbatim** for documentation completeness but is not advertised in the README, the daemon CLI's help output, or the welcome text.

## Consequences

- **One less trust boundary to defend in M4.** The skill manifest vector is removed from the scanner table in ADR-0015. The four-crossing table there is final; the skill row is replaced by "(out of scope M4 per ADR-0016)."
- **Operator extension path is contribution, not installation.** Operators who want new capabilities submit a PR. The review process is the audit trail; the project's CI is the regression net.
- **The skill code path is dormant but compilable.** Gate G1's allowlist, the `internalapi` routes, and the tool envelope do not change. A future decision to re-enable the path is one PR (and probably another ADR).
- **A future M5/M6 might revisit.** If the project grows large enough that the contribution-only model breaks down, a different extension story (plugin manifests with cryptographic signing, a private skill registry, etc.) is a coherent future direction. That is a different ADR.

## Not in this ADR

- The §26 Skill object's *specification* is unchanged. The schema, the mount lifecycle, the manifest format — all stay as documented. This ADR is about *policy*, not *schema*.
- The existing skill code paths in the codebase are not deleted. They remain compilable, testable, and exercisable by anyone who deliberately opts in.

## Forward references

- The README refresh in the M4 close-out commit (after T2/T3/T4) adds the third-party-skills warning paragraph.
- If the project revisits the skill path in a future milestone, this ADR is the starting point for the discussion.
