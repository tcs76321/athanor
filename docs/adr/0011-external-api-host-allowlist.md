# ADR 0011 — External API: Host-header allowlist to close the DNS-rebinding class

**Status:** Accepted · **Date:** 2026-09-01 · **Refs:** ARCHITECTURE §21.8; ROADMAP M3-T2 carry-over (GLM review of 2026-09-01)

## Context

ARCHITECTURE §21.8 (verified at `ARCHITECTURE.md:1166`):

> "The UI binds to localhost only. For headless/remote access, use SSH port forwarding rather than exposing a network listener. The Core internal API (used by Job Pods) authenticates via per-job ephemeral tokens as described in §3.2."

The implementation matches the spec on the *listen* side. `internal/server/server.go:129–141` (`LocalhostAddr`) accepts only `127.0.0.1`, `::1`, `localhost`, or the wildcard forms `0.0.0.0`/`::` (which it rewrites to `127.0.0.1`); any other host fails closed with a specific error. The test surface `internal/server/server_test.go:80–92` exercises the rejection cases. Gate G1 is satisfied.

What the implementation does **not** do: validate the `Host` header on incoming requests. The mux returned by `server.Handler()` (`server.go:57–59`) is the bare `*http.ServeMux`; no middleware is wrapped around it. The internal API at `/internal/v1/` is protected separately by the per-job bearer-token middleware (ADR-0008) and is *not* what this ADR is about — its threat model is "a Job Pod on the same network namespace with a stolen token," and `ConstantTimeCompare` plus per-job binding is the right defense for that.

The external API surface that this ADR addresses is:

- `GET  /healthz`
- `GET  /freeze`, `POST /freeze`, `DELETE /freeze` (the §22 kill switch)
- `POST /projects`, `GET /projects/{id}`
- `POST /projects/{id}/goals`
- `GET  /projects/{id}/artifacts`
- `GET  /jobs/{id}`, `GET /jobs/{id}/events`

None of these has authentication. They rely entirely on the listen address being loopback. Per ARCHITECTURE §1 ("Mountains and rivers — security constraints never change"), the loopback-only invariant is invariant-class: cheap to harden, expensive to retro-fit.

The gap: a web page loaded in a browser on the same machine (a future M6-T8a dashboard, or any third-party web content the user opens) could issue requests to `http://127.0.0.1:7420/projects` (or, more dangerously, to `http://localhost:7420/freeze` with `POST`). A DNS-rebinding attack resolves an attacker-controlled hostname to `127.0.0.1` and then makes the same-origin browser policy issue the request with the user's cookies/permissions. Ollama shipped with this class of bug in 2024 and fixed it with a `Host`-header allowlist.

The risk is theoretical today: there is no browser, no UI, and no third-party web content on the same machine. But the gap is invariant-class — the moment any UI lands (M6-T8a is the first browser target), the gap is exploitable. Closing it now is a few lines of middleware; closing it after a UI exists is a security incident.

## Decision

Add a `Host`-header allowlist middleware to `internal/server` that wraps every external-API route. The internal API at `/internal/v1/` is **not** affected (it has its own auth; mixing the two would be a Gate G2 complication).

### D1. The allowlist

The allowlist is a small set of well-known loopback hostnames that the daemon, the CLI, and reasonable third-party callers can legitimately use:

