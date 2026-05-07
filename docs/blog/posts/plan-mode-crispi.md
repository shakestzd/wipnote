---
date: 2026-04-12
authors:
  - shakes
categories:
  - Features
slug: plan-mode-structured-plans
---

# Plan Mode: From Freeform Text to Structured, Critiqued, Human-Reviewed Plans

Claude Code has a built-in plan mode. You press Shift+Tab twice, Claude researches your codebase in read-only mode, and produces a plan as freeform text. You review it, approve it, and Claude executes. It works for simple tasks.

But when you're planning a multi-week initiative with 8 interdependent slices, design decisions that need human input, and architectural trade-offs that need scrutiny, freeform text isn't enough. The plan vanishes when the session ends. There's no structured approval flow. Nobody critiques the plan before you act on it. And there's no mechanism to wire the approved plan into executable work items.

wipnote's CRISPI plan system fills these gaps.

<!-- more -->

## What native plan mode gives you

Claude Code's plan mode is a permission mode, not a planning framework. It puts Claude in read-only mode so it can research without modifying files, then produces a text plan. You can:

- Approve it and switch to auto mode for execution
- Refine it interactively
- Escalate to Ultraplan for cloud-based browser review (requires Claude Code on the Web + GitHub)

This is useful for tactical work. But it has key limitations:

- **No schema:** The plan is unstructured text. There's no consistent format for slices, dependencies, or acceptance criteria.
- **No persistence:** The plan exists only in the session. Close the terminal and it's gone.
- **No critique:** Nobody reviews the plan for architectural risks, false assumptions, or missing edge cases before execution.
- **No human approval tracking:** You either approve the whole thing or reject it. There's no per-section review.
- **No dispatch:** There's no mechanism to convert the plan into tracked work items with dependencies.

## CRISPI: a structured alternative

wipnote's plan system produces a YAML document where each slice is a self-contained executable spec card:

```yaml
meta:
  id: plan-3a88d8a9
  title: "Session Ingestion Pipeline"
  track_id: trk-97f85b3b
  status: active

design:
  problem: "Agent sessions generate tool calls but no persistent record..."
  goals:
    - "**Durability** — sessions survive process restarts"
  constraints:
    - "No new runtime dependencies"

slices:
  - id: slice-1
    num: 1
    title: "Hook Hierarchy Fix"
    effort: S
    risk: Low
    what: |
      Restructure hook registration in internal/hooks/registry.go so
      UserPromptSubmit fires before PreToolUse on every invocation.
    why: |
      The ingestion pipeline depends on prompt context being available
      when the first tool call fires. The current ordering drops it.
    done_when:
      - "UserPromptSubmit fires before PreToolUse in all observed traces"
      - "Existing hook tests pass unmodified"
    tests: |
      Unit: TestHookOrder — assert prompt hook index < tool hook index
      Regression: go test ./internal/hooks/... -count=1
    approval_status: pending
    execution_status: not_started
    deps: []
```

Every slice card has effort estimates, risk levels, dependencies, and concrete acceptance criteria. Questions live inside the slice that needs the answer. Critic revisions are embedded per slice so reviewers see the feedback exactly where it applies. The schema is machine-readable, so agents can implement against it and track progress per slice.

## Dual-agent critique

Before a human sees the plan, two AI critics review it:

- **Design critic** (Haiku): Reviews architectural coherence, separation of concerns, API design
- **Feasibility critic** (Sonnet): Checks assumptions, identifies risks, validates effort estimates, flags missing dependencies

The critique produces structured output: assumption verification (verified/unknown/falsified), risk tables with severity ratings, and a synthesis summary. When the critics disagree, that disagreement surfaces explicitly, and it's often the most valuable signal in the review.

This catches problems that a single agent generating a plan would miss. The plan author has blind spots; the critics don't share them.

## The prototyping story: Marimo then Go

