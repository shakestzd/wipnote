---
name: wipnote:execute
description: Execute a parallel plan using dependency-driven dispatch. Checks file overlap among unblocked tasks, partitions into non-conflicting waves, and dispatches simultaneously. Merges completed work, then dispatches newly unblocked tasks. No manual wave sequencing.
---

# wipnote Parallel Execute

Execute development tasks in parallel with dependency-driven dispatch and worktree isolation.

**Trigger keywords:** execute plan, run plan, run tasks, parallelize work, work in parallel, start execution, dispatch agents

## Environment and efficiency

In a worktree, `WIPNOTE_PROJECT_DIR` is set, so `wipnote` commands resolve to the main project's `.wipnote/`. Never use bare `cd` (the hook blocks it); use a subshell `(cd dir && command)`.

Dispatch the first subagent within **≤5 tool calls**:
1. One discovery call: `wipnote execute-preview <trk-id> --format json` returns the track (title at `.track.title`), linked features/bugs/plans and git state. Do not call `track show`, `feature show`, `plan show` and `git status` separately.
2. Chain any extra git probes with `&&` in one Bash call.
3. Do not retry flag variants; the flags in this skill are validated by a build-time test.

## Attribution (mandatory)

Confirm the active item with `wipnote status` (start it if needed). Every agent prompt carries its work-item ID, runs `wipnote feature start <id>` first, and completes the item after its gate passes. Without attribution the work is invisible to the project graph.

## Choosing the dispatch mechanism

- **This skill:** a wipnote track or plan whose slices have dependencies and file overlap to manage.
- **Native `/batch`:** 5-30 independent units with no wipnote plan behind them (e.g. a mechanical change across many files).
- **Workflows:** dozens of units or a repeatable multi-step pipeline.

Whatever the mechanism, every brief still carries the work-item ID so commits and sessions stay attributed.

## The dispatch loop

```
LOOP:
  1. Ready = pending tasks with no blockedBy
  2. Check file overlap among ready tasks; keep the largest non-conflicting subset
  3. Dispatch that subset in ONE message, one worktree per agent
  4. Wait, merge completed branches to main, run the gate on the merged result
  5. Mark merged tasks completed; newly unblocked tasks → LOOP; none left → DONE
```

If 10 of 13 tasks are independent, all 10 run in the first dispatch. Slices promoted with `wipnote plan promote-slice <plan-id> <num>` (one at a time or all at once) become ordinary features on the track and enter the same loop as soon as their `blocked_by` dependencies complete.

### File overlap

Dependency independence is not enough: two unblocked agents editing the same file produce merge conflicts. For each candidate run `wipnote trace <feat-id>` and intersect the file sets pairwise.
- No overlap: dispatch all ready candidates.
- Partial overlap: dispatch the largest non-overlapping subset; defer the rest to the next cycle.
- Everything conflicts (e.g. all touch `go.mod` or a registration file): dispatch one, or record an explicit decision to accept merge conflicts.

### Paused agents

SendMessage is the Claude Code resume primitive for budget-paused agents, but it only loads with agent teams and cannot resume worktree subagents (Claude Code issue #42596); without it, treat a paused task as needing a rerun or fall back to `wipnote yolo --feature <id>`.

## Populating tasks

Create one task per approved slice, using the slice title as the subject (the execute log shows it as the badge label; a slice number or raw ID is unreadable):

```
TaskCreate(
    subject="<slice.title>",
    description="<slice.what, max 200 chars>",
    activeForm="Implementing <slice.title>",
    metadata={"feature_id": "<slice.id>", "num": <slice.num>, "agent": "wipnote:feature-coder"}
)
```

Use the track title, not its ID, as the outer coordinator's `Agent()` description.

## Dispatching

Spawn all ready tasks in a single message, each with `isolation="worktree"`, and mark them `in_progress`:

```
Agent(
    description="feat-001: Add check command",
    subagent_type="wipnote:feature-coder",
    isolation="worktree",
    prompt="[task prompt below]"
)
```

### Task prompt template

```
## Task: {task.subject}
**Feature:** {feature_id}

## Step 0: Attribution (FIRST)
wipnote feature start {feature_id}

## Plan context (omit if no plan)
Part of plan {plan_id} on track {track_id}.
Design decisions: {3-5 relevant answered questions from the plan YAML}
Critique (HIGH/DANGER only): {items touching this slice}
Siblings (do NOT implement): {ID + title + status}

## Goal
{task.description}

## Files to create/edit
{files}
## Shared registration files (add your lines; the orchestrator resolves conflicts)
{e.g. main.go}
## Do not touch
{files owned by concurrent tasks}

## Protocol
Follow /wipnote:tdd-protocol: failing tests first, then the gate from /wipnote:code-quality-skill.
Name {feature_id} in every commit subject or a `Refs: {feature_id}` trailer so
`wipnote feature complete` links it automatically; otherwise run
`wipnote feature link-commit {feature_id} <sha>`.

Report: files changed, lines added, tests passing, test names.
```

Plan context comes from the feature's `planned_in` (plan) and `part_of` (track) edges, the plan YAML (`wipnote plan show {plan_id}`), and `wipnote track show {track_id}` for siblings. Keep it to 5-10 lines.

## Merging and gates

```bash
git checkout main
git merge --no-ff worktree-agent-XXXX -m "feat: merge {task title} ({feature_id})"
```

Conflicts in shared registration files are additive: keep every agent's lines. Conflicts in real logic mean a missed dependency: decide which change is right and add `addBlockedBy` for future runs.

Run the project gate from `/wipnote:code-quality-skill` on the merged result and record it with `wipnote check --gate`. If it fails, find the breaking merge (`git log --oneline`), fix small breaks on main or `git revert` the merge and re-dispatch with a fix note. The gate must pass before dispatching blocked tasks. Then `TaskUpdate(status="completed")` the merged tasks and query `TaskList()` again.

## Review, integrate, clean up

1. **Review (mandatory before merging the feature branch):** run native `/code-review` on the branch and address medium+ findings, or acknowledge them with a justification.
2. **Merge:** `git merge --no-ff <branch> -m "feat: merge <track> — <title>"`, resolve conflicts, then run the project gate again.
3. **Clean up:** `git worktree prune && git worktree list`, then `git worktree remove <path> && git branch -D worktree-agent-XXXX` per worktree (or `/wipnote:cleanup`).

## Error handling

- **Agent fails its tests:** do not merge it; create a `Fix: <task>` follow-up task and keep merging the others.
- **All remaining tasks blocked:** a dependency failed or never completed; inspect with `TaskList()` / `TaskGet(id)` and fix the blocker or drop a stale dependency.
- **Monitoring:** `TaskList()` for status and blockers, `git worktree list`, and `git for-each-ref --sort=-committerdate refs/heads/ --format='%(refname:short) %(committerdate:relative) %(subject)' | head -20`.

## Related skills

- **[/wipnote:plan](/wipnote:plan)** - Create the dependency graph before executing
