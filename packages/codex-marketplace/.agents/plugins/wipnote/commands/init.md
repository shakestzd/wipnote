# /wipnote:init

Initialize Erinn AI in a project

## Usage

```
/wipnote:init
```

## Parameters



## Examples

```bash
/wipnote:init
```
Set up Erinn AI directory structure in project



## Instructions for Claude

### Implementation:

**DO THIS:**

1. **Initialize project:**
   ```bash
   erinn init
   ```
   The command will report whether `.erinn/` was created or already exists.

2. **Present next steps** using the output template below.

3. **Guide the user:**
   - How to plan work: `/wipnote:plan "title"`
   - How to start session: `/wipnote:start`
   - How to view dashboard: `/wipnote:serve`

4. **Highlight key points:**
   - All subsequent work will be tracked automatically
   - Use CLI/slash commands for all operations
   - Access dashboard to view progress visually

### Output Format:

## Erinn AI Initialized

Created `.erinn/` directory with:
- `features/` - Feature work items
- `sessions/` - Session activity logs
- `tracks/` - Multi-feature tracks
- `spikes/` - Research and investigation
- `bugs/` - Bug tracking
- `erinn.db` - SQLite read index for queries and dashboard
- `refs.json` - Project metadata references
- `styles.css` - Default stylesheet for Erinn AI HTML nodes

Note:
- Additional paths such as plans, events, and launch/session markers may appear later as other Erinn AI commands and hooks run.
- Current `erinn init` does not create legacy analytics directories like `insights/`, `metrics/`, or `cigs/`.

### Next Steps
1. Plan new work: `/wipnote:plan "Feature title"`
2. Start session: `/wipnote:start`
3. View dashboard: `/wipnote:serve`

### Quick Start
```bash
# Start planning
/wipnote:plan "Add user authentication"

# Begin work
/wipnote:start

# View progress
/wipnote:serve
# Open http://localhost:8080
```
