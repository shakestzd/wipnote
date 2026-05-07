# CLI Reference

All commands are invoked as `wipnote <command>`. Run `wipnote help --compact` for a quick summary, or `wipnote <command> --help` for detailed usage on any command.

---

## Work Items

Commands for managing the core work item types. All four types share the same lifecycle subcommands.

| Command | Description |
|---------|-------------|
| `feature [create\|show\|start\|complete\|list\|add-step\|delete]` | Feature work items |
| `bug [create\|show\|start\|complete\|list\|add-step\|delete]` | Bug tracking |
| `spike [create\|show\|start\|complete\|list\|add-step\|delete]` | Time-boxed investigation spikes |
| `track [create\|show\|start\|complete\|list\|add-step\|delete]` | Multi-feature tracks (initiatives) |

### Common subcommands

| Subcommand | Usage | Description |
|------------|-------|-------------|
| `create` | `wipnote feature create "Title" --track <trk-id> --description "..."` | Create a new work item |
| `show` | `wipnote feature show <id>` | Display work item details |
| `start` | `wipnote feature start <id>` | Mark as in-progress and set as active |
| `complete` | `wipnote feature complete <id>` | Mark as done |
| `list` | `wipnote feature list [--status todo\|in-progress\|done]` | List work items with optional status filter |
| `add-step` | `wipnote feature add-step <id> "Step description"` | Add an implementation step |
| `delete` | `wipnote feature delete <id>` | Delete a work item |

!!! note "Required flags"
    `feature create` and `bug create` require `--track <trk-id>` and `--description "..."`.

---

## Search & Status

Quick commands for finding work items and checking project state.

| Command | Usage | Description |
|---------|-------|-------------|
| `find` | `wipnote find <query>` | Search work items by title or ID |
| `wip` | `wipnote wip` | Show all in-progress work items |
| `status` | `wipnote status` | Quick project status summary |
| `snapshot` | `wipnote snapshot [--summary]` | Full project overview with counts and details |

---

## Planning

Commands for creating, reviewing, and executing structured CRISPI plans.

| Command | Usage | Description |
|---------|-------|-------------|
| `plan create` | `wipnote plan create "Title" --track <trk-id>` | Create a new plan |
| `plan create-yaml` | `wipnote plan create-yaml "Title" --track <trk-id>` | Create a v2 YAML plan file |
| `plan show` | `wipnote plan show <id>` | Display plan details |
| `plan start` | `wipnote plan start <id>` | Mark plan as in-progress |
| `plan complete` | `wipnote plan complete <id>` | Mark plan as done |
| `plan list` | `wipnote plan list` | List all plans |
| `plan list-yaml` | `wipnote plan list-yaml` | List all YAML plans sorted by created_at |
| `plan generate` | `wipnote plan generate <trk-id>` | Generate a CRISPI YAML plan for a track |
| `plan rewrite-yaml` | `wipnote plan rewrite-yaml <id> --file <path>` | Validated atomic update of plan YAML |
| `plan validate-yaml` | `wipnote plan validate-yaml <id>` | Validate a YAML plan's schema |

### v2 Slice Lifecycle

Commands for per-slice review and incremental promotion (v2 plans only).

| Command | Usage | Description |
|---------|-------|-------------|
| `plan approve-slice` | `wipnote plan approve-slice <plan-id> <num>` | Set `approval_status=approved` for a slice |
| `plan reject-slice` | `wipnote plan reject-slice <plan-id> <num> [--changes-requested]` | Set `approval_status=rejected` (or `changes_requested`) |
| `plan answer-slice-question` | `wipnote plan answer-slice-question <plan-id> <num> <question-id> <answer-key>` | Record answer to a slice-local question |
| `plan set-slice-status` | `wipnote plan set-slice-status <plan-id> <num> <status>` | Set execution status (`not_started\|promoted\|in_progress\|done\|blocked\|superseded`) |
| `plan promote-slice` | `wipnote plan promote-slice <plan-id> <num> [--waive-deps]` | Promote an approved slice to a feature work item |

---

## Specifications & Quality

Commands for feature specs, test generation, code review, and quality enforcement.

| Command | Usage | Description |
|---------|-------|-------------|
| `spec` | `wipnote spec [generate\|show] <feature-id>` | Generate or view feature specifications |
| `tdd` | `wipnote tdd <feature-id>` | Generate test stubs from spec acceptance criteria |
| `review` | `wipnote review` | Structured diff summary against base branch |
| `compliance` | `wipnote compliance <feature-id>` | Score implementation against spec |
| `check` | `wipnote check` | Run automated quality gate checks |
| `health` | `wipnote health` | Code health metrics (module sizes, function lengths) |

---

## Sessions & Observability

Commands for session management, analytics, and work item relationships.

| Command | Usage | Description |
|---------|-------|-------------|
| `session list` | `wipnote session list` | List recorded sessions |
| `session show` | `wipnote session show <id>` | Display session details and tool calls |
| `analytics summary` | `wipnote analytics summary` | Work analytics overview |
| `analytics velocity` | `wipnote analytics velocity` | Development velocity insights |
| `link add` | `wipnote link add <from-id> <to-id> --type <type>` | Create a typed edge between work items |
| `link remove` | `wipnote link remove <from-id> <to-id>` | Remove an edge |
| `link list` | `wipnote link list <id>` | List edges for a work item |

---

## Data Management

Commands for data import, export, and index maintenance.

| Command | Usage | Description |
|---------|-------|-------------|
| `batch apply` | `wipnote batch apply <file.yaml>` | Apply bulk work item operations from YAML |
| `batch export` | `wipnote batch export` | Export work items to YAML |
| `ingest` | `wipnote ingest` | Ingest Claude Code session transcripts (JSONL) |
| `backfill` | `wipnote backfill [feature-files\|tool-calls-feature]` | Rebuild derived tables |
| `reindex` | `wipnote reindex` | Sync HTML work items to SQLite index |

---

## Development & Operations

Commands for autonomous development, building, serving, agent configuration, and maintenance.

| Command | Usage | Description |
|---------|-------|-------------|
| `claude` | `wipnote claude [--dev] [--continue\|--resume <session-id>]` | Launch Claude Code with wipnote plugin; `--resume <id>` resumes a specific prior session |
| `yolo` | `wipnote yolo --feature <id> [--track <id>] [--resume <session-id>]` | Autonomous dev mode with engineering guardrails |
| `build` | `wipnote build` | Build Go binary (dev workflow) |
| `serve` | `wipnote serve` | Start local dashboard server at `localhost:4000` |
| `agent-init` | `wipnote agent-init` | Output shared agent context (safety, attribution, quality gates) |
| `statusline` | `wipnote statusline` | OMP/Starship prompt integration |
| `upgrade` / `update` | `wipnote upgrade [--check] [--version 0.54.9]` | Self-update CLI from GitHub releases |

---

## Work Item Types

| Type | Prefix | Purpose |
|------|--------|---------|
| Feature | `feat-` | Units of deliverable work |
| Bug | `bug-` | Defects to fix |
| Spike | `spk-` | Time-boxed investigations |
| Track | `trk-` | Initiatives grouping related work |
| Plan | `plan-` | CRISPI implementation plans |
