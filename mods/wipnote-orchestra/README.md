# wipnote-orchestra (dev mod)

A Claude Code mod (function hooks) that turns the line under the prompt into an orchestration band: where you are, what is being worked on, how the subagents are getting on with it, and the project's `wipnote delegate` runs. It also draws `wipnote-ref` fences in replies as work-item cards. It replaces the former wipnote-cards mod.

```
⏵⏵ auto mode on · (shift+tab to cycle) · esc to interrupt · ← for agents        ← Claude Code's own hint line, unchanged
  5h 42% resets 9:40 PM   7d 83% resets Fri 3:00 PM    Tue 20 · 7:40 PM        ← usage limits, then the clock
 ionna-rca   main !3 ⇡1  ✻ Opus 5.5  ctx 33%  Raw-data RCA agents [1/46]  → Routine setup script  ◉ 3 agents · ✔ 12 ✘ 1 
   ⚠ Explore no tool call for 6m12s  Survey workbench types  ↳ $ npm test         ← needs a look
   ⇄ w1  codex · interactive  ● live  Waiting on you  Working on it.             ← delegate runs
   ⇄ Fix the tests  ccx · oneshot  ✘ ended exit 2  Waiting on you  ccx: failed in 3s
   ▸ Routine setup script 2/5 steps   ● 2 running  ✔ 1 done                     ← agents by work item
```

## What it shows

