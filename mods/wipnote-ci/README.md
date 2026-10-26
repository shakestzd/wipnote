# wipnote-ci (mod)

A Claude Code mod that follows a pull request's CI live in a pane beside the chat. It is a thin view over `wipnote ci status --json`: every GitHub read happens in the Go CLI (`internal/gitwatch`), so the mod makes no `gh` calls and every harness shares the same logic.

```
CI pane
PR #85 CI pane over the CLI
feat/ci-pane → main · open · feat-8354cf43
1 passed · 1 failed · 1 running · 1 skipped

✗ go-test 3:00
https://example.test/run/2
● lint 5:00
✓ build 4:10
– docs

review: approved · 2 open threads · merge blocked · requested core-team
branch feat/ci-pane: 2 ahead, 1 behind origin/main · 3 dirty · 1 unpushed
stale worktree old-pane (feat/old-pane): merged, idle 9d
base main: red
  ✗ Release (failure)
    https://example.test/run/9
release v0.82.1 (2026-10-20), 14 commits since · release: green
note: release: gh release view: HTTP 502

live, every 15 s · read 23:05:00
```

## What it does

- **`/ci`** follows the current branch's pull request (else your newest open one), **`/ci 85`** pull request 85, **`/ci <branch>`** that branch's. The argument goes to `wipnote ci status` unchanged; the CLI resolves it.
- **Polls** at `wipnote ci watch`'s cadence: every 15 s while any check is pending, otherwise every 60 s. It stops once settled: at least one check and none pending, the PR merged or closed, or no PR matched. An open PR with no checks is watched for a 2-minute grace from the first read (`NO_CHECKS_GRACE_MS`, as `ci watch --no-checks-grace`), the last wait cut short so the grace ends on a fresh read; still none then, it settles showing `no checks reported`. Checks that appear inside the grace are followed as normal. The elapsed time of each check ticks every second.
- **Shows** the title and PR line with the linked work item, the counts, the checks (failed first, each failure with its link), review (decision, open threads, merge state or `computing`, requested reviewers), branch health (ahead/behind, dirty, unpushed, stale worktrees), the base branch's CI with its failing workflows, the latest release, and the CLI's notes.
- **`available: false`** (gh missing, signed out, a failed read) shows the reason dimmed and keeps polling every 60 s. Output it cannot read keeps the last snapshot and adds a `last read failed` line.
- **Status line:** `CI #85: 3/5 passed · 1 failed · 1 running`. **Toast** when it settles: `CI #85: 2 failed`, `PR #85 merged`, `CI #85: no checks reported` or `CI #85: all 5 checks finished`.
- **`/ci stop`** cancels both timers (the poll and the one-second tick) and clears the status line; the pane keeps the last read, marked stopped. Closing the pane does not stop the polling; only `/ci stop` or the end of the session does.

## Files

```
.claude-plugin/plugin.json   manifest (name, types contract)
hooks/hooks.json             { "modules": ["./register.tsx"] }
hooks/register.tsx           hooks: session.start (/ci), command.run, session.end, ui.render (Pane); the two timers
hooks/pane.tsx               the pane's tree, section by section
hooks/format.ts              pure helpers: parse, fail-first sort, elapsed time, settle rule, cadence, section lines
types/index.d.ts             the ci status JSON and the $.state contract (PluginState['wipnote-ci'])
prompt.md                    the one-line rule `wipnote claude` appends to the system prompt
tests/*.test.ts(x)           claude plugin test suites; tests/fixtures holds snapshot JSON
```

## Run it

`wipnote claude` loads it with every mod in `mods/`; `WIPNOTE_NO_MODS=1` skips the mods. It runs `wipnote` from `PATH`. On its own:

```bash
claude --plugin-dir mods/wipnote-ci
```

## Check it

```bash
claude plugin validate mods/wipnote-ci
claude plugin test mods/wipnote-ci     # 23 tests
tsc -p mods/wipnote-ci                 # after a load lays .claude-plugin/types/
```
