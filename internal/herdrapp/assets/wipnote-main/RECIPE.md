# wipnote main

Herdr's `main` layout for projects tracked by wipnote. Spaces, the tab bar and the status bar work as they do in `main`. Two things differ: each delegate worker appears once in the agents list, nested under the agent that started it, and each pane has a header that names its worker and work item.

## What you see

- **Spaces:** every open project, as in `main`.
- **Agents (`mods/wipnote-agents/`):** Herdr's `agents` mod with one change. A worker has a pane, so Herdr lists it as an agent, and it has a parent, so Herdr also nests it under that parent. This mod keeps only the nested row. A worker's status still reaches its delegator, and the chevron folds the workers.
- **Tabs:** Herdr's own. Each project has one `workers` tab, whichever delegator started the workers. Click it to see the workers as a grid.
- **Pane header (`mods/wipnote-pane-header/`):** left to right, the program icon, the worker's name (`wipnote.worker` label), the work item (`wipnote.title` label, muted, truncated), and the status on the right (spinner when working, "Needs you" when blocked, a dot when done). It has no split, zoom or close buttons, because closing a worker's pane would kill it.

## How it is built

The layout is a clone of Herdr's `main` that names `wipnote-agents` instead of `agents` and `wipnote-pane-header` as the pane header. `mods/wipnote-agents/main.js` is a clone of `agents`, so it does not receive Herdr's updates to that mod. To refresh it, clone `agents` again and reapply the filter marked in `render`.

## Install

`wipnote delegate install-layout` copies the layout and mods into the Herdr app's home. Your own layouts stay as they are, and `ctrl-shift-l` switches between them. It refuses when a target exists; `--replace` moves the existing files to a dated folder first.