| Row | Content | Source |
|---|---|---|
| Hint | Claude Code's hint line exactly as it draws it (the mode pill stays beside it) | `PromptHint` props |
| Limits | each rate-limit window (`5h`, `7d`, `spend`): % used, green/amber/red at 50/80, local reset time; then the clock: weekday, day of the month, local time (without a limits row, in compact or before limits are known, the clock is the last segment) | `$.session.usage().rateLimits`, `timeZone` |
| Segments | repo/worktree name · git branch, dirty count, ahead/behind · model · context % · per track: title `[done/total]` · per work item: `→ title n/m` (steps), coloured by status · live agent count and the session's finished tally (`✔ done ✘ failed`) | `git`, `$.session.*`, `wipnote` |
| Needs a look | a running agent quiet for 5 min: on one call that long (`◷ one call running for …`) or with no tool call at all (`⚠ no tool call for …`), with its last call; and a failed or stopped agent for 10 min (`✘`) | `tool.call`, `$.agent.list()` |
| Delegate runs | one row per `wipnote delegate` run, one-shot or interactive (never one per pane or split): its name (interactive) or label, harness and mode, `● live` or ended (`✔ ended`, `✘ ended exit 2`, `■ ended (stop)`), its Projects Overview group (Working, Waiting on you, Ready for review, Landing, Idle, Resolved; the table in `internal/delegate/session/groups.go`, an open worker's from the `state.json` that `wipnote delegate list` keeps), and the first line of its `last.md`. Live runs first, then runs that ended in the last hour, newest first | `.wipnote/delegate/runs/` (incl. `state.json`), `.wipnote/delegations-ledger.html` |
| Work items | one row per work item the agents' briefs name: title, steps, and how many of its agents are running, waiting, done, failed (finished ones counted 90 s) | `agent.spawn`, `$.agent.list()`, `wipnote` |

**No row per agent.** Claude Code's own agent panel under the prompt already lists every subagent with its time, tokens and current activity, and opens one with Enter (or from `/tasks`). The band only adds what that panel does not show: agents grouped by work item, a stall warning, failures that stay past the panel's 30 s, and a session tally.

Segments are powerline-style with rounded caps and **wrap between segments** to the pane width (re-laid out on every resize); a segment wider than the pane is cut with `…`. Every line is one truncated Text, so nothing wraps mid-row.

## wipnote integration

- **Active work items**: the session's claim from `wipnote who --json` comes first. It is read at session start, after `/resume`, `/branch` or `/clear`, and after each turn (a swap is seen as a changed session id, after `session.end` and on the 1 s tick; each swap rehydrates once); when `who` fails, the last claim stays. Then come the items started by shell commands any loop runs: `wipnote <feature|bug|spike|track> start <id>` adds, `… complete <id>` removes. A subagent's work item is the first `feat-/bug-/spk-/trk-` id in its brief.
- **Titles and progress**: every 5 s, `wipnote <kind> show <id> --format json` per item (title, status, steps, `part_of` track) and `wipnote track show <trk>` per track (title and `(n/m done)`, which only the text form carries). Up to 12 items.
- **Delegate runs**: every second the band lists `.wipnote/delegate/runs/` and reads each open run's folder (`label`, `start`, `end`, `exit`, `last.md`; see `internal/delegate/runstore`), so a run folder Go writes shows within 2 s. A run has ended once its folder has an `exit` file or its ledger row is closed (a stopped worker has no exit code). Mode, harness and name come from the run's row in `.wipnote/delegations-ledger.html`, read again only when that file changes; a folder with no row shows its label alone. The project is found as wipnote's CLI finds it: `WIPNOTE_PROJECT_DIR`, else the main checkout of a git worktree, else the nearest folder with a `.wipnote/` up from the session's directory. Files only, so it works outside herdr.
- The band and the cards never write to wipnote: the run reader only lists, stats and reads.

## Cards (`wipnote-ref`)

While the mod is loaded, a one-line rule (`prompt.md`, which `wipnote claude` appends to the system prompt when it loads the mods) asks the model to end a report on a work item with a fenced block whose info string is `wipnote-ref` and whose only line is the id. The mod draws that block as a card: title, coloured status, step bar. The saved message keeps only the short block (about 10 tokens), and without the mod it still reads as an id.

- **Fetches**: when a reply with a fence is stored, right after a Bash call that runs `wipnote <kind> add-step|complete-step|start|complete <id>` on a card already drawn (so the bar moves mid-turn), after each turn (open or failed cards, stalest first, up to 10), and on start or resume for fences already in the transcript. A card is fetched again after 60 s.
- **One lookup per item**: an item that is also in the band takes its card from the band's 5-second refresh.
- **Failures** read as `not in this checkout (pull to update)`, `wipnote unavailable` or `details unavailable`.

The former wipnote-cards status line (`wipnote · <id> · <status> · n/m steps`) is gone: the band shows the same fields.

## Commands

`/orchestra full | compact | off | above | below` — full band (with the needs-a-look and work-item rows), one segment line, hidden; draw under the prompt (default) or in the band above it.

## Files

```
.claude-plugin/plugin.json   manifest (name, types contract, timeZone option)
hooks/hooks.json             { "modules": ["./register.tsx"] }
hooks/register.tsx           hooks: session.start timer, session claim, agent.spawn (permission mode), tool.call, session.end, ui.render
hooks/swap.ts                conversation swaps (/clear, /resume, /branch): one tracker so each swap rehydrates once
hooks/refcards.tsx           wipnote-ref cards: fetch on reply and turn, the model rule, the AssistantMessage drawing
hooks/refs.ts                pure helpers for cards: fence parsing, show argv, card parsing, TTL, refresh order
hooks/band.tsx               the band's tree (pure: data + room → elements)
hooks/model.ts               pure helpers: parsing (git, wipnote, hint), agents (by work item, needs a look, tally), run rows, layout (wrap, fit), formatting (time, limits)
hooks/runs.ts                delegate runs, read-only: run folders, the delegations ledger, the project lookup, which runs show
types/index.d.ts             $.state contract (PluginState['wipnote-orchestra'])
tests/*.test.ts(x)           claude plugin test suites (pure helpers + mounted band on terminal and desktop)
tests/memfs.ts               an in-memory $.fs for the run tests, logging every fs op
tests/fixtures/runs.ts       run folders and ledger rows written by the Go run store; generated, do not edit
```

## Run it

`wipnote claude` loads it (with every mod in `mods/`) by default; `WIPNOTE_NO_MODS=1` skips the mods. On its own:

```bash
claude --plugin-dir mods/wipnote-orchestra
```

Mods draw only where Claude Code itself draws: a local terminal or a local desktop
session. A cloud session viewed in the Claude app has no drawing surface, so the band
does not show there.

## Check it

```bash
claude plugin validate mods/wipnote-orchestra
claude plugin test mods/wipnote-orchestra     # 65 tests
tsc -p mods/wipnote-orchestra                 # after a load lays .claude-plugin/types/
```

`tests/fixtures/runs.ts` is written by the Go run store, ledger and renderers, and
`go test ./internal/delegate/render -run TestOrchestraFixture` fails when it drifts from
what they write. To regenerate it after a run-folder or ledger change:

```bash
WIPNOTE_UPDATE_FIXTURES=1 go test ./internal/delegate/render -run TestOrchestraFixture
```

Built on Claude Code 2.1.292 and checked on 2.1.310. The function-hooks API is early
access and changes between releases. `.claude-plugin/types/` is generated on each load
and is not committed.

It is not yet part of the generated plugin tree; shipping mods needs `build-ports` support.
The mod stays at `mods/wipnote-orchestra`, and it is tracked and shipped as part of the
`wipnote delegate` feature: its run rows replace the codex-herdr-pane mod's Claude Code run pane.

## Settings

| Option | Default | Effect |
|---|---|---|
| `timeZone` | `America/New_York` | IANA zone for the clock segment and the usage-limit reset times. An unknown zone falls back to the default. |

Set it in `/config`, or in settings under `pluginConfigs["wipnote-orchestra"].options`
(`wipnote-orchestra@inline` for a `--plugin-dir` load).

## Known limits

- **No classic hooks**: the mod reads no `classic.*` event. A managed (prepend-tier) plugin such as `cc-plugin-sec-default` continues every `classic.*` event past user-tier plugins, and Claude Code prints a transcript line for each hook it skips; with none hooked, that block costs nothing. The same plugin also continues `prompt.attachment` and `session.append` rows from the `attachment` door, so the mod hooks neither: the plan-mode attachments (`plan_mode`, `plan_mode_reentry`, `plan_mode_exit`) would never reach it there.
- **Mode pill alignment**: Claude Code draws the permission-mode pill beside the mod's tree, so the band rows are shifted left by the pill's width. Neither the `PromptHint` props (the engine cuts the pill out of `hint`) nor `SessionMode` (the footer's memory labels) carry the mode, so it is learned from a main-loop `agent.spawn`'s `permissionMode` alone. With no mode learned, a `(shift+tab …)` hint, which only shows beside a pill, takes the auto pill's width, and any other hint none. Until the first spawn, and after a shift+tab until the next one, the band can sit a few cells off: about one in plan mode (the auto fallback is 18 cells, the plan pill 17), three in accept-edits, and in auto beside a hint that does not start with `(` it is not shifted at all.
- **Usage limits** appear only with a subscription sign-in (an API key reports no windows) and after the first API response.
- **Glyphs** need a Nerd Font: powerline arrow and caps U+E0B0, U+E0B4, U+E0B6; branch U+E0A0; clock U+F017; gauge U+F0E4. The example above shows them as spaces. Write them as `\u` escapes in code, never literal: a byte-level edit tool once double-encoded the file and the engine refused the tree.
- **Colors**: hex colors fall back to the 16 ANSI colors when the terminal advertises none (`TERM=xterm`, no `COLORTERM`); set `COLORTERM=truecolor`.
- **State keys** are under the plugin name `wipnote-orchestra`: the `atom({ plugin: 'wipnote-orchestra', … })` refs in hooks/register.tsx and the `PluginState` key in types/index.d.ts change together with the manifest's `name`. The slash command stays `/orchestra`.
