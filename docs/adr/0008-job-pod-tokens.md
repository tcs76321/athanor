# ADR 0008 — Per-job tokens + internal API for M2-T3

**Status:** Accepted · **Date:** 2026-08-27 · **Refs:** ARCHITECTURE §21.2; ROADMAP M2-T3; ADR-0007

## Context

M2-T2 shipped the Job Pod manager with `Spec.Token` and `Spec.TokenDir`
fields and a `buildArgs` step that constructs the `--mount` argv to
bind-mount a host directory at `/run/athanor` inside the pod. ADR-0007
documented the spike's protocol but not the M2-T3 implementation. This
ADR records the M2-T3 design decisions: how the token is generated and
stored, how the internal API surface is shaped, what the auth
middleware guarantees, and what Gate G2 structurally proves.

The M2-T3 implementation lives in three new files:

- `internal/jobpod/token.go` — token generation and host dir
  lifecycle.
- `internal/internalapi/` — bearer-token middleware plus three
  handler routes (GET context, POST heartbeat, POST log).
- `cmd/athanor/token_store_adapter.go` — a one-call-site bridge
  between `jobpod.Manager` and `internalapi.TokenStore`.

Plus Gate G2 (`internal/gate/gate_g2_test.go`) — the structural
proof that the internal API is correctly contained.

## Decision

### D1. Token format and storage

16 random bytes from `crypto/rand`, hex-encoded to 32 chars. The
token's truth is the file `<state-dir>/tokens/<job-id>/token` (mode
0600). The token is never persisted to SQLite, the EventLog, or
any artifact. The daemon's process-local `map[podID]token` is a
correctness check on `Stop`, not the source of truth.

`crypto/rand` failing is not a runtime condition on supported
platforms; the constructor panics loudly rather than handing out
a known-weak token.

### D2. Internal API path prefix

`/internal/v1/`. The prefix is the architectural seam: every
request inside the prefix requires the bearer token; every request
outside it does not. M1 routes at `/projects/...`, `/jobs/...`,
`/freeze`, `/healthz` are unchanged.

A future `/internal/v2/` is possible without a migration. The
version is in the path, not a header, so curl-on-the-pod debugging
stays human-readable.

### D3. Token transport

`Authorization: Bearer <token>` over loopback HTTP. The server is
loopback-only (Gate G1 + `server.LocalhostAddr`); no TLS. The token
is also already a one-time secret (per-job, ephemeral) — not a
long-lived API key. The 32-char hex format matches `crypto/rand`'s
output directly; no base64 step.

### D4. Token validation: constant-time compare, per-job binding

`==` on the token is a Gate G2 violation. The structural test
searches `middleware.go` for the literal `ConstantTimeCompare`
and fails the build if absent.

### D5. Internal API routes (M2-T3 scope)

Three routes, intentionally minimal — M2-T3 proves the auth
mechanism and the basic shape of an authenticated round-trip.
`execute_code` and `run_tests` are M2-T4.

| Method | Path | Body | Response | Purpose |
|---|---|---|---|---|
| `GET`  | `/internal/v1/jobs/{id}` | — | task context | "what am I supposed to do?" |
| `POST` | `/internal/v1/jobs/{id}/heartbeat` | `{}` | `{ok: true}` | "I'm alive" — M2-T5 uses for stuck-pod detection |
| `POST` | `/internal/v1/jobs/{id}/log` | `{stream, line}` | `{ok: true}` | stream a log line to the EventLog |

### D6. Gate G2

A new test file `internal/gate/gate_g2_test.go` with three checks:

1. `internal/internalapi/` does not import `internal/llm` (text
   search). The internal API serves the pod, not the model; if a
   future change imports the LLM client, the pod has an indirect
   path to model calls that bypasses every other containment
   check.
2. `internal/internalapi/middleware.go` references
   `ConstantTimeCompare` by name (text search). The structural
   defense against timing attacks; bypassing it with `==` is a
   Gate G2 violation.
3. `handlers.go` references `authMiddleware` for every
   `mux.Handle` call (counted match). The package convention is
   "all routes register here" — a single source of truth, easy
   to audit. A future refactor that splits handlers across files
   would have to update the test deliberately.

Gate G2 has been verified to catch violations: replacing
`subtle.ConstantTimeCompare` with a non-constant-time
alternative fails `TestGateG2ConstantTimeComparePresent`;
replacing `authMiddleware(a.tokens, http.HandlerFunc(...))` with
`http.HandlerFunc(...)` fails
`TestGateG2InternalAPIRoutesGoThroughMiddleware`.

### D7. Manager interface extension

Added `Manager.TokenFor(jobID) (string, error)` to the public
interface. This is the only call site that needs the raw token;
the internal API auth middleware is the only caller. The
returned string is the secret itself — the middleware must not
log it. Errors are mapped in `cmd/athanor/token_store_adapter.go`
(`jobpod.ErrNotFound` → `internalapi.ErrTokenNotFound`) so the two
packages stay decoupled.

## Consequences

- The internal API is the only path a Job Pod can use to do
  anything. Loopback-only + bearer-token + per-job binding
  bounds the attack surface to "the pod's own auth secret, on
  the daemon's own network namespace."
- Gate G1 still passes. Gate G2 is the M2-T3-specific backstop.
  Both run in `make test-race` and fail the build on regression.
- `Spec.Token` and `Spec.TokenDir` (M2-T2 fields) are now
  populated by the manager itself when `tokenBase` is set; the
  M2-T4 engine will pass them through from a separate
  `TokenIssuer` if/when one is needed.
- The smoke test in commit 5 confirms the daemon rejects every
  malformed-auth scenario with 401 and accepts well-formed
  requests with the right token. (Acceptance is verified
  behaviorally in `internal/internalapi/handlers_test.go`.)

## Not in M2-T3

- `execute_code` and `run_tests` routes — M2-T4.
- The full `Kill -9 leaves no orphan token dirs` test — M2-T5.
- A Linux/macOS parity test for the internal API — M2-T6.
- A M3-T1 multi-candidate divergence test that exercises the
  `GET /internal/v1/jobs/{id}` round-trip end-to-end via the
  engine.

The middleware:

1. Extracts `{id}` from the path (Go 1.22+ `r.PathValue`).
2. Parses `Authorization: Bearer <token>`. Rejects missing /
   wrong-scheme / malformed with 401.
3. Looks up `tokens.Get(id)`. Rejects with 401 (uniform — does
   not leak whether the job ID exists; the pod already knows
   its own ID).
4. Constant-time length-compare (so the fail path doesn't leak
   the expected length via timing).
5. `crypto/subtle.ConstantTimeCompare`. Reject with 401.
6. Attach the authenticated job ID to the request context and
   call the handler.

`==` on the token is a Gate G2 violation. The structural test
searches `middleware.go` for the literal `ConstantTimeCompare`
and fails the build if absent.
