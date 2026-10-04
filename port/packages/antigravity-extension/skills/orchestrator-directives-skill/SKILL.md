---
name: Orchestrator Directives Skill
description: >-
  Decision rules for acting as the wipnote orchestrator: choosing which agent or external CLI
  gets a task, writing a subagent brief, dispatching in parallel, recovering a stalled or
  budget-paused agent, and completing work that a subagent implemented. Not needed for
  hands-on coding done directly in a non-orchestrator session.
---

# Orchestrator Directives

You coordinate; subagents execute. Delegation keeps retries, hook failures, and merge conflicts out of your context. Detailed examples, spawner commands, and long-form procedures are in [reference.md](./reference.md). The always-loaded system prompt already covers work tracking, batching, and step tracking.

## Harness-Aware Research Delegation Contract

Web/docs research and multi-file codebase exploration are tactical work. They MUST run in a sidecar context, not in the main orchestrator context, whenever a dispatch surface exists. Research-first means dispatching a researcher, reader, or external CLI sidecar before committing to an approach, not researching broadly yourself (it burns the context you need for decisions).

Inspect the active harness first:

- **Claude Code**: `Task` / subagent dispatch.
- **Codex**: when `multi_agent_v1` or native `wipnote-*` custom agents are exposed, prefer them (`wipnote-researcher`, `wipnote-patch-coder`, `wipnote-feature-coder`, `wipnote-test-runner`); use nested `codex exec` only when they are unavailable.
- **Antigravity**: harness-native multi-agent or custom-agent spawn.
- **External CLI sidecar**: `agy`, `codex exec`, or another documented CLI when native dispatch is unavailable and the sidecar fits the task.
- **No dispatch surface**: stop and report `delegation unavailable in this harness/session`; do not quietly turn broad research into main-context work.

Do not use main-context `web.search_query`, `web.open`, or broad local glob/search loops for docs gathering or repository exploration when a sidecar is expected. Narrow exceptions: the user explicitly asks you to browse or verify one fact; a higher-priority instruction requires latest-fact verification and no sidecar exists; or a sidecar failed and you do one confirmation before reporting the limitation. When you use an exception, keep it minimal and record why.

## Choosing the executor

| Work | Executor | Why |
|------|----------|-----|
| Exploration, research, analysis | `researcher`, or `agy` sidecar if installed | read-only investigation; wide reading stays out of your context |
| Multi-file or glob reads, raw data retrieval | `reader` | zero-skill, boots fast, no analysis |
| Fully specified 1-2 file edit | `patch-coder` | fast tier; requirements need no investigation |
| Feature work, ~3-8 files, mostly clear | `feature-coder` | default balanced tier |
| 10+ files, ambiguous scope, design choices | `architect-coder` | highest-capability tier |
| Builds, tests, quality gates | `test-runner` | interprets failures without flooding your context |
| Commits, PRs, branches | `copilot` CLI if installed, else `patch-coder` | git operations cascade (hooks, conflicts) and belong in a subagent |
| Code generation outside Claude | `codex exec` (or native Codex agents) | fall back to feature-coder if unavailable |

Each external CLI is optional: if it is missing or fails, dispatch the in-harness agent rather than retrying. Treat `codex exec` sandbox errors ("bwrap", "bubblewrap", "Operation not permitted", "cannot create namespace") as permanent for the session. Spawner command lines are in reference.md.

Match the model to the work, not the other way round: a coder agent's tier is fixed by its definition, so pick the agent whose scope fits rather than overriding its model.

You do not read source or write files yourself. The orchestrator hook counts direct `Read`/`Edit`/`Write`/`Grep`/`Glob` as violations; use `reader` for one-off data-file reads.

When a task may spiral into retries (failing commit hooks, flaky tests, conflicts), delegate it and let the subagent handle the retries; you receive one clean success or failure.

## Writing a brief

Two agents once sat for 22 and 24 minutes with zero tool calls: the stall happened inside the model's turn, before it acted, so no hook could catch it. The same tasks relaunched with a differently shaped brief made 12 and 23 calls in the first minute (GH-#179). Shape matters, so:

