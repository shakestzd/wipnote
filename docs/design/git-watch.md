# Git watch — design note

Status: implemented · Date: 2026-10-26 · Plan: plan-999ea9c7

## Purpose

`wipnote ci status` and `wipnote ci watch` show a pull request's CI, review and
branch health in every harness. The code lives in `internal/gitwatch`. It shells
out to `gh` and `git`. It is read-only toward GitHub and never handles a token:
`gh` owns authentication. Nothing runs from a hook.

Three front ends share it:

- `wipnote ci status [pr|branch] [--work-item <id>] [--json]` prints one snapshot.
- `wipnote ci watch [pr|branch]` polls in the foreground until the PR settles.
- `/wipnote:ci` (`plugin/commands/ci.md`) tells the agent to run the two commands.
  It ships to Claude Code, Codex and Antigravity.

In Claude Code, the `mods/wipnote-ci` pane (`/ci`) polls `wipnote ci status --json`.
It makes no `gh` calls of its own.

## Sections and their reads

The argument picks the PR. All digits is a PR number. Other text is a head branch.
Empty is the current branch's PR, else your newest open PR.

| Section | Reads | Command |
|---|---|---|
| PR lookup | number from branch | `gh pr view --json number`; else `gh pr list --author @me --state open --limit 1 --json number`; for a branch `gh pr list --head <branch> --state all --limit 1 --json number` |
| PR | basics | `gh pr view <n> --json number,title,headRefName,baseRefName,state,url` |
| Work item | id in head branch, else title | none (regex `feat-`/`bug-`/`spk-` + 8 hex) |
| Work item lookup (`--work-item`) | newest PR naming the id | `gh pr list --state all --search "<id> in:title" --limit 20`, then `gh pr list --state open --limit 100` with head branches matched locally |
| Checks | state, bucket, times, link | `gh pr checks <n> --json name,state,bucket,startedAt,completedAt,link,workflow,event` |
| Review | decision, requests, merge state | `gh pr view <n> --json reviewDecision,reviewRequests,mergeable,mergeStateStatus,baseRefOid` |
| Review threads, required checks | open threads; `isRequired` per check | `gh api graphql` (`reviewThreads`, `statusCheckRollup.contexts`, `isRequired(pullRequestNumber:)`) |
| Base CI | latest completed run per workflow | `gh run list --branch <base> --limit 20 --json headSha,status,conclusion,workflowName,url,createdAt` |
| Release | latest tag | `gh release view --json tagName,publishedAt,url` |
| Release commits | commits since the tag | `git rev-list --count <tag>..origin/<base>`, else `<tag>..<base>` |
| Release workflow | its latest run | `gh workflow list --json name,path` (when not configured), then `gh run list --workflow <name> --limit 1 --json status,conclusion,url,createdAt` |
| Branch | dirty, upstream, unpushed | `git status --porcelain=v2 --branch`; `git rev-list --count HEAD --not --remotes` |
| Branch | ahead/behind the base | `git for-each-ref` with `%(ahead-behind:<base>)` (git 2.41+), else `git rev-list --left-right --count <base>...HEAD` |
| Worktrees | list | `git worktree list --porcelain` |
| Worktrees | merged, squash-merged | `git merge-base --is-ancestor`; `git merge-tree --write-tree`; `git rev-parse <base>^{tree}`; `git rev-list --first-parent <base>` |
| Worktrees | closed PRs | `gh pr list --state closed --json headRefName,state --limit 100` |

The base is `origin/<base>` when it exists, else the local `<base>`. With no PR the base is `main`.

Checks are bucketed as gh buckets them. Repeated runs of one check keep the latest
only. `STARTUP_FAILURE` counts as fail. The base is red when a workflow's latest
completed run ended `failure`, `startup_failure` or `timed_out`. `base_red_too` is
set when the base is red or a run at the PR's base commit failed.

A missing or signed-out `gh`, or a failed PR or checks read, makes the snapshot
`available: false` with a reason. `ci status` still exits 0. A failed later
section stays empty and adds a note.

## Watch cadence and settle rules

