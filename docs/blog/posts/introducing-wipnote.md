---
date: 2026-04-08
authors:
  - shakes
categories:
  - Architecture
slug: introducing-wipnote
---

# Introducing wipnote: Local-First Observability for AI-Assisted Development

My background is data analysis, not software engineering. At Sunnova and SunStrong, I wrote Python scripts and built ETL pipelines because the work required it, not because I set out to be a developer. But I've never been able to leave tools alone. If something doesn't work the way I think it should, I want to change it.

AI coding tools made that possible in a way it wasn't before. With Claude Code and Codex, a data analyst can build real developer tooling, not just scripts. wipnote is the result of that: a local-first observability and coordination platform for AI-assisted development, built by someone who needed it for his own workflow.

It stores everything as HTML files in your repo (work items, plans, session records), all human-readable, git-diffable, and version-controlled. No Docker, no external databases, no proprietary formats. Just a single Go binary and your git repo.

I had an employment gap due to work authorization issues that gave me several months to go deep on this. What started as curiosity turned into a real project.

<!-- more -->

## The problem with AI-assisted development today

The AI coding tools are genuinely good. I can delegate a feature to a sub-agent, have it research the codebase, write the implementation, and run the tests, all in minutes. But the coordination layer is missing. When I'm running five agents in parallel across different worktrees, I need answers to basic questions:

- What's each agent working on right now?
- Did anyone already investigate this bug?
- What decisions were made in yesterday's session?
- How much has this feature cost in API calls?
- Did the agent actually run tests before committing?

Without tooling, these questions require manually reading session logs and hoping you remember what happened. That doesn't scale.

## Why HTML

Most coordination tools store state in SQLite, JSON, or a cloud database. Those approaches work, and they may be the right choice for many use cases. I was curious whether a different storage format could offer properties that databases don't.

HTML gives you three things for free:

1. **Human-readable by default.** Open an HTML file in any browser and you can read it. No special tooling, no viewers, no parsing. This matters when you're reviewing what an agent did at 2am.

2. **Git-diffable.** HTML is plain text. When a work item's status changes from `in-progress` to `complete`, that shows up in a git diff as a clear, reviewable change. You get version history for every work item without building a version control system.

3. **Graph traversal.** This is where the name comes from. HTML links between files form a graph. A feature links to the track it belongs to, to the sessions that worked on it, to the plan that spawned it. You don't need a separate graph database; the web already is one. Traverse the links to find related features, past failures, prior decisions.

That last property is the long-term bet. Agents that can traverse a project's history structurally, following links between work items, finding what was tried before, seeing which approaches failed, stop re-researching the same problems. The context isn't stuffed into a prompt; it's encoded in the relationships between files.

## How the architecture evolved

The first version of wipnote had zero dependencies. Just HTML files, a Python script to create them, and git. The purist version.

That didn't last. HTML files are great for storage but slow for queries. Listing all in-progress features meant reading and parsing every HTML file in the directory. So SQLite joined as a cache layer: a derived read index that makes queries fast while the HTML files remain the source of truth. Delete the database and it rebuilds itself from the files.

This pattern (HTML as canonical store, SQLite as derived index) turned out to be the right architecture. It wasn't planned; it emerged from daily use. The same pragmatism later drove the Python-to-Go migration (covered in a separate post) and the addition of a third production dependency for the CLI framework.

The lesson: starting with the strictest possible constraints and relaxing them only when you hit a real wall produces a cleaner design than starting with everything and trying to simplify later.

## What wipnote does

wipnote tracks features, bugs, and research spikes across agent sessions. It captures tool calls automatically via hooks. It enforces quality gates before commits. It serves a local dashboard for real-time visibility. And it coordinates plan-driven development with human review loops before agents execute.

The core capabilities:

- **Work item tracking:** Features, bugs, spikes, and tracks as HTML files in `.wipnote/`. Every change is a git diff.
- **Session observability:** Hooks capture every tool call and attribute them to the active work item.
- **Custom agents:** Five specialized agents at different model tiers: a researcher for investigation, fast coders for simple fixes, deep reasoning agents for architecture, and a test runner for quality gates.
- **Hooks and automation:** Event-driven hooks on SessionStart, PreToolUse, PostToolUse, and Stop. Enforce safety rules, capture telemetry, block dangerous operations.
- **Quality gates:** Build, lint, and test before every commit. No exceptions.
- **Plan-driven development:** CRISPI plans with structured YAML schemas, dual-agent critique, and interactive human review before agents start executing.
- **Real-time dashboard:** Activity feed, kanban board, session viewer, and work item detail served locally.

## The stack today

| Layer | Role |
|-------|------|
| `.wipnote/*.html` | Canonical store: single source of truth |
| SQLite (`.wipnote/wipnote.db`) | Derived read index for queries and dashboard |
| Go binary (`wipnote`) | CLI + hook handler |

The Go binary handles everything: creating work items, managing sessions, serving the dashboard, and processing hooks. Three chosen production dependencies: goquery for HTML parsing, cobra for the CLI framework, and modernc.org/sqlite for the embedded database (pure Go, no CGO). Two additional direct dependencies (cascadia and golang.org/x/net/html) support the HTML parsing layer. No external infrastructure required.

## Built with itself

wipnote is developed using wipnote. That's not a marketing line; the `.wipnote/` directory in the repo contains real work items, not demos. As of April 2026: 850+ completed features across 11 completed tracks, ~1,900 commits, all tracked and attributed.

The feedback loop of "use the tool, hit a friction point, fix it immediately" is surprisingly productive when the tool you're building is the tool you're building with. Every rough edge gets noticed because I'm the user. Every missing feature surfaces organically because I need it for the next piece of work.

Over time, wipnote started encoding how I think about problems. The sub-agents reflect how I decide what to delegate and at what cost. The slash commands and skills encode my workflow for breaking down tasks, reviewing plans, and running quality checks. The guardrails encode lessons from every mistake an agent made. It's not a generic framework; it's my approach to work turned into software.

This dogfooding has shaped the project in ways I wouldn't have predicted. The YOLO mode guardrails exist because I watched an autonomous agent make a mess on main. The commit budget guard exists because an agent once staged a 47-file commit. The research-before-writing guard exists because agents kept diving into implementation without reading the existing code first. These aren't theoretical safeguards; they're scar tissue from real incidents.

## What's next

I have a lot more to share about the specific features and the decisions behind them. In upcoming posts I'll cover the Python-to-Go migration, the plan mode system with its dual-agent critique and interactive review, autonomous YOLO mode with its engineering guardrails, and the sub-agent orchestration system.

If you're interested in trying wipnote, it's available as a Claude Code plugin:

```bash
claude plugin install wipnote
```

Or build from source:

```bash
git clone https://github.com/shakestzd/wipnote.git
cd wipnote && go build -o wipnote ./cmd/wipnote/
```

The source is at [github.com/shakestzd/wipnote](https://github.com/shakestzd/wipnote).
