# wipnote

**Causal lineage and observability for AI-assisted development.**

Answer "why does this code exist?" in one command. wipnote traces causal chains across work items, commits, sessions, and agent spawns — then stores everything as HTML files in your repo. No external infrastructure required.

## What this is NOT

- **Not a hosted platform.** Local-first. Your data stays on your machine — no cloud sync, no telemetry.
- **Not a general-purpose dev tool yet.** Today it's power-user and Claude-Code-centric. Other CLI integrations exist but are less mature.
- **Not a behavioral agent coordinator.** It observes and attributes what agents do — it does not dispatch, steer, or enforce behavior on agents at runtime.

## Architecture

| Layer | Role |
|-------|------|
| `.wipnote/*.html` | Canonical CRUD store — single source of truth |
| SQLite (`.wipnote/wipnote.db`) | Rebuildable read cache for fast queries and the dashboard |
| Go binary (`wipnote`) | CLI + hook handler |

HTML is the source of truth; SQLite is derived. If they drift, `wipnote reindex` drops the database and rebuilds it from the HTML files. No external infrastructure — no Postgres, no Redis, no cloud sync.

## Install

```bash
# Homebrew (macOS / Linux)
brew install shakestzd/wipnote/wipnote

# Or universal curl install
curl -fsSL https://raw.githubusercontent.com/shakestzd/wipnote/main/install.sh | sh

# Or build from source
git clone https://github.com/shakestzd/wipnote.git
cd wipnote && go build -o wipnote ./cmd/wipnote/
```

The release tarball (and the Homebrew formula) bundle the plugin trees for
Claude Code, Codex CLI, and Gemini CLI alongside the `wipnote` binary. There is
no separate `claude plugin install` step — `wipnote claude` loads the bundled
plugin via `--plugin-dir` automatically.

For subsequent rebuilds after the binary is on your PATH, use `wipnote build`
(it rebuilds the binary AND mirrors the plugin trees into
`~/.local/share/wipnote/`).

### Using wipnote with each harness

```bash
wipnote claude   # Claude Code with bundled plugin
wipnote codex    # Codex CLI with bundled marketplace
wipnote gemini   # Gemini CLI with bundled extension
```

Each launcher resolves the bundled tree (from `~/.local/share/wipnote/` for the
curl/dev install or `$(brew --prefix)/share/wipnote/` for Homebrew) and points
the harness at it. Override the resolved path per-tree with
`WIPNOTE_PLUGIN_DIR`, `WIPNOTE_CODEX_DIR`, or `WIPNOTE_GEMINI_DIR`.

For users who want the bare `claude` / `codex` / `gemini` commands to route
through wipnote, opt in via shell aliases:

```bash
wipnote shell-alias >> ~/.zshrc    # or ~/.bashrc
```

This is intentionally opt-in — per-project `wipnote claude` invocation is the
recommended flow because it avoids surprising shell behavior in non-wipnote
projects.

### Upgrading

```bash
wipnote upgrade            # latest release
wipnote upgrade --check    # check without installing
wipnote update             # alias for upgrade
```

## Quick Start

```bash
wipnote init                          # creates .wipnote/ in your repo
wipnote track create "Auth Overhaul"
wipnote feature create "Add OAuth" --track <trk-id> --description "Implement OAuth2 flow"
wipnote feature start <feat-id>
# ... do work ...
wipnote feature complete <feat-id>
wipnote serve                         # dashboard at localhost:4000
```

## What It Does

**Causal lineage** — Trace the full causal chain for any work item, commit, session, or file. Three commands cover the common queries:

```bash
# Unified causal chain: forward edges (what this caused) + backward edges (what caused this)
wipnote lineage feat-abc1234

# Reverse direction: given a feature ID, list every commit and session it produced
wipnote trace feat-abc1234

# Temporal lineage: git log for a work item's HTML file — every edit, in order
wipnote history feat-abc1234
```

**Work item tracking** — Features, bugs, spikes, and tracks as HTML files in `.wipnote/`. Every change is a git diff. Every item has a lifecycle: create, start, complete.

**Session observability** — Hooks capture every tool call, every prompt, and attribute them to the active work item. See exactly what happened in any session via the dashboard.

**Custom agents** — Define specialized agents with specific models, tools, and system prompts. A researcher agent for investigation, a coder for implementation, a test runner for quality — each scoped to its job.

**Hooks & automation** — Event-driven hooks on SessionStart, PreToolUse, PostToolUse, and Stop. Enforce safety rules, capture telemetry, block dangerous operations, or trigger custom workflows automatically.

**Skills & slash commands** — Reusable workflows as slash commands: `/deploy`, `/diagnose`, `/plan`, `/code-quality`. Package complex multi-step procedures into single invocations.

**Quality gates** — Enforce software engineering discipline: build, lint, and test before every commit. Spec compliance scoring, code health metrics, and structured diff reviews.

**Real-time dashboard** — Activity feed, kanban board, session viewer, and work item detail — served locally by `wipnote serve`.

**Multi-agent attribution and observation** — Claude Code, Gemini CLI, Codex, and GitHub Copilot all read from and write to the same work items via the CLI. Every tool call, file edit, and session is attributed to a work item so you can see what each agent actually did. (Session transcript ingestion currently supports Claude Code JSONL format.)

**Plans & specifications** — CRISPI plans break initiatives into trackable steps. Feature specs define acceptance criteria. Agents execute against the plan and report progress.

## Work Item Types

| Type | Prefix | Purpose |
|------|--------|---------|
| Feature | `feat-` | Units of deliverable work |
| Bug | `bug-` | Defects to fix |
| Spike | `spk-` | Time-boxed investigations |
| Track | `trk-` | Initiatives grouping related work |
| Plan | `plan-` | CRISPI implementation plans |

## Roadmap

The lineage command family covers work items, commits, sessions, and files within a single repo. Two natural follow-ups are explicitly out of scope for now:

- **Spec-as-node** — treating feature specs as first-class lineage nodes so acceptance criteria appear in the causal chain alongside the code that satisfies them.
- **Cross-project lineage** — tracing chains across multiple repos registered in `~/.local/share/wipnote/projects.json`. Today each project's lineage is self-contained.

## CLI Reference

```
wipnote help --compact
```

See full CLI documentation at [wipnote.dev/reference/cli](https://wipnote.dev/reference/cli/).

## Contributing

wipnote is developed using wipnote itself (dogfooding). `.wipnote/` contains real work items — not demos.

```bash
git clone https://github.com/shakestzd/wipnote
cd wipnote
go build -o wipnote ./cmd/wipnote/
./wipnote init
```

Quality gates: `go build ./... && go vet ./... && go test ./...`

## License

MIT

## Links

- [Documentation](https://wipnote.dev/)
- [GitHub](https://github.com/shakestzd/wipnote)
