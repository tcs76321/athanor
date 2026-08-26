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

Daemon flags: `-config config.yaml` · `-addr 127.0.0.1:7420` · `-state-dir state`. The HTTP surface binds **loopback only** (ARCHITECTURE §21.8); non-loopback addresses are rejected at startup. Check `http://127.0.0.1:7420/healthz` for `{status, version, uptime}`. Boot order is fixed: config → logging → store → migrations → kill switch → engine → serve → recover active jobs; SIGINT/SIGTERM triggers graceful shutdown, and every start/stop is recorded in the append-only event log.

**No config file yet?** The daemon boots on built-in defaults and logs a notice — only *absence* falls back; a present-but-invalid `config.yaml` still fails loudly with a specific error. `config.example.yaml` in the repo root documents every option with its default value (a test enforces that the example always matches the built-in defaults, so copying it is a no-op starting point), and `athanor init` writes a marshaled default config for you.

## CLI (M1)

Client commands talk to a running daemon (default `http://127.0.0.1:7420`, override with `-addr`):

```bash
athanor project create -name demo -archetype text -goal "..." [-criteria "a;b"]
athanor goal submit -project <id> -goal "..."
athanor job watch -job <id>            # prints each phase transition, then the artifact
athanor artifacts -project <id>
athanor freeze                         # §22 kill switch; frozen state survives restarts
athanor unfreeze -reason "..."         # requires a reason; recorded in the event log
```

The full M1 walkthrough is [`docs/demo-m1.md`](docs/demo-m1.md) — it doubles as the Gate G1 demo script.

## Repository layout

| Path | Role |
|---|---|
| `cmd/athanor/` | daemon entry point + CLI client commands |
| `internal/` | real implementation packages (config, logging, store, power, llm, prompt, job, artifact, project, engine, control, api, server, gate, …) |
| `migrations/` | embedded forward-only SQL migrations (`NNNN_description.sql`) |
| `spikes/` | **throwaway** validation code. Prove or kill an idea here, record findings under `docs/`, then implement properly in `internal/`. Spike code never gets imported by `internal/`. |
| `docs/adr/` | Architecture Decision Records |
| `docs/demo-m1.md` | Gate G1 demo script (executable walkthrough) |
| `docs/probes/` | quality-probe protocols and findings |

## Workflow conventions

- **Commits:** one per roadmap task, titled `M#-T#: <title>`; docs/infra changes use plain prefixes (`docs:`, `ci:`, `chore:`).
- **ADRs:** any decision future-you would ask "why did they do *that*?" about gets a short ADR in `docs/adr/NNNN-title.md` (context → decision → consequences).
- **Roadmap:** update the status table as milestones progress — it is the project's honest heartbeat.
- **Spikes:** timeboxed, with an explicit hypothesis. Findings land in `docs/`; the spike itself stays disposable.

## License

AGPL-3.0 — see [LICENSE](LICENSE).
