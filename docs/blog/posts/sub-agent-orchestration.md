---
date: 2026-04-16
authors:
  - shakes
categories:
  - Architecture
slug: sub-agent-orchestration
---

# Sub-Agent Orchestration: Cost-Aware Delegation With Work Attribution

Claude Code recently shipped Agent Teams, an experimental feature where fully independent Claude Code instances communicate with each other via mailboxes and shared task lists. Teammates can challenge each other's findings, collaborate on problems, and coordinate through a team lead.

wipnote's orchestration system solves a different problem. It's not about agents talking to each other; it's about dispatching the right agent for each task at the right cost, tracking what each one produces, and merging the results with quality gates.

<!-- more -->

## The 5-agent system

wipnote ships five specialized sub-agents, each scoped to a specific role:

| Agent | Model | Purpose |
|-------|-------|---------|
| `researcher` | Sonnet | Investigation, debugging, visual QA. Evidence-first: documentation before trial-and-error. |
| `haiku-coder` | Haiku | Quick fixes, 1-2 files, clear requirements. Fast and cheap. |
| `sonnet-coder` | Sonnet | Feature implementation, 3-8 files, moderate complexity. The default. |
| `opus-coder` | Opus | Complex architecture, 10+ files, ambiguous requirements, design decisions. |
| `test-runner` | Haiku | Testing, quality gates, lint, type checking. |

Each agent has a system prompt tailored to its role, scoped tool access, and a specific model tier. The researcher can read files but not write them. The test-runner runs commands but doesn't edit code. The coders write code but are instructed to delegate to the researcher when they need to understand unfamiliar systems.

## Cost-aware model selection

Not every task needs Opus. A typo fix doesn't require deep reasoning. A config change doesn't need a researcher. The orchestrator's job is to match task complexity to the appropriate model tier:

- **Simple** (Haiku): Typo fixes, config changes, single-file edits
- **Moderate** (Sonnet): Feature implementation, bug fixes, refactors across a few files
- **Complex** (Opus): Architecture decisions, large refactors, ambiguous scope requiring judgment

This isn't just about saving money, though that matters when you're running dozens of sub-agents per day. It's about using the right tool for the job. A haiku-coder that finishes a config change in 30 seconds is better than an opus-coder that spends 3 minutes on the same task and produces the same result.

The orchestrator directives skill encodes these patterns. It includes fallback logic: try external CLIs first (Gemini CLI for research, Codex for code generation), fall back to wipnote agents if those aren't available.

## The orchestrator pattern

The main session (the orchestrator) decides WHAT to do and WHO should do it. It does not implement directly. This is a deliberate architectural choice.

When a user asks the orchestrator to implement a feature, the flow is:

1. Orchestrator creates or claims a work item
2. Orchestrator analyzes the task and selects the appropriate agent
3. Orchestrator dispatches the agent with a self-contained prompt
4. Agent executes in its own context (potentially its own worktree)
5. Agent returns results to the orchestrator
6. Orchestrator verifies the work, runs quality gates, and marks the item complete

The orchestrator's prompt explicitly says: "Do NOT use Read, Edit, Write, Grep, or Glob directly. Delegate to wipnote subagents." The only tools the orchestrator uses directly are Bash (for `wipnote` CLI commands) and the Agent tool (for dispatching sub-agents).

## Work attribution

Every sub-agent must register its work item before writing code. The `PreToolUse` hook blocks multi-file writes from any agent that hasn't called `wipnote feature start <id>`. This prevents anonymous modifications: every line of code traces to a tracked feature, a responsible agent, and a session.

When the orchestrator dispatches a sub-agent, it includes the work item ID in the prompt: "Feature: feat-123. Run `wipnote feature start feat-123` before writing code." The sub-agent registers itself, does the work, and the session record captures every tool call attributed to that feature.

After the agent returns, the orchestrator checks whether the work item was completed. If it's still in-progress, the orchestrator marks it complete as a safety net.

## Parallel dispatch with worktrees

The `execute` skill takes this further. Given an approved CRISPI plan with dependency-tracked slices, it:

1. Queries the plan for all unblocked slices (no pending dependencies)
2. Dispatches ALL independent tasks simultaneously, each with `isolation: worktree`
3. Each sub-agent works in its own git branch, isolated from the others
4. As agents complete, their branches are merged back
5. Quality gates run after each merge
6. Newly unblocked slices are dispatched in the next wave

This is dependency-driven dispatch, not manual sequencing. You don't specify Wave 1 and Wave 2; the execute skill reads the dependency graph and figures out what can run in parallel.

Conflict resolution for shared files (like import registrations or config files) is handled by instructing agents to use additive operations rather than replacing file contents.

## How it differs from Agent Teams

Claude Code's Agent Teams and wipnote's orchestration address distinctly different coordination problems:

| Dimension | Agent Teams | wipnote Orchestration |
|-----------|------------|------------------------|
| **Communication** | Teammates talk to each other via mailbox | Sub-agents report to orchestrator only |
| **Task allocation** | Self-claiming from shared task list | Orchestrator explicitly assigns |
| **Cost model** | Each teammate is a full Claude Code session | Explicit model tiers per role |
| **Isolation** | Shared context, optional worktrees | Worktree isolation by default |
| **Use case** | Collaborative exploration, peer review | Isolated parallel implementation |

Agent Teams shine when you need agents to challenge each other, when the interaction between agents produces better results than either would alone. wipnote's orchestration shines when you need predictable, attributed, cost-controlled parallel execution with quality gates.

They're complementary. You could run an Agent Teams session where the team lead uses wipnote's orchestration patterns, getting both inter-agent collaboration and cost-aware delegation. The two approaches operate at different layers.

## The result

This system has been used to build wipnote itself. 850+ features completed across 11 tracks, each one dispatched through this orchestration layer. The researcher investigates, the haiku-coder handles simple fixes, the sonnet-coder implements features, and the opus-coder makes architectural decisions. Every change is attributed, every commit passes quality gates, and the dashboard shows exactly what each agent contributed.

The numbers validate the approach. But more than that, the experience of using it daily, watching the right agent handle the right task efficiently, seeing attribution chains from plan to feature to commit, makes AI-assisted development feel less like working with a black box and more like managing a team.
