# wipnote-plan-review (mod)

A Claude Code mod that keeps a wipnote plan's review state in a pane beside the chat. It replaces the dashboard's plan review: the review itself happens in the conversation (`/wipnote:plan-review`), and the pane shows where every slice stands while it does.

```
Review pane
plan-3a88d8a9 · review · 1 approved · 1 changes · 2 pending
✔ 1. Slices command
   approved · trivial · promoted
↺ 2. The mod
   changes · standard · M · Low
○ 3. Docs
   pending · standard · S · Low
```

## What it does

- **Opens** on the first `wipnote plan slices|approve-slice|reject-slice <plan-id>` a Bash call runs, or with `/plan-review-pane <plan-id>`. `/plan-review-pane` with no id reopens the plan in view. `prompt.md` asks the model to run `wipnote plan slices <plan-id>` when a review starts, so the pane comes up by itself.
- **Redraws** after each Bash call that runs `wipnote plan <sub> <plan-id>` on the plan in view (approve, reject, promote, rewrite, set-slice-status, …), and every 5 s, so a change made in another terminal shows too.
- **Reads** `wipnote plan slices <plan-id>` (JSON: number, title, complexity, effort, risk, `approval_status`, `execution_status`, feature id). It never writes to wipnote.
- A failed read shows the CLI's first error line in the pane.

## Files

```
.claude-plugin/plugin.json   manifest (name, types contract)
hooks/hooks.json             { "modules": ["./register.tsx"] }
hooks/register.tsx           hooks: session.start (command, 5 s poll), command.run, tool.call (Bash), ui.render (Pane)
hooks/model.ts               pure helpers: plan id in a command, slices JSON, tally and detail lines
types/index.d.ts             $.state contract (PluginState['wipnote-plan-review'])
prompt.md                    the one-line rule `wipnote claude` appends to the system prompt
tests/*.test.ts(x)           claude plugin test suites
```

## Run it

`wipnote claude` loads it with every mod in `mods/`; `WIPNOTE_NO_MODS=1` skips the mods. On its own:

```bash
claude --plugin-dir mods/wipnote-plan-review
```

## Check it

```bash
claude plugin validate mods/wipnote-plan-review
claude plugin test mods/wipnote-plan-review     # 7 tests
tsc -p mods/wipnote-plan-review                 # after a load lays .claude-plugin/types/
```
