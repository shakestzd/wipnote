# wipnote board

A Herdr layout for projects tracked by wipnote. A side board lists every agent by what it needs from you, and shows the work item it is working on. Click an agent to see its terminals.

## What you see

Two columns, left to right, in your own theme:

1. **The board**, 320px wide (resizable, 200 to 560px). A "WORK" heading, then groups in this order: Needs you, Failed, Working, Done and Idle. An empty group is not shown. Each group has its name and count. Each agent is one row: a 3px status stripe, the program icon, the work item on the first line (12px), and the worker's name and space on the second line (11px, muted). With no agents it says "No agents are running."
2. **The work area**, the rest. Herdr's own tab bar (30px), then the active tab's terminals as cards, each under a 30px header (see below). A tab's only pane gets a card and header too.

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

Clicking a row focuses that agent, so its terminals fill the work area. The focused agent's row is highlighted. Herdr's next-agent and previous-agent keys follow the board's order.

The first line of a row is the terminal's `wipnote.title` label, or the tab name when it has none. The second line uses the delegate run's worker name, then the `wipnote.worker` label, then the program's name.

## Built from

The layout (`layouts/wipnote-board.json`): a root row with a divider. It holds a `block` showing the mod, and a column with the shipped `tabs` mod and a `{ "tab": "active" }` block. It sets no theme or colors.

Two mods, each with `place: "none"`: `mods/wipnote-board/` and `mods/wipnote-pane-header/`.

Data: `agents()`, `terminals()` for labels and `spaces()` for folders. `theme()` gives every color.

Commands: `run("agent.focus", { terminal })` on click and `setAgentOrder(addresses)` on render.

Programs: every 5 seconds the mod runs `wipnote delegate list --json --project-dir <folder>` through `exec`, once per local space folder, and matches each row's `pane` to an agent's address.

## Watch out

The mod runs a program (`wipnote`), and reads no files itself. It makes no network calls.

It asks only about local spaces: a space on another machine shows its agents, but no delegate runs.

Agents appear here only when their program reports status (OSC 7501). herdr-core adds no per-agent integration.

## Done when

Every agent is in the group that matches its status, and the counts add up.

A terminal started with `wipnote claude` while a work item is claimed shows that work item on its row.

Clicking a row shows that agent's terminals.

`herdr-app check` passes on both mods, and switching to another layout and back works.
