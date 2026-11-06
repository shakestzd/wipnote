# /wipnote:init

Initialize wipnote in a project

## Usage

```
/wipnote:init
```

## Parameters



## Examples

```bash
/wipnote:init
```
Set up wipnote directory structure in project



## Instructions

### Implementation:

**DO THIS:**

1. **Initialize project:**
   ```bash
   wipnote init
   ```
   This creates the project's store under `~/.wipnote/projects/`, with nothing added to the project folder, and says where it is. A project that already has a store keeps it, including an in-repo `.wipnote/`. `wipnote claude`, `wipnote codex`, `wipnote yolo` and `wipnote antigravity` do the same on their own when no store is found. In a folder that is not a git repo, a store made by `wipnote init` also serves the folders below it, while one a launcher made serves only its own folder. Any wipnote command that needs a store, run where none serves the project, makes and uses the project's app-owned store the way a launcher does: a git repo's is keyed by its main checkout, so linked worktrees and submodules share it, and any other folder's by that folder. Nothing is added to the project. Background callers (the status line, the MCP server, reports and doctor checks) never make one. Work items older versions kept in `~/.wipnote` itself, from running wipnote in your home folder, can be copied into the store for your home folder with `wipnote store import-home` (`--dry-run` first lists what it would copy). To keep the store in the project instead, run `wipnote init --in-repo`; it is refused when an app-owned store already serves the project, since an in-repo `.wipnote/` would hide it.

2. **Present next steps** using the output template below.

3. **Guide the user:**
   - How to plan work: `/wipnote:plan "title"`
   - How to see open work: `wipnote wip` (or `/wipnote:recommend` for what to do next)
   - How to trace why code exists: `wipnote lineage <id>`

4. **Highlight key points:**
   - All subsequent work will be tracked automatically
   - Use CLI/slash commands for all operations

### Output Format:

## wipnote Initialized

Created the store (`.wipnote/`, under `~/.wipnote/projects/<project>/` unless `--in-repo` was used; `wipnote status` prints its path) with:
- `features/` - Feature work items
- `sessions/` - Session activity logs
- `tracks/` - Multi-feature tracks
- `spikes/` - Research and investigation
- `bugs/` - Bug tracking
- `refs.json` - Project metadata references
- `styles.css` - Default stylesheet for wipnote HTML nodes

Note:
- Additional paths such as plans, events, and launch/session markers may appear later as other wipnote commands and hooks run.
- Nothing wipnote stores lives outside the store. The store is `.wipnote/` inside `~/.wipnote/projects/<project>/`, a folder that is its own git repo, or `.wipnote/` in the project with `--in-repo`. The canonical record is the files under `.wipnote/` — work-item and architecture HTML, plan YAML, the session/claim/gate ledgers, per-session NDJSON — together with git history, from which commit and file attribution is derived. SQLite is used only as a process-local in-memory query engine, rebuilt inside a single command and discarded when it exits; `wipnote init` creates no database, cache directory, or per-user index.
- Current `wipnote init` does not create legacy analytics directories like `insights/`, `metrics/`, or `cigs/`.

### Next Steps
1. Plan new work: `/wipnote:plan "Feature title"`
2. See open work: `wipnote wip`
3. Trace why code exists: `wipnote lineage <id>`

### Quick Start
```bash
# Start planning
/wipnote:plan "Add user authentication"

# Begin work
wipnote feature start <id>

# See open work
wipnote wip
```