1. **Open with a concrete command to run verbatim**, not reading or judgement, e.g. `wipnote feature start <id>` then `mkdir -p .wipnote/logs/progress && echo started >> .wipnote/logs/progress/<id>.md`. `wipnote context-pack <id>` emits this as section 0; paste it rather than paraphrasing.
2. **Ask for incremental deliverables** (append a progress line, write a partial report, commit after each unit) so work survives if the agent is stopped. One ranked report at the end is the shape that stalls.
3. **Name the one thing to prove first** for multi-part work: one page before seventeen, one test before the suite.
4. **Keep the front short**: action first, context after.
5. Include the work item ID, and restate in the brief itself that agents should use `wipnote search '<pattern>'` for structural code search and `wipnote sh "<command>"` for verbose output.

**Dispatch-time context.** Run `wipnote arch resolve --for <work-item-id>` (or `--for "<path>,<path>"`) and paste the output under `## Architectural context`; it is already budget-capped and flags UNVERIFIED cards, which the agent should treat as leads to confirm in code. If it prints "No arch cards matched.", omit the heading. Do not restate environment hazards by hand; the arch cards carry fresher text.

**Detecting a stall.** `wipnote session list` shows `CALLS` and `LAST CALL`; an agent with no row or `CALLS -` a few minutes after dispatch never started. Stop it and re-dispatch with a tighter brief. Stop every agent once its output is verified.

## Running agents in parallel

With 2+ independent tasks, check dependencies and file overlap first, and default to parallel worktrees when there is neither:

| Dependency | File overlap | Do |
|------------|--------------|----|
| no | no | parallel agents with `isolation="worktree"`, `run_in_background=True` |
| no | yes | sequential (shared files conflict on merge) |
| yes | no | pipeline; parallelize what the dependencies allow |
| yes | yes | sequential |

Source edits by concurrent agents belong in per-agent worktrees, never the shared checkout. Afterwards merge branches, run the gate, and clean up.

## When an agent returns

**Budget-paused agents.** Some runtimes (notably devcontainer / agent teams) pause a subagent at a low tool budget and return a non-final message: it ends mid-step with no summary, SHA, or deliverable list. That is a pause, not a failure or a finish. Resume the same agent (Claude Code: `SendMessage` to its `agentId`) with the exact remaining deliverables; a fresh dispatch loses its context. Expect 2-3 resumes.

**Completing delegated work.** A subagent's gate record is session-local and its commits are not auto-linked (bug-3718b630), so completion needs two things from you: run `wipnote check --gate --work-item <id>` yourself, then complete with the real commit SHAs in the rationale, as separate calls:
```bash
wipnote feature complete <id> --accepted-advisory "Subagent implementation verified. Commits: <sha1>, <sha2>."
```
Capture durable learnings with `--learning "<max 120 words>" --learning-kind <hazard|invariant|decision|subsystem-map>` (validated before the item is marked done) or `wipnote arch add <slug> --kind <kind> --body "..." --paths "<glob>" --created-by agent`. If completion prints drift-suspect cards, run `wipnote arch verify <slug>` or `wipnote arch edit <slug> --body "..."`.

## Debugging third-party libraries

Reproduce the failure first (a direct Bash run is fine). Then delegate, in this order: official docs search, GitHub issues search, and only then source reading. Source reading first is slow and usually unnecessary.

## Pre-work validation (hook behavior)

The PreToolUse hook denies a Write/Edit touching 3+ files when no work item is active, and warns on 1 file or on a spike. If denied, create and start a work item, then retry:
```bash
wipnote feature create "Title" --track <trk-id>
wipnote feature start <feat-id>
```
Single file under ~30 minutes can proceed with the warning; 3+ files, new tests, or multi-component work gets a feature first.

## Agent teams vs subagents

Teams (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`, Claude Code 2.1.32+) suit competing-hypothesis debugging, multi-lens review, and splitting feature ownership; subagents suit sequential chains and isolated tasks. One team per session; teammates ignore `skills:`/`mcpServers:` frontmatter and cannot be `/resume`d. Setup, prompts, and caveats are in reference.md.

## More

- [reference.md](./reference.md): spawner commands, delegation patterns and examples, budget-pause and completion-gate detail, post-compact behavior, git isolation, arch-memory procedures, agent-team details.
- `/wipnote:code-quality-skill`: quality gates and pre-commit workflow.
