# Developing Athanor

Short guide for working on this repository. The authoritative design lives in [`ARCHITECTURE.md`](ARCHITECTURE.md); the build order in [`ROADMAP.md`](ROADMAP.md).

## Build & test

**CGO is required.** `internal/store` uses `mattn/go-sqlite3` so sqlite-vec can be loaded at runtime later (task-000 findings: `docs/sqlite-setup.md`, decision: [ADR 0003](docs/adr/0003-sqlite-driver-cgo-single-connection.md)).

```bash
make build        # CGO_ENABLED=1 go build ./...
make test         # go test ./...
make test-race    # go test -race ./...
make vet          # go vet ./...
make run          # run the daemon locally
```

Don't run bare `go build` / `go test` — they silently drop CGO and fail on the sqlite3 driver. The Makefile sets the flag for you, and CI enforces the same targets.

When FTS5 support is needed, add the build tag: `go build -tags sqlite_fts5 .`

## Running the daemon

```bash
make run          # equivalent to: go run ./cmd/athanor
```

Flags: `-config config.yaml` · `-addr 127.0.0.1:7420` · `-state-dir state`. The HTTP surface binds **loopback only** (ARCHITECTURE §21.8); non-loopback addresses are rejected at startup. Check `http://127.0.0.1:7420/healthz` for `{status, version, uptime}`. Boot order is fixed: config → logging → store → migrations → serve; SIGINT/SIGTERM triggers graceful shutdown, and every start/stop is recorded in the append-only event log.

## Repository layout

| Path | Role |
|---|---|
| `cmd/athanor/` | daemon entry point |
| `internal/` | real implementation packages (config, store, logging, power, …) |
| `migrations/` | embedded forward-only SQL migrations (`NNNN_description.sql`) |
| `spikes/` | **throwaway** validation code. Prove or kill an idea here, record findings under `docs/`, then implement properly in `internal/`. Spike code never gets imported by `internal/`. |
| `docs/adr/` | Architecture Decision Records |

## Workflow conventions

- **Commits:** one per roadmap task, titled `M#-T#: <title>`; docs/infra changes use plain prefixes (`docs:`, `ci:`, `chore:`).
- **ADRs:** any decision future-you would ask "why did they do *that*?" about gets a short ADR in `docs/adr/NNNN-title.md` (context → decision → consequences).
- **Roadmap:** update the status table as milestones progress — it is the project's honest heartbeat.
- **Spikes:** timeboxed, with an explicit hypothesis. Findings land in `docs/`; the spike itself stays disposable.

## License

AGPL-3.0 — see [LICENSE](LICENSE).
