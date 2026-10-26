# /wipnote:ci

Summarise a pull request's CI, review and branch health, and follow its checks until they settle when asked.

## Usage

```
/wipnote:ci [pr|branch|--work-item <id>] [watch]
```

## Parameters

- `pr` (optional): A pull request number (e.g., `85`)
- `branch` (optional): A head branch name; its pull request is used
- `--work-item <id>` (optional): The newest PR whose title or head branch names that work item (e.g., `feat-5a274de6`)
- `watch` (optional): Follow the checks in the foreground until they settle

With no argument, the current branch's PR is used, else your newest open PR.

## Instructions

1. Run `wipnote ci status [pr|branch|--work-item <id>]` (add `--json` if you need the fields). It never fails: a missing or signed-out `gh`, or a failed read, prints `CI status not available: <reason>`. Report that reason and stop.
2. Summarise the output in this order:
   - **Failed checks first**, each with its link. Cancelled counts as failed.
   - **Review blockers:** changes requested, unresolved review threads, a blocked merge state.
   - **Conflicts:** a conflicting or dirty merge state. `computing` means GitHub is still working it out; it is not a conflict.
   - **Base red too:** when the base branch's CI is red, say so, so the user can tell a PR's own failure from a broken base.
   - **Stale worktrees:** each with its reasons (`merged`, `upstream_gone`, `idle`, `pr_closed`). Report them only; never remove a worktree or branch.
   - Then the counts, branch ahead/behind, the latest release and any notes, in one or two lines.
3. If the user asks to watch (or passed `watch`), run `wipnote ci watch [pr|branch]` in the foreground. It re-reads checks every 15 s while any is pending, and the rest every 60 s. It prints a snapshot on every change and exits with:
   - `0`: all checks passed, or still no checks after the 2-minute no-checks grace (`--no-checks-grace`).
   - `1`: a check failed or was cancelled. Skipped is fine.
   - `2`: `--timeout` was reached (default 60 minutes).
   - `3`: not available: the first read failed, or 3 reads failed in a row. A single failed read keeps the last snapshot and is retried.
   - `130`: interrupted.
   `ci watch` takes no `--work-item`: for a work item, take the PR number from `ci status --work-item <id>` first. When it exits, summarise the final snapshot as in step 2 and name the exit code's meaning. Use a long enough tool timeout, or pass a shorter `--timeout`.
4. In Claude Code with the wipnote mods loaded, the wipnote-ci pane (`/ci-pane`, `/ci-pane 85`, `/ci-pane <branch>`, `/ci-pane stop`) follows the same data live beside the chat. Mention it once if the user wants to keep watching while working; it polls until `/ci-pane stop` or the pane is closed.

## Examples

```bash
/wipnote:ci
```
Summarise the current branch's PR

```bash
/wipnote:ci 85 watch
```
Follow PR 85's checks until they settle

```bash
/wipnote:ci --work-item feat-5a274de6
```
Summarise the PR linked to a work item
