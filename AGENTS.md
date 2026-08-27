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
- No multiline input via `heredoc` (`<<EOF`) or `printf` line-stacking.
  Multiline shell input is fragile in the tool's terminal: long heredocs
  produce garbled output and silently fail mid-stream. For multi-line file
  content, write the file with the editor tool, not by piping to `cat`,
  `tee`, or `printf`. If a single command genuinely needs more than one
  line of input, break it into separate calls.

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

- Run `make check` (lint + vet + test-race) after every commit, not at the
  end. If something breaks, find the breaking commit, don't bisect.
- Run `make hooks` once after cloning to install the pre-push gate so CI
  lint/vet failures surface locally before the push lands. Bypass with
  `git push --no-verify` only when you know why the hook is unhappy.
- Re-prove Gate G1 (`CGO_ENABLED=1 go test ./internal/gate/`) after any
  change to `internal/` that touches imports or the engine surface.

## Commits — agent ↔ human handoff

- The human signs every commit (GPG). The agent stages the
  files, runs `make check`, and shows the staged diff and the
  proposed commit message. The agent then runs `git commit -m
  "<type>: <title>"` and the human types the GPG passphrase in
  the same terminal. One commit per logical change; do not
  batch unrelated work.
- The agent must never use `--no-gpg-sign`, `git -c
  commit.gpgsign=false`, `GIT_GPG_PROGRAM=true`, or any other
  mechanism that bypasses the human's GPG signing. The
  signature is the audit trail.
- If a commit is malformed (wrong message, missing file),
  reset with `git reset --soft HEAD~1` and re-stage. Do not
  force-amend a signed commit.
- For multi-commit work (e.g. a roadmap task broken into 5
  commits per the plan), do one commit at a time: stage,
  check, show diff, run `git commit`, wait for the human to
  confirm the signature, then move to the next commit. Do not
  pre-stage the next commit's changes while waiting.

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
