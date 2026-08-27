# AGENTS.md

Working agreements for AI coding agents and human contributors. Short on
purpose; details live in `ARCHITECTURE.md`, `ROADMAP.md`, and `DEVELOPMENT.md`.

## Commands

- One tool call per command. No multi-command strings, no compound shell
  expressions, no nested quoting. If two things need checking, make two calls.
- Never run bare `go build` or `go test` — use the `make` targets so
  `CGO_ENABLED=1` is set. See `DEVELOPMENT.md`.
- Keep command output short. Pipe through `head`, `tail`, or `grep` when
  inspecting long output; the full result is rarely needed.

## Git and pagers

- `git log`, `git diff`, `git show`, and similar open a pager that waits
  for `q` and hangs the terminal. Always use `git --no-pager <command>`,
  or set `GIT_PAGER=cat` in the environment, for every history or diff
  inspection.
- Use `git --no-pager log --oneline -n` for compact history.

## Commits

- One commit per logical change. Match the project's existing style:
  `M#-T#: <title>` for roadmap tasks, `chore:`, `docs:`, `ci:`,
  `fix:` for everything else. Lowercase, terse, no trailing period.
- Commit bodies are short — one or two lines, only if they add
  information not in the title. No multi-paragraph essays.
- Update `ROADMAP.md` status table when a milestone-level task lands.

## Work incrementally

- Run `make vet` and `make test-race` after every commit, not at the end.
  If something breaks, find the breaking commit, don't bisect.
- Re-prove Gate G1 (`CGO_ENABLED=1 go test ./internal/gate/`) after any
  change to `internal/` that touches imports or the engine surface.

## Plan mode

- In plan mode: only read, search, and inspect. Do not edit files, run
  state-changing commands, or create directories.
- When reality disagrees with the plan, stop and surface it. Update the
  plan, write an ADR if needed, then continue. No silent drift.

## Dependencies

- The project is deliberately lean: two deps today (`mattn/go-sqlite3`,
  `gopkg.in/yaml.v3`). Adding a dependency is a project decision, not
  an agent decision. Surface the request in the plan, don't act on it.

## Containment

- Gate G1 (`internal/gate/gate_test.go`) forbids `os/exec`, container
  clients, and `syscall` in `internal/`. Do not add them.
- Spikes (`spikes/`) may use whatever they need. Spike code never gets
  imported by `internal/`.
