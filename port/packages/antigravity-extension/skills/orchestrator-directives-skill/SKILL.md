---
name: Orchestrator Directives Skill
description: Rules for the wipnote orchestrator role - which agent gets a task, subagent briefs, parallel dispatch, stalled agents.
---

# Orchestrator Directives

You coordinate; subagents execute. Delegation keeps retries, hook failures, and merge conflicts out of your context. Detailed examples, spawner commands, and long-form procedures are in [reference.md](./reference.md). The always-loaded system prompt already covers work tracking, batching, and step tracking.

## Harness-Aware Research Delegation Contract

Web/docs research and multi-file codebase exploration are tactical work. They MUST run in a sidecar context, not in the main orchestrator context, whenever a dispatch surface exists. Research-first means dispatching a researcher, the native code explorer, or an external CLI sidecar before committing to an approach, not researching broadly yourself (it burns the context you need for decisions).

Inspect the active harness first:

- **Claude Code**: `Task` / subagent dispatch (native `Explore` for code reading).
- **Codex**: when `multi_agent_v1` or native `wipnote-*` custom agents are exposed, prefer them (`explorer`, `wipnote-researcher`, `wipnote-patch-coder`, `wipnote-feature-coder`, `wipnote-test-runner`); use nested `codex exec` only when they are unavailable.
- **Antigravity**: harness-native multi-agent or custom-agent spawn.
- **External CLI sidecar**: `agy`, `codex exec`, or another documented CLI when native dispatch is unavailable and the sidecar fits the task.
- **No dispatch surface**: stop and report `delegation unavailable in this harness/session`; do not quietly turn broad research into main-context work.

Do not use main-context `web.search_query`, `web.open`, or broad local glob/search loops for docs gathering or repository exploration when a sidecar is expected. Narrow exceptions (keep them minimal and record why): the user asks you to verify one fact, no sidecar exists for a required latest-fact check, or one confirmation after a sidecar failed.

## Choosing the executor

| Work | Executor | Why |
|------|----------|-----|
| Code exploration, multi-file or glob reads | native `Explore` (Claude) / `explorer` (Codex) | read-only; wide reading stays out of your context |
| Research, analysis, external docs | `researcher`, or `agy` sidecar if installed | investigation with judgement |
| Fully specified 1-2 file edit | `patch-coder` | fast tier; requirements need no investigation |
| Feature work, ~3-8 files, mostly clear | `feature-coder` | default balanced tier |
| 10+ files, ambiguous scope, design choices | `architect-coder` | highest-capability tier |
| Builds, tests, quality gates | `test-runner` | interprets failures without flooding your context |
| Commits, PRs, branches | `copilot` CLI if installed, else `patch-coder` | git operations cascade (hooks, conflicts) and belong in a subagent |
| Code generation outside Claude | `codex exec` (or native Codex agents) | fall back to feature-coder if unavailable |

Each external CLI is optional: if it is missing or fails, dispatch the in-harness agent rather than retrying. Treat `codex exec` sandbox errors ("bwrap", "bubblewrap", "Operation not permitted", "cannot create namespace") as permanent for the session. Spawner command lines are in reference.md.

Read short files (≤200 lines, or one targeted offset/limit range) directly when you need exact text to decide. Delegate long files, multi-file reads and exploration to a subagent (for code, the native explorer: `Explore` on Claude Code, `explorer` on Codex, `researcher` on Antigravity) to keep your context for coordination. Never `Edit`/`Write` yourself.

When a task may spiral into retries (failing commit hooks, flaky tests, conflicts), delegate it and let the subagent handle the retries; you receive one clean success or failure.

## Writing a brief

Brief shape decides whether an agent stalls before its first tool call, where no hook can see it (GH-#179):

1. **Open with a concrete command to run verbatim**, not reading or judgement, e.g. `wipnote feature start <id>` then `mkdir -p .wipnote/logs/progress && echo started >> .wipnote/logs/progress/<id>.md`. `wipnote context-pack <id>` emits this as section 0; paste it rather than paraphrasing.
2. **Ask for incremental deliverables** (append a progress line, write a partial report, commit after each unit) so work survives if the agent is stopped. One ranked report at the end is the shape that stalls.
3. **Name the one thing to prove first** for multi-part work: one page before seventeen, one test before the suite.
4. **Keep the front short**: action first, context after.
5. Include the work item ID, and restate in the brief itself that agents should use `wipnote search '<pattern>'` for structural code search and `wipnote sh "<command>"` for verbose output.

**Dispatch-time context.** Paste `wipnote arch resolve --for <work-item-id>` output under `## Architectural context` (omit the heading on "No arch cards matched."). UNVERIFIED cards are leads to confirm in code; do not restate environment hazards by hand.

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

**Budget-paused agents.** A non-final return (mid-step, no summary, SHA, or deliverable list) is a runtime pause, not a failure or a finish. Resume the same agent (Claude Code: `SendMessage` to its `agentId`) with the exact remaining deliverables; a fresh dispatch loses its context. Expect 2-3 resumes.

**Completing delegated work.** A subagent's gate record is session-local and its commits are not auto-linked (bug-3718b630), so completion needs two things from you: run `wipnote check --gate --work-item <id>` yourself, then complete with the real commit SHAs in the rationale, as separate calls:
```bash
wipnote feature complete <id> --accepted-advisory "Subagent implementation verified. Commits: <sha1>, <sha2>."
```
Capture durable learnings with `--learning "<max 120 words>" --learning-kind <kind>` or `wipnote arch add`; act on drift-suspect cards with `wipnote arch verify|edit <slug>` (see reference.md).

## Hooks and edge cases

- **Third-party library bugs:** reproduce first, then delegate docs search, then GitHub issues, and only then source reading.
- **Pre-work validation:** the PreToolUse hook denies a Write/Edit touching 3+ files with no active work item (warns on 1 file or a spike). Create and start a work item, then retry.
- **Agent teams** (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`) suit competing-hypothesis debugging, multi-lens review and feature ownership splitting; subagents suit chains and isolated tasks. Details in reference.md.

## More

- [reference.md](./reference.md): spawner commands, brief examples, budget-pause and completion-gate detail, git isolation, arch-memory procedures, agent-team details.
- `/wipnote:code-quality-skill`: quality gates and pre-commit workflow.
