@AGENTS.md

# wipnote — Claude Code

Local-first observability and coordination platform for AI-assisted development. This project uses wipnote to develop itself: `.wipnote/` holds real work items.

## Build

Always use `wipnote build`, never `go build` directly. It writes to `~/.local/bin/wipnote`, which is on your PATH. A bare `go build -o wipnote ./cmd/wipnote/` leaves the binary in the working directory.

## Quality gate

```bash
go build ./... && go vet ./... && scripts/go-test-streaming.sh ./...
```

Commit only when all three pass. Use `scripts/go-test-streaming.sh` instead of bare `go test` for anything but a quick cached rerun. Plain `go test` buffers output per package. A cold run stays silent for minutes. If the test binary dies mid-package, that package's output is lost and the run stops with no test name (bug-61973a05). The script streams live output and keeps an event log that names the last test to start.

For the full pre-commit check, run `wipnote check --gate`. It also fails on changed code that crosses a size trigger. Rules: `.claude/rules/code-hygiene.md`.

## Deploy

```bash
./scripts/deploy-all.sh X.Y.Z --no-confirm
```

Or `/deploy X.Y.Z`, a repo-local skill in `.claude/skills/deploy/`. Rules: `.claude/rules/deployment.md`.

## Orchestration

Read short files (200 lines or fewer, or one offset/limit range) directly. Delegate long files, multi-file reads and exploration to a subagent.

## Quick commands

| Task | Command |
|------|---------|
| View work | `wipnote snapshot --summary` |
| Status | `wipnote status` |
| Build binary | `wipnote build` |
| Self-update | `wipnote upgrade` |

## Where the rest lives

- Plugin source and generated trees: `.claude/rules/plugin-source-layering.md` and `.claude/rules/plugin-development.md`.
- Worktrees and `.wipnote/` commits: `.claude/rules/worktree-artifacts.md`.
- Launch modes, dev mode and resume: `docs/guides/launching.md`.
- Codespaces and tmux: `docs/guides/codespaces.md`.
- Upstream harness monitoring: `docs/runbook/upstream-monitoring.md`.
