# M2-T6 — Security test suite (implementation plan)

**Status:** Proposed · **Milestone:** M2 (Container Spine) · **Refs:** ROADMAP M2-T6; ARCHITECTURE §21, §31.2; ADR-0007, ADR-0008, ADR-0009; ADR-0010

## Goal

Close Gate G2 by proving — not merely asserting — that a Job Pod
cannot reach anything it is not supposed to. The structural half of
Gate G2 lives in `internal/gate/gate_g2_test.go` (text-search tests
over the source tree); the behavioral half is M2-T6. Together they
make the §21 containment boundary durable across refactors.

## Acceptance criteria (from ROADMAP M2-T6)

> pod→internet denied, pod→Ollama denied, pod→podman.sock denied,
> pod→host FS denied, credential absence verified. All attempts fail
> from inside the pod; failures logged as security events.

## What gets built

Three deliverables, sequenced as three commits after the planning
commit (this document + ADR-0010):

| # | Commit | Surface | Runs in CI? | Purpose |
|---|---|---|---|---|
| 1 | `chore: structural argv regression test for M2-T6` | `internal/gate/gate_g2_test.go` (new test) | **yes** | CI-enforced backstop: argv from `buildArgs` never contains a flag or mount that would let a pod escape |
| 2 | `M2-T6: behavioral security probes (pod → internet, pod → Ollama, pod → podman.sock, pod → host FS, credential absence)` | `internal/jobpod/security_test.go` (new file, `//go:build integration`) | **no** (opt-in via `ATHANOR_RUN_INTEGRATION=1`) | Five behavioral probes that bring up a real pod, run a probe script, and assert denial |
| 3 | `docs: M2-T6 evidence — Gate G2 green, security suite script` | `docs/demo-m2.md`, `CHANGELOG.md`, `ROADMAP.md` | n/a | Operator-facing demo script + status updates |

## The five behavioral probes

Each probe is a single `func TestSecurity_*` in
`internal/jobpod/security_test.go` (build tag `integration`). Each
brings up a real `alpine:3.20` pod via `podman run --rm` with the
same argv the production manager produces, runs a probe script, and
tears the pod down. `t.Cleanup` removes the container even on
assertion failure.

| # | Probe | Inside-pod command | Pass criterion |
|---|---|---|---|
| 1 | internet | `wget -q -T 2 http://1.1.1.1/ -O - \|\| echo BLOCKED` | exit non-zero and stdout contains `BLOCKED` |
| 2 | Ollama | `wget -q -T 2 http://host.containers.internal:11434/api/tags -O - \|\| echo BLOCKED` (macOS) **and** `wget -q -T 2 http://10.0.2.2:11434/api/tags -O - \|\| echo BLOCKED` (Linux/podman-machine default gateway) | both target probes fail |
| 3 | podman.sock | `ls -la /var/run/podman/podman.sock 2>&1 \|\| true` | output reports "No such file" or equivalent |
| 4 | host FS | `cat /etc/shadow 2>&1; ls /root 2>&1; ls /home 2>&1; touch /usr/share/probe 2>&1` | every line reports "Permission denied" / "Read-only" / "No such" |
| 5 | credentials | `env` filtered through `grep -Ei 'token\|key\|secret\|ollama\|openai\|anthropic'` | empty output |

Each probe appends a single `internal/store` event (category
`podman`, see ADR-0010 D5) capturing the probe name, the
inside-pod script, the observed stdout/stderr, and a pass/fail flag.
A test that fails the assertion still produces the event, so the
audit log records the breach attempt even when the test goes red.
That is the "failures logged as security events" half of the
acceptance criterion.

## The structural argv test

A new test in `internal/gate/gate_g2_test.go` walks every cell of
the `buildArgs` argv produced for a representative Spec and asserts
the joined-argv string does **not** contain any of:

- `--network=host`, `--net=host`, `--network=slirp4netns`,
  `--net=slirp4netns` (only `none` is acceptable — the existing
  argv uses `--network none`, two-token form)
- `--privileged`
- `--security-opt apparmor=unconfined`
- Bind mounts whose source is under `/etc`, `/root`, `/home`,
  `~/.ollama`, or the host's `state/` directory
- The literal string `podman.sock` anywhere in the argv

The test runs in CI (no podman needed). It is the durable backstop
for the integration suite, which CI cannot run.

## Out of scope (deferred)

- Adversarial probing of the host kernel from inside a rootless
  container (no privilege to attempt, and the assumption is that
  the kernel is patched — operator's responsibility per §21.1).
- The M2 Gate G2 line that requires "a pytest suite runs inside a
  pod and returns results." That capability is owned by M3-T2
  (M2-T4 already proves the route works; M3-T2 will exercise it
  with real tests). Tracked at the M2-boundary plan review.
- Linux seccomp enforcement. ADR-0007 documents that rootless
  macOS/podman-machine does not have a kernel seccomp; the
  integration suite runs on whatever the developer has, and the
  seccomp-related argv assertions in the structural test cover
  Linux.

## Risks surfaced during planning

- **Podman availability in CI** is not solved and will not be in
  this task. The structural test is what protects CI; the
  integration suite is opt-in by `ATHANOR_RUN_INTEGRATION=1` per
  the existing Makefile convention. This is consistent with how
  M2-T2 and M2-T5 shipped their integration tests.
- **macOS vs Linux Ollama target.** `host.containers.internal` is
  the documented macOS/podman-machine name; `10.0.2.2` is the
  slirp4netns default gateway on Linux. Probe 2 runs both and
  asserts both fail. The probe is a `t.Skip` if the host has no
  podman at all.
- **The "security" category does not exist** in the §28.1 closed
  set. The T6 events will use category `podman` (the pod-surface
  category) to avoid widening the closed set. ADR-0010 D5 records
  the reasoning.