- Checks and PR state are read every 15 s (`--interval-checks`) while a check is pending.
- Review, base, release and branch health are read every 60 s (`--interval-slow`).
- With nothing pending, everything is read every 60 s.
- Both intervals have a 5 s floor.
- Output repeats only when the snapshot changes.
- It settles when at least one check is reported and none is pending, or when the PR is merged or closed.
- A PR with no checks is watched for a 2-minute grace (`--no-checks-grace`). Still none then: a final snapshot notes `no checks reported` and it exits 0.
- No PR matched: one snapshot, exit 0.
- A failed read after a good one keeps the last snapshot with a note. It is retried on the next tick.
- `--timeout` defaults to 60 minutes.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | All checks passed, no checks after the grace, or no PR |
| 1 | A check failed or was cancelled (skipped is fine) |
| 2 | Timeout |
| 3 | Not available: the first read, or 3 failed reads in a row |
| 130 | Interrupted |

The pane uses the same cadence and the same 2-minute grace.

## Gate warning

`wipnote check --gate --work-item <id>` looks up the work item's linked PR.

- Only an `OPEN` PR can warn. Merged, closed or no PR prints nothing.
- It warns and never fails the gate.
- The whole lookup is capped at 10 s.
- It warns on failed or cancelled checks, a merge conflict, or unresolved review threads.
- When the review read fails, it warns from the checks alone, marked `(review not read)`.
- A failed lookup or checks read prints nothing.

`wipnote <type> show <id>` adds a line `PR #N <state> · CI <counts>`, capped at 5 s.

## Stale worktree reasons

Reported only. Nothing removes, prunes or edits a worktree or branch. The base
branch and detached worktrees get no reasons.

| Reason | Rule |
|---|---|
| `merged` | The branch tip is in the base, or merging it into the base changes nothing (a squash merge). A branch with no commits of its own is not merged. |
| `upstream_gone` | Its remote branch was deleted. |
| `idle` | No commit for `gitwatch.stale_days` days. |
| `pr_closed` | Its PR was closed without merging. A branch with any merged PR is left out. |

## Config

Read from the layered config: `~/.wipnote/config.json`, then
`.wipnote/config.json`, then `.wipnote/config.local.json`.

| Key | Default | Meaning |
|---|---|---|
| `gitwatch.stale_days` | 7 | Idle days before a worktree is `idle`. Not positive means the default. |
| `gitwatch.release_workflow` | unset | Release workflow name. Unset: the first workflow whose file name contains `release`. |

## Ported code

All MIT, with attribution in the file headers and `THIRD-PARTY-NOTICES.md`.

| Function | Source repo | Source file | Commit | License |
|---|---|---|---|---|
| `bucketFor`, `eliminateDuplicates` (`aggregate.go`) | cli/cli | `pkg/cmd/pr/checks/aggregate.go` | `4f2cfe92e3499f8581bf39db77e0a1431fa7b55c` | MIT |
| for-each-ref format, `determineSyncStatus` (`branch.go`) | git-town/git-town | `internal/git/commands.go` | `d0cc7951b01e74ba36ba057c0f31fd5eee6e83b8` | MIT |
| `SyncStatus` values (`branch.go`) | git-town/git-town | `internal/git/gitdomain/sync_status.go` | `d0cc7951b01e74ba36ba057c0f31fd5eee6e83b8` | MIT |
| `parseAheadBehindField` (`branch.go`) | jesseduffield/lazygit | `pkg/commands/git_commands/ahead_behind.go` | `4e1eebe278790f257a875da6633234391a5a85b3` | MIT |
| `parseWorktrees` (`worktrees.go`) | seachicken/gh-poi | `conn/command.go` | `abf237be0d79d754922f3cf72a905f9854d2b096` | MIT |
| `isFullyMerged` (`worktrees.go`) | seachicken/gh-poi | `cmd/root.go` | `abf237be0d79d754922f3cf72a905f9854d2b096` | MIT |

Changes from upstream: `STARTUP_FAILURE` is fail, not pending. An unknown track is
`unknown`, not a panic. Ahead/behind is for one base. Merged and squash-merged are
decided from local git, not from a PR's commit list.

## Known limits

- The GraphQL fixtures in `internal/gitwatch/testdata` are hand-written, not recorded.
- Squash detection is local, through `git merge-tree --write-tree` (git 2.38+). On older git, squash merges go undetected.
- `pr_closed` only covers PRs of the current repository.
- No GitHub Enterprise in v1.
- The pane keeps polling after it is closed, until `/ci stop`.
