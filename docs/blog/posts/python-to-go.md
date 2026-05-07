---
date: 2026-04-10
authors:
  - shakes
categories:
  - Engineering
slug: python-to-go
---

# Why I Switched from Python to Go for AI Development Tooling

wipnote started in Python. The Claude Code SDK was Python-based, the hook system used Python scripts, and I was comfortable in the ecosystem. It worked — until the project outgrew it.

The breaking point people ask about is latency, and that was real: every Claude Code hook invocation spawned a fresh Python process with a ~500ms cold start. A session with 200 tool calls meant 100 seconds of pure hook overhead. The agent would visibly pause between actions.

But latency wasn't the only friction. Running tests was slow. Distribution meant explaining virtualenvs and Python versions. As the codebase grew, the development experience itself was becoming a drag, not just for the tool's users, but for me building it every day.

<!-- more -->

## The cold start problem

Claude Code hooks run as subprocesses. They receive a CloudEvent JSON payload on stdin, process it, and return a response. The hook handler fires on every event: session start, every file read, every file write, every bash command, session end. In a typical development session, that's hundreds of invocations.

With Python, each invocation meant:

1. Spawn a new Python process
2. Load the interpreter
3. Import dependencies (PyYAML, sqlite3 bindings, the SDK)
4. Parse the event
5. Process the logic
6. Return the response
7. Tear down the process

Steps 1-3 alone took 300-500ms. The actual business logic in step 5 was usually a few milliseconds. I was spending 99% of the hook execution time on startup overhead.

## Go: near-zero cold start

A compiled Go binary doesn't have this problem. There's no interpreter to load, no dependencies to import at runtime. Everything is statically linked into a single executable. The same hook that took hundreds of milliseconds in Python runs in single-digit milliseconds in Go.

The difference is night and day. Hook processing becomes invisible. The agent doesn't pause between actions. The development experience goes from "noticeably laggy" to "I forget hooks are even running."

## The single-binary advantage

Go's compilation model produces a single standalone binary with no runtime dependencies. No virtualenv, no pip install, no Docker container, no "which Python version do you have?" conversations.

For wipnote, this means installation is:

```bash
# Download the binary for your platform
curl -fsSL https://github.com/shakestzd/wipnote/raw/main/install.sh | bash
```

Or as a Claude Code plugin:

```bash
claude plugin install wipnote
```

Either way, users get one file that works. No runtime to configure, no dependency conflicts to resolve. This matters enormously for developer tools; the install friction determines whether anyone actually uses it.

## Minimal dependencies

I'm deliberately minimal about dependencies. wipnote's `go.mod` has three chosen production dependencies:

| Dependency | Purpose |
|-----------|---------|
| `github.com/PuerkitoBio/goquery` | HTML parsing and manipulation |
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | Embedded SQLite database |

Two additional direct dependencies (cascadia and golang.org/x/net/html) support the HTML parsing layer, but the core design choices were these three. No web framework, no ORM, no logging library, no configuration framework. The standard library handles HTTP serving, JSON encoding, file I/O, and most everything else.

The SQLite dependency deserves special mention: `modernc.org/sqlite` is a pure Go implementation: no CGO, no C compiler required. This means the binary cross-compiles cleanly for any platform Go supports. A contributor on Windows can build wipnote without installing a C toolchain.

## The "no infrastructure" constraint

One of wipnote's core constraints is "no external infrastructure required." No Postgres, no Redis, no cloud sync, no message queues. Everything runs locally.

This constraint drove the architecture: HTML files as the canonical store (they're just files in your repo), SQLite as a derived read index (it's embedded in the binary), and the Go binary as the only runtime component.

In Python, achieving this would have been possible but awkward. You'd need to bundle a virtualenv or use something like PyInstaller to create a standalone package. In Go, it's the default. The binary IS the application; there's nothing else to install, configure, or run.

## What I lost

The switch wasn't free. Python has an incredible ecosystem for interactive development, particularly notebooks. When I needed to prototype the plan review UI (an interactive workflow with reactive approvals, dependency graphs, and AI chat) I couldn't do it in Go. I turned to Marimo, a reactive Python notebook framework, and prototyped the entire review experience there.

That ended up being a feature, not a bug. Marimo let me iterate on the interaction design much faster than building a web UI from scratch. Once I understood what the workflow should feel like, I ported everything back to Go and vanilla JavaScript inside the dashboard. The Marimo prototype became the spec for the production implementation.

I wrote more about this prototyping story in a separate post on the CRISPI plan system.

## Why I actually made the switch

The latency numbers and distribution model were the rational arguments. But what actually pushed me over the edge was watching other developers build similar tools in Go. Wes McKinney's work on Go-based developer tools, and the broader pattern of "compiled binary + embedded SQLite + no external deps" showing up repeatedly in the AI tooling space; it was clear this wasn't theoretical. The pattern keeps appearing because it works.

The other factor: AI agents write solid Go. I was already using Claude Code to build wipnote, and Go's type system, standard library, and explicit error handling give agents clear guardrails. The code they produce tends to be correct on the first pass in a way that Python code often isn't.

## The switch was faster than expected

I expected the migration to take weeks. It took days.

Months of Python development (the hook system, the HTML parser, the SQLite indexer, the CLI) converted quickly. More surprising: problems that had been technically painful in Python became straightforward in Go. Concurrency handling that required careful asyncio choreography in Python was natural with goroutines. The binary just compiled and ran, no environment to configure.

## The result

wipnote today is ~33K lines of production Go code (~50K including tests) across 19 packages with 770+ test files. It compiles in seconds, cross-compiles for every major platform, and installs without friction. The hook system processes hundreds of events per session without perceptible latency. And when something breaks, `go build && go vet && go test` catches it before it ships.

I don't regret starting in Python; it let me prove the concept quickly and iterate on the design. But the move to Go was the decision that made wipnote viable as a tool other people could actually use.
