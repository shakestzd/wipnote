# wipnote-orchestra (dev mod)

A Claude Code mod (function hooks) that turns the line under the prompt into an orchestration band: where you are, what is being worked on, and how the subagents are getting on with it. It also draws `wipnote-ref` fences in replies as work-item cards. It replaces the former wipnote-cards mod.

```
⏵⏵ auto mode on · (shift+tab to cycle) · esc to interrupt · ← for agents        ← Claude Code's own hint line, unchanged
  5h 42% resets 9:40 PM   7d 83% resets Fri 3:00 PM    Tue 20 · 7:40 PM        ← usage limits, then the clock
 ionna-rca   main !3 ⇡1  ✻ Opus 5.5  ctx 33%  Raw-data RCA agents [1/46]  → Routine setup script  ◉ 3 agents · ✔ 12 ✘ 1 
   ⚠ Explore no tool call for 6m12s  Survey workbench types  ↳ $ npm test         ← needs a look
   ▸ Routine setup script 2/5 steps   ● 2 running  ✔ 1 done                     ← agents by work item
```

## What it shows

| Row | Content | Source |
|---|---|---|
| Hint | Claude Code's hint line exactly as it draws it (the mode pill stays beside it) | `PromptHint` props |
| Limits | each rate-limit window (`5h`, `7d`, `spend`): % used, green/amber/red at 50/80, local reset time; then the clock: weekday, day of the month, local time (without a limits row, in compact or before limits are known, the clock is the last segment) | `$.session.usage().rateLimits`, `timeZone` |
| Segments | repo/worktree name · git branch, dirty count, ahead/behind · model · context % · per track: title `[done/total]` · per work item: `→ title n/m` (steps), coloured by status · live agent count and the session's finished tally (`✔ done ✘ failed`) | `git`, `$.session.*`, `wipnote` |
| Needs a look | a running agent quiet for 5 min: on one call that long (`◷ one call running for …`) or with no tool call at all (`⚠ no tool call for …`), with its last call; and a failed or stopped agent for 10 min (`✘`) | `tool.call`, `$.agent.list()` |
| Work items | one row per work item the agents' briefs name: title, steps, and how many of its agents are running, waiting, done, failed (finished ones counted 90 s) | `agent.spawn`, `$.agent.list()`, `wipnote` |

**No row per agent.** Claude Code's own agent panel under the prompt already lists every subagent with its time, tokens and current activity, and opens one with Enter (or from `/tasks`). The band only adds what that panel does not show: agents grouped by work item, a stall warning, failures that stay past the panel's 30 s, and a session tally.

Segments are powerline-style with rounded caps and **wrap between segments** to the pane width (re-laid out on every resize); a segment wider than the pane is cut with `…`. Every line is one truncated Text, so nothing wraps mid-row.

## wipnote integration

- **Active work items**: the session's claim from `wipnote who --json` comes first. It is read at session start, after `/resume`, `/branch` or `/clear`, and after each turn; when `who` fails, the last claim stays. Then come the items started by shell commands any loop runs: `wipnote <feature|bug|spike|track> start <id>` adds, `… complete <id>` removes. A subagent's work item is the first `feat-/bug-/spk-/trk-` id in its brief.
- **Titles and progress**: every 5 s, `wipnote <kind> show <id> --format json` per item (title, status, steps, `part_of` track) and `wipnote track show <trk>` per track (title and `(n/m done)`, which only the text form carries). Up to 12 items.
- The band and the cards never write to wipnote.

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
hooks/register.tsx           hooks: session.start timer, session claim, agent.spawn, tool.call, classic.* (permission mode, resume), ui.render
hooks/refcards.tsx           wipnote-ref cards: fetch on reply and turn, the model rule, the AssistantMessage drawing
hooks/refs.ts                pure helpers for cards: fence parsing, show argv, card parsing, TTL, refresh order
hooks/band.tsx               the band's tree (pure: data + room → elements)
hooks/model.ts               pure helpers: parsing (git, wipnote, hint), agents (by work item, needs a look, tally), layout (wrap, fit), formatting (time, limits)
types/index.d.ts             $.state contract (PluginState['wipnote-orchestra'])
tests/*.test.ts(x)           claude plugin test suites (pure helpers + mounted band on terminal and desktop)
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
claude plugin test mods/wipnote-orchestra     # 34 tests
tsc -p mods/wipnote-orchestra                 # after a load lays .claude-plugin/types/
```

Built on Claude Code 2.1.292 and checked on 2.1.303. The function-hooks API is early
access and changes between releases. `.claude-plugin/types/` is generated on each load
and is not committed.

It is not yet part of the generated plugin tree; shipping mods needs `build-ports` support.

## Settings

| Option | Default | Effect |
|---|---|---|
| `timeZone` | `America/New_York` | IANA zone for the clock segment and the usage-limit reset times. An unknown zone falls back to the default. |

Set it in `/config`, or in settings under `pluginConfigs["wipnote-orchestra"].options`
(`wipnote-orchestra@inline` for a `--plugin-dir` load).

## Known limits

- **Mode pill alignment**: Claude Code draws the permission-mode pill beside the mod's tree, so the band rows are shifted left by the pill's width, computed from `permission_mode` (classic hook events). It is learned on the next prompt or tool call, so after shift+tab between two non-default modes the indent can lag by a cell or two.
- **Usage limits** appear only with a subscription sign-in (an API key reports no windows) and after the first API response.
- **Glyphs** need a Nerd Font: powerline arrow and caps U+E0B0, U+E0B4, U+E0B6; branch U+E0A0; clock U+F017; gauge U+F0E4. The example above shows them as spaces. Write them as `\u` escapes in code, never literal: a byte-level edit tool once double-encoded the file and the engine refused the tree.
- **Colors**: hex colors fall back to the 16 ANSI colors when the terminal advertises none (`TERM=xterm`, no `COLORTERM`); set `COLORTERM=truecolor`.
- **State keys** are under the plugin name `wipnote-orchestra`: the `atom({ plugin: 'wipnote-orchestra', … })` refs in hooks/register.tsx and the `PluginState` key in types/index.d.ts change together with the manifest's `name`. The slash command stays `/orchestra`.
