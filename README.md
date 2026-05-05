# erinn

**Causal lineage and observability for AI-assisted development.**

Answer "why does this code exist?" in one command. erinn traces causal chains across work items, commits, sessions, and agent spawns — then stores everything as HTML files in your repo. No external infrastructure required.

## What this is NOT

- **Not a hosted platform.** Local-first. Your data stays on your machine — no cloud sync, no telemetry.
- **Not a general-purpose dev tool yet.** Today it's power-user and Claude-Code-centric. Other CLI integrations exist but are less mature.
- **Not a behavioral agent coordinator.** It observes and attributes what agents do — it does not dispatch, steer, or enforce behavior on agents at runtime.

## Architecture

| Layer | Role |
|-------|------|
| `.htmlgraph/*.html` | Canonical CRUD store — single source of truth |
| SQLite (`.htmlgraph/erinn.db`) | Rebuildable read cache for fast queries and the dashboard |
| Go binary (`erinn`) | CLI + hook handler |

HTML is the source of truth; SQLite is derived. If they drift, `erinn reindex` drops the database and rebuilds it from the HTML files. No external infrastructure — no Postgres, no Redis, no cloud sync.

## Install

```bash
# Install (universal)
curl -fsSL https://raw.githubusercontent.com/shakestzd/erinn/main/install.sh | sh

# Or as a Claude Code plugin
claude plugin install erinn

# Or build from source
git clone https://github.com/shakestzd/erinn.git
cd erinn && go build -o erinn ./cmd/erinn/
```

For subsequent rebuilds after the binary is on your PATH, use `erinn build` instead.

### Upgrading

```bash
erinn upgrade            # latest release
erinn upgrade --check    # check without installing
erinn update             # alias for upgrade
```

## Quick Start

```bash
erinn init                          # creates .htmlgraph/ in your repo
erinn track create "Auth Overhaul"
erinn feature create "Add OAuth" --track <trk-id> --description "Implement OAuth2 flow"
erinn feature start <feat-id>
# ... do work ...
erinn feature complete <feat-id>
erinn serve                         # dashboard at localhost:4000
```

## What It Does

**Causal lineage** — Trace the full causal chain for any work item, commit, session, or file. Three commands cover the common queries:

```bash
# Unified causal chain: forward edges (what this caused) + backward edges (what caused this)
erinn lineage feat-abc1234

# Reverse direction: given a feature ID, list every commit and session it produced
erinn trace feat-abc1234

# Temporal lineage: git log for a work item's HTML file — every edit, in order
erinn history feat-abc1234
```

**Work item tracking** — Features, bugs, spikes, and tracks as HTML files in `.htmlgraph/`. Every change is a git diff. Every item has a lifecycle: create, start, complete.

**Session observability** — Hooks capture every tool call, every prompt, and attribute them to the active work item. See exactly what happened in any session via the dashboard.

**Custom agents** — Define specialized agents with specific models, tools, and system prompts. A researcher agent for investigation, a coder for implementation, a test runner for quality — each scoped to its job.

**Hooks & automation** — Event-driven hooks on SessionStart, PreToolUse, PostToolUse, and Stop. Enforce safety rules, capture telemetry, block dangerous operations, or trigger custom workflows automatically.

**Skills & slash commands** — Reusable workflows as slash commands: `/deploy`, `/diagnose`, `/plan`, `/code-quality`. Package complex multi-step procedures into single invocations.

**Quality gates** — Enforce software engineering discipline: build, lint, and test before every commit. Spec compliance scoring, code health metrics, and structured diff reviews.

**Real-time dashboard** — Activity feed, kanban board, session viewer, and work item detail — served locally by `erinn serve`.

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
- **Cross-project lineage** — tracing chains across multiple repos registered in `~/.local/share/erinn/projects.json`. Today each project's lineage is self-contained.

## CLI Reference

```
erinn help --compact
```

See full CLI documentation at [erinnai.com/reference/cli](https://erinnai.com/reference/cli/).

## Contributing

erinn is developed using erinn itself (dogfooding). `.htmlgraph/` contains real work items — not demos.

```bash
git clone https://github.com/shakestzd/erinn
cd erinn
go build -o erinn ./cmd/erinn/
./erinn init
```

Quality gates: `go build ./... && go vet ./... && go test ./...`

## License

MIT

## Links

- [Documentation](https://erinnai.com/)
- [GitHub](https://github.com/shakestzd/erinn)
