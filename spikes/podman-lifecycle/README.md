# M2-T1 spike — rootless Podman lifecycle

M2-T1 is a 4-hour timeboxed spike to prove that rootless Podman can
host a hardened, network-isolated Job Pod and that the §21.2
containment flags work on a developer machine. Findings land in
[docs/adr/0007-podman-lifecycle.md](../../docs/adr/0007-podman-lifecycle.md).

## Usage

```bash
go run ./spikes/podman-lifecycle
```

The spike pulls `alpine:3.20`, brings up a rootless container with
the §21.2 hardening flags, runs seven containment checks, prints a
per-check table, and tears the container down. Exits non-zero if any
check fails.

## Recorded results (macOS 14 / podman-machine 5.8.2 / applehv)

| # | Check | Result |
|---|---|---|
| 1 | read-only rootfs | PASS |
| 2 | tmpfs at /tmp writable | PASS |
| 3 | token visible at /run/athanor/token | PASS |
| 4 | no network: cannot reach external host | PASS |
| 5 | /etc/hosts is the container's own | PASS |
| 6 | no podman socket exposed | PASS |
| 7 | cgroup pids.max reflects --pids-limit=64 | PASS |

Linux behavior was not exercised. M2-T6 (security suite) is the
place to close that gap; see the ADR's Caveats section.

## Test

This spike is intentionally throwaway. It is not unit-tested. The
spike's value is the live run, captured in the ADR. Future
regressions of the same shape belong in `internal/` as M2-T2
tests, not here.