| Host              | Source                              |
|-------------------|-------------------------------------|
| `127.0.0.1:7420`  | canonical (`cmd/athanor/cli.go:18`) |
| `localhost:7420`  | humans and the CLI both resolve it  |
| `athanor.local`   | hostname for `~/.ssh/config` users   |
| `[::1]:7420`      | IPv6 loopback (matches `LocalhostAddr`'s accept set) |

Port is part of the comparison because the same `Host` header may legitimately appear on a different port (a future reverse-proxy with a different listener); matching only the host is the right scope.

`r.Host` from Go's `net/http` already strips the port for `Host` and leaves it on `Host`-with-`:port` form via the `Request.Host` field. We compare against the literal strings in the allowlist. The default port in `config.example.yaml` is `7420`; if the operator changes it, the allowlist changes with it (a small slice derived from `cfg.Server.Addr`).

### D2. Behavior

- `Host` header missing → 400 Bad Request. The HTTP/1.1 spec requires it for HTTP/1.1 requests; missing is a protocol violation, not a normal case.
- `Host` header not in the allowlist → 421 Misdirected Request (the same status Ollama settled on). The response body explains the allowlist; the audit event `host_header_rejected` records the offending value.
- `Host` header matches → request proceeds unchanged.

The middleware logs a `host_header_rejected` event row to the EventLog under the `network` category. The body of the request is **not** logged (a future browser might POST JSON with session cookies; the EventLog is append-only and not access-controlled).

### D3. Where it lives

`internal/server/middleware.go` (new file). The middleware is composed in `server.Handler()` (`server.go:57`) before the mux. The internal API is registered on the **same** mux (`server.Mux()`) but its `authMiddleware` runs after the `Host` check — the `Host` check is a *request filter*, not a route wrapper, so the two layers compose cleanly.

The CLI side is unchanged. Go's `http.Client` does not set a `Host` header by default; the CLI's `apiCall` (`cmd/athanor/cli.go:22`) calls `http.NewRequest(method, url, body)`, which derives the host from the URL (`127.0.0.1:7420` or whatever `-addr` is given). The literal URL is what the allowlist already permits.

### D4. The 5th `network` event category

The EventLog already has a `network` category (`internal/store/eventlog.go` and the §28.1 closed set). `host_header_rejected` is a new event in that category. No schema migration is required — the `data_json` payload is a free-form map. The category is already enabled by default in `config.example.yaml`.

### D5. Tests

Three test layers, all in `internal/server/`:

1. `TestHostHeaderMiddleware_AcceptsLoopback` — table-driven positive cases (`127.0.0.1:7420`, `localhost:7420`, `[::1]:7420`, `athanor.local:7420`).
2. `TestHostHeaderMiddleware_RejectsNonAllowlist` — table-driven negative cases (`evil.example.com:7420`, `127.0.0.1:9999`, attacker-controlled DNS-resolved hosts). Asserts 421 and the audit event shape.
3. `TestHostHeaderMiddleware_MissingHost` — sends a request with the `Host` header stripped; asserts 400.

The internal API tests (`internal/internalapi/*_test.go`) are unchanged because their `httptest.NewRequest` calls supply a synthetic `Host: example.com`, which now fails the allowlist. The fix is to update those tests to use `httptest.NewRequest` with the loopback `Host`. A targeted test run after the change confirms no behavioral drift in the internal API.

### D6. Configuration

A new block in `config.example.yaml` (under `network:`):

```yaml
network:
  # External API Host-header allowlist (§21.8 hardening, ADR-0011).
  # Requests whose Host header is not in this set are rejected with 421
  # Misdirected Request. The default is the canonical loopback set
  # and is what the in-tree CLI uses. Operators behind a reverse
  # proxy may add their proxy's Host. The internal API at
  # /internal/v1/ is unaffected (it has its own bearer-token auth).
  external_api_host_allowlist:
    - "127.0.0.1:7420"
    - "localhost:7420"
    - "[::1]:7420"
    - "athanor.local:7420"
```

The `config.Config` struct gains a `Network.ExternalAPIHostAllowlist []string` field. `validateRaw` rejects empty entries; an empty list is allowed (and disables the check — a documented escape hatch for test environments).

## Consequences

- The DNS-rebinding class of attacks is closed with a few lines of middleware plus a closed-set allowlist. The invariant ("external API is reachable only from a loopback caller") is now expressed in *both* the listen address and the request `Host` header.
- The internal API is unchanged. ADR-0008's bearer-token design is unaffected.
- The M6-T8a web UI is safe-by-default: a future browser running the dashboard on `athanor.local:7420` works; a malicious web page on the same machine trying `POST http://127.0.0.1:7420/freeze` is rejected at the middleware layer before any handler runs.
- The CLI is unchanged. The CI tests that use `httptest.NewRequest` without an explicit `Host` header need a one-line update.
- A future operator who wants to expose the daemon to a non-loopback caller (e.g., a LAN-only deployment) must add their proxy's `Host` to the allowlist. SSH port forwarding (the §21.8 recommended path) continues to work because the `Host` header from a forwarded connection is the loopback one.
- `config.example.yaml` gains the allowlist block. `config.example.yaml` is test-enforced to match `defaults.go`; a parallel update in `internal/config/defaults.go` is required.

## Not in M3

- A full `Host`-header test corpus (real DNS-rebinding PoC, browser scenarios). That's M7-T5 work (the doctor + headless packaging milestone), where browser exposure becomes a real concern.
- A non-loopback HTTP listener (e.g., LAN access via mTLS). Out of scope per §21.8. A future operator who needs it can add a `Host` to the allowlist; the daemon will still bind loopback and require an external tunnel.
- An automatic "trust X-Forwarded-Host from a known proxy" path. Trusting proxy headers is a well-known footgun; the right answer is "the operator configures their proxy's hostname in the allowlist," not "we'll trust whatever the proxy says."

## Forward references

- M3-T2 (Evaluation phase) lands this ADR's implementation in the same commit as the §19 evaluation rubric, because the `Host`-header middleware is independent of the engine and is a small, isolated change.
- M7-T5 (doctor + headless packaging) extends the test corpus with real DNS-rebinding scenarios and may add a "first-boot" hint that prints the allowlist alongside the loopback listen address.
