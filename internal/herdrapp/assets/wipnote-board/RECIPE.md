# wipnote board

A Herdr layout for projects tracked by wipnote. A side board lists every agent by what it needs from you, and shows the work item it is working on. Click an agent to see its terminals.

## What you see

Two columns, left to right, in your own theme:

1. **The board**, 320px wide (resizable, 200 to 560px). A "WORK" heading, then groups in this order: Needs you, Failed, Working, Done and Idle. An empty group is not shown. Each group has its name and count. Each agent is one row (a delegator also has its workers under it, see below): a 3px status stripe, the program icon, the work item on the first line (12px), and the worker's name and space on the second line (11px, muted). With no agents it says "No agents are running."
2. **The work area**, the rest. a tab bar (30px, the `wipnote-tabs` mod: Herdr's own, without settled tabs), then the active tab's terminals as cards, each under a 30px header (see below). A tab's only pane gets a card and header too.

## The pane header

A second mod (`mods/wipnote-pane-header/`) draws each pane's header, left to right: the program icon, the worker's name (`wipnote.worker` label), the work item (`wipnote.title` label, muted, truncated), and the status on the right (spinner when working, "Needs you" when blocked, a dot when done). A pane with no wipnote labels shows what Herdr's own header shows: the program name or title, and the folder. Clicking focuses the pane. It has no split, zoom or close buttons, because closing a worker's pane kills it. The layout turns it on with `header` and `lone: true` on the `main` block's `pane`.

## How it behaves

| Status | Group | Stripe |
|---|---|---|
| blocked | Needs you | theme `blocked` |
| error | Failed | theme `blocked` |
| working | Working | theme `working` |
| done | Done | theme `done` |
| idle | Idle | theme `idle` |

## The tree

`wipnote delegate spawn` starts each worker as a child of its caller (hc's `parent` field), in a workers tab that wipnote puts away with `tab.settle`. The board reads `terminals()` for `parent` and shows:

- a **delegator**: an agent with live agent children (an agent with no parent is a flat row until it has some). Its row is like any other; under it a muted "workers · N" row, then
- one **worker row** per child: the `wipnote.title` first line, the worker's name second, and a stripe in the worker's own status colour. A worker row is indented.

An agent with no workers stays a flat row. At the top level the groups above still apply, and a delegator sits in the group of its most urgent descendant (a blocked worker puts its delegator under "Needs you").

Clicking a delegator focuses it (`agent.focus`). Clicking a worker runs `terminal.peek`, because showing a settled tab brings it back into the tab bar. The focused agent's row is highlighted. Herdr's next-agent and previous-agent keys follow the board's order.

Herdr's next-agent and previous-agent keys follow the board's order (each delegator, then its workers).

The first line of a row is the terminal's `wipnote.title` label, or the tab name when it has none. The second line uses the delegate run's worker name, then the `wipnote.worker` label, then the program's name.

## Built from

The layout (`layouts/wipnote-board.json`): a root row with a divider. It holds a `block` showing the mod, and a column with the `wipnote-tabs` mod and a `{ "tab": "active" }` block. It sets no theme or colors.

Three mods, each with `place: "none"`: `mods/wipnote-board/`, `mods/wipnote-pane-header/` and `mods/wipnote-tabs/`. The last is a copy of Herdr's shipped `tabs` mod with one change, `filter((c) => !c.settled)`, and its icon paths renamed to `mods/wipnote-tabs/`. Re-copy it when Herdr's `tabs` changes.

**Limit:** the hiding applies to this layout's tab bar only. Herdr's own `main` layout and its agents mod ignore `settled`, so there the workers tab and the duplicate rows still show. A settled tab comes back when shown or when an agent in it needs you.

Data: `agents()`, `terminals()` for labels and `parent`, and `spaces()` for folders. `theme()` gives every color.

Commands: `run("agent.focus", { terminal })` on a delegator or flat row, `run("terminal.peek", { terminal })` on a worker row, and `setAgentOrder(addresses)` on render.

Programs: every 5 seconds the mod runs `wipnote delegate list --json --project-dir <folder>` through `exec`, once per local space folder, and matches each row's `pane` to an agent's address.

## Watch out

The mod runs a program (`wipnote`), and reads no files itself. It makes no network calls.

It asks only about local spaces: a space on another machine shows its agents, but no delegate runs.

Agents appear here only when their program reports status (OSC 7501). herdr-core adds no per-agent integration.

## Done when

Every agent is in the group that matches its status, and the counts add up.

A terminal started with `wipnote claude` while a work item is claimed shows that work item on its row.

Clicking a row shows that agent's terminals; clicking a worker peeks at it.

`herdr-app check` passes on all three mods, and switching to another layout and back works.
