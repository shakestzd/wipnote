# Before-and-after check for prompt changes

Feature feat-217fb5f2, slice 2 of plan-2622b3f7. A change to agent guidance needs proof that behaviour held. This check runs fixed tasks on an old and a new git ref and compares what the agent did.

## Run it

```
scripts/prompt-ab.sh --old main --new my-branch
scripts/prompt-ab.sh --old main --new main --runs 3
scripts/prompt-ab.sh --old main --plant-no-tdd
scripts/prompt-ab.sh --dry-run --old main --new HEAD
```

Each real run costs model money: tasks times runs times two refs. The default is 3 tasks, 3 runs, 2 refs, so 18 runs. The script exits 1 when it flags a regression and all runs finished. It exits 2 for bad usage, for any failed run, and for a wrong result count. A failed run exits 2 even when the comparison is clean. It refuses an `--out` folder inside the source checkout, with exit 2.

## What it does

1. It adds one git worktree per ref and builds `wipnote` there. The binary differs per ref because prompts are embedded in it (`cmd/wipnote/prompts/`).
2. For each task in `plugin/evals/prompt-ab/tasks/` it copies the fixture into a fresh git project, creates a work item, and runs `wipnote delegate run -- claude -p`. The worker gets the ref's guidance (`system-prompt.md` and `worker-rules.md`) as extra system prompt and the ref's binary first on `PATH`.
3. It reads what the agent did, not what it said. The run folder under `.wipnote/delegate/runs/` keeps `raw.log`, the worker's stream with each command, file write and result. `prompt_ab.py behaviours` pairs each `tool_use` with its `tool_result` by id and turns that into four facts.
4. It writes `comparison.md` with the counts per task and behaviour.
5. It removes both worktrees at the end. A worktree it cannot remove is named in a warning on stderr, and it runs `git worktree prune`.

## Checked behaviours

A behaviour holds only when the paired tool result is not an error. A tool call with no result does not count. A shell command also fails when its result body shows a non-zero `Exit code N`. A command that ends in `|| true`, `|| :` or `|| exit 0` does not count as a gate run, because it hides the exit status.

| Behaviour | Holds when |
|---|---|
| `started` | The agent ran `wipnote feature start <item>` |
| `test_first` | A test file was written before the first non-test `.go` write, by file tool or shell redirect, in command order. The record does not show whether the test failed, so the check does not read that |
| `gate` | The agent ran `wipnote check --gate`, or `go build`, `go vet` and `go test` |
| `commit_names_item` | A `git commit` command names the item id |

The ledger events file records tool names but not commands, and it stays empty when the worker runs without the wipnote plugin hooks. So the check reads `raw.log`. Each run happens in a clean environment inside a throwaway project, so it cannot touch the real project.

## Pass rule

Each task needs exactly the requested number of result files on each side. A side with fewer or more is a wrong count: the analyzer names the task and side and exits 2, with no verdict.

A behaviour that holds in three of three runs on the old ref and in fewer than two on the new ref is a regression.

Two of three on the new ref passes. A behaviour that did not hold in three of three on the old ref is never flagged, because it was not stable to begin with.

## Limits

The stream log records an error flag on each result. For a failed shell command it adds an `Exit code N` line to the result body. It records no exit status for a successful command, so the check reads the error flag and that line, and it cannot read a status from any other source. A command that fails but prints no `Exit code` line and sets no error flag counts as held. The mask check reads the command text, so a wrapper script that hides a failure is not caught. The check does not read whether a test failed before the implementation.

## Planted regression

`--plant-no-tdd` takes the old ref, deletes the "Write failing tests first" rule and its checklist line from `worker-rules.md`, and uses that as the new ref. The check must flag `test_first`.

## Not used

A judged score from a second model. It adds cost and a second source of variance.