I initially started building the plan review UI directly in the Go dashboard. But the interaction design was complex: I needed reactive approval checkboxes that update a progress bar, a dependency graph that colors nodes green as slices get approved, a chat sidebar where you can discuss the plan with Claude and propose amendments, and SQLite persistence for every click.

Building all of that in Go templates and vanilla JavaScript, iterating on the UX, and getting the interaction patterns right, it was slow. Too many compile-rebuild-reload cycles for exploratory UI work.

So I switched to Marimo, a reactive Python notebook framework. Marimo's cell-based reactivity was perfect for this: click a checkbox, the progress bar updates, the graph recolors, the finalize button enables, all without writing any event wiring code. The notebook became a rapid prototyping environment where I could try interaction patterns in minutes instead of hours.

The Marimo prototype grew into a substantial tool: 8 Python modules covering plan rendering, persistence, critique display, dependency graphs (via anywidget + dagre-d3), Claude chat with streaming responses, and an amendment system where the AI can propose changes to the plan that the reviewer accepts or rejects.

Once I understood what the workflow should feel like (once I'd lived with it through several real plan reviews), I ported everything back to Go and vanilla JavaScript, embedded directly in the `wipnote serve` dashboard.

The custom dashboard version now has full feature parity with the Marimo notebook: reactive approvals, dependency graph with approval coloring, SSE-streamed Claude chat, amendment tracking, critique rendering, YAML viewer with syntax highlighting, and a progress bar with finalize button. All with zero Python dependency, integrated into the same dashboard you use for work items and sessions.

## The standalone package idea

The Marimo version has clean enough separation that it could become its own Python package. The 8 modules (plan_notebook, plan_ui, plan_persistence, critique_renderer, dagre_widget, claude_chat, chat_widget, amendment_parser) are already self-contained; they just need a YAML plan file and a SQLite database.

A standalone `crispi-plan-review` package could be useful for anyone doing structured plan review with AI, even outside the wipnote ecosystem. It's something I'm actively considering.

## Human review: local and persistent

Whether you use the Marimo notebook or the embedded dashboard, the review experience is the same:

1. **Section-by-section approval:** Each design decision and vertical slice has its own approval checkbox. No all-or-nothing.
2. **Persistent state:** Every checkbox click, every comment, every question answer writes to SQLite immediately. Close the browser, reopen it, your review state is intact.
3. **No cloud dependency:** Everything runs locally. No GitHub required, no specific subscription tier. Works on any Claude Code installation.
4. **Chat-driven amendments:** Discuss the plan with Claude in the sidebar. The AI can propose structured amendments (`AMEND slice-3: add done_when "Integration tests for error paths"`). You accept or reject each one. Accepted amendments are applied at finalization.

## From plan to execution

The connection between plan and execution is incremental. As slices are approved during review, they are promoted one at a time:

```bash
# Review a slice, then promote it to a feature:
wipnote plan approve-slice plan-3a88d8a9 1
wipnote plan promote-slice plan-3a88d8a9 1
# → prints feat-7a3c1f0b
```

`promote-slice` creates a `feat-XXX` work item, wires it to the track and plan via typed edges, and checks dependency readiness. The feature immediately enters the execute skill's dependency-driven dispatch loop — no manual sequencing required. Dep-blocked slices wait in the `blocked` bucket until their dependencies complete.

This incremental model means you can start executing approved slices while the rest of the plan is still under review. The plan doesn't need to be fully finalized to make progress. When all slices are done, rejected, or superseded, you close the plan with `wipnote plan set-status <id> completed`.

The guardrails are static (file count limits, test requirements, diff review), not plan-aware. But combining structured slice cards with tracked features and linked sessions means there's always a clear record of what was intended, what was approved, and what was actually built.

## The key distinction

We don't replace Claude Code's plan mode. We give it a schema, a critique process, a persistent review workflow, and an execution engine. If your task is "refactor this function," native plan mode is fine. If your task is "redesign the session ingestion pipeline across 8 interdependent slices with architectural trade-offs that need human judgment," CRISPI gives you the structure to do it well.
